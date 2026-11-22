// Package app: hồ sơ KN riêng — năm sản xuất theo loại máy của HTX (TT-10) và cây trồng của thửa (TT-11), gói
// BF-SRV-KN-ATTRS (#119). Khai báo loại × số lượng máy (pf/mechanization) và hình thửa (pf/cooperative) giữ nguyên ở
// module nền; KN chỉ đọc HTX/thửa qua publication (pf_party_pub.v1_org_tree, pf_cooperative_pub.v1_plots) TRƯỚC giao
// dịch ghi và ghi sổ của chính mình (pf_extension.kn_machine_year, kn_plot_crop). Quyền theo ngữ cảnh phiên KN
// (port.RequireSession): Cấp Xã và Admin ghi trong xã phụ trách; Cấp Tỉnh/Quốc gia chỉ xem; phòng ban tách hồ sơ.
package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors.md, mục pf/extension — hồ sơ KN riêng BF-SRV-KN-ATTRS).
var (
	ErrCoopNotFound    = problem.New(http.StatusNotFound, "extension.kn_attrs_cooperative_not_found", "Không tìm thấy HTX trong phạm vi phụ trách")
	ErrPlotNotFound    = problem.New(http.StatusNotFound, "extension.kn_attrs_plot_not_found", "Không tìm thấy thửa trong phạm vi phụ trách")
	ErrPlotRetired     = problem.New(http.StatusConflict, "extension.kn_attrs_plot_retired", "Thửa đã ngừng dùng — không sửa được cây trồng")
	ErrMachineType     = problem.New(http.StatusUnprocessableEntity, "extension.kn_attrs_machine_type_unknown", "Loại máy không có trong danh mục Loại máy móc (hoặc đã ngừng dùng)")
	ErrYearMade        = problem.New(http.StatusUnprocessableEntity, "extension.kn_attrs_year_invalid", "Năm sản xuất phải là số nguyên từ 1900 đến năm hiện tại")
	ErrCrop            = problem.New(http.StatusUnprocessableEntity, "extension.kn_attrs_crop_unknown", "Cây trồng không có trong danh mục Cây trồng (hoặc đã ngừng dùng)")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Cấp Quốc gia chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrOutOfScope      = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errWriterForbidden = problem.Forbidden.WithDetail("chỉ Cấp Xã và Admin được cập nhật hồ sơ máy và thửa")
)

// DefaultDepartment là phòng ban khi Admin không chọn phòng ban (như thửa KN và sổ vật tư).
const DefaultDepartment = "trong_trot"

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// Service là use case hồ sơ KN riêng.
type Service struct {
	DB       tenant.Beginner // pool vai r_extension (db.ForModule)
	Clock    clock.Clock
	Catalogs mdport.Catalogs    // danh mục machine_type, crop
	Orgs     partyport.Orgs     // HTX qua pf_party_pub.v1_org_tree
	Plots    port.PlotDirectory // thửa qua pf_cooperative_pub.v1_plots
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k port.SessionContext
}

func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	return caller{p: p, k: k}, err
}

func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	switch {
	case c.k.ReadOnly():
		return c, ErrReadOnly
	case c.k.Role != port.SessionRoleCommune && c.k.Role != port.SessionRoleAdmin:
		return c, errWriterForbidden
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return c, ErrSupportReason
	}
	return c, nil
}

// dept là phòng ban của hồ sơ: phòng ban của phiên; Admin chọn bằng tham số (mặc định trong_trot). Phiên không có
// phòng ban (không phải Admin) → không thấy, không ghi gì (mặc định chặn).
func (c caller) dept(param string) (string, error) {
	if c.k.Role != port.SessionRoleAdmin {
		if c.k.Department == "" {
			return "", problem.Forbidden.WithDetail("ngữ cảnh phiên chưa có phòng ban")
		}
		return c.k.Department, nil
	}
	param = strings.TrimSpace(param)
	if param == "" {
		return DefaultDepartment, nil
	}
	if !deptRe.MatchString(param) {
		return "", problem.Validation.WithField("department", "mã phòng ban không hợp lệ")
	}
	return param, nil
}

func (c caller) support() (*uuid.UUID, *string) {
	if c.k.Support == nil {
		return nil, nil
	}
	r := strings.TrimSpace(c.k.Support.Reason)
	if c.k.Support.OnBehalfOf == uuid.Nil {
		return nil, &r
	}
	v := c.k.Support.OnBehalfOf
	return &v, &r
}

