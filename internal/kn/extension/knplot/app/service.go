// Package app: thửa ruộng của KN — Trồng trọt FN-XA-03.05 "Vẽ ranh giới thửa ruộng" (web /v1/kn/plots, field-app đồng bộ
// ngoại tuyến kn_plot / kn_plot_boundary). KN-STANDALONE-5-TT-DATA (#271, quyết định D1): dữ liệu ở kho KN
// (pf_extension.kn_plot*), HTX của thửa là hồ sơ KN loại HTX (kn_producer) — KN không đọc/ghi bảng HTX nên chạy được khi
// tenant tắt pf/cooperative. Giữ hành vi FN: Cấp Xã và Admin ghi trong xã phụ trách, Cấp Tỉnh và Cấp Quốc gia chỉ xem,
// diện tích tự tính trong PostGIS, chặn mọi chồng lấn vượt ngưỡng geo.overlap_tolerance_pct (409 geo.plot_overlap kèm
// phần chồng lấn), sửa ranh giới là phiên bản mới có lý do, tên nông dân ở kho định danh (API trả nhãn che).
// Lời gọi module/dịch vụ khác (kho định danh, địa giới xã của pf/geo, tham số) chạy NGOÀI giao dịch ghi.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi (giữ mã geo.* của FN-XA-03.05; docs/errors/kn-extension-plots.md).
var (
	ErrPlotNotFound    = problem.New(http.StatusNotFound, "geo.plot_not_found", "Không tìm thấy thửa")
	ErrVersionConflict = problem.New(http.StatusConflict, "geo.version_conflict", "Ranh giới đã được sửa bởi người khác — hãy tải phiên bản hiện hành rồi sửa lại")
	ErrPlotState       = problem.New(http.StatusConflict, "geo.plot_state_conflict", "Trạng thái thửa không cho phép thao tác này")
	ErrOutOfScope      = problem.New(http.StatusForbidden, "geo.out_of_scope", "Bạn không có quyền truy cập lô ruộng này.")
	ErrTooFewPoints    = problem.New(http.StatusUnprocessableEntity, domain.CodeTooFewPoints, "Cần tối thiểu 3 điểm ranh giới không thẳng hàng để khép vùng.")
	ErrInvalidGeometry = problem.New(http.StatusUnprocessableEntity, domain.CodeInvalidGeometry, "Ranh giới không hợp lệ")
	ErrCRS             = problem.New(http.StatusUnprocessableEntity, domain.CodeCRSUnsupported, "Hệ toạ độ không được hỗ trợ")
	ErrAreaOutOfRange  = problem.New(http.StatusUnprocessableEntity, "geo.area_out_of_range", "Diện tích thửa ngoài giới hạn hợp lý")
	ErrOutsideCommune  = problem.New(http.StatusUnprocessableEntity, "geo.outside_commune", "Ranh giới nằm ngoài địa giới xã")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "geo.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrPlotOverlap     = problem.New(http.StatusConflict, "geo.plot_overlap", "Ranh giới bị chồng lấn với lô đã đăng ký — không lưu được, vui lòng vẽ lại.")
	ErrReadOnly        = problem.New(http.StatusForbidden, "geo.read_only", "Cấp Quốc gia chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "geo.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	errWriter          = problem.Forbidden.WithDetail("chỉ Cấp Xã và Admin được vẽ, sửa thửa")
)

// Giới hạn đọc/xuất.
const (
	MaxExport         = 5000
	ProductKNPlotFile = "kn_plot_file"
)

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// Service là use case thửa ruộng KN.
type Service struct {
	DB       tenant.Beginner       // pool vai r_extension (db.ForModule)
	Clock    clock.Clock           //
	Vault    client.Client         // tên nông dân (mục đích extension.contact); nil → ghi 503, đọc không nhãn
	Geometry geoport.AdminGeometry // tùy chọn: kiểm thửa nằm trong địa giới xã
	Params   params.Params         // tùy chọn: geo.overlap_tolerance_pct
	Names    func() partyport.ActorNames
	// Egress (A5, ADR-0024): tải tệp thửa là xuất NỘI BỘ — ghi egress_event, không bao giờ chặn.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k port.SessionContext
}

func (s *Service) caller(ctx context.Context, write bool) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	c := caller{p: p, k: k}
	if err != nil || !write {
		return c, err
	}
	switch {
	case k.ReadOnly():
		return c, ErrReadOnly
	case k.Role != port.SessionRoleCommune && k.Role != port.SessionRoleAdmin:
		return c, errWriter
	case k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(k.Support.Reason)) < domain.MinReason:
		return c, ErrSupportReason
	}
	return c, nil
}

