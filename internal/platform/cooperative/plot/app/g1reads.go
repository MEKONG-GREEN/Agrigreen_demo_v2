package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// cooperativeport.Plots: số thửa đã số hoá (G-02, G-03)
// ---------------------------------------------------------------------------

// DigitizedByCommune thực hiện cooperativeport.Plots.
func (s *Service) DigitizedByCommune(ctx context.Context, communes []string) ([]port.PlotStat, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []store.StatRow
	err = s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		rows, e = store.StatsByCommune(ctx, tx, communes == nil, communes)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]port.PlotStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, port.PlotStat{CommuneCode: r.Key, Plots: r.Plots, AreaHa: r.AreaHa})
	}
	return out, nil
}

// DigitizedByCooperative thực hiện cooperativeport.Plots.
func (s *Service) DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.PlotStat, error) {
	out := map[uuid.UUID]port.PlotStat{}
	if len(coops) == 0 {
		return out, nil
	}
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []store.StatRow
	err = s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		rows, e = store.StatsByCooperative(ctx, tx, coops)
		return e
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ref, err := uuid.Parse(r.Key)
		if err != nil {
			return nil, err
		}
		out[ref] = port.PlotStat{CooperativeRef: ref, Plots: r.Plots, AreaHa: r.AreaHa}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Chi tiết xung đột ranh giới: giữ cả hai phiên bản (US-SYNC-03, mục 15)
// ---------------------------------------------------------------------------

// ConflictDetail là xung đột kèm mọi phiên bản liên quan (không có dữ liệu cá nhân).
type ConflictDetail struct {
	Conflict store.Conflict
	Versions []ConflictVersion
}

// ConflictVersion là một phiên bản ranh giới trong chi tiết xung đột.
type ConflictVersion struct {
	Role     string // syncport.VersionRole*
	Plot     store.Plot
	Boundary store.Boundary
	Current  bool
}

// GetConflict đọc một xung đột ranh giới kèm: phiên bản gây xung đột, phiên bản
// hiện hành của thửa (nếu khác) và phiên bản hiện hành của các thửa bị chồng lấn.
func (s *Service) GetConflict(ctx context.Context, id uuid.UUID) (ConflictDetail, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return ConflictDetail{}, err
	}
	return s.conflictDetail(ctx, a, id)
}

func (s *Service) conflictDetail(ctx context.Context, a access, id uuid.UUID) (ConflictDetail, error) {
	var d ConflictDetail
	err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		c, err := store.ConflictByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if c == nil || !a.sc.Allows(c.CommuneCode) || (a.coopOnly && c.OwnerRef != uuid.Nil && c.OwnerRef != a.p.OrgRef) {
			return ErrConflictNotFound
		}
		d.Conflict = *c
		rec, err := store.BoundaryWithGeom(ctx, tx, c.BoundaryID)
		if err != nil {
			return err
		}
		plot, err := store.PlotByRef(ctx, tx, c.PlotRef, false)
		if err != nil {
			return err
		}
		if rec != nil && plot != nil {
			d.Versions = append(d.Versions, ConflictVersion{Role: syncport.VersionRoleRecord, Plot: *plot, Boundary: *rec,
				Current: plot.CurrentBoundaryID == rec.ID})
		}
		if plot != nil && (rec == nil || plot.CurrentBoundaryID != rec.ID) {
			cur, err := store.BoundaryWithGeom(ctx, tx, plot.CurrentBoundaryID)
			if err != nil {
				return err
			}
			if cur != nil {
				d.Versions = append(d.Versions, ConflictVersion{Role: syncport.VersionRoleCurrent, Plot: *plot, Boundary: *cur, Current: true})
			}
		}
		for _, o := range c.OtherPlotRefs {
			op, err := store.PlotByRef(ctx, tx, o, false)
			if err != nil {
				return err
			}
			if op == nil {
				continue
			}
			ob, err := store.BoundaryWithGeom(ctx, tx, op.CurrentBoundaryID)
			if err != nil {
				return err
			}
			if ob != nil {
				d.Versions = append(d.Versions, ConflictVersion{Role: syncport.VersionRoleOverlapping, Plot: *op, Boundary: *ob, Current: true})
			}
		}
		return nil
	})
	return d, err
}

// VersionData là dữ liệu (không PII) của một phiên bản trong chi tiết xung đột đồng bộ.
type VersionData struct {
	PlotRef        string          `json:"plot_ref"`
	PlotCode       string          `json:"plot_code"`
	BoundaryID     string          `json:"boundary_id"`
	Version        int64           `json:"version"`
	AreaHa         string          `json:"area_ha"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef *string         `json:"cooperative_ref"`
	Source         string          `json:"source"`
	Geometry       json.RawMessage `json:"geometry"`
}

// ConflictVersions thực hiện syncport.ConflictViewer cho entity plot_boundary:
// người xử lý xung đột đồng bộ xem cả hai phiên bản (trong phạm vi của mình).
func (snk SyncSink) ConflictVersions(ctx context.Context, scope authz.Scope, id uuid.UUID) ([]syncport.ConflictVersion, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	d, err := snk.Svc.conflictDetail(ctx, access{p: p, sc: scope}, id)
	if errors.Is(err, ErrConflictNotFound) {
		return nil, syncport.ErrConflictUnknown
	}
	if err != nil {
		return nil, err
	}
	out := make([]syncport.ConflictVersion, 0, len(d.Versions))
	for _, v := range d.Versions {
		data, err := json.Marshal(VersionData{
			PlotRef: v.Plot.Ref.String(), PlotCode: v.Plot.Code, BoundaryID: v.Boundary.ID.String(), Version: v.Boundary.Version,
			AreaHa: v.Boundary.AreaHa.StringFixed(4), CommuneCode: v.Boundary.CommuneCode, CooperativeRef: refPtr(v.Plot.CooperativeRef),
			Source: v.Boundary.Source, Geometry: json.RawMessage(v.Boundary.GeoJSON),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, syncport.ConflictVersion{RecordID: v.Boundary.ID, Role: v.Role, Label: v.Plot.Code, Current: v.Current,
			EventTime: v.Boundary.EventTime, Data: data})
	}
	return out, nil
}

var _ syncport.ConflictViewer = SyncSink{}
