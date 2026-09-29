package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// RequestBuild tạo yêu cầu dựng mạng (R-52: > 2 s → 202 + job): ghi job + outbox
// pf.waterway.network_build.requested trong MỘT giao dịch; worker dựng. Tham số
// dựng được chốt NGAY LÚC YÊU CẦU và lưu bất biến trên job — kết quả tất định.
// Trả replayed = true nếu idem_key đã có job.
func (s *Service) RequestBuild(ctx context.Context, idemKey string) (domain.BuildJob, bool, error) {
	p, err := authz.Require(ctx, port.RoleEditor)
	if err != nil {
		return domain.BuildJob{}, false, err
	}
	if idemKey == "" {
		return domain.BuildJob{}, false, problem.IdempotencyRequired
	}
	params, err := s.BuildParamsAt(ctx, s.now())
	if err != nil {
		return domain.BuildJob{}, false, err
	}
	j := domain.BuildJob{ID: ids.New(), TenantID: p.TenantID, Status: domain.JobQueued, Params: params, RequestedBy: p.ActorRef, IdemKey: idemKey}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.JobByIdemKey(ctx, tx, idemKey); err != nil {
			return err
		} else if prev != nil {
			j, replayed = *prev, true
			return nil
		}
		if err := store.InsertJob(ctx, tx, &j); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: j.TenantID, Type: port.EventBuildRequested, SchemaVersion: 1,
			OccurredAt: j.RequestedAt, TraceID: telemetry.TraceID(ctx), Producer: Producer,
			Payload: port.BuildRequestedV1{BuildID: j.ID.String(), RequestedBy: j.RequestedBy.String(), RequestedAt: formatTime(j.RequestedAt)},
		})
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var prev *domain.BuildJob
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.JobByIdemKey(ctx, tx, idemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return j, replayed, err
}

// Build đọc trạng thái một job dựng.
func (s *Service) Build(ctx context.Context, id uuid.UUID) (domain.BuildJob, error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return domain.BuildJob{}, err
	}
	var j *domain.BuildJob
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		j, e = store.JobByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return domain.BuildJob{}, err
	}
	if j == nil {
		return domain.BuildJob{}, ErrBuildNotFound
	}
	return *j, nil
}

// HandleBuildRequested là inbox.Handler của worker cho pf.waterway.network_build.requested.
// Chạy trong giao dịch tenant do inbox.Process mở (pool vai waterway_app): phiên
// bản + đồ thị + trạng thái job + inbox commit cùng nhau (R-61). Job đã xong → bỏ qua.
func (s *Service) HandleBuildRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.BuildRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("waterway: payload %s hỏng: %w", env.EventType, err)
	}
	id, err := uuid.Parse(pl.BuildID)
	if err != nil {
		return fmt.Errorf("waterway: build_id không hợp lệ: %w", err)
	}
	j, err := store.JobByID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if j == nil || j.Status != domain.JobQueued {
		return nil // tenant khác hoặc đã xử lý — giao lặp an toàn
	}
	if err := store.LockTenantBuilds(ctx, tx, j.TenantID); err != nil {
		return err
	}
	g, err := buildGraph(ctx, tx, j.Params, j.RequestedAt)
	if err != nil {
		var pe *problem.Problem
		if errors.As(err, &pe) && pe.Status < 500 {
			return store.FailJob(ctx, tx, j.ID, pe.Code)
		}
		return err
	}
	now := s.now()
	if prev, err := store.ReusableVersion(ctx, tx, g.SegmentSetSHA256); err != nil {
		return err
	} else if prev != nil {
		// Tập đoạn + tham số trùng một phiên bản đã dựng/đang phát hành: dựng tất
		// định cho cùng đồ thị — trả lại phiên bản đó, không tạo bản trùng.
		return store.CompleteJob(ctx, tx, j.ID, prev.ID, true, now)
	}
	no, err := store.NextVersionNo(ctx, tx)
	if err != nil {
		return err
	}
	v := domain.NetworkVersion{
		ID: ids.New(), TenantID: j.TenantID, VersionNo: no, Status: domain.VersionBuilt, SegmentSetSHA256: g.SegmentSetSHA256,
		GraphSHA256: g.GraphSHA256, Params: j.Params, RuleVersion: domain.RuleVersion, AsOf: j.RequestedAt,
		SegmentCount: len(g.Segments), StructureCount: g.StructureCount, VertexCount: len(g.Vertices), EdgeCount: len(g.Edges),
		ComponentCount: g.Components, FindingCount: len(g.Findings), BuildJobID: j.ID, BuiltBy: j.RequestedBy,
	}
	if err := store.InsertVersion(ctx, tx, &v); err != nil {
		return err
	}
	if err := store.InsertGraph(ctx, tx, j.TenantID, v.ID, g); err != nil {
		return err
	}
	return store.CompleteJob(ctx, tx, j.ID, v.ID, false, now)
}