func (s *Service) meta(c caller, now time.Time) store.Meta {
	onBehalf, reason := c.support()
	return store.Meta{ID: ids.New(), EventTime: now, ActorRef: c.p.ActorRef, OnBehalfOf: onBehalf, SupportReason: reason,
		IdemKey: "kn-attrs:" + ids.New().String()}
}

// catalogItem tra một mục danh mục đang dùng; thiếu/ngừng dùng → lỗi bad (theo trường).
func (s *Service) catalogItem(ctx context.Context, catalog, code, field string, bad *problem.Problem) (mdport.CatalogItem, error) {
	if s.Catalogs == nil {
		return mdport.CatalogItem{}, ErrDependency.WithDetail("chưa nối danh mục dùng chung")
	}
	if !domain.ValidCode(code) {
		return mdport.CatalogItem{}, bad.WithField(field, "mã không hợp lệ")
	}
	it, err := s.Catalogs.Get(ctx, catalog, code)
	if errors.Is(err, mdport.ErrCatalogItemNotFound) || (err == nil && !it.Active) {
		return it, bad.WithField(field, "không có hoặc đã ngừng dùng trong danh mục")
	}
	if err != nil {
		return it, ErrDependency.WithDetail("không tra được danh mục").WithCause(err)
	}
	return it, nil
}

// ---------------------------------------------------------------------------
// TT-10: năm sản xuất theo loại máy của HTX
// ---------------------------------------------------------------------------

// Cooperative là HTX đã kiểm phạm vi.
type Cooperative struct {
	Ref         uuid.UUID
	CommuneCode string
	Active      bool
}

// cooperative đọc HTX qua publication (trước giao dịch) và kiểm xã thuộc phạm vi phiên; ngoài phạm vi → 404.
func (s *Service) cooperative(ctx context.Context, c caller, ref uuid.UUID) (Cooperative, error) {
	if s.Orgs == nil {
		return Cooperative{}, ErrDependency.WithDetail("chưa nối danh mục tổ chức")
	}
	o, err := s.Orgs.Get(ctx, ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return Cooperative{}, ErrCoopNotFound
	}
	if err != nil {
		return Cooperative{}, ErrDependency.WithDetail("không tra được HTX").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative || o.AdminUnitCode == "" || !c.k.Scope.Allows(o.AdminUnitCode) {
		return Cooperative{}, ErrCoopNotFound
	}
	return Cooperative{Ref: o.Ref, CommuneCode: o.AdminUnitCode, Active: o.Active}, nil
}

// MachineProfile là hồ sơ năm sản xuất máy của một HTX trong một phòng ban.
type MachineProfile struct {
	CooperativeRef uuid.UUID
	CommuneCode    string
	Department     string
	Items          []store.MachineYear
}

// GetMachines đọc hồ sơ năm máy hiện hành (mọi vai KN trong phạm vi).
func (s *Service) GetMachines(ctx context.Context, coopRef uuid.UUID, deptParam string) (MachineProfile, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return MachineProfile{}, err
	}
	dept, err := c.dept(deptParam)
	if err != nil {
		return MachineProfile{}, err
	}
	coop, err := s.cooperative(ctx, c, coopRef)
	if err != nil {
		return MachineProfile{}, err
	}
	out := MachineProfile{CooperativeRef: coop.Ref, CommuneCode: coop.CommuneCode, Department: dept}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Items, e = store.CurrentMachines(ctx, tx, dept, coop.Ref)
		return e
	})
	return out, err
}

// MachineYearInput là một dòng gửi lên: năm sản xuất là số JSON dạng chuỗi (nil = thiếu).
type MachineYearInput struct {
	MachineType string
	YearMade    *string
}

