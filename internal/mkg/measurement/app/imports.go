package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MaxImportRows giới hạn một lần nhập (thao tác đồng bộ < 2 s, R-52).
const MaxImportRows = 200

// ImportInput là một lần nhập số cân của khách (C-5 — TẠM, chờ khách).
type ImportInput struct {
	Channel     string // file | api | manual
	CustomerRef *uuid.UUID
	EvidenceRef *uuid.UUID
	FileSHA256  string
	Rows        []store.ImportRow
	Reason      string
	IdemKey     string
}

// ImportResult là lần nhập kèm đối soát tự động của các lô đã đủ phiếu.
type ImportResult struct {
	Import          store.Import
	Reconciliations []store.Reconciliation
}

// ImportCustomerWeights ghi lần nhập + một phiếu CUSTOMER_IN mỗi dòng (cùng
// giao dịch; một dòng sai → cả lần nhập bị từ chối). file/api → nguồn
// customer_integration; manual → manual_evidence (bắt buộc bằng chứng + lý do).
func (s *Service) ImportCustomerWeights(ctx context.Context, in ImportInput) (res ImportResult, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleScaleSupervisor)
	if err != nil {
		return res, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return res, false, err
	}
	src := port.SourceCustomerIntegration
	switch in.Channel {
	case "file":
		if in.EvidenceRef == nil {
			return res, false, problem.Validation.WithField("evidence_ref", "nhập tệp cần tham chiếu tệp gốc")
		}
		if in.FileSHA256 != "" && !hex64Re.MatchString(in.FileSHA256) {
			return res, false, problem.Validation.WithField("file_sha256", "SHA-256 hex thường")
		}
	case "api":
	case "manual":
		src = port.SourceManualEvidence
		if in.EvidenceRef == nil || !domain.ValidReason(in.Reason) {
			return res, false, problem.Validation.WithDetail("nhập tay cần evidence_ref và lý do 10..500 ký tự")
		}
	default:
		return res, false, problem.Validation.WithField("channel", "file, api hoặc manual")
	}
	if len(in.Rows) == 0 || len(in.Rows) > MaxImportRows {
		return res, false, problem.Validation.WithField("rows", fmt.Sprintf("1..%d dòng", MaxImportRows))
	}
	im := store.Import{ID: ids.New(), Channel: in.Channel, CustomerRef: in.CustomerRef, EvidenceRef: in.EvidenceRef,
		FileSHA256: reasonPtr(in.FileSHA256), Rows: in.Rows, Provisional: true, Reason: reasonPtr(in.Reason), EventTime: s.Clock.Now(),
		ActorRef: p.ActorRef, IdemKey: in.IdemKey}
	drafts := make([]port.TicketDraft, 0, len(in.Rows))
	seen := map[uuid.UUID]bool{}
	for i, r := range in.Rows {
		if seen[r.LotRef] {
			return res, false, problem.Validation.WithField("rows["+strconv.Itoa(i)+"].lot_ref", "một lô xuất hiện hai lần")
		}
		seen[r.LotRef] = true
		route := port.Route(r.Route)
		if route == "" {
			route = port.RouteHub
		}
		d := port.TicketDraft{ID: ids.New(), IdemKey: in.IdemKey + "#" + strconv.Itoa(i), LotRef: r.LotRef, Point: port.PointCustomerIn,
			Route: route, Source: src, MassKg: r.MassKg, EventTime: r.WeighedAt, CustomerRef: in.CustomerRef, ActorRef: p.ActorRef}
		if r.MoisturePct != nil {
			d.MoisturePct = *r.MoisturePct
		}
		if src == port.SourceManualEvidence {
			d.EvidenceRef, d.Reason = in.EvidenceRef, in.Reason
		}
		if d.EventTime.IsZero() || d.EventTime.After(s.Clock.Now().Add(5*time.Minute)) {
			return res, false, problem.Validation.WithField("rows["+strconv.Itoa(i)+"].weighed_at", "bắt buộc, không ở tương lai")
		}
		if err := domain.NormalizeDraft(&d); err != nil {
			return res, false, ErrInvalidTicket.WithDetail("dòng " + strconv.Itoa(i) + ": " + err.Error())
		}
		im.TicketIDs = append(im.TicketIDs, d.ID)
		drafts = append(drafts, d)
	}
	run := func() error {
		return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			prev, err := store.ImportByIdem(ctx, tx, in.IdemKey)
			if err != nil {
				return err
			}
			if prev != nil {
				res.Import, replayed = *prev, true
				return nil
			}
			if err := insert(store.InsertImport(ctx, tx, p.TenantID, &im)); err != nil {
				return err
			}
			for i, d := range drafts {
				if _, _, err := recordTx(ctx, tx, p.TenantID, d, &im.ID); err != nil {
					return fmt.Errorf("dòng %d: %w", i, err)
				}
				r, _, err := s.reconcileTx(ctx, tx, p, d.LotRef, d.IdemKey+"#recon", true)
				if err != nil {
					return err
				}
				if r != nil {
					res.Reconciliations = append(res.Reconciliations, *r)
				}
			}
			res.Import = im
			return nil
		})
	}
	err = run()
	if errors.Is(err, ledger.ErrDuplicate) {
		err = run()
	}
	return res, replayed, mapErr(err)
}

