package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// port.PlotStats (G-02, G-03)
// ---------------------------------------------------------------------------

var _ partyport.PlotStats = (*Service)(nil)

// DigitizedByCommune thực hiện port.PlotStats.
func (s *Service) DigitizedByCommune(ctx context.Context, communes []string) ([]partyport.PlotStat, error) {
	var rows []store.StatRow
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		rows, e = store.StatsByCommune(ctx, tx, communes == nil, communes)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]partyport.PlotStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, partyport.PlotStat{CommuneCode: r.Key, Plots: r.Plots, AreaHa: r.AreaHa})
	}
	return out, nil
}

// DigitizedByCooperative thực hiện port.PlotStats.
func (s *Service) DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]partyport.PlotStat, error) {
	out := map[uuid.UUID]partyport.PlotStat{}
	if len(coops) == 0 {
		return out, nil
	}
	var rows []store.StatRow
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
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
		out[ref] = partyport.PlotStat{CooperativeRef: ref, Plots: r.Plots, AreaHa: r.AreaHa}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// G-05: ranh giới hành chính theo cấp hoặc đơn vị cha
// ---------------------------------------------------------------------------

// AdminBoundaryItem là hình hiện hành của một đơn vị kèm mã đơn vị cha.
type AdminBoundaryItem struct {
	store.AdminBoundary
	ParentCode string
}

// AdminBoundaryList là kết quả liệt kê: hình có được và mã đơn vị chưa có hình.
type AdminBoundaryList struct {
	Items   []AdminBoundaryItem
	Missing []string
}

// MaxAdminBoundaries giới hạn số đơn vị một lần đọc (tỉnh ĐBSCL ≈ 100 xã).
const MaxAdminBoundaries = 500

// ListAdminBoundaries trả ranh giới hiện hành của mọi đơn vị cấp level thuộc
// parent (rỗng: mọi tỉnh; với cấp xã bắt buộc có parent). Danh sách đơn vị lấy
// từ pf/masterdata (hiệu lực hiện tại); đơn vị chưa nhập hình nằm ở Missing.
// Ranh giới hành chính là dữ liệu tham chiếu chung — không lọc theo phạm vi.
func (s *Service) ListAdminBoundaries(ctx context.Context, level, parent string, zoom int) (AdminBoundaryList, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return AdminBoundaryList{}, err
	}
	switch level {
	case string(partyport.LevelProvince), string(partyport.LevelCommune):
	default:
		return AdminBoundaryList{}, problem.Validation.WithField("level", "phải là province hoặc commune")
	}
	if level == string(partyport.LevelCommune) && parent == "" {
		return AdminBoundaryList{}, problem.Validation.WithField("parent", "bắt buộc với level=commune (mã tỉnh)")
	}
	units, err := s.unitsOf(ctx, level, parent)
	if err != nil {
		return AdminBoundaryList{}, err
	}
	if len(units) > MaxAdminBoundaries {
		return AdminBoundaryList{}, problem.Validation.WithField("parent", "quá nhiều đơn vị, thu hẹp theo tỉnh")
	}
	codes := make([]string, 0, len(units))
	for _, u := range units {
		codes = append(codes, u.Code)
	}
	var bs []store.AdminBoundary
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		bs, e = store.AdminBoundaries(ctx, tx, codes, SimplifyTolerance(zoom))
		return e
	})
	if err != nil {
		return AdminBoundaryList{}, err
	}
	have := map[string]store.AdminBoundary{}
	for _, b := range bs {
		have[b.UnitCode] = b
	}
	out := AdminBoundaryList{Items: []AdminBoundaryItem{}, Missing: []string{}}
	for _, u := range units {
		b, ok := have[u.Code]
		if !ok {
			out.Missing = append(out.Missing, u.Code)
			continue
		}
		if u.Name != "" {
			b.Name = u.Name // tên hiện hành theo danh mục đơn vị hành chính
		}
		out.Items = append(out.Items, AdminBoundaryItem{AdminBoundary: b, ParentCode: u.ParentCode})
	}
	return out, nil
}

// unitsOf trả đơn vị cấp level thuộc parent, hiệu lực hiện tại, sắp theo mã.
func (s *Service) unitsOf(ctx context.Context, level, parent string) ([]partyport.AdminUnit, error) {
	if s.Units == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	now := s.now()
	if parent != "" {
		pu, err := s.Units.Get(ctx, parent, now)
		if errors.Is(err, partyport.ErrUnitNotFound) {
			return nil, problem.Validation.WithField("parent", "không có đơn vị hành chính còn hiệu lực với mã này")
		}
		if err != nil {
			return nil, ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
		}
		if level == string(partyport.LevelProvince) {
			if pu.Level != partyport.LevelProvince {
				return nil, problem.Validation.WithField("parent", "với level=province, parent phải là mã tỉnh")
			}
			return []partyport.AdminUnit{pu}, nil
		}
	}
	var out []partyport.AdminUnit
	if tree, ok := s.Units.(partyport.AdminUnitTree); ok {
		us, err := tree.Children(ctx, parent, now)
		if err != nil {
			return nil, ErrDependency.WithDetail("không liệt kê được đơn vị hành chính").WithCause(err)
		}
		for _, u := range us {
			if string(u.Level) == level {
				out = append(out, u)
			}
		}
	} else {
		if parent == "" {
			return nil, ErrDependency.WithDetail("danh mục đơn vị hành chính không hỗ trợ liệt kê")
		}
		codes, err := s.Units.Descendants(ctx, parent, now)
		if err != nil {
			return nil, ErrDependency.WithDetail("không liệt kê được đơn vị hành chính").WithCause(err)
		}
		for _, c := range codes {
			u, err := s.Units.Get(ctx, c, now)
			if err == nil && string(u.Level) == level && u.ParentCode == parent {
				out = append(out, u)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
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