func (c caller) readDept() string {
	if c.k.Role == port.SessionRoleAdmin {
		return ""
	}
	return c.k.Department
}

func dept(d string) string {
	if d == "" {
		return domain.LegacyDepartment
	}
	return d
}

func (c caller) sees(p store.Snapshot) bool {
	return c.k.Scope.Allows(p.CommuneCode) && c.k.AllowsDepartment(dept(p.Department))
}

func (c caller) writeDept(param string) (string, error) {
	if c.k.Role != port.SessionRoleAdmin {
		return dept(c.k.Department), nil
	}
	param = strings.TrimSpace(param)
	if param == "" {
		return domain.LegacyDepartment, nil
	}
	if !deptRe.MatchString(param) {
		return "", problem.Validation.WithField("department", "mã phòng ban không hợp lệ")
	}
	return param, nil
}

// meta điền người ghi, chế độ hỗ trợ, thời điểm vào ảnh chụp.
func (c caller) meta(v *store.Version, now time.Time, idem string) {
	v.ID, v.EventTime, v.ActorRef, v.IdemKey = ids.New(), now, c.p.ActorRef, idem
	if c.k.Support != nil {
		r := strings.TrimSpace(c.k.Support.Reason)
		v.SupportReason = &r
		if c.k.Support.OnBehalfOf != uuid.Nil {
			o := c.k.Support.OnBehalfOf
			v.OnBehalfOf = &o
		}
	}
}

func idemKey(k string) string { return "knplot:" + k }

func textField(field, v string, max int, required bool) (string, error) {
	out, why := domain.Text(v, max, required)
	if why != "" {
		return "", problem.Validation.WithField(field, why)
	}
	return out, nil
}

func shapeOf(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return domain.ShapePolygon, nil
	}
	if !domain.KnownShape(s) {
		return "", problem.Validation.WithField("shape", "phải là polygon, circle hoặc square")
	}
	return s, nil
}

func geomProblem(err error) error {
	var v *domain.Violation
	if !errors.As(err, &v) {
		return err
	}
	switch v.Code {
	case domain.CodeTooFewPoints:
		return ErrTooFewPoints.WithDetail(v.Msg)
	case domain.CodeCRSUnsupported:
		return ErrCRS.WithDetail(v.Msg)
	}
	return ErrInvalidGeometry.WithDetail(v.Msg)
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// Query là bộ lọc danh sách.
type Query struct {
	Commune string
	Coop    *uuid.UUID
	After   uuid.UUID
	Limit   int
}

// Page là một trang thửa (không hình) kèm nhãn nông dân.
type Page struct {
	Items  []store.Plot
	Labels map[uuid.UUID]string
	Next   *uuid.UUID
}

// List trả thửa trong phạm vi ngữ cảnh (mặc định mọi xã phụ trách).
func (s *Service) List(ctx context.Context, q Query) (Page, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return Page{}, err
	}
	q.Commune = strings.TrimSpace(q.Commune)
	if q.Commune != "" && !c.k.Scope.Allows(q.Commune) {
		return Page{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	if c.k.Scope.Empty() {
		return Page{Items: []store.Plot{}, Labels: map[uuid.UUID]string{}}, nil
	}
	var rows []store.Plot
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.List(ctx, tx, store.Filter{All: c.k.Scope.All, Units: c.k.Scope.Units, Commune: q.Commune, Coop: q.Coop,
			Dept: c.readDept(), After: q.After, Limit: q.Limit + 1})
		return e
	})
	if err != nil {
		return Page{}, err
	}
	pg := Page{Items: rows}
	if len(rows) > q.Limit {
		pg.Items = rows[:q.Limit]
		n := pg.Items[q.Limit-1].Ref
		pg.Next = &n
	}
	pg.Labels = s.FarmerLabels(ctx, pg.Items...)
	return pg, nil
}

