package app

// KN v7.7 FN-XA-03.05 "Vẽ ranh giới thửa ruộng" (gói P7): route /v1/kn/plots… theo ngữ cảnh phiên KN (authz.RequireKN —
// người dùng KN chỉ có vai realm kn_user). Cấp Xã (kn_xa) và Admin ghi trong các xã phụ trách (k.Scope); Cấp Tỉnh, Cấp
// Quốc gia chỉ xem. Khác API thửa G1: trực tuyến CHẶN MỌI chồng lấn vượt ngưỡng geo.overlap_tolerance_pct (409
// geo.plot_overlap kèm phần chồng lấn) — không có "xác nhận giữ nguyên". Tên nông dân vào kho định danh (farmer_ref),
// API chỉ trả nhãn đã che (mục đích extension.contact).

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Mã lỗi KN v7.7 của pf/geo (docs/errors.md).
var (
	ErrPlotOverlap   = problem.New(http.StatusConflict, "geo.plot_overlap", "Ranh giới bị chồng lấn với lô đã đăng ký — không lưu được, vui lòng vẽ lại.")
	ErrReadOnly      = problem.New(http.StatusForbidden, "geo.read_only", "Cấp Quốc gia chỉ xem")
	ErrSupportReason = problem.New(http.StatusUnprocessableEntity, "geo.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
)

// DefaultKNDepartment: phòng ban mặc định khi Admin ghi không chọn phòng ban (tạm, chờ khách).
const DefaultKNDepartment = "trong_trot"

// MaxKNExport là số thửa tối đa của một tệp xuất (tạm: một xã; > 2 s cần job — chờ số liệu thực tế).
const MaxKNExport = 5000

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// KNInput là thuộc tính v7.7 + chế độ hỗ trợ của một lần ghi.
type KNInput struct {
	Attrs         store.KNAttrs
	OnBehalf      *uuid.UUID
	SupportReason *string
}

func (k *KNInput) boundary(b *store.Boundary, declared *decimal.Decimal) {
	if k == nil {
		return
	}
	b.Shape, b.DeclaredAreaHa, b.OnBehalfOf, b.SupportReason = k.Attrs.Shape, declared, k.OnBehalf, k.SupportReason
}

func (k *KNInput) event(e *store.PlotEvent) {
	if k == nil {
		return
	}
	e.OnBehalfOf, e.SupportReason = k.OnBehalf, k.SupportReason
}

// gate áp quy tắc chồng lấn khi ghi trực tuyến: G1 (onlineGate) hoặc KN v7.7 (chặn mọi chồng lấn vượt ngưỡng).
func gate(ctx context.Context, tx pgx.Tx, m mode, kn *KNInput, c checked, maxDev decimal.Decimal, confirm bool) error {
	if m != modeOnline {
		return nil
	}
	if kn == nil {
		return onlineGate(c.same, c.cross, c.devPct, maxDev, confirm)
	}
	all := append(append([]domain.Overlap{}, c.same...), c.cross...)
	if len(all) == 0 {
		return nil
	}
	its, err := store.Intersections(ctx, tx, c.geojson, refsOf(all))
	if err != nil {
		return err
	}
	geom := map[uuid.UUID]store.Intersection{}
	for _, it := range its {
		geom[it.PlotRef] = it
	}
	list := make([]map[string]any, 0, len(all))
	codes := make([]string, 0, len(all))
	for _, o := range all {
		it := geom[o.PlotRef]
		m := map[string]any{"plot_ref": o.PlotRef.String(), "code": o.Code, "area_ha": o.IntersectHa.StringFixed(4),
			"overlap_pct": o.Pct.StringFixed(4), "geometry": nil}
		if it.GeoJSON != "" {
			m["geometry"] = json.RawMessage(it.GeoJSON)
		}
		list = append(list, m)
		codes = append(codes, o.Code)
	}
	return ErrPlotOverlap.WithDetail("Ranh giới bị chồng lấn với lô đã đăng ký ("+strings.Join(codes, ", ")+") — vui lòng vẽ lại.").
		With("overlaps", list)
}

// knCaller là người gọi route KN: quyền thửa (phạm vi = k.Scope) + ngữ cảnh phiên.
type knCaller struct {
	a access
	k authz.KNContext
}

// kn kiểm ngữ cảnh phiên; write: Cấp Quốc gia → geo.read_only, vai khác kn_xa/admin → forbidden, hỗ trợ thiếu lý do → 422.
func (s *Service) kn(ctx context.Context, write bool) (knCaller, error) {
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return knCaller{}, err
	}
	if write {
		if err := knWriter(k); err != nil {
			return knCaller{}, err
		}
	}
	return knCaller{a: access{p: p, sc: k.Scope}, k: k}, nil
}

