// Package app: Trồng trọt FN-XA-03.04 — Quản lý máy móc của HTX ở kho KN (KN-STANDALONE-5-TT-DATA-MAY #306, kế hoạch
// KN-STANDALONE quyết định D1). Khai báo loại máy × số lượng của từng HTX, tổng theo loại của xã (= Σ các HTX), lịch sử
// thay đổi số lượng. Dữ liệu ở pf_extension, HTX là hồ sơ KN loại HTX (kn_producer) — KN không đọc/ghi module CGH, HTX nền
// tảng nên chạy được khi tenant tắt pf/mechanization, pf/cooperative.
//
// Giữ hành vi trước khi chuyển kho: Cấp Xã và Admin thêm/sửa trong xã phụ trách; Cấp Tỉnh, Cấp Quốc gia chỉ xem; Admin ở
// chế độ hỗ trợ phải có lý do; khai báo mang phòng ban của người ghi, đọc KHÔNG lọc phòng ban (máy của HTX là dữ liệu
// chung); mã lỗi mechanization.* đã công bố giữ nguyên để web và field-app không đổi.
package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knmachine/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knmachine/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi FN-XA-03.04 (docs/errors/pf-extension-kn-machines.md — giữ mã đã công bố).
var (
	ErrReadOnly        = problem.New(http.StatusForbidden, "mechanization.read_only", "Vai trò của ngữ cảnh phiên chỉ được xem máy móc HTX")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "mechanization.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do (ít nhất 10 ký tự)")
	ErrCoopNotFound    = problem.New(http.StatusNotFound, "mechanization.cooperative_not_found", "Không tìm thấy HTX")
	ErrCoopInvalid     = problem.New(http.StatusUnprocessableEntity, "mechanization.cooperative_invalid", "HTX không tồn tại hoặc đã ngừng hoạt động")
	ErrTypeUnknown     = problem.New(http.StatusUnprocessableEntity, "mechanization.machine_type_unknown", "Loại máy không có trong danh mục Loại máy móc (hoặc đã ngừng dùng)")
	ErrTypeDuplicate   = problem.New(http.StatusUnprocessableEntity, "mechanization.machine_type_duplicate", "Loại máy đã có trong khai báo của HTX — cập nhật dòng đã có thay vì thêm dòng mới")
	ErrQuantityInvalid = problem.New(http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "Số lượng máy phải là số nguyên lớn hơn 0")
	ErrOutOfScope      = problem.New(http.StatusForbidden, "mechanization.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "mechanization.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

// DefaultDepartment là phòng ban mặc định của Admin khi không truyền tham số department.
const DefaultDepartment = "trong_trot"

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// Service là use case máy móc HTX của KN.
type Service struct {
	DB       tenant.Beginner // pool vai r_extension (db.ForModule)
	Clock    clock.Clock
	Catalogs mdport.Catalogs // danh mục machine_type
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
	if k.ReadOnly() || k.Role == port.SessionRoleProvince {
		return c, ErrReadOnly
	}
	if k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(k.Support.Reason)) < 10 {
		return c, ErrSupportReason
	}
	return c, nil
}

// cooperative tra HTX (hồ sơ KN loại HTX, hoặc HTX cũ đã chép chưa có hồ sơ KN) và kiểm phạm vi: không có hoặc ngoài
// phạm vi → 404 (không lộ sự tồn tại); ghi vào HTX đã ngừng hoạt động → 422 mechanization.cooperative_invalid.
func (s *Service) cooperative(ctx context.Context, c caller, ref uuid.UUID, write bool) (store.Cooperative, error) {
	var co *store.Cooperative
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) { co, e = store.CooperativeOf(ctx, tx, ref); return }); err != nil {
		return store.Cooperative{}, err
	}
	if co == nil || !c.k.Scope.Allows(co.CommuneCode) {
		return store.Cooperative{}, ErrCoopNotFound
	}
	if write && !co.Active {
		return store.Cooperative{}, ErrCoopInvalid.WithDetail("HTX đã ngừng hoạt động")
	}
	return *co, nil
}