func (s *Service) plot(ctx context.Context, c caller, ref uuid.UUID) (store.Plot, error) {
	var p *store.Plot
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		p, e = store.Get(ctx, tx, ref)
		return e
	})
	if err != nil {
		return store.Plot{}, err
	}
	if p == nil || !c.sees(p.Snapshot) {
		return store.Plot{}, ErrPlotNotFound
	}
	return *p, nil
}

// Get đọc một thửa (kèm hình) trong phạm vi; ngoài phạm vi → 404.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (store.Plot, map[uuid.UUID]string, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return store.Plot{}, nil, err
	}
	p, err := s.plot(ctx, c, ref)
	if err != nil {
		return store.Plot{}, nil, err
	}
	return p, s.FarmerLabels(ctx, p), nil
}

// FarmerLabels tra nhãn đã che của nông dân; lỗi tra nhãn không chặn màn hình.
func (s *Service) FarmerLabels(ctx context.Context, plots ...store.Plot) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Vault == nil {
		return out
	}
	for _, p := range plots {
		if p.FarmerRef == nil {
			continue
		}
		if _, ok := out[*p.FarmerRef]; ok {
			continue
		}
		if d, err := s.Vault.Display(ctx, client.PurposeContact, *p.FarmerRef); err == nil || d.Label != "" {
			out[*p.FarmerRef] = d.Label
		}
	}
	return out
}

// PlotLocation thực hiện port.PlotDirectory (hồ sơ cây trồng của thửa, BF-SRV-KN-ATTRS) từ kho KN.
func (s *Service) PlotLocation(ctx context.Context, ref uuid.UUID) (port.PlotLocation, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.PlotLocation{}, false, problem.Unauthenticated
	}
	var pl *store.Plot
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		pl, e = store.Get(ctx, tx, ref)
		return e
	})
	if err != nil || pl == nil {
		return port.PlotLocation{}, false, err
	}
	out := port.PlotLocation{PlotRef: pl.Ref, CommuneCode: pl.CommuneCode, Status: Status(pl.Snapshot)}
	if pl.CooperativeRef != nil {
		out.CooperativeRef = *pl.CooperativeRef
	}
	return out, true, nil
}

// Status là trạng thái hiển thị: thửa chưa gán HTX là "draft" (như trước khi chuyển kho).
func Status(p store.Snapshot) string {
	if p.CooperativeRef == nil {
		return "draft"
	}
	return "active"
}

// ---------------------------------------------------------------------------
// Ghi
// ---------------------------------------------------------------------------

// CreateInput là "Vẽ lô mới".
type CreateInput struct {
	PlotRef        uuid.UUID
	CommuneCode    string
	CooperativeRef *uuid.UUID
	FarmerName     string `pii:"true"`
	LotName        string
	Note           string
	DeclaredAreaHa *decimal.Decimal
	Shape          string
	Geometry       []byte
	Department     string // Admin: phòng ban ghi (tùy chọn)
	IdemKey        string
	Source         string
	DeviceID       *string
	EventTime      time.Time // field-app: lúc vẽ (R-13); zero = bây giờ
}

// Result là thửa sau lần ghi.
type Result struct {
	Plot     store.Plot
	Replayed bool
}

func (s *Service) replay(ctx context.Context, c caller, key string) (*store.Plot, error) {
	var v *store.Version
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) { v, e = store.ByIdem(ctx, tx, key); return }); err != nil || v == nil {
		return nil, err
	}
	p, err := s.plot(ctx, c, v.Ref)
	return &p, err
}