func knWriter(k authz.KNContext) error {
	if k.ReadOnly() {
		return ErrReadOnly
	}
	if k.Role != authz.KNXa && k.Role != authz.KNAdmin {
		return problem.Forbidden.WithDetail("chỉ Cấp Xã và Admin được vẽ, sửa thửa")
	}
	if k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(k.Support.Reason)) < 10 {
		return ErrSupportReason
	}
	return nil
}

// readDept là phòng ban lọc khi đọc: Admin mọi phòng ban (rỗng).
func (c knCaller) readDept() string {
	if c.k.Role == authz.KNAdmin {
		return ""
	}
	return c.k.Department
}

// sees: xã trong phạm vi và (thửa không mang phòng ban hoặc phòng ban thuộc ngữ cảnh).
func (c knCaller) sees(p store.Plot) bool {
	return c.a.sees(p.CommuneCode, p.CooperativeRef) && (p.Department == "" || c.k.AllowsDepartment(p.Department))
}

// writeDept là phòng ban ghi vào thửa mới: ngữ cảnh; Admin: tham số hoặc mặc định.
func (c knCaller) writeDept(param string) (string, error) {
	if c.k.Role != authz.KNAdmin {
		if c.k.Department == "" {
			return DefaultKNDepartment, nil
		}
		return c.k.Department, nil
	}
	param = strings.TrimSpace(param)
	if param == "" {
		return DefaultKNDepartment, nil
	}
	if !deptRe.MatchString(param) {
		return "", problem.Validation.WithField("department", "mã phòng ban không hợp lệ")
	}
	return param, nil
}

func (c knCaller) input(attrs store.KNAttrs) *KNInput {
	in := &KNInput{Attrs: attrs}
	if c.k.Support != nil {
		if c.k.Support.OnBehalfOf != uuid.Nil {
			v := c.k.Support.OnBehalfOf
			in.OnBehalf = &v
		}
		r := strings.TrimSpace(c.k.Support.Reason)
		in.SupportReason = &r
	}
	return in
}

// farmerRef ghi tên nông dân vào kho định danh (chủ thể "người") và trả mã tham chiếu; tên rỗng → Nil.
func (s *Service) farmerRef(ctx context.Context, name, commune string) (uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return uuid.Nil, nil
	}
	if utf8.RuneCountInString(name) > domain.MaxFarmerName {
		return uuid.Nil, problem.Validation.WithField("farmer_name", "tối đa 200 ký tự")
	}
	if domain.ContainsPII(name) {
		return uuid.Nil, problem.Validation.WithField("farmer_name", "chỉ nhập họ tên, không ghi SĐT/CCCD")
	}
	if s.Vault == nil {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	ref, err := s.Vault.RegisterFarmer(ctx, name, commune)
	if err != nil {
		var pr *problem.Problem
		if errors.As(err, &pr) && pr.Status == http.StatusUnprocessableEntity {
			return uuid.Nil, problem.Validation.WithField("farmer_name", "không hợp lệ")
		}
		return uuid.Nil, ErrDependency.WithDetail("không ghi được tên nông dân vào kho định danh").WithCause(err)
	}
	return ref, nil
}

