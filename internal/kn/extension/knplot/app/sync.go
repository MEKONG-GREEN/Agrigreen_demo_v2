package app

// Đồng bộ ngoại tuyến của thửa KN (docs/dev/sync-contract.md): chiều xuống entity "kn_plot" (con trỏ kn_plot.sync_seq,
// thửa trong phạm vi + phòng ban của ngữ cảnh phiên, kèm hình để vẽ nền và kiểm chồng lấn trên máy); chiều lên entity
// "kn_plot_boundary" (ranh giới vẽ ngoại tuyến ở field-app: op create = vẽ lô mới, op correct = sửa ranh giới với
// supersedes_id = boundary_id hiện hành). Cùng quy tắc như ghi trực tuyến: chồng lấn hay sửa trên phiên bản cũ → bản ghi
// bị từ chối (máy giữ lại, người dùng vẽ lại), không có hàng chờ xung đột.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Entity đồng bộ.
const (
	EntityPlot         = "kn_plot"
	EntityPlotBoundary = "kn_plot_boundary"
)

// SyncSource là chiều xuống "kn_plot".
type SyncSource struct{ Svc *Service }

// SyncSink là chiều lên "kn_plot_boundary".
type SyncSink struct{ Svc *Service }

var (
	_ syncport.Source = SyncSource{}
	_ syncport.Sink   = SyncSink{}
)

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string { return []string{EntityPlot} }

// PlotSyncData là một thửa gửi xuống máy — không dữ liệu cá nhân (tên nông dân không xuống máy).
type PlotSyncData struct {
	Ref            string          `json:"ref"`
	Code           string          `json:"code"`
	Name           *string         `json:"name"`
	LotName        *string         `json:"lot_name"`
	Note           *string         `json:"note"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef *string         `json:"cooperative_ref"`
	Status         string          `json:"status"`
	Version        int64           `json:"version"`
	BoundaryID     string          `json:"boundary_id"`
	AreaHa         string          `json:"area_ha"`
	DeclaredAreaHa *string         `json:"declared_area_ha"`
	Shape          *string         `json:"shape"`
	DepartmentCode *string         `json:"department_code"`
	Geometry       json.RawMessage `json:"geometry"`
	UpdatedAt      string          `json:"updated_at"`
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Changes thực hiện syncport.Source; người gọi không có ngữ cảnh phiên KN → không có thay đổi.
func (src SyncSource) Changes(ctx context.Context, _ authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	if entity != EntityPlot {
		return nil, after, fmt.Errorf("knplot: entity %q không thuộc module", entity)
	}
	c, err := src.Svc.caller(ctx, false)
	if err != nil || c.k.Scope.Empty() {
		return []syncport.Change{}, after, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []store.Plot
	err = src.Svc.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) {
		rows, e = store.ChangesAfter(ctx, tx, c.k.Scope.All, c.k.Scope.Units, c.readDept(), after, limit)
		return
	})
	if err != nil {
		return nil, after, err
	}
	out := make([]syncport.Change, 0, len(rows))
	last := after
	for _, r := range rows {
		d := PlotSyncData{Ref: r.Ref.String(), Code: r.Code, Name: optStr(r.LotName), LotName: optStr(r.LotName), Note: optStr(r.Note),
			CommuneCode: r.CommuneCode, Status: Status(r.Snapshot), Version: r.Version, BoundaryID: r.BoundaryID.String(),
			AreaHa: r.AreaHa.StringFixed(4), Shape: optStr(r.Shape), DepartmentCode: optStr(r.Department),
			Geometry: rawJSON(r.GeoJSON), UpdatedAt: r.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
		if r.CooperativeRef != nil {
			s := r.CooperativeRef.String()
			d.CooperativeRef = &s
		}
		if r.DeclaredAreaHa != nil {
			s := r.DeclaredAreaHa.StringFixed(4)
			d.DeclaredAreaHa = &s
		}
		b, err := json.Marshal(d)
		if err != nil {
			return nil, after, err
		}
		out = append(out, syncport.Change{Entity: EntityPlot, ID: r.Ref, Version: r.Version, Op: syncport.OpUpsert, Data: b})
		last = r.SyncSeq
	}
	return out, last, nil
}

// Entities thực hiện syncport.Sink.
func (SyncSink) Entities() []string { return []string{EntityPlotBoundary} }

// boundaryPayload là thân "kn_plot_boundary" (trường lạ của máy bị bỏ qua).
type boundaryPayload struct {
	PlotRef        string          `json:"plot_ref"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef *string         `json:"cooperative_ref"`
	Geometry       json.RawMessage `json:"geometry"`
	DeclaredAreaHa json.RawMessage `json:"declared_area_ha"`
	LotName        string          `json:"lot_name"`
	Note           string          `json:"note"`
	FarmerName     string          `json:"farmer_name" pii:"true"`
	Shape          string          `json:"shape"`
}

