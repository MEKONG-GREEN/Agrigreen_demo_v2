package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// PackageURLTTL là thời hạn URL tải gói (≤ objstore.MaxPresignTTL).
const PackageURLTTL = 10 * time.Minute

var exportReaders = []string{RoleExporter, RoleViewer}

// RequestExport tạo yêu cầu xuất gói bằng chứng (mục 12): ghi job + outbox
// pf.mrv.export.requested trong MỘT giao dịch; worker dựng gói. Bộ lọc được giải
// phạm vi ngay lúc yêu cầu (danh sách xã cụ thể) và lưu bất biến trên job.
func (s *Service) RequestExport(ctx context.Context, f domain.ExportFilters, idemKey string) (domain.ExportJob, bool, error) {
	p, err := authz.Require(ctx, RoleExporter)
	if err != nil {
		return domain.ExportJob{}, false, err
	}
	if idemKey == "" {
		return domain.ExportJob{}, false, problem.IdempotencyRequired
	}
	if err := validation(f.Validate()); err != nil {
		return domain.ExportJob{}, false, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.ExportJob{}, false, err
	}
	switch {
	case sc.Empty():
		return domain.ExportJob{}, false, ErrOutOfScope
	case len(f.AdminUnitCodes) == 0 && sc.All:
		f.AllUnits = true
	case len(f.AdminUnitCodes) == 0:
		f.AdminUnitCodes = append([]string(nil), sc.Units...)
	default:
		for _, c := range f.AdminUnitCodes {
			if !sc.Allows(c) {
				return domain.ExportJob{}, false, ErrOutOfScope.WithDetail("đơn vị " + c + " ngoài phạm vi")
			}
		}
	}
	j := domain.ExportJob{
		ID: ids.New(), TenantID: p.TenantID, Status: domain.ExportQueued, Filters: f.Normalize(),
		RequestedBy: p.ActorRef, IdemKey: idemKey,
	}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.JobByIdemKey(ctx, tx, idemKey); err != nil {
			return err
		} else if prev != nil {
			j, replayed = *prev, true
			return nil
		}
		if err := s.packageEgress(ctx, tx, j, p.ActorRef); err != nil {
			return err
		}
		if err := store.InsertJob(ctx, tx, &j); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: j.TenantID, Type: port.EventExportRequested, SchemaVersion: 1,
			OccurredAt: j.RequestedAt, TraceID: telemetry.TraceID(ctx), Producer: Producer,
			Payload: port.ExportRequestedV1{
				ExportID: j.ID.String(), RequestedBy: j.RequestedBy.String(), RequestedAt: domain.FormatTime(j.RequestedAt),
			},
		})
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var prev *domain.ExportJob
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

// jobVisible: người gọi chỉ thấy job mà bộ lọc nằm trọn trong phạm vi của mình.
func jobVisible(sc authz.Scope, j domain.ExportJob) bool {
	if sc.All {
		return true
	}
	if j.Filters.AllUnits {
		return false
	}
	for _, c := range j.Filters.AdminUnitCodes {
		if !sc.Allows(c) {
			return false
		}
	}
	return true
}

// Export đọc trạng thái job xuất.
func (s *Service) Export(ctx context.Context, id uuid.UUID) (domain.ExportJob, error) {
	p, err := authz.Require(ctx, exportReaders...)
	if err != nil {
		return domain.ExportJob{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.ExportJob{}, err
	}
	var j *domain.ExportJob
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		j, e = store.JobByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return domain.ExportJob{}, err
	}
	if j == nil || !jobVisible(sc, *j) {
		return domain.ExportJob{}, ErrExportNotFound
	}
	return *j, nil
}

// PackageLink là URL tải gói có hạn.
type PackageLink struct {
	Job       domain.ExportJob
	URL       string
	ExpiresAt time.Time
}