// PutMachines thay toàn bộ hồ sơ năm máy của HTX: dòng mới/đổi năm → set, loại vắng → removed; không đổi → không ghi.
// Kiểm hết (rẻ trước, danh mục sau) rồi mới mở giao dịch; lỗi bất kỳ → không ghi dòng nào.
func (s *Service) PutMachines(ctx context.Context, coopRef uuid.UUID, deptParam string, lines []MachineYearInput) (MachineProfile, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return MachineProfile{}, err
	}
	dept, err := c.dept(deptParam)
	if err != nil {
		return MachineProfile{}, err
	}
	if len(lines) > domain.MaxMachineLines {
		return MachineProfile{}, problem.Validation.WithField("lines", "tối đa 200 loại máy")
	}
	now := s.now()
	verr, yerr := problem.Validation, ErrYearMade
	badV, badY := false, false
	seen := map[string]bool{}
	next := make([]domain.MachineLine, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l.MachineType)
		if t == "" {
			verr, badV = verr.WithField("lines.machine_type", "bắt buộc"), true
			continue
		}
		if seen[t] {
			verr, badV = verr.WithField("lines."+t+".machine_type", "loại máy trùng trong hồ sơ"), true
			continue
		}
		seen[t] = true
		raw := ""
		if l.YearMade != nil {
			raw = *l.YearMade
		}
		y, e := domain.ParseYear(raw, now)
		if errors.Is(e, domain.ErrYearRequired) {
			verr, badV = verr.WithField("lines."+t+".year_made", "bắt buộc"), true
			continue
		}
		if e != nil {
			yerr, badY = yerr.WithField("lines."+t+".year_made", e.Error()), true
			continue
		}
		next = append(next, domain.MachineLine{MachineType: t, YearMade: y})
	}
	if badV {
		return MachineProfile{}, verr
	}
	if badY {
		return MachineProfile{}, yerr
	}
	coop, err := s.cooperative(ctx, c, coopRef)
	if err != nil {
		return MachineProfile{}, err
	}
	if !coop.Active {
		return MachineProfile{}, problem.Validation.WithField("cooperative_ref", "HTX đã ngừng hoạt động")
	}
	for i := range next {
		it, err := s.catalogItem(ctx, mdport.CatalogMachineType, next[i].MachineType, "lines."+next[i].MachineType+".machine_type", ErrMachineType)
		if err != nil {
			return MachineProfile{}, err
		}
		next[i].TypeName = it.Name
	}
	out := MachineProfile{CooperativeRef: coop.Ref, CommuneCode: coop.CommuneCode, Department: dept}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockMachines(ctx, tx, dept, coop.Ref); err != nil {
			return err
		}
		cur, err := store.CurrentMachines(ctx, tx, dept, coop.Ref)
		if err != nil {
			return err
		}
		curMap := make(map[string]domain.MachineLine, len(cur))
		for _, m := range cur {
			curMap[m.MachineType] = domain.MachineLine{MachineType: m.MachineType, TypeName: m.TypeName, YearMade: *m.YearMade}
		}
		for _, ch := range domain.PlanMachines(curMap, next) {
			row := store.MachineYear{Meta: s.meta(c, now), CooperativeRef: coop.Ref, CommuneCode: coop.CommuneCode, Department: dept,
				MachineType: ch.MachineType, TypeName: ch.TypeName, Event: ch.Event, YearMade: ch.YearMade}
			if err := store.InsertMachine(ctx, tx, c.p.TenantID, &row); err != nil {
				return err
			}
		}
		out.Items, err = store.CurrentMachines(ctx, tx, dept, coop.Ref)
		return err
	})
	if err != nil {
		return MachineProfile{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// TT-11: cây trồng của thửa
// ---------------------------------------------------------------------------

// plot đọc thửa qua publication (trước giao dịch) và kiểm xã thuộc phạm vi phiên; ngoài phạm vi → 404.
func (s *Service) plot(ctx context.Context, c caller, ref uuid.UUID) (port.PlotLocation, error) {
	if s.Plots == nil {
		return port.PlotLocation{}, ErrDependency.WithDetail("chưa nối nguồn thửa")
	}
	p, ok, err := s.Plots.PlotLocation(ctx, ref)
	if err != nil {
		return port.PlotLocation{}, ErrDependency.WithDetail("không tra được thửa").WithCause(err)
	}
	if !ok || !c.k.Scope.Allows(p.CommuneCode) {
		return port.PlotLocation{}, ErrPlotNotFound
	}
	return p, nil
}

// PlotProfile là cây trồng hiện hành của thửa trong một phòng ban (Crop nil = chưa khai báo).
type PlotProfile struct {
	PlotRef     uuid.UUID
	CommuneCode string
	Department  string
	Status      string
	Crop        *store.PlotCrop
}

// GetPlot đọc cây trồng hiện hành của thửa (mọi vai KN trong phạm vi).
func (s *Service) GetPlot(ctx context.Context, plotRef uuid.UUID, deptParam string) (PlotProfile, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return PlotProfile{}, err
	}
	dept, err := c.dept(deptParam)
	if err != nil {
		return PlotProfile{}, err
	}
	p, err := s.plot(ctx, c, plotRef)
	if err != nil {
		return PlotProfile{}, err
	}
	out := PlotProfile{PlotRef: p.PlotRef, CommuneCode: p.CommuneCode, Department: dept, Status: p.Status}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Crop, e = store.CurrentCrop(ctx, tx, dept, p.PlotRef)
		return e
	})
	return out, err
}