// Cooperative thực hiện port.CooperativeDirectory cho hồ sơ năm sản xuất máy (knattrs): HTX theo đúng khóa của khai báo
// máy, trong tenant của người gọi. ok = false khi không có.
func (s *Service) Cooperative(ctx context.Context, ref uuid.UUID) (port.CooperativeRecord, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.CooperativeRecord{}, false, problem.Unauthenticated
	}
	var co *store.Cooperative
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) (e error) { co, e = store.CooperativeOf(ctx, tx, ref); return })
	if err != nil || co == nil {
		return port.CooperativeRecord{}, false, err
	}
	return port.CooperativeRecord{Ref: co.Ref, CommuneCode: co.CommuneCode, Active: co.Active}, true, nil
}

// typeCatalog trả danh mục loại máy theo mã.
func (s *Service) typeCatalog(ctx context.Context) (map[string]mdport.CatalogItem, error) {
	if s.Catalogs == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục")
	}
	items, err := s.Catalogs.List(ctx, mdport.CatalogMachineType)
	if err != nil {
		return nil, ErrDependency.WithDetail("không đọc được danh mục loại máy").WithCause(err)
	}
	out := make(map[string]mdport.CatalogItem, len(items))
	for _, it := range items {
		out[it.Code] = it
	}
	return out, nil
}

// Line là một dòng khai báo hiện hành.
type Line struct {
	store.Row
	TypeName string
}

// CoopMachines là khai báo của một HTX. Name rỗng với HTX cũ đã chép chưa có hồ sơ KN.
type CoopMachines struct {
	CooperativeRef uuid.UUID
	Name           string
	CommuneCode    string
	Lines          []Line
	Total          int
}

// CommuneTotal là tổng theo loại máy của một xã (= Σ các HTX của xã).
type CommuneTotal struct {
	CommuneCode string
	MachineType string
	TypeName    string
	Quantity    int
}

// Overview là màn "Máy móc HTX": danh sách HTX có khai báo + tổng xã theo loại máy.
type Overview struct {
	Cooperatives  []CoopMachines
	CommuneTotals []CommuneTotal
}

func lines(rows []store.Row, cat map[string]mdport.CatalogItem) ([]Line, int) {
	out := make([]Line, 0, len(rows))
	total := 0
	for _, r := range rows {
		out = append(out, Line{Row: r, TypeName: cat[r.MachineType].Name})
		total += r.Quantity
	}
	return out, total
}

// Overview trả khai báo của mọi HTX trong phạm vi (hoặc một xã) và tổng theo loại của từng xã.
func (s *Service) Overview(ctx context.Context, commune string) (Overview, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return Overview{}, err
	}
	commune = strings.TrimSpace(commune)
	sc := c.k.Scope
	if commune != "" && !sc.Allows(commune) {
		return Overview{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	out := Overview{Cooperatives: []CoopMachines{}, CommuneTotals: []CommuneTotal{}}
	all, units := sc.All, sc.Units
	if commune != "" {
		all, units = false, []string{commune}
	}
	if !all && len(units) == 0 {
		return out, nil
	}
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return Overview{}, err
	}
	var rows []store.Row
	var names map[uuid.UUID]string
	byCoop := map[uuid.UUID][]store.Row{}
	order := []uuid.UUID{}
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if rows, e = store.In(ctx, tx, all, units); e != nil {
			return e
		}
		for _, r := range rows {
			if _, ok := byCoop[r.CooperativeRef]; !ok {
				order = append(order, r.CooperativeRef)
			}
			byCoop[r.CooperativeRef] = append(byCoop[r.CooperativeRef], r)
		}
		names, e = store.CooperativeNames(ctx, tx, order)
		return e
	}); err != nil {
		return Overview{}, err
	}
	totals := map[[2]string]int{}
	for _, r := range rows {
		totals[[2]string{r.CommuneCode, r.MachineType}] += r.Quantity
	}
	for _, ref := range order {
		rs := byCoop[ref]
		ls, total := lines(rs, cat)
		out.Cooperatives = append(out.Cooperatives, CoopMachines{CooperativeRef: ref, Name: names[ref], CommuneCode: rs[0].CommuneCode, Lines: ls, Total: total})
	}
	for k, q := range totals {
		out.CommuneTotals = append(out.CommuneTotals, CommuneTotal{CommuneCode: k[0], MachineType: k[1], TypeName: cat[k[1]].Name, Quantity: q})
	}
	sort.Slice(out.CommuneTotals, func(i, j int) bool {
		x, y := out.CommuneTotals[i], out.CommuneTotals[j]
		if x.CommuneCode != y.CommuneCode {
			return x.CommuneCode < y.CommuneCode
		}
		return x.MachineType < y.MachineType
	})
	return out, nil
}