// Create tạo thửa với ranh giới đầu tiên; chặn mọi chồng lấn; < 3 đỉnh/không khép → 422.
func (s *Service) Create(ctx context.Context, in CreateInput) (Result, error) {
	c, err := s.caller(ctx, true)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return Result{}, problem.IdempotencyRequired
	}
	key := idemKey(in.IdemKey)
	if p, err := s.replay(ctx, c, key); err != nil || p != nil {
		if p != nil {
			return Result{Plot: *p, Replayed: true}, nil
		}
		return Result{}, err
	}
	snap := store.Snapshot{Ref: in.PlotRef}
	if snap.Ref == uuid.Nil {
		snap.Ref = ids.New()
	}
	if snap.LotName, err = textField("lot_name", in.LotName, domain.MaxLotName, true); err != nil {
		return Result{}, err
	}
	if snap.Note, err = textField("note", in.Note, domain.MaxNote, false); err != nil {
		return Result{}, err
	}
	if snap.Shape, err = shapeOf(in.Shape); err != nil {
		return Result{}, err
	}
	if in.DeclaredAreaHa != nil && !in.DeclaredAreaHa.IsPositive() {
		return Result{}, problem.Validation.WithField("declared_area_ha", "phải dương")
	}
	snap.DeclaredAreaHa = in.DeclaredAreaHa
	if snap.Department, err = c.writeDept(in.Department); err != nil {
		return Result{}, err
	}
	snap.CommuneCode = strings.TrimSpace(in.CommuneCode)
	if snap.CommuneCode == "" {
		return Result{}, problem.Validation.WithField("commune_code", "bắt buộc")
	}
	if !c.k.Scope.Allows(snap.CommuneCode) {
		return Result{}, ErrOutOfScope.WithDetail("chỉ vẽ trong địa bàn phân công")
	}
	shape, err := domain.ParseGeometry(in.Geometry)
	if err != nil {
		return Result{}, geomProblem(err)
	}
	if err := s.cooperative(ctx, c, in.CooperativeRef); err != nil {
		return Result{}, err
	}
	snap.CooperativeRef = in.CooperativeRef
	if err := s.insideCommune(ctx, shape.GeoJSON, snap.CommuneCode); err != nil {
		return Result{}, err
	}
	tol, err := s.tolerance(ctx)
	if err != nil {
		return Result{}, err
	}
	farmer, err := s.farmerRef(ctx, in.FarmerName, snap.CommuneCode)
	if err != nil {
		return Result{}, err
	}
	snap.FarmerRef = &farmer
	v := store.Version{Snapshot: snap, Event: domain.EventCreated, Source: sourceOf(in.Source), DeviceID: in.DeviceID, GeoJSON: shape.GeoJSON}
	c.meta(&v, s.eventTime(in.EventTime), key)
	v.BoundaryID = v.ID
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockTenant(ctx, tx, c.p.TenantID); err != nil {
			return err
		}
		if prev, err := store.ByIdem(ctx, tx, key); err != nil || prev != nil {
			if prev != nil {
				v.Ref, replayed = prev.Ref, true
			}
			return err
		}
		if cur, err := store.Get(ctx, tx, v.Ref); err != nil || cur != nil {
			if cur != nil {
				return ErrPlotState.WithDetail("mã thửa đã có")
			}
			return err
		}
		area, err := checkShape(ctx, tx, shape.GeoJSON, uuid.Nil, tol)
		if err != nil {
			return err
		}
		v.AreaHa = area
		if v.Code, err = store.NextCode(ctx, tx); err != nil {
			return err
		}
		return store.InsertVersion(ctx, tx, c.p.TenantID, &v)
	})
	if err != nil {
		return Result{}, err
	}
	p, err := s.plot(ctx, c, v.Ref)
	return Result{Plot: p, Replayed: replayed}, err
}

func sourceOf(s string) string {
	if s == domain.SourceFieldApp {
		return s
	}
	return domain.SourceWeb
}

func (s *Service) eventTime(t time.Time) time.Time {
	if t.IsZero() {
		return s.now()
	}
	return t.UTC()
}