// buildGraph dựng đồ thị từ bản ĐÃ XÁC NHẬN của sổ tại as_of — tuyến nháp không vào.
func buildGraph(ctx context.Context, tx pgx.Tx, params domain.BuildParams, asOf time.Time) (domain.Graph, error) {
	segs, err := store.ConfirmedSegmentsAt(ctx, tx, asOf)
	if err != nil {
		return domain.Graph{}, err
	}
	structs, err := store.ActiveStructuresAt(ctx, tx, asOf)
	if err != nil {
		return domain.Graph{}, err
	}
	in := make([]domain.SegmentInput, 0, len(segs))
	confirmed := map[uuid.UUID]bool{}
	for _, sg := range segs {
		confirmed[sg.SegmentRef] = true
		in = append(in, domain.SegmentInput{
			Ref: sg.SegmentRef, RevisionID: sg.ID, Revision: sg.Revision, RowHash: sg.RowHash, Name: sg.Name, Class: sg.Class,
			DataSource: sg.DataSource, LengthM: sg.LengthM, Points: sg.Points, WidthM: sg.WidthM, DepthM: sg.DepthM,
			DeclaredTons: sg.MaxLoadTons,
		})
	}
	var sin []domain.StructureInput
	for _, st := range structs {
		if !confirmed[st.SegmentRef] {
			continue // công trình trên đoạn nháp/ngừng dùng không vào phiên bản
		}
		sin = append(sin, domain.StructureInput{
			Ref: st.StructureRef, RevisionID: st.ID, RowHash: st.RowHash, Kind: st.Kind, Name: st.Name, SegmentRef: st.SegmentRef,
			At: st.At, ClearanceHeightM: st.ClearanceHeightM, ClearanceWidthM: st.ClearanceWidthM, DepthM: st.DepthM,
		})
	}
	g, err := domain.Build(in, sin, params)
	if err != nil {
		return g, ErrParamsInvalid.WithDetail(err.Error())
	}
	return g, nil
}

// ---------------------------------------------------------------------------
// Phiên bản

// Versions liệt kê phiên bản mạng mới → cũ.
func (s *Service) Versions(ctx context.Context, before, limit int) ([]domain.NetworkVersion, *int, error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return nil, nil, err
	}
	limit = ClampLimit(limit)
	var items []domain.NetworkVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListVersions(ctx, tx, before, limit+1)
		return e
	})
	var next *int
	if len(items) > limit {
		items = items[:limit]
		n := items[limit-1].VersionNo
		next = &n
	}
	return items, next, err
}

// Version đọc một phiên bản (Admin API).
func (s *Service) Version(ctx context.Context, no int) (domain.NetworkVersion, error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return domain.NetworkVersion{}, err
	}
	var v *domain.NetworkVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.VersionByNo(ctx, tx, no, false)
		return e
	})
	if err != nil {
		return domain.NetworkVersion{}, err
	}
	if v == nil {
		return domain.NetworkVersion{}, ErrVersionNotFound
	}
	return *v, nil
}

// Findings liệt kê phát hiện khi dựng của một phiên bản.
func (s *Service) Findings(ctx context.Context, no int, kind string, after int64, limit int) ([]store.FindingRow, *int64, error) {
	p, err := authz.Require(ctx, adminReaders...)
	if err != nil {
		return nil, nil, err
	}
	switch kind {
	case "", domain.FindingCrossing, domain.FindingTJoin, domain.FindingGapJoined, domain.FindingGapReported, domain.FindingDangling, domain.FindingIsolated:
	default:
		return nil, nil, problem.Validation.WithField("kind", "loại phát hiện không hợp lệ")
	}
	limit = ClampLimit(limit)
	var items []store.FindingRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := store.VersionByNo(ctx, tx, no, false)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrVersionNotFound
		}
		items, err = store.ListFindings(ctx, tx, v.ID, kind, after, limit+1)
		return err
	})
	var next *int64
	if len(items) > limit {
		items = items[:limit]
		n := items[limit-1].ID
		next = &n
	}
	return items, next, err
}

// Publish phát hành một phiên bản đã dựng: người phát hành (waterway_publisher)
// KHÁC người dựng (R-56). Phiên bản đang phát hành chuyển sang superseded (vẫn
// giữ, vẫn định tuyến được khi ghim); chỉ một phiên bản có hiệu lực tại một thời
// điểm. Phát sự kiện pf.waterway.network.published cho pf/simulation.
func (s *Service) Publish(ctx context.Context, no int) (domain.NetworkVersion, error) {
	p, err := authz.Require(ctx, port.RolePublisher)
	if err != nil {
		return domain.NetworkVersion{}, err
	}
	var out domain.NetworkVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockTenantBuilds(ctx, tx, p.TenantID); err != nil {
			return err
		}
		v, err := store.VersionByNo(ctx, tx, no, true)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrVersionNotFound
		}
		if err := authz.EnsureDistinctApproverFor(v.BuiltBy, p, port.RolePublisher); err != nil {
			return err
		}
		if v.Status != domain.VersionBuilt {
			return ErrVersionNotPublishable.WithDetail("trạng thái: " + v.Status)
		}
		prev, err := store.PublishedVersion(ctx, tx, true)
		if err != nil {
			return err
		}
		now := s.now()
		var supersedesNo *int
		var supersededID *string
		if prev != nil {
			if err := store.Supersede(ctx, tx, prev.ID, v.ID, now); err != nil {
				return err
			}
			n, id := prev.VersionNo, prev.ID.String()
			supersedesNo, supersededID = &n, &id
		}
		if err := store.Publish(ctx, tx, v.ID, p.ActorRef, now); err != nil {
			return err
		}
		pub, err := store.VersionByID(ctx, tx, v.ID)
		if err != nil {
			return err
		}
		out = *pub
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventNetworkPublished, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: Producer,
			Payload: port.NetworkPublishedV1{
				NetworkVersionID: out.ID.String(), VersionNo: out.VersionNo, SegmentSetSHA256: out.SegmentSetSHA256,
				GraphSHA256: out.GraphSHA256, EffectiveFrom: formatTime(*out.EffectiveFrom), PublishedBy: p.ActorRef.String(),
				BuiltBy: out.BuiltBy.String(), SupersedesVersionNo: supersedesNo, VertexCount: out.VertexCount,
				EdgeCount: out.EdgeCount, RuleVersion: out.RuleVersion, SupersededVersionID: supersededID,
			},
		})
	})
	return out, err
}