// PackageURL cấp URL tải gói ngắn hạn (vai mrv_exporter). Không ghi URL vào log (R-92).
func (s *Service) PackageURL(ctx context.Context, id uuid.UUID) (PackageLink, error) {
	if _, err := authz.Require(ctx, RoleExporter); err != nil {
		return PackageLink{}, err
	}
	j, err := s.Export(ctx, id)
	if err != nil {
		return PackageLink{}, err
	}
	if j.Status != domain.ExportCompleted || j.ObjectKey == nil {
		return PackageLink{}, ErrExportNotReady.WithDetail("trạng thái: " + string(j.Status))
	}
	if s.Store == nil {
		return PackageLink{}, ErrDependencyUnavailable
	}
	if err := s.downloadEgress(ctx, j); err != nil {
		return PackageLink{}, err
	}
	u, err := s.Store.PresignGet(ctx, *j.ObjectKey, PackageURLTTL)
	if err != nil {
		return PackageLink{}, err
	}
	return PackageLink{Job: j, URL: u, ExpiresAt: s.Clock.Now().Add(PackageURLTTL)}, nil
}

// Verification là kết quả dựng lại ảnh chụp của một gói đã xuất.
type Verification struct {
	Job     domain.ExportJob
	Stored  string // snapshot_sha256 đã ghi sổ
	Rebuilt string // dựng lại từ sổ tại as_of
	Match   bool
}

// VerifyExport dựng lại ảnh chụp dữ liệu tại as_of của gói từ sổ và so băm —
// cho bên thẩm định kiểm gói không bị sửa và dữ liệu nguồn không bị đổi.
func (s *Service) VerifyExport(ctx context.Context, id uuid.UUID) (Verification, error) {
	j, err := s.Export(ctx, id)
	if err != nil {
		return Verification{}, err
	}
	if j.Status != domain.ExportCompleted || j.SnapshotSHA256 == nil {
		return Verification{}, ErrExportNotReady.WithDetail("trạng thái: " + string(j.Status))
	}
	var sum string
	err = s.tx(ctx, j.TenantID, func(tx pgx.Tx) error {
		snap, err := buildSnapshot(ctx, tx, j)
		if err != nil {
			return err
		}
		_, sum, err = snap.Canonical()
		return err
	})
	if err != nil {
		return Verification{}, err
	}
	return Verification{Job: j, Stored: *j.SnapshotSHA256, Rebuilt: sum, Match: sum == *j.SnapshotSHA256}, nil
}

// HandleExportRequested là inbox.Handler của worker cho pf.mrv.export.requested.
// Chạy trong giao dịch tenant do inbox.Process mở (pool vai mrv_app), nên kết
// quả + inbox + outbox commit cùng nhau (R-61). Job đã xong → không làm gì.
//
// P0-POOL-SHARED-2 (#143): ghi gói vào kho đối tượng NGOÀI giao dịch inbox (txdeps.Get — inbox.Process hủy giao dịch,
// ghi, chạy lại handler). Lần chạy lại dựng lại ảnh chụp tại as_of (tất định) và chỉ ghi sổ khi gói cùng băm đã ghi.
func (s *Service) HandleExportRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.ExportRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("mrv: payload %s hỏng: %w", env.EventType, err)
	}
	id, err := uuid.Parse(pl.ExportID)
	if err != nil {
		return fmt.Errorf("mrv: export_id không hợp lệ: %w", err)
	}
	j, err := store.JobByID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if j == nil || j.Status != domain.ExportQueued {
		return nil // không có (tenant khác/đã xóa) hoặc đã xử lý — giao lặp an toàn
	}
	if s.Store == nil {
		return ErrDependencyUnavailable
	}
	snap, err := buildSnapshot(ctx, tx, *j)
	if err != nil {
		return err
	}
	body, snapSHA, err := domain.BuildPackage(*j, snap)
	if err != nil {
		return err
	}
	pkgSHA := objstore.SHA256Hex(body)
	key := domain.ObjectKey(j.TenantID, j.ID)
	if _, err := txdeps.Get(ctx, "mrv.export.put:"+key+":"+pkgSHA, func(ctx context.Context) (objstore.Info, error) {
		return s.Store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), pkgSHA, "application/json")
	}); err != nil {
		if errors.Is(err, objstore.ErrExists) {
			// Đã có gói khác nội dung dưới cùng khóa (dữ liệu tại as_of đổi giữa hai lần giao) — không ghi đè.
			return store.FailJob(ctx, tx, j.ID, "package_conflict")
		}
		return err
	}
	now := s.Clock.Now()
	row := store.PackageRow{
		ExportID: j.ID, ObjectKey: key, PackageSHA256: pkgSHA, SnapshotSHA256: snapSHA, SizeBytes: int64(len(body)),
		EvidenceCount: len(snap.Evidence), AsOf: j.RequestedAt, ActorRef: j.RequestedBy, EventTime: now,
	}
	if err := store.InsertPackage(ctx, tx, j.TenantID, &row); err != nil {
		return ledger.Classify(err)
	}
	if err := store.CompleteJob(ctx, tx, j.ID, key, pkgSHA, snapSHA, row.EvidenceCount, now); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: j.TenantID, Type: port.EventExportCompleted, SchemaVersion: 1,
		OccurredAt: now, TraceID: env.TraceID, Producer: Producer,
		Payload: port.ExportCompletedV1{
			ExportID: j.ID.String(), ObjectKey: key, PackageSHA256: pkgSHA, SnapshotSHA256: snapSHA,
			EvidenceCount: row.EvidenceCount, AsOf: domain.FormatTime(j.RequestedAt), CompletedAt: domain.FormatTime(now),
		},
	})
}

