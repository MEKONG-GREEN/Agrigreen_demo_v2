package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// SyncSource là chiều xuống của entity "plot" (docs/dev/sync-contract.md).
type SyncSource struct{ Svc *Service }

// SyncSink là chiều lên của entity "plot_boundary": ranh giới vẽ ngoại tuyến.
type SyncSink struct{ Svc *Service }

var (
	_ syncport.FarmerSource = SyncSource{}
	_ syncport.Source       = SyncSource{}
	_ syncport.Sink         = SyncSink{}
)

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string { return []string{port.EntityPlot} }

// PlotSyncData là dữ liệu một thửa gửi xuống máy — không có dữ liệu cá nhân (mục 10.4).
type PlotSyncData struct {
	Ref            string          `json:"ref"`
	Code           string          `json:"code"`
	Name           *string         `json:"name"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef *string         `json:"cooperative_ref"`
	HouseholdRef   *string         `json:"household_ref"`
	Status         string          `json:"status"`
	Version        int64           `json:"version"`
	BoundaryID     string          `json:"boundary_id"`
	AreaHa         string          `json:"area_ha"`
	Source         string          `json:"source"`
	NeedsReview    bool            `json:"needs_review"`
	Geometry       json.RawMessage `json:"geometry"`
	UpdatedAt      string          `json:"updated_at"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func refPtr(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

// FarmerEntities thực hiện syncport.FarmerSource: nông dân nhận thửa của nông hộ mình.
func (SyncSource) FarmerEntities() []string { return []string{port.EntityPlot} }

// FarmerChanges thực hiện syncport.FarmerSource (App HTX): chỉ thửa của các nông hộ liên kết
// với tài khoản; thửa ngừng dùng hoặc đã chuyển sang nông hộ khác → tombstone. Không bao
// giờ theo xã — thửa của hộ khác cùng xã không đi xuống máy nông dân.
func (src SyncSource) FarmerChanges(ctx context.Context, fs syncport.FarmerScope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if entity != port.EntityPlot {
		return nil, after, fmt.Errorf("geo: entity %q không thuộc module", entity)
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, after, problem.Unauthenticated
	}
	if !p.Has(port.RoleFarmer) || len(fs.Households) == 0 {
		return []syncport.Change{}, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []store.Plot
	err := tenant.WithTenantTx(ctx, src.Svc.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ChangesAfterForHouseholds(ctx, tx, fs.Households, after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	return plotChanges(rows, after, func(r store.Plot) bool { return slices.Contains(fs.Households, r.HouseholdRef) })
}

// Changes thực hiện syncport.Source. Con trỏ là pf_geo.plot.sync_seq. Thửa ngừng
// dùng, hoặc (với quản lý HTX) đã chuyển sang HTX khác → tombstone. Thửa ở xã
// ngoài phạm vi không bao giờ hiện trên máy (xã của thửa không đổi) nên bỏ qua.
func (src SyncSource) Changes(ctx context.Context, scope authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if entity != port.EntityPlot {
		return nil, after, fmt.Errorf("geo: entity %q không thuộc module", entity)
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, after, problem.Unauthenticated
	}
	a := accessFor(p, scope)
	if a.sc.Empty() {
		return []syncport.Change{}, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []store.Plot
	err := tenant.WithTenantTx(ctx, src.Svc.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ChangesAfter(ctx, tx, a.sc.All, a.sc.Units, after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	return plotChanges(rows, after, func(r store.Plot) bool { return a.sees(r.CommuneCode, r.CooperativeRef) })
}

// plotChanges dựng thay đổi chiều xuống; thửa ngừng dùng hoặc người gọi không còn thấy → tombstone.
func plotChanges(rows []store.Plot, after int64, sees func(store.Plot) bool) ([]syncport.Change, int64, error) {
	out := make([]syncport.Change, 0, len(rows))
	last := after
	for _, r := range rows {
		last = r.SyncSeq
		ch := syncport.Change{Entity: port.EntityPlot, ID: r.Ref, Version: r.Revision}
		if r.Status == port.StatusRetired || !sees(r) {
			ch.Op = syncport.OpTombstone
			out = append(out, ch)
			continue
		}
		data, err := json.Marshal(PlotSyncData{
			Ref: r.Ref.String(), Code: r.Code, Name: strPtr(r.Name), CommuneCode: r.CommuneCode,
			CooperativeRef: refPtr(r.CooperativeRef), HouseholdRef: refPtr(r.HouseholdRef), Status: r.Status,
			Version: r.CurrentVersion, BoundaryID: r.CurrentBoundaryID.String(), AreaHa: r.AreaHa.StringFixed(4),
			Source: r.Source, NeedsReview: r.NeedsReview, Geometry: json.RawMessage(r.GeoJSON),
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return nil, after, err
		}
		ch.Op, ch.Data = syncport.OpUpsert, data
		out = append(out, ch)
	}
	return out, last, nil
}

// Entities thực hiện syncport.Sink.
func (SyncSink) Entities() []string { return []string{port.EntityPlotBoundary} }

// BoundaryPayload là payload của bản ghi plot_boundary từ máy.
//
//	op create: plot_ref (UUIDv7 sinh trên máy), commune_code, geometry bắt buộc.
//	op correct: supersedes_id = phiên bản đang sửa; plot_ref suy ra từ phiên bản đó.
type BoundaryPayload struct {
	PlotRef        string          `json:"plot_ref"`
	Name           string          `json:"name"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef string          `json:"cooperative_ref"`
	HouseholdRef   string          `json:"household_ref"`
	Geometry       json.RawMessage `json:"geometry"`
	GPSAccuracyM   *string         `json:"gps_accuracy_m"`
	DeclaredAreaHa *string         `json:"declared_area_ha"`
	Source         string          `json:"source"`
	// ConfirmOverlap: người vẽ đã xác nhận giữ nguyên chồng lấn cùng HTX/thửa nháp trên máy.
	ConfirmOverlap bool `json:"confirm_overlap"`
}

func rejected(id uuid.UUID, err error) syncport.Result {
	var pr *problem.Problem
	if !errors.As(err, &pr) {
		pr = problem.Internal // lỗi hệ thống: pf/sync coi internal_error là thử lại được
	}
	return syncport.Result{ID: id, Status: syncport.StatusRejected, Code: pr.Code, Errors: fieldErrors(pr)}
}

func fieldErrors(pr *problem.Problem) []problem.FieldError {
	if len(pr.Errors) > 0 {
		return pr.Errors
	}
	reason := pr.Detail
	if reason == "" {
		reason = pr.Title
	}
	return []problem.FieldError{{Field: "payload", Reason: reason}}
}

func optRef(field, s string) (uuid.UUID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return uuid.Nil, nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, problem.Validation.WithField("payload."+field, "phải là UUID")
	}
	return u, nil
}

// Accept thực hiện syncport.Sink: idempotent theo IdemKey. Không chặn được máy
// ngoại tuyến (mục 15): chồng lấn HTX khác, sửa đồng thời, hoặc chồng lấn cùng
// HTX chưa xác nhận → giữ cả hai, mở xung đột (StatusConflict); chồng lấn cùng
// HTX đã xác nhận (confirm_overlap) → accepted, xung đột vẫn mở để xem xét.
// Lệch diện tích khai báo không chặn — ghi vào sự kiện pf.geo.plot_boundary.recorded.
func (snk SyncSink) Accept(ctx context.Context, scope authz.Scope, rec syncport.Record) syncport.Result {
	s := snk.Svc
	p, err := authz.Require(ctx, syncWriteRoles...)
	if err != nil {
		return rejected(rec.ID, err)
	}
	if rec.Entity != port.EntityPlotBoundary {
		return rejected(rec.ID, problem.Validation.WithField("entity", "không thuộc module pf/geo"))
	}
	if rec.IdemKey == "" {
		return rejected(rec.ID, problem.Validation.WithField("idem_key", "bắt buộc"))
	}
	if rec.ID.Version() != 7 {
		return rejected(rec.ID, problem.Validation.WithField("id", "phải là UUIDv7"))
	}
	// Lặp: idem_key đã ghi → duplicate (máy coi là thành công).
	var prev, byID *store.Boundary
	if err := s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if prev, e = store.BoundaryByIdemKey(ctx, tx, rec.IdemKey); e != nil || prev != nil {
			return e
		}
		byID, e = store.BoundaryByID(ctx, tx, rec.ID)
		return e
	}); err != nil {
		return rejected(rec.ID, err)
	}
	if prev != nil {
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	}
	if byID != nil {
		return rejected(rec.ID, problem.Validation.WithField("id", "đã dùng cho bản ghi khác"))
	}
	var pl BoundaryPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil {
		return rejected(rec.ID, problem.Validation.WithField("payload", "phải là JSON hợp lệ"))
	}
	b := BoundaryInput{ID: rec.ID, Geometry: pl.Geometry, Source: strings.TrimSpace(pl.Source), EventTime: rec.EventTime, Confirm: pl.ConfirmOverlap}
	if pl.DeclaredAreaHa != nil {
		d, err := decimal.NewFromString(*pl.DeclaredAreaHa)
		if err != nil {
			return rejected(rec.ID, problem.Validation.WithField("payload.declared_area_ha", "phải là số thập phân dạng chuỗi"))
		}
		b.DeclaredAreaHa = &d
	}
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		b.DeviceID = &d
	}
	if pl.GPSAccuracyM != nil {
		d, err := decimal.NewFromString(*pl.GPSAccuracyM)
		if err != nil {
			return rejected(rec.ID, problem.Validation.WithField("payload.gps_accuracy_m", "phải là số thập phân dạng chuỗi"))
		}
		b.GPSAccuracyM = &d
	}
	if rec.EventTime.IsZero() {
		return rejected(rec.ID, problem.Validation.WithField("event_time", "bắt buộc (R-13)"))
	}
	a := accessFor(p, scope)
	var res WriteResult
	switch rec.Op {
	case syncport.OpCreate:
		ref, err := ids.Parse(pl.PlotRef)
		if err != nil {
			return rejected(rec.ID, problem.Validation.WithField("payload.plot_ref", "bắt buộc, phải là UUIDv7 sinh trên máy"))
		}
		coop, err := optRef("cooperative_ref", pl.CooperativeRef)
		if err != nil {
			return rejected(rec.ID, err)
		}
		hh, err := optRef("household_ref", pl.HouseholdRef)
		if err != nil {
			return rejected(rec.ID, err)
		}
		res, err = s.create(ctx, a, CreateInput{
			PlotRef: ref, Name: pl.Name, CommuneCode: pl.CommuneCode, CooperativeRef: coop, HouseholdRef: hh,
			Boundary: b, IdemKey: rec.IdemKey,
		}, modeOffline, domain.SourceKNFieldApp)
		if err != nil {
			return rejected(rec.ID, err)
		}
	case syncport.OpCorrect:
		if rec.SupersedesID == nil || rec.CorrectionReason == nil {
			return rejected(rec.ID, problem.Validation.WithField("supersedes_id", "bản ghi bù cần supersedes_id và correction_reason"))
		}
		var old *store.Boundary
		if err := s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			old, e = store.BoundaryByID(ctx, tx, *rec.SupersedesID)
			return e
		}); err != nil {
			return rejected(rec.ID, err)
		}
		if old == nil {
			return rejected(rec.ID, problem.Validation.WithField("supersedes_id", "không có phiên bản ranh giới này"))
		}
		res, err = s.correct(ctx, a, CorrectInput{
			PlotRef: old.PlotRef, SupersedesID: old.ID, Reason: *rec.CorrectionReason, Boundary: b, IdemKey: rec.IdemKey,
		}, modeOffline, domain.SourceKNFieldApp)
		if err != nil {
			return rejected(rec.ID, err)
		}
	default:
		return rejected(rec.ID, problem.Validation.WithField("op", "chỉ nhận create hoặc correct (ghi thêm, không sửa/xóa)"))
	}
	if res.Replayed {
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	}
	// Chồng lấn cùng HTX/thửa nháp đã được xác nhận trên máy → accepted (xung đột vẫn mở để
	// xem xét); còn lại (HTX khác, sửa đồng thời, chưa xác nhận) → conflict.
	for _, cf := range res.Conflicts {
		if cf.Kind != domain.ConflictSameCoop || !pl.ConfirmOverlap {
			id := cf.ID
			return syncport.Result{ID: rec.ID, Status: syncport.StatusConflict, ConflictID: &id}
		}
	}
	return syncport.Result{ID: rec.ID, Status: syncport.StatusAccepted}
}