// Import đọc một lần nhập.
func (s *Service) Import(ctx context.Context, id uuid.UUID) (store.Import, error) {
	var out store.Import
	err := s.read(ctx, func(tx pgx.Tx) error {
		im, err := store.ImportByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if im == nil {
			return ErrImportNotFound
		}
		out = *im
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Van mrv_collection_evidence (dự thảo, chờ C-1)
// ---------------------------------------------------------------------------

// PublishMRVEvidence lập bằng chứng thu gom cho MỖI thửa nguồn của lô, chỉ gửi BẬC
// (mrv.bucket.v1). Không bao giờ gửi khối lượng chính xác, lô, phiếu, dung sai, tranh
// chấp hay khách (mục 5). Idempotent theo khóa; mỗi lần xuất lại cùng (lô, thửa) giữ
// evidence_ref, tăng revision.
//
//   - Tuyến HUB: khối lượng = HUB_IN × tỷ lệ nguồn thửa của lô hub (mass_basis HUB_IN).
//   - Tuyến DIRECT (G3-2): không có HUB_IN — khối lượng = CUSTOMER_IN × tỷ lệ thửa, gắn cờ
//     GỘP (mass_basis CUSTOMER_IN_AGGREGATED: chưa tách hao hụt vận chuyển / ẩm).
//
// Tỷ lệ thửa: nguồn `plot` của lô hub (hub/port.LotOrigins, tenant tường minh — không
// cần vai hub); lô không có ở hub hoặc không có nguồn thửa → tỷ lệ theo số cuộn của lệnh
// gom liên kết (port.CollectionPlots, mkg/fieldops).
func (s *Service) PublishMRVEvidence(ctx context.Context, lot uuid.UUID, season *uuid.UUID, idem string) (out []store.ValveExport, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleReconciliationOfficer)
	if err != nil {
		return nil, false, err
	}
	if err := requireIdem(idem); err != nil {
		return nil, false, err
	}
	plots, err := s.plotShares(ctx, p.TenantID, lot)
	if err != nil {
		return nil, false, err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		lm, err := store.GetLotMass(ctx, tx, lot, false)
		if err != nil {
			return err
		}
		if lm == nil {
			return ErrNoHubIn
		}
		point, basis := port.PointHubIn, port.MassBasisHubIn
		if lm.Route == port.RouteDirect {
			point, basis = port.PointCustomerIn, port.MassBasisCustomerInAggregated
		}
		hin, ok := lm.Points[point]
		if !ok {
			if lm.Route == port.RouteDirect {
				return ErrNoCustomerIn
			}
			return ErrNoHubIn
		}
		c, err := publication.Get(port.ContractMRVCollectionEvidence)
		if err != nil {
			return ErrValveDenied.WithCause(err)
		}
		evType, evVer, ok := c.EventType()
		if !ok {
			return ErrValveDenied.WithDetail("hợp đồng không khai sự kiện")
		}
		for _, sh := range plots {
			kg, err := domain.ShareOf(hin.Kg, sh.Ratio)
			if err != nil {
				return err
			}
			prev, err := store.LastValveFor(ctx, tx, lot, sh.PlotRef)
			if err != nil {
				return err
			}
			evidence, rev := ids.New(), 1
			if prev != nil {
				evidence, rev = prev.SubjectRef, prev.Revision+1
			}
			pub := port.MRVCollectionEvidencePublishedV1{
				EvidenceRef: evidence, PlotRef: sh.PlotRef, SeasonRef: season, CollectedOn: clock.BusinessDate(hin.At).String(),
				CollectedKgBucket: domain.Bucket(kg), BucketRuleVersion: domain.RuleMRVBucket, Revision: rev, MassBasis: basis,
			}
			raw, err := json.Marshal(pub)
			if err != nil {
				return err
			}
			if err := c.Validate(raw); err != nil {
				return ErrValveDenied.WithCause(err)
			}
			x := store.ValveExport{ID: ids.New(), Contract: c.Name, ContractVersion: c.Version, EventType: evType, SchemaVersion: evVer,
				SubjectRef: evidence, SourceRef: lot, PlotRef: sh.PlotRef, Revision: rev, Payload: raw, EventTime: s.Clock.Now(),
				ActorRef: p.ActorRef, IdemKey: idem + "#" + sh.PlotRef.String()}
			if err := insert(store.InsertValveExport(ctx, tx, p.TenantID, &x)); err != nil {
				return err
			}
			if err := outbox.Enqueue(ctx, tx, outbox.Event{
				ID: x.ID, TenantID: p.TenantID, Type: evType, SchemaVersion: evVer, OccurredAt: x.EventTime.UTC(),
				TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pub, Kind: outbox.KindPlatform,
			}); err != nil {
				return err
			}
			out = append(out, x)
		}
		return nil
	}, func(tx pgx.Tx) (bool, error) {
		out = nil
		for _, sh := range plots {
			v, err := store.ValveByIdem(ctx, tx, idem+"#"+sh.PlotRef.String())
			if err != nil || v == nil {
				return false, err
			}
			out = append(out, *v)
		}
		return true, nil
	})
	return out, replayed, err
}

// plotShares trả tỷ lệ thửa của lô: nguồn thửa của lô hub, không có thì theo lệnh gom.
func (s *Service) plotShares(ctx context.Context, tid, lot uuid.UUID) ([]port.PlotShare, error) {
	if s.Origins == nil && s.Plots == nil {
		return nil, ErrNoPlotSources.WithDetail("chưa nối nguồn gốc lô (mkg/hub) hay lệnh gom (mkg/fieldops)")
	}
	var plots []port.PlotShare
	if s.Origins != nil {
		info, err := s.Origins.LotOrigin(ctx, tid, lot)
		switch {
		case err == nil:
			for _, src := range info.Sources {
				if src.Kind == "plot" {
					plots = append(plots, port.PlotShare{PlotRef: src.Ref, Ratio: src.Ratio})
				}
			}
		case !errors.Is(err, hubport.ErrLotNotFound):
			return nil, ErrNoPlotSources.WithCause(err)
		}
	}
	if len(plots) == 0 && s.Plots != nil {
		link, err := s.Writer.CollectionOfLot(ctx, tid, lot)
		if err != nil {
			return nil, err
		}
		ps, err := s.Plots.CollectionPlots(ctx, tid, link.CollectionOrderRef)
		if err != nil && !errors.Is(err, port.ErrNotFound) {
			return nil, ErrNoPlotSources.WithCause(err)
		}
		plots = ps
	}
	if len(plots) == 0 {
		return nil, ErrNoPlotSources
	}
	return plots, nil
}

// Publications liệt kê bản ghi đã xuất qua van của một lô (kiểm toán).
func (s *Service) Publications(ctx context.Context, lot uuid.UUID) ([]store.ValveExport, error) {
	var out []store.ValveExport
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ValvesOfLot(ctx, tx, lot)
		return e
	})
	return out, err
}