// checkShape (trong giao dịch, sau LockTenant): hình hợp lệ, diện tích hợp lý, không chồng lấn vượt ngưỡng.
func checkShape(ctx context.Context, tx pgx.Tx, geojson string, exclude uuid.UUID, tol decimal.Decimal) (decimal.Decimal, error) {
	an, err := store.Analyze(ctx, tx, geojson)
	if err != nil {
		return decimal.Zero, err
	}
	if !an.Valid {
		return decimal.Zero, ErrInvalidGeometry.WithDetail("Ranh giới tự cắt hoặc sai cấu trúc (" + an.Reason + ") — hãy chấm các điểm theo đúng thứ tự đi vòng quanh thửa")
	}
	if an.AreaHa.LessThan(domain.MinAreaHa) || an.AreaHa.GreaterThan(domain.MaxAreaHa) {
		return decimal.Zero, ErrAreaOutOfRange.WithDetail("Diện tích "+an.AreaHa.String()+" ha nằm ngoài khoảng "+
			domain.MinAreaHa.String()+"–"+domain.MaxAreaHa.String()+" ha").With("area_ha", an.AreaHa.String())
	}
	hits, err := store.Overlaps(ctx, tx, geojson, exclude)
	if err != nil {
		return decimal.Zero, err
	}
	var list []map[string]any
	var codes []string
	for _, h := range hits {
		pct := domain.OverlapPct(h.IntersectHa, h.AreaHa, an.AreaHa)
		if !pct.GreaterThan(tol) {
			continue
		}
		m := map[string]any{"plot_ref": h.Ref.String(), "code": h.Code, "area_ha": h.IntersectHa.StringFixed(4),
			"overlap_pct": pct.StringFixed(4), "geometry": nil}
		if h.GeoJSON != "" {
			m["geometry"] = rawJSON(h.GeoJSON)
		}
		list = append(list, m)
		codes = append(codes, h.Code)
	}
	if len(list) > 0 {
		return decimal.Zero, ErrPlotOverlap.WithDetail("Ranh giới bị chồng lấn với lô đã đăng ký ("+strings.Join(codes, ", ")+") — vui lòng vẽ lại.").
			With("overlaps", list)
	}
	return an.AreaHa, nil
}

func (s *Service) tolerance(ctx context.Context) (decimal.Decimal, error) {
	if s.Params == nil {
		return domain.DefaultOverlapTolerancePct, nil
	}
	v, err := s.Params.At(ctx, domain.ParamOverlapTolerancePct, s.now())
	if errors.Is(err, params.ErrParamNotFound) {
		return domain.DefaultOverlapTolerancePct, nil
	}
	if err != nil {
		return decimal.Zero, ErrDependency.WithDetail("không đọc được tham số ngưỡng chồng lấn").WithCause(err)
	}
	if v.Value.IsNegative() {
		return domain.DefaultOverlapTolerancePct, nil
	}
	return v.Value, nil
}

func (s *Service) insideCommune(ctx context.Context, geojson, commune string) error {
	if s.Geometry == nil {
		return nil
	}
	out, err := s.Geometry.OutsideCommunePct(ctx, geojson, commune)
	if err != nil {
		return ErrDependency.WithDetail("không kiểm được địa giới xã").WithCause(err)
	}
	if out == nil {
		return nil
	}
	tol, err := s.tolerance(ctx)
	if err != nil {
		return err
	}
	if out.GreaterThan(tol) {
		return ErrOutsideCommune.WithDetail(out.StringFixed(2)+"% diện tích nằm ngoài địa giới xã "+commune).
			With("outside_pct", out.StringFixed(4))
	}
	return nil
}

// cooperative kiểm hồ sơ KN loại HTX: có, đang hoạt động, trong phạm vi người gọi.
func (s *Service) cooperative(ctx context.Context, c caller, ref *uuid.UUID) error {
	if ref == nil {
		return nil
	}
	var rec *store.Cooperative
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) { rec, e = store.CooperativeRecord(ctx, tx, *ref); return })
	if err != nil {
		return err
	}
	if rec == nil || !c.k.Scope.Allows(rec.CommuneCode) {
		return problem.Validation.WithField("cooperative_ref", "không có hồ sơ HTX trong phạm vi")
	}
	if !rec.Active {
		return problem.Validation.WithField("cooperative_ref", "hồ sơ HTX đã ngừng hoạt động")
	}
	return nil
}