// Get trả khai báo hiện hành của một HTX.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (CoopMachines, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return CoopMachines{}, err
	}
	co, err := s.cooperative(ctx, c, ref, false)
	if err != nil {
		return CoopMachines{}, err
	}
	return s.current(ctx, c.p.TenantID, co)
}

func (s *Service) current(ctx context.Context, tid uuid.UUID, co store.Cooperative) (CoopMachines, error) {
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return CoopMachines{}, err
	}
	var rows []store.Row
	if err := s.tx(ctx, tid, func(tx pgx.Tx) (e error) { rows, e = store.Of(ctx, tx, co.Ref, false); return }); err != nil {
		return CoopMachines{}, err
	}
	ls, total := lines(rows, cat)
	return CoopMachines{CooperativeRef: co.Ref, Name: co.Name, CommuneCode: co.CommuneCode, Lines: ls, Total: total}, nil
}

// PutInput thay toàn bộ khai báo của HTX.
type PutInput struct {
	CooperativeRef uuid.UUID
	Lines          []domain.Line
	Reason         string
	Department     string // chỉ Admin: phòng ban của bản ghi (mặc định trong_trot)
	IdemKey        string
}

// Put thay toàn bộ khai báo của HTX: mỗi loại đổi = một sự việc (set/removed) trên sổ, bảng chiếu cập nhật cùng giao
// dịch. Gọi lặp cùng Idempotency-Key → khai báo hiện tại, không ghi thêm dòng. Kiểm rẻ trước (số lượng, ghi chú, trùng
// loại), rồi HTX và danh mục, rồi mới mở giao dịch ghi.
func (s *Service) Put(ctx context.Context, in PutInput) (CoopMachines, error) {
	c, err := s.caller(ctx, true)
	if err != nil {
		return CoopMachines{}, err
	}
	if len(in.Lines) > domain.MaxLines {
		return CoopMachines{}, problem.Validation.WithField("lines", "tối đa 100 dòng")
	}
	for i := range in.Lines {
		in.Lines[i].MachineType = strings.TrimSpace(in.Lines[i].MachineType)
		in.Lines[i].Note = strings.TrimSpace(in.Lines[i].Note)
		l := in.Lines[i]
		if l.MachineType == "" {
			return CoopMachines{}, problem.Validation.WithField("lines.machine_type", "bắt buộc")
		}
		if l.Quantity <= 0 || l.Quantity > domain.MaxQuantity {
			return CoopMachines{}, ErrQuantityInvalid.WithField("lines."+l.MachineType+".quantity", domain.ErrQuantityRange.Error())
		}
		if !domain.ValidNote(l.Note) {
			return CoopMachines{}, problem.Validation.WithField("lines."+l.MachineType+".note", "tối đa 500 ký tự")
		}
	}
	if d := domain.DuplicateType(in.Lines); d != "" {
		return CoopMachines{}, ErrTypeDuplicate.With("machine_type", d)
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) > 500 {
		return CoopMachines{}, problem.Validation.WithField("reason", "tối đa 500 ký tự")
	}
	dept := c.k.Department
	if c.k.Role == port.SessionRoleAdmin {
		if dept = strings.TrimSpace(in.Department); dept == "" {
			dept = DefaultDepartment
		}
	}
	if !deptRe.MatchString(dept) {
		return CoopMachines{}, problem.Validation.WithField("department", "mã phòng ban không hợp lệ")
	}
	co, err := s.cooperative(ctx, c, in.CooperativeRef, true)
	if err != nil {
		return CoopMachines{}, err
	}
	if in.IdemKey == "" {
		in.IdemKey = "kn-machines-" + ids.New().String()
	}
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return CoopMachines{}, err
	}
	for _, l := range in.Lines {
		if it, ok := cat[l.MachineType]; !ok || !it.Active {
			return CoopMachines{}, ErrTypeUnknown.With("machine_type", l.MachineType)
		}
	}
	var ob, sid *uuid.UUID
	var sr *string
	if c.k.Support != nil {
		if c.k.Support.OnBehalfOf != uuid.Nil {
			v := c.k.Support.OnBehalfOf
			ob = &v
		}
		id, r := c.k.Support.SessionID, strings.TrimSpace(c.k.Support.Reason)
		sid, sr = &id, &r
	}
	var reason *string
	if r := strings.TrimSpace(in.Reason); r != "" {
		reason = &r
	}
	now := s.now()
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, co.Ref); err != nil {
			return err
		}
		if done, err := store.RequestRecorded(ctx, tx, in.IdemKey); err != nil || done {
			return err
		}
		rows, err := store.Of(ctx, tx, co.Ref, true)
		if err != nil {
			return err
		}
		cur := map[string]domain.Line{}
		for _, r := range rows {
			if r.Active {
				cur[r.MachineType] = domain.Line{MachineType: r.MachineType, Quantity: r.Quantity, Note: r.Note}
			}
		}
		for _, ch := range domain.Plan(cur, in.Lines) {
			e := store.Event{ID: ids.New(), TenantID: c.p.TenantID, CooperativeRef: co.Ref, CommuneCode: co.CommuneCode,
				MachineType: ch.MachineType, Action: ch.Action, Quantity: ch.Quantity, PrevQuantity: ch.PrevQuantity, PrevNote: ch.PrevNote,
				DepartmentCode: dept, Reason: reason, RequestKey: in.IdemKey, OnBehalfOf: ob, SupportSessionID: sid, SupportReason: sr,
				EventTime: now, ActorRef: c.p.ActorRef, IdemKey: in.IdemKey + "|" + ch.MachineType}
			if ch.Action == domain.Set && ch.Note != "" {
				n := ch.Note
				e.Note = &n
			}
			if err := store.InsertEvent(ctx, tx, &e); err != nil {
				return err
			}
			if err := store.Upsert(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err = ledger.Classify(err); err != nil && !errors.Is(err, ledger.ErrDuplicate) {
		return CoopMachines{}, err
	}
	return s.current(ctx, c.p.TenantID, co)
}

// LogEntry là một dòng lịch sử số lượng.
type LogEntry struct {
	store.Event
	TypeName string
}

// Log trả lịch sử thay đổi số lượng của HTX theo seq.
func (s *Service) Log(ctx context.Context, ref uuid.UUID, after int64, limit int) ([]LogEntry, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return nil, err
	}
	if _, err := s.cooperative(ctx, c, ref, false); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var evs []store.Event
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) { evs, e = store.Events(ctx, tx, ref, after, limit); return }); err != nil {
		return nil, err
	}
	out := make([]LogEntry, 0, len(evs))
	for _, e := range evs {
		out = append(out, LogEntry{Event: e, TypeName: cat[e.MachineType].Name})
	}
	return out, nil
}