// buildSnapshot dựng ảnh chụp dữ liệu của job tại as_of = requested_at, chỉ từ sổ.
func buildSnapshot(ctx context.Context, tx pgx.Tx, j domain.ExportJob) (domain.Snapshot, error) {
	rows, err := store.SnapshotAt(ctx, tx, j.Filters, j.RequestedAt)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var snap domain.Snapshot
	type catKey struct {
		t string
		v int
	}
	seen := map[catKey]bool{}
	for _, r := range rows {
		snap.Evidence = append(snap.Evidence, snapshotEvidence(r))
		k := catKey{r.Evidence.EvidenceType, r.Evidence.CatalogVersion}
		if seen[k] {
			continue
		}
		seen[k] = true
		cat, err := store.Approved(ctx, tx, k.t, k.v)
		if err != nil {
			return domain.Snapshot{}, err
		}
		if cat == nil {
			return domain.Snapshot{}, fmt.Errorf("mrv: minh chứng ghim danh mục %s v%d chưa duyệt", k.t, k.v)
		}
		snap.Catalogs = append(snap.Catalogs, snapshotCatalog(*cat))
	}
	if err := addG3Sections(ctx, tx, j, &snap); err != nil {
		return domain.Snapshot{}, err
	}
	snap.Sort()
	return snap, nil
}

func strPtr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func snapshotEvidence(r store.SnapshotRow) domain.SnapshotEvidence {
	e := r.Evidence
	values, _ := json.Marshal(e.Values)
	files := e.FileRefs
	if files == nil {
		files = []domain.FileRef{}
	}
	out := domain.SnapshotEvidence{
		ID: e.ID.String(), RootID: e.RootID.String(), ChainVersion: e.ChainVersion, SupersedesID: strPtr(e.SupersedesID),
		CorrectionReason: e.CorrectionReason, EvidenceType: e.EvidenceType, CatalogVersion: e.CatalogVersion,
		AdminUnitCode: e.AdminUnitCode, PlotRef: strPtr(e.PlotRef), HouseholdRef: strPtr(e.HouseholdRef),
		SeasonCode: e.SeasonCode, Values: values, FileRefs: files, EventTime: domain.FormatTime(e.EventTime),
		RecordedAt: domain.FormatTime(e.RecordedAt), CapturedBy: e.ActorRef.String(), DeviceID: e.DeviceID,
		RowHash: store.HexHash(e.RowHash),
	}
	if e.GPS != nil {
		g := &domain.SnapshotGPS{Lat: e.GPS.Lat.StringFixed(7), Lon: e.GPS.Lon.StringFixed(7)}
		if e.GPS.AccuracyM != nil {
			a := e.GPS.AccuracyM.StringFixed(2)
			g.AccuracyM = &a
		}
		out.GPS = g
	}
	if r.Review != nil {
		out.Review = &domain.SnapshotReview{
			ID: r.Review.ID.String(), Decision: string(r.Review.Decision), Reason: r.Review.Reason,
			ReviewerRef: r.Review.ActorRef.String(), EventTime: domain.FormatTime(r.Review.EventTime),
			RecordedAt: domain.FormatTime(r.Review.RecordedAt),
		}
	}
	return out
}