// farmerRef kiểm tên chủ hộ bắt buộc (TT-11), rồi ghi vào kho định danh và trả mã tham chiếu.
func (s *Service) farmerRef(ctx context.Context, name, commune string) (uuid.UUID, error) {
	name, err := textField("farmer_name", name, domain.MaxFarmerName, true)
	if err != nil {
		return uuid.Nil, err
	}
	if s.Vault == nil {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	cr, err := s.Vault.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectPerson, FullName: name, AreaCode: commune})
	if errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil {
		return cr.Ref, nil
	}
	if err != nil {
		var pr *problem.Problem
		if errors.As(err, &pr) && pr.Status == http.StatusUnprocessableEntity {
			return uuid.Nil, problem.Validation.WithField("farmer_name", "không hợp lệ")
		}
		return uuid.Nil, ErrDependency.WithDetail("không ghi được tên nông dân vào kho định danh").WithCause(err)
	}
	return cr.Ref, nil
}

// CorrectInput là "Sửa ranh giới" (phiên bản mới thay phiên bản SupersedesID).
type CorrectInput struct {
	PlotRef      uuid.UUID
	SupersedesID uuid.UUID
	Geometry     []byte
	Shape        string
	Reason       string
	IdemKey      string
	Source       string
	DeviceID     *string
	EventTime    time.Time
}

// Correct ghi phiên bản ranh giới mới; chặn mọi chồng lấn; sửa trên phiên bản cũ → 409 geo.version_conflict.
func (s *Service) Correct(ctx context.Context, in CorrectInput) (Result, error) {
	c, err := s.caller(ctx, true)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return Result{}, problem.IdempotencyRequired
	}
	key := idemKey(in.IdemKey)
	if p, err := s.replay(ctx, c, key); err != nil || p != nil {
		if p != nil {
			return Result{Plot: *p, Replayed: true}, nil
		}
		return Result{}, err
	}
	shapeName, err := shapeOf(in.Shape)
	if err != nil {
		return Result{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) < domain.MinReason {
		return Result{}, problem.Validation.WithField("reason", "bắt buộc, ít nhất 10 ký tự")
	}
	if _, why := domain.Text(reason, domain.MaxReason, true); why != "" {
		return Result{}, problem.Validation.WithField("reason", why)
	}
	cur, err := s.plot(ctx, c, in.PlotRef)
	if err != nil {
		return Result{}, err
	}
	if !c.k.Scope.Allows(cur.CommuneCode) {
		return Result{}, ErrOutOfScope
	}
	if in.SupersedesID != cur.BoundaryID {
		return Result{}, ErrVersionConflict
	}
	shape, err := domain.ParseGeometry(in.Geometry)
	if err != nil {
		return Result{}, geomProblem(err)
	}
	if err := s.insideCommune(ctx, shape.GeoJSON, cur.CommuneCode); err != nil {
		return Result{}, err
	}
	tol, err := s.tolerance(ctx)
	if err != nil {
		return Result{}, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, in.PlotRef); err != nil {
			return err
		}
		if err := store.LockTenant(ctx, tx, c.p.TenantID); err != nil {
			return err
		}
		if prev, err := store.ByIdem(ctx, tx, key); err != nil || prev != nil {
			return err
		}
		now, err := store.Get(ctx, tx, in.PlotRef)
		if err != nil {
			return err
		}
		if now == nil || now.BoundaryID != in.SupersedesID {
			return ErrVersionConflict
		}
		area, err := checkShape(ctx, tx, shape.GeoJSON, in.PlotRef, tol)
		if err != nil {
			return err
		}
		v := store.Version{Snapshot: now.Snapshot, Event: domain.EventBoundaryCorrected, Source: sourceOf(in.Source), Reason: &reason,
			DeviceID: in.DeviceID, GeoJSON: shape.GeoJSON}
		c.meta(&v, s.eventTime(in.EventTime), key)
		v.BoundaryID, v.AreaHa, v.Shape = v.ID, area, shapeName
		return store.InsertVersion(ctx, tx, c.p.TenantID, &v)
	})
	if err != nil {
		return Result{}, err
	}
	p, err := s.plot(ctx, c, in.PlotRef)
	return Result{Plot: p}, err
}