// FarmerLabels tra nhãn đã che của nông dân (mục đích extension.contact); lỗi tra nhãn không chặn màn hình.
func (s *Service) FarmerLabels(ctx context.Context, plots ...store.Plot) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Vault == nil {
		return out
	}
	for _, p := range plots {
		if p.FarmerRef == uuid.Nil {
			continue
		}
		if _, ok := out[p.FarmerRef]; ok {
			continue
		}
		label, err := s.Vault.FarmerLabel(ctx, p.FarmerRef)
		if err == nil || label != "" {
			out[p.FarmerRef] = label
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// KNPlotQuery là bộ lọc danh sách thửa KN.
type KNPlotQuery struct {
	Commune        string
	CooperativeRef uuid.UUID
	After          uuid.UUID
	Limit          int
}

// KNPlotPage là một trang thửa KN (không hình học) kèm nhãn nông dân.
type KNPlotPage struct {
	Items  []store.Plot
	Labels map[uuid.UUID]string
	Next   *uuid.UUID
}

// KNListPlots trả thửa trong phạm vi ngữ cảnh (mặc định mọi xã phụ trách).
func (s *Service) KNListPlots(ctx context.Context, q KNPlotQuery) (KNPlotPage, error) {
	c, err := s.kn(ctx, false)
	if err != nil {
		return KNPlotPage{}, err
	}
	q.Commune = strings.TrimSpace(q.Commune)
	if q.Commune != "" && !c.a.sc.Allows(q.Commune) {
		return KNPlotPage{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	if c.a.sc.Empty() {
		return KNPlotPage{Items: []store.Plot{}, Labels: map[uuid.UUID]string{}}, nil
	}
	var rows []store.Plot
	err = s.inTx(ctx, c.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ListKN(ctx, tx, store.KNFilter{All: c.a.sc.All, Units: c.a.sc.Units, Commune: q.Commune, Coop: q.CooperativeRef,
			Dept: c.readDept(), After: q.After, Limit: q.Limit + 1})
		return e
	})
	if err != nil {
		return KNPlotPage{}, err
	}
	pg := KNPlotPage{Items: rows}
	if len(rows) > q.Limit {
		pg.Items = rows[:q.Limit]
		n := pg.Items[q.Limit-1].Ref
		pg.Next = &n
	}
	if pg.Items == nil {
		pg.Items = []store.Plot{}
	}
	pg.Labels = s.FarmerLabels(ctx, pg.Items...)
	return pg, nil
}

func (s *Service) knPlot(ctx context.Context, c knCaller, ref uuid.UUID) (store.Plot, error) {
	var p *store.Plot
	err := s.inTx(ctx, c.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		p, e = store.PlotByRef(ctx, tx, ref, false)
		return e
	})
	if err != nil {
		return store.Plot{}, err
	}
	if p == nil || !c.sees(*p) {
		return store.Plot{}, ErrPlotNotFound
	}
	return *p, nil
}

// KNGetPlot đọc một thửa (kèm hình) trong phạm vi; ngoài phạm vi → 404.
func (s *Service) KNGetPlot(ctx context.Context, ref uuid.UUID) (store.Plot, map[uuid.UUID]string, error) {
	c, err := s.kn(ctx, false)
	if err != nil {
		return store.Plot{}, nil, err
	}
	p, err := s.knPlot(ctx, c, ref)
	if err != nil {
		return store.Plot{}, nil, err
	}
	return p, s.FarmerLabels(ctx, p), nil
}

// ---------------------------------------------------------------------------
// Ghi
// ---------------------------------------------------------------------------

// KNCreateInput là "Vẽ lô mới" của Cấp Xã.
type KNCreateInput struct {
	PlotRef        uuid.UUID
	CommuneCode    string
	CooperativeRef uuid.UUID
	FarmerName     string `pii:"true"`
	LotName        string
	Note           string
	DeclaredAreaHa *decimal.Decimal
	Shape          string
	Geometry       json.RawMessage
	Department     string // Admin: phòng ban ghi (tùy chọn)
	IdemKey        string
}

func knText(field, v string, max int, required bool) (string, error) {
	v = strings.TrimSpace(v)
	if required && v == "" {
		return "", problem.Validation.WithField(field, "bắt buộc")
	}
	if r := domain.TextOK(v, max); r != "" {
		return "", problem.Validation.WithField(field, r)
	}
	return v, nil
}

func knShape(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return domain.ShapePolygon, nil
	}
	if !domain.KnownShape(s) {
		return "", problem.Validation.WithField("shape", "phải là polygon, circle hoặc square")
	}
	return s, nil
}

// KNCreatePlot tạo thửa với ranh giới đầu tiên; chặn mọi chồng lấn (409 geo.plot_overlap), < 3 đỉnh/không khép → 422.
func (s *Service) KNCreatePlot(ctx context.Context, in KNCreateInput) (WriteResult, error) {
	c, err := s.kn(ctx, true)
	if err != nil {
		return WriteResult{}, err
	}
	attrs, err := s.knCreateAttrs(ctx, c, in)
	if err != nil {
		return WriteResult{}, err
	}
	return s.create(ctx, c.a, CreateInput{
		PlotRef: in.PlotRef, Name: attrs.Attrs.LotName, CommuneCode: in.CommuneCode, CooperativeRef: in.CooperativeRef,
		Boundary: BoundaryInput{Geometry: in.Geometry, DeclaredAreaHa: in.DeclaredAreaHa, Source: domain.SourceKNWeb},
		IdemKey:  in.IdemKey, KN: attrs,
	}, modeOnline, domain.SourceKNWeb)
}

// knCreateAttrs kiểm thuộc tính v7.7 (rẻ trước), kiểm phạm vi xã, rồi mới ghi tên nông dân vào kho định danh.
func (s *Service) knCreateAttrs(ctx context.Context, c knCaller, in KNCreateInput) (*KNInput, error) {
	if in.IdemKey == "" {
		return nil, problem.IdempotencyRequired
	}
	lot, err := knText("lot_name", in.LotName, domain.MaxLotName, true)
	if err != nil {
		return nil, err
	}
	note, err := knText("note", in.Note, domain.MaxNote, false)
	if err != nil {
		return nil, err
	}
	shape, err := knShape(in.Shape)
	if err != nil {
		return nil, err
	}
	dept, err := c.writeDept(in.Department)
	if err != nil {
		return nil, err
	}
	commune := strings.TrimSpace(in.CommuneCode)
	if commune == "" {
		return nil, problem.Validation.WithField("commune_code", "bắt buộc")
	}
	if !c.a.sc.Allows(commune) {
		return nil, ErrOutOfScope.WithDetail("chỉ vẽ trong địa bàn phân công")
	}
	farmer, err := s.farmerRef(ctx, in.FarmerName, commune)
	if err != nil {
		return nil, err
	}
	return c.input(store.KNAttrs{FarmerRef: farmer, LotName: lot, Note: note, DeclaredAreaHa: in.DeclaredAreaHa, Shape: shape, Department: dept}), nil
}

// KNCorrectInput là "Sửa ranh giới" (phiên bản mới thay phiên bản SupersedesID).
type KNCorrectInput struct {
	PlotRef      uuid.UUID
	SupersedesID uuid.UUID
	Geometry     json.RawMessage
	Shape        string
	Reason       string
	IdemKey      string
}

// KNCorrectBoundary ghi phiên bản ranh giới mới (bản ghi bù); chặn mọi chồng lấn; sửa trên phiên bản cũ → 409.
func (s *Service) KNCorrectBoundary(ctx context.Context, in KNCorrectInput) (WriteResult, error) {
	c, err := s.kn(ctx, true)
	if err != nil {
		return WriteResult{}, err
	}
	shape, err := knShape(in.Shape)
	if err != nil {
		return WriteResult{}, err
	}
	p, err := s.knPlot(ctx, c, in.PlotRef)
	if err != nil {
		return WriteResult{}, err
	}
	kin := c.input(p.KNAttrs)
	kin.Attrs.Shape = shape
	return s.correct(ctx, c.a, CorrectInput{
		PlotRef: in.PlotRef, SupersedesID: in.SupersedesID, Reason: in.Reason,
		Boundary: BoundaryInput{Geometry: in.Geometry, Source: domain.SourceKNWeb}, IdemKey: in.IdemKey, KN: kin,
	}, modeOnline, domain.SourceKNWeb)
}

// KNUpdateInput sửa thuộc tính thửa (sự kiện bù 'attributes_changed'). Con trỏ nil = giữ nguyên; chuỗi rỗng = xóa.
type KNUpdateInput struct {
	PlotRef        uuid.UUID
	LotName        *string
	Note           *string
	FarmerName     *string    `pii:"true"`
	CooperativeRef *uuid.UUID // &uuid.Nil = bỏ gán HTX
	DeclaredAreaHa *decimal.Decimal
	ClearDeclared  bool
	Reason         string
	IdemKey        string
}

// KNUpdatePlot ghi sự kiện bù với ảnh chụp thuộc tính sau khi sửa; không đổi gì → trả hiện trạng, không ghi sổ.
func (s *Service) KNUpdatePlot(ctx context.Context, in KNUpdateInput) (AttrResult, error) {
	c, err := s.kn(ctx, true)
	if err != nil {
		return AttrResult{}, err
	}
	if in.IdemKey == "" {
		in.IdemKey = "kn-plot-attrs:" + ids.New().String()
	}
	cur, err := s.knPlot(ctx, c, in.PlotRef)
	if err != nil {
		return AttrResult{}, err
	}
	if err := c.a.canWrite(cur.CommuneCode, cur.CooperativeRef); err != nil {
		return AttrResult{}, err
	}
	next := cur.KNAttrs
	if in.LotName != nil {
		if next.LotName, err = knText("lot_name", *in.LotName, domain.MaxLotName, true); err != nil {
			return AttrResult{}, err
		}
	}
	if in.Note != nil {
		if next.Note, err = knText("note", *in.Note, domain.MaxNote, false); err != nil {
			return AttrResult{}, err
		}
	}
	if in.DeclaredAreaHa != nil {
		if !in.DeclaredAreaHa.IsPositive() {
			return AttrResult{}, problem.Validation.WithField("declared_area_ha", "phải dương")
		}
		next.DeclaredAreaHa = in.DeclaredAreaHa
	} else if in.ClearDeclared {
		next.DeclaredAreaHa = nil
	}
	reason := strings.TrimSpace(in.Reason)
	if r := domain.TextOK(reason, 500); r != "" {
		return AttrResult{}, problem.Validation.WithField("reason", r)
	}
	coop := cur.CooperativeRef
	if in.CooperativeRef != nil {
		coop = *in.CooperativeRef
		if err := s.checkCooperative(ctx, c.a, coop); err != nil {
			return AttrResult{}, err
		}
	}
	if in.FarmerName != nil {
		if next.FarmerRef, err = s.farmerRef(ctx, *in.FarmerName, cur.CommuneCode); err != nil {
			return AttrResult{}, err
		}
	}
	kin := c.input(next)
	now := s.now()
	return s.writeAttrs(ctx, c.a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (AttrResult, error) {
		plot, err := store.PlotByRef(ctx, tx, in.PlotRef, true)
		if err != nil {
			return AttrResult{}, err
		}
		if plot == nil || !c.sees(*plot) {
			return AttrResult{}, ErrPlotNotFound
		}
		if plot.Status == port.StatusRetired {
			return AttrResult{}, ErrPlotState.WithDetail("thửa đã ngừng dùng")
		}
		p := *plot
		p.KNAttrs = kin.Attrs
		p.Name = p.LotName
		if coop != plot.CooperativeRef {
			p.CooperativeRef, p.HouseholdRef, p.Status = coop, uuid.Nil, domain.StatusFor(coop)
		}
		if sameAttrs(*plot, p) {
			return AttrResult{Plot: *plot}, nil
		}
		ev := store.PlotEvent{
			ID: ids.New(), TenantID: c.a.p.TenantID, PlotRef: p.Ref, Action: domain.ActionAttributesChanged, Code: p.Code, Name: p.Name,
			CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef, Status: p.Status,
			Reason: reason, EventTime: now, ActorRef: c.a.p.ActorRef, IdemKey: in.IdemKey, KNAttrs: p.KNAttrs,
		}
		kin.event(&ev)
		if err := store.InsertPlotEvent(ctx, tx, &ev); err != nil {
			return AttrResult{}, err
		}
		if err := store.SetKNAttrs(ctx, tx, p); err != nil {
			return AttrResult{}, err
		}
		after, err := store.PlotByRef(ctx, tx, p.Ref, false)
		if err != nil {
			return AttrResult{}, err
		}
		return AttrResult{Plot: *after, Event: ev}, nil
	})
}

func sameAttrs(a, b store.Plot) bool {
	return a.Name == b.Name && a.CooperativeRef == b.CooperativeRef && a.HouseholdRef == b.HouseholdRef && a.Status == b.Status &&
		a.FarmerRef == b.FarmerRef && a.LotName == b.LotName && a.Note == b.Note && decEq(a.DeclaredAreaHa, b.DeclaredAreaHa) &&
		a.Shape == b.Shape && a.Department == b.Department
}

func decEq(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// ---------------------------------------------------------------------------
// Log
// ---------------------------------------------------------------------------

// LogChange là một trường đổi (giá trị cũ → mới; không PII — tên nông dân hiện bằng farmer_ref).
type LogChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// LogItem là một dòng khung Log chuẩn (plan §1.5).
type LogItem struct {
	ID           uuid.UUID
	Event        string
	ActorRef     uuid.UUID
	ActorDisplay *string
	At           time.Time
	Changes      []LogChange
	Reason       *string
}

// KNLogPage là một trang Log.
type KNLogPage struct {
	Items []LogItem
	Next  *uuid.UUID
}

func snapshot(e store.PlotEvent) [][2]any {
	ref := func(u uuid.UUID) any {
		if u == uuid.Nil {
			return nil
		}
		return u.String()
	}
	str := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	var dec any
	if e.DeclaredAreaHa != nil {
		dec = e.DeclaredAreaHa.StringFixed(4)
	}
	return [][2]any{
		{"commune_code", str(e.CommuneCode)}, {"cooperative_ref", ref(e.CooperativeRef)}, {"household_ref", ref(e.HouseholdRef)},
		{"status", str(e.Status)}, {"lot_name", str(e.LotName)}, {"farmer_ref", ref(e.FarmerRef)}, {"note", str(e.Note)},
		{"declared_area_ha", dec}, {"shape", str(e.Shape)}, {"department_code", str(e.Department)},
	}
}

func diff(prev *store.PlotEvent, cur store.PlotEvent) []LogChange {
	now := snapshot(cur)
	var old [][2]any
	if prev != nil {
		old = snapshot(*prev)
	}
	out := []LogChange{}
	for i, f := range now {
		var o any
		if old != nil {
			o = old[i][1]
		}
		if o != f[1] {
			out = append(out, LogChange{Field: f[0].(string), Old: o, New: f[1]})
		}
	}
	return out
}

func strPtrOrNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// KNPlotLog trả lịch sử thửa: biến động thuộc tính (plot_event) và phiên bản ranh giới (plot_boundary), mới nhất trước.
func (s *Service) KNPlotLog(ctx context.Context, ref, after uuid.UUID, limit int) (KNLogPage, error) {
	c, err := s.kn(ctx, false)
	if err != nil {
		return KNLogPage{}, err
	}
	if _, err := s.knPlot(ctx, c, ref); err != nil {
		return KNLogPage{}, err
	}
	var evs []store.PlotEvent
	var bs []store.Boundary
	err = s.inTx(ctx, c.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if evs, e = store.PlotEvents(ctx, tx, ref); e != nil {
			return e
		}
		bs, e = store.Boundaries(ctx, tx, ref)
		return e
	})
	if err != nil {
		return KNLogPage{}, err
	}
	var items []LogItem
	for i, e := range evs {
		var prev *store.PlotEvent
		if i > 0 {
			prev = &evs[i-1]
		}
		items = append(items, LogItem{ID: e.ID, Event: e.Action, ActorRef: e.ActorRef, At: e.RecordedAt, Changes: diff(prev, e), Reason: strPtrOrNil(e.Reason)})
	}
	for i, b := range bs {
		it := LogItem{ID: b.ID, Event: "boundary_recorded", ActorRef: b.ActorRef, At: b.RecordedAt, Reason: b.CorrectionReason}
		var oldArea, oldID, oldShape any
		if i > 0 {
			p := bs[i-1]
			oldArea, oldID = p.AreaHa.StringFixed(4), p.ID.String()
			if p.Shape != "" {
				oldShape = p.Shape
			}
		}
		if b.SupersedesID != nil {
			it.Event = "boundary_corrected"
		}
		it.Changes = []LogChange{{Field: "boundary_id", Old: oldID, New: b.ID.String()}, {Field: "area_ha", Old: oldArea, New: b.AreaHa.StringFixed(4)}}
		if b.Shape != "" && oldShape != any(b.Shape) {
			it.Changes = append(it.Changes, LogChange{Field: "shape", Old: oldShape, New: b.Shape})
		}
		if !b.BecameCurrent {
			it.Changes = append(it.Changes, LogChange{Field: "became_current", Old: nil, New: false})
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].At.Equal(items[j].At) {
			return items[i].At.After(items[j].At)
		}
		return items[i].ID.String() > items[j].ID.String()
	})
	if after != uuid.Nil {
		for i, it := range items {
			if it.ID == after {
				items = items[i+1:]
				break
			}
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	pg := KNLogPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		n := pg.Items[limit-1].ID
		pg.Next = &n
	}
	if pg.Items == nil {
		pg.Items = []LogItem{}
	}
	s.actorNames(ctx, pg.Items)
	return pg, nil
}

func (s *Service) actorNames(ctx context.Context, items []LogItem) {
	if s.Names == nil || len(items) == 0 {
		return
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		refs = append(refs, it.ActorRef)
	}
	m, err := s.Names.DisplayNames(ctx, refs)
	if err != nil {
		return
	}
	for i := range items {
		if n, ok := m[items[i].ActorRef]; ok && n != "" {
			v := n
			items[i].ActorDisplay = &v
		}
	}
}

// ---------------------------------------------------------------------------
// Xuất tệp
// ---------------------------------------------------------------------------

// ExportFile là tệp xuất cho màn xã.
type ExportFile struct {
	Name        string
	ContentType string
	Body        []byte
}

// KNExport xuất thửa của một xã trong phạm vi ra GeoJSON (FeatureCollection) hoặc KML.
func (s *Service) KNExport(ctx context.Context, commune, format string) (ExportFile, error) {
	c, err := s.kn(ctx, false)
	if err != nil {
		return ExportFile{}, err
	}
	commune = strings.TrimSpace(commune)
	if commune == "" {
		return ExportFile{}, problem.Validation.WithField("commune", "bắt buộc")
	}
	if !c.a.sc.Allows(commune) {
		return ExportFile{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if format == "" {
		format = "geojson"
	}
	if format != "geojson" && format != "kml" {
		return ExportFile{}, problem.Validation.WithField("format", "phải là geojson hoặc kml")
	}
	var rows []store.ExportRow
	err = s.inTx(ctx, c.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ExportCommune(ctx, tx, commune, c.readDept(), MaxKNExport)
		return e
	})
	if err != nil {
		return ExportFile{}, err
	}
	plots := make([]store.Plot, 0, len(rows))
	for _, r := range rows {
		plots = append(plots, r.Plot)
	}
	labels := s.FarmerLabels(ctx, plots...)
	base := "thua-ruong-" + commune
	// A5 (ADR-0024): tải tệp thửa KN là xuất NỘI BỘ — qua điểm kiểm soát dùng chung (ghi egress_event, đếm, cảnh báo
	// bất thường) nhưng không bao giờ bị chặn, kể cả AG_EGRESS_MODE=enforce (Internal → observe; lỗi chỉ ghi log).
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.a.p.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.a.p.ActorRef, Product: ProductKNPlotFile,
		Records: int64(len(rows)), OccurredAt: s.now(), ActorRef: c.a.p.ActorRef, TraceID: telemetry.TraceID(ctx), Internal: true,
	}, s.Log)
	if format == "kml" {
		return ExportFile{Name: base + ".kml", ContentType: "application/vnd.google-earth.kml+xml", Body: kmlDoc(commune, rows, labels)}, nil
	}
	feats := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		feats = append(feats, map[string]any{"type": "Feature", "id": r.Ref.String(), "geometry": json.RawMessage(r.GeoJSON),
			"properties": exportProps(r.Plot, labels)})
	}
	body, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
	if err != nil {
		return ExportFile{}, err
	}
	return ExportFile{Name: base + ".geojson", ContentType: "application/geo+json", Body: body}, nil
}