func snapshotCatalog(c domain.CatalogVersion) domain.SnapshotCatalog {
	out := domain.SnapshotCatalog{
		EvidenceType: c.EvidenceType, Version: c.Version, Provisional: c.Provisional, SourceNote: c.SourceNote,
		SetterRef: c.SetterRef.String(), Fields: make([]domain.SnapshotField, 0, len(c.Fields)),
	}
	if c.ApproverRef != nil {
		out.ApproverRef = c.ApproverRef.String()
	}
	if c.DecidedAt != nil {
		out.ApprovedAt = domain.FormatTime(*c.DecidedAt)
	}
	for _, f := range c.Fields {
		sf := domain.SnapshotField{
			Key: f.Key, LabelVi: f.LabelVi, DataType: string(f.Type), Required: f.Required, MaxScale: f.MaxScale,
			EnumValues: f.EnumValues, Position: f.Position,
		}
		if f.EnumValues == nil {
			sf.EnumValues = []string{}
		}
		if f.Unit != "" {
			u := f.Unit
			sf.Unit = &u
		}
		if f.Min != nil {
			m := f.Min.String()
			sf.Min = &m
		}
		if f.Max != nil {
			m := f.Max.String()
			sf.Max = &m
		}
		out.Fields = append(out.Fields, sf)
	}
	return out
}

// ProductMRVPackage là tên sản phẩm của kênh mrv_package ở điểm kiểm soát xuất (ADR-0024).
const ProductMRVPackage = "mrv_package"

// packageEgress: yêu cầu xuất gói đi qua điểm kiểm soát xuất kênh mrv_package (ADR-0024 mục 3) —
// phần tử = số bằng chứng ước (ảnh chụp tại lúc yêu cầu), nguồn = export_id, bên nhận = người yêu
// cầu. Chưa nối checkpoint → không đọc thêm gì (hành vi cũ). enforce + từ chối → không tạo job.
func (s *Service) packageEgress(ctx context.Context, tx pgx.Tx, j domain.ExportJob, actor uuid.UUID) error {
	if s.Egress == nil {
		return nil
	}
	rows, err := store.SnapshotAt(ctx, tx, j.Filters, s.Clock.Now())
	if err != nil {
		return err
	}
	return meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: j.ID, Channel: meter.ChannelMRVPackage, DataTenantID: j.TenantID, CounterpartyKind: meter.CounterpartyActor,
		CounterpartyRef: actor, Product: ProductMRVPackage, Records: int64(len(rows)), OccurredAt: s.Clock.Now(), ActorRef: actor,
		TraceID: telemetry.TraceID(ctx),
	}, s.Log)
}

// downloadEgress: mỗi lần cấp URL tải gói là một lần xuất (nguồn mới) — phần tử = số bằng chứng của
// gói, byte = cỡ gói (sổ export_package).
func (s *Service) downloadEgress(ctx context.Context, j domain.ExportJob) error {
	if s.Egress == nil {
		return nil
	}
	p, _ := authz.FromContext(ctx)
	var size int64
	if err := s.tx(ctx, j.TenantID, func(tx pgx.Tx) error {
		pk, err := store.PackagesOf(ctx, tx, j.ID)
		if len(pk) > 0 {
			size = pk[len(pk)-1].SizeBytes
		}
		return err
	}); err != nil {
		return err
	}
	var n int64
	if j.EvidenceCount != nil {
		n = int64(*j.EvidenceCount)
	}
	return meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: ids.New(), Channel: meter.ChannelMRVPackage, DataTenantID: j.TenantID, CounterpartyKind: meter.CounterpartyActor,
		CounterpartyRef: p.ActorRef, Product: ProductMRVPackage, Records: n, Bytes: size, OccurredAt: s.Clock.Now(), ActorRef: p.ActorRef,
		TraceID: telemetry.TraceID(ctx),
	}, s.Log)
}