// PutPlot đặt cây trồng (bắt buộc, danh mục crop đang dùng) cho thửa; cùng cây trồng → không ghi sổ.
func (s *Service) PutPlot(ctx context.Context, plotRef uuid.UUID, deptParam, cropCode string) (PlotProfile, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return PlotProfile{}, err
	}
	dept, err := c.dept(deptParam)
	if err != nil {
		return PlotProfile{}, err
	}
	cropCode = strings.TrimSpace(cropCode)
	if cropCode == "" {
		return PlotProfile{}, problem.Validation.WithField("crop_code", "bắt buộc")
	}
	p, err := s.plot(ctx, c, plotRef)
	if err != nil {
		return PlotProfile{}, err
	}
	if p.Status == port.PlotRetired {
		return PlotProfile{}, ErrPlotRetired
	}
	it, err := s.catalogItem(ctx, mdport.CatalogCrop, cropCode, "crop_code", ErrCrop)
	if err != nil {
		return PlotProfile{}, err
	}
	now := s.now()
	out := PlotProfile{PlotRef: p.PlotRef, CommuneCode: p.CommuneCode, Department: dept, Status: p.Status}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockCrop(ctx, tx, dept, p.PlotRef); err != nil {
			return err
		}
		cur, err := store.CurrentCrop(ctx, tx, dept, p.PlotRef)
		if err != nil {
			return err
		}
		if cur != nil && cur.CropCode == it.Code {
			out.Crop = cur
			return nil
		}
		row := store.PlotCrop{Meta: s.meta(c, now), PlotRef: p.PlotRef, CommuneCode: p.CommuneCode, Department: dept, CropCode: it.Code, CropName: it.Name}
		if err := store.InsertCrop(ctx, tx, c.p.TenantID, &row); err != nil {
			return err
		}
		out.Crop = &row
		return nil
	})
	if err != nil {
		return PlotProfile{}, err
	}
	return out, nil
}

// CropQuery là bộ lọc danh sách cây trồng hiện hành.
type CropQuery struct {
	Commune    string
	Department string // Admin (tùy chọn, mặc định trong_trot)
	After      uuid.UUID
	Limit      int
}

// CropPage là một trang cây trồng hiện hành.
type CropPage struct {
	Items []store.PlotCrop
	Next  *uuid.UUID
}

// ListCrops trả cây trồng hiện hành của các thửa trong phạm vi + phòng ban (cột "Cây trồng" của danh sách thửa).
func (s *Service) ListCrops(ctx context.Context, q CropQuery) (CropPage, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return CropPage{}, err
	}
	dept, err := c.dept(q.Department)
	if err != nil {
		return CropPage{}, err
	}
	commune := strings.TrimSpace(q.Commune)
	if commune != "" && !c.k.Scope.Allows(commune) {
		return CropPage{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	if c.k.Scope.Empty() {
		return CropPage{Items: []store.PlotCrop{}}, nil
	}
	var rows []store.PlotCrop
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ListCrops(ctx, tx, store.CropFilter{All: c.k.Scope.All, Units: c.k.Scope.Units, Commune: commune, Dept: dept,
			After: q.After, Limit: q.Limit + 1})
		return e
	})
	if err != nil {
		return CropPage{}, err
	}
	pg := CropPage{Items: rows}
	if len(rows) > q.Limit {
		pg.Items = rows[:q.Limit]
		n := pg.Items[q.Limit-1].PlotRef
		pg.Next = &n
	}
	return pg, nil
}