// ProductKNPlotFile là tên sản phẩm của lần tải tệp thửa KN trong sổ egress_event (pf/metering).
const ProductKNPlotFile = "kn_plot_file"

func exportProps(p store.Plot, labels map[uuid.UUID]string) map[string]any {
	m := map[string]any{"ref": p.Ref.String(), "code": p.Code, "lot_name": strOrNilAny(p.LotName), "commune_code": p.CommuneCode,
		"cooperative_ref": refPtr(p.CooperativeRef), "farmer_label": nil, "note": strOrNilAny(p.Note), "area_ha": p.AreaHa.StringFixed(4),
		"declared_area_ha": decStr(p.DeclaredAreaHa), "shape": strOrNilAny(p.Shape), "status": p.Status}
	if l, ok := labels[p.FarmerRef]; ok && p.FarmerRef != uuid.Nil {
		m["farmer_label"] = l
	}
	return m
}

func strOrNilAny(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func xmlEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func kmlDoc(commune string, rows []store.ExportRow, labels map[uuid.UUID]string) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<kml xmlns="http://www.opengis.net/kml/2.2"><Document><name>` + xmlEsc("Thửa ruộng xã "+commune) + "</name>\n")
	for _, r := range rows {
		name := r.LotName
		if name == "" {
			name = r.Code
		}
		b.WriteString("<Placemark><name>" + xmlEsc(name) + "</name><ExtendedData>")
		props := exportProps(r.Plot, labels)
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := props[k]
			if v == nil {
				continue
			}
			var sv string
			switch t := v.(type) {
			case string:
				sv = t
			case *string:
				if t == nil {
					continue
				}
				sv = *t
			default:
				continue
			}
			b.WriteString(`<Data name="` + xmlEsc(k) + `"><value>` + xmlEsc(sv) + "</value></Data>")
		}
		b.WriteString("</ExtendedData>" + r.KML + "</Placemark>\n")
	}
	b.WriteString("</Document></kml>\n")
	return b.Bytes()
}