func rejected(id uuid.UUID, code string, fields ...problem.FieldError) syncport.Result {
	return syncport.Result{ID: id, Status: syncport.StatusRejected, Code: code, Errors: fields}
}

// Accept thực hiện syncport.Sink: ghi như API trực tuyến (cùng quyền, phạm vi, chặn chồng lấn), idempotent theo IdemKey.
func (s SyncSink) Accept(ctx context.Context, _ authz.Scope, rec syncport.Record) syncport.Result {
	var p boundaryPayload
	if err := json.Unmarshal(rec.Payload, &p); err != nil {
		return rejected(rec.ID, "validation_failed", problem.FieldError{Field: "payload", Reason: "không đọc được"})
	}
	ref, err := uuid.Parse(strings.TrimSpace(p.PlotRef))
	if err != nil {
		return rejected(rec.ID, "validation_failed", problem.FieldError{Field: "plot_ref", Reason: "phải là UUID"})
	}
	dev := rec.DeviceID.String()
	var res Result
	switch rec.Op {
	case syncport.OpCreate:
		var coop *uuid.UUID
		if p.CooperativeRef != nil && strings.TrimSpace(*p.CooperativeRef) != "" {
			u, err := uuid.Parse(strings.TrimSpace(*p.CooperativeRef))
			if err != nil {
				return rejected(rec.ID, "validation_failed", problem.FieldError{Field: "cooperative_ref", Reason: "phải là UUID"})
			}
			coop = &u
		}
		declared, why := domain.ParseDeclared(p.DeclaredAreaHa)
		if why != "" {
			return rejected(rec.ID, "validation_failed", problem.FieldError{Field: "declared_area_ha", Reason: why})
		}
		res, err = s.Svc.Create(ctx, CreateInput{PlotRef: ref, CommuneCode: p.CommuneCode, CooperativeRef: coop, FarmerName: p.FarmerName,
			LotName: p.LotName, Note: p.Note, DeclaredAreaHa: declared, Shape: p.Shape, Geometry: p.Geometry,
			IdemKey: rec.IdemKey, Source: domain.SourceFieldApp, DeviceID: &dev, EventTime: rec.EventTime})
	case syncport.OpCorrect:
		if rec.SupersedesID == nil || rec.CorrectionReason == nil {
			return rejected(rec.ID, "validation_failed", problem.FieldError{Field: "supersedes_id", Reason: "bắt buộc khi sửa ranh giới"})
		}
		res, err = s.Svc.Correct(ctx, CorrectInput{PlotRef: ref, SupersedesID: *rec.SupersedesID, Geometry: p.Geometry, Shape: p.Shape,
			Reason: *rec.CorrectionReason, IdemKey: rec.IdemKey, Source: domain.SourceFieldApp, DeviceID: &dev, EventTime: rec.EventTime})
	default:
		return rejected(rec.ID, "validation_failed", problem.FieldError{Field: "op", Reason: "create | correct"})
	}
	if err != nil {
		var pr *problem.Problem
		if !errors.As(err, &pr) {
			pr = problem.Internal // lỗi hệ thống: pf/sync coi internal_error là thử lại được
		}
		fields := pr.Errors
		if len(fields) == 0 {
			reason := pr.Detail
			if reason == "" {
				reason = pr.Title
			}
			fields = []problem.FieldError{{Field: "geometry", Reason: reason}}
		}
		return rejected(rec.ID, pr.Code, fields...)
	}
	if res.Replayed {
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	}
	return syncport.Result{ID: rec.ID, Status: syncport.StatusAccepted}
}