// UpdateInput sửa thuộc tính thửa. Con trỏ nil = giữ nguyên; chuỗi rỗng = xóa.
type UpdateInput struct {
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

// Update ghi ảnh chụp 'attributes_changed'; không đổi gì → trả hiện trạng, không ghi sổ.
func (s *Service) Update(ctx context.Context, in UpdateInput) (Result, error) {
	c, err := s.caller(ctx, true)
	if err != nil {
		return Result{}, err
	}
	key := idemKey("upd:" + ids.New().String())
	if strings.TrimSpace(in.IdemKey) != "" {
		key = idemKey(in.IdemKey)
		if p, err := s.replay(ctx, c, key); err != nil || p != nil {
			if p != nil {
				return Result{Plot: *p, Replayed: true}, nil
			}
			return Result{}, err
		}
	}
	cur, err := s.plot(ctx, c, in.PlotRef)
	if err != nil {
		return Result{}, err
	}
	if !c.k.Scope.Allows(cur.CommuneCode) {
		return Result{}, ErrOutOfScope
	}
	next := cur.Snapshot
	if in.LotName != nil {
		if next.LotName, err = textField("lot_name", *in.LotName, domain.MaxLotName, true); err != nil {
			return Result{}, err
		}
	}
	if in.Note != nil {
		if next.Note, err = textField("note", *in.Note, domain.MaxNote, false); err != nil {
			return Result{}, err
		}
	}
	if in.DeclaredAreaHa != nil {
		if !in.DeclaredAreaHa.IsPositive() {
			return Result{}, problem.Validation.WithField("declared_area_ha", "phải dương")
		}
		next.DeclaredAreaHa = in.DeclaredAreaHa
	} else if in.ClearDeclared {
		next.DeclaredAreaHa = nil
	}
	reason, err := textField("reason", in.Reason, domain.MaxReason, false)
	if err != nil {
		return Result{}, err
	}
	if in.CooperativeRef != nil {
		next.CooperativeRef = nil
		if *in.CooperativeRef != uuid.Nil {
			ref := *in.CooperativeRef
			if err := s.cooperative(ctx, c, &ref); err != nil {
				return Result{}, err
			}
			next.CooperativeRef = &ref
		}
	}
	if in.FarmerName != nil {
		f, err := s.farmerRef(ctx, *in.FarmerName, cur.CommuneCode)
		if err != nil {
			return Result{}, err
		}
		next.FarmerRef = &f
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, in.PlotRef); err != nil {
			return err
		}
		if prev, err := store.ByIdem(ctx, tx, key); err != nil || prev != nil {
			return err
		}
		now, err := store.Get(ctx, tx, in.PlotRef)
		if err != nil {
			return err
		}
		if now == nil || !c.sees(now.Snapshot) {
			return ErrPlotNotFound
		}
		n := next
		n.BoundaryID, n.AreaHa, n.Shape, n.Code, n.CommuneCode, n.Department = now.BoundaryID, now.AreaHa, now.Shape, now.Code, now.CommuneCode, now.Department
		if same(now.Snapshot, n) {
			return nil
		}
		v := store.Version{Snapshot: n, Event: domain.EventAttributesChanged, Source: domain.SourceWeb}
		if reason != "" {
			v.Reason = &reason
		}
		c.meta(&v, s.now(), key)
		return store.InsertVersion(ctx, tx, c.p.TenantID, &v)
	})
	if err != nil {
		return Result{}, err
	}
	p, err := s.plot(ctx, c, in.PlotRef)
	return Result{Plot: p}, err
}

func same(a, b store.Snapshot) bool {
	return refEq(a.CooperativeRef, b.CooperativeRef) && refEq(a.FarmerRef, b.FarmerRef) && a.LotName == b.LotName &&
		a.Note == b.Note && decEq(a.DeclaredAreaHa, b.DeclaredAreaHa)
}

func refEq(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func decEq(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
