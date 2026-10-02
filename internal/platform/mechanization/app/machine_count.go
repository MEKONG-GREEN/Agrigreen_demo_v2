package app

// KN v7.7 FN-XA-03.04 — Quản lý máy móc của HTX: khai báo loại máy cơ giới × số lượng của từng HTX, xem tổng
// theo loại của xã (AC: tổng theo loại của xã = Σ các HTX), lưu lịch sử thay đổi số lượng (hợp đồng P7 §2).
//
// Quyền theo ngữ cảnh phiên (authz.RequireKN): Cấp Xã thêm/sửa trong xã phụ trách; Cấp Tỉnh xem trong tỉnh;
// Cấp Quốc gia xem; Admin mọi thứ (chế độ hỗ trợ: lý do, on_behalf_of). Khai báo mang department_code của người
// ghi; đọc KHÔNG lọc phòng ban — máy của HTX là dữ liệu chung (tạm, chờ khách).

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi FN-XA-03.04 (docs/errors.md, mục pf/mechanization).
var (
	ErrReadOnly        = problem.New(http.StatusForbidden, "mechanization.read_only", "Vai trò của ngữ cảnh phiên chỉ được xem máy móc HTX")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "mechanization.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do (ít nhất 10 ký tự)")
	ErrCoopNotFound    = problem.New(http.StatusNotFound, "mechanization.cooperative_not_found", "Không tìm thấy HTX")
	ErrTypeUnknown     = problem.New(http.StatusUnprocessableEntity, "mechanization.machine_type_unknown", "Loại máy không có trong danh mục Loại máy móc (hoặc đã ngừng dùng)")
	ErrTypeDuplicate   = problem.New(http.StatusUnprocessableEntity, "mechanization.machine_type_duplicate", "Loại máy đã có trong khai báo của HTX — cập nhật dòng đã có thay vì thêm dòng mới")
	ErrQuantityInvalid = problem.New(http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "Số lượng máy phải là số nguyên không âm")
)

// DefaultDepartment là phòng ban mặc định của Admin khi không truyền tham số department (hợp đồng P7 §0).
const DefaultDepartment = "trong_trot"

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

type knActor struct {
	p authz.Principal
	k authz.KNContext
}

func (s *Service) knActor(ctx context.Context, write bool) (knActor, error) {
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return knActor{}, err
	}
	a := knActor{p: p, k: k}
	if !write {
		return a, nil
	}
	if k.ReadOnly() || k.Role == authz.KNTinh {
		return a, ErrReadOnly
	}
	if k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(k.Support.Reason)) < 10 {
		return a, ErrSupportReason
	}
	return a, nil
}

// coopIn tra HTX qua pf/party và kiểm phạm vi: không có, không phải HTX hoặc ngoài phạm vi → 404; ghi vào HTX
// đã ngừng hoạt động → 422 mechanization.cooperative_invalid.
func (s *Service) coopIn(ctx context.Context, a knActor, ref uuid.UUID, write bool) (partyport.Org, error) {
	if s.Orgs == nil {
		return partyport.Org{}, ErrDependency.WithDetail("chưa nối pf/party")
	}
	o, err := s.Orgs.Get(tenant.WithID(ctx, a.p.TenantID), ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return partyport.Org{}, ErrCoopNotFound
	}
	if err != nil {
		return partyport.Org{}, ErrDependency.WithDetail("không tra được HTX").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative || o.AdminUnitCode == "" || !a.k.Scope.Allows(o.AdminUnitCode) {
		return partyport.Org{}, ErrCoopNotFound
	}
	if write && !o.Active {
		return partyport.Org{}, ErrCoopInvalid.WithDetail("HTX đã ngừng hoạt động")
	}
	return o, nil
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

// MachineLine là một dòng khai báo hiện hành.
type MachineLine struct {
	store.MachineCountRow
	TypeName string
}

// CoopMachines là khai báo của một HTX.
type CoopMachines struct {
	CooperativeRef uuid.UUID
	Name           string
	CommuneCode    string
	Lines          []MachineLine
	Total          int
}

// CommuneTotal là tổng theo loại máy của một xã (= Σ các HTX của xã).
type CommuneTotal struct {
	CommuneCode string
	MachineType string
	TypeName    string
	Quantity    int
}

// MachineOverview là màn "Máy móc HTX": danh sách HTX có khai báo + tổng xã theo loại máy.
type MachineOverview struct {
	Cooperatives  []CoopMachines
	CommuneTotals []CommuneTotal
}

func lines(rows []store.MachineCountRow, cat map[string]mdport.CatalogItem) ([]MachineLine, int) {
	out := make([]MachineLine, 0, len(rows))
	total := 0
	for _, r := range rows {
		out = append(out, MachineLine{MachineCountRow: r, TypeName: cat[r.MachineType].Name})
		total += r.Quantity
	}
	return out, total
}

// MachineCountsOverview trả khai báo của mọi HTX trong phạm vi (hoặc một xã) và tổng theo loại của từng xã.
func (s *Service) MachineCountsOverview(ctx context.Context, commune string) (MachineOverview, error) {
	a, err := s.knActor(ctx, false)
	if err != nil {
		return MachineOverview{}, err
	}
	commune = strings.TrimSpace(commune)
	sc := a.k.Scope
	if commune != "" && !sc.Allows(commune) {
		return MachineOverview{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	out := MachineOverview{Cooperatives: []CoopMachines{}, CommuneTotals: []CommuneTotal{}}
	all, units := sc.All, sc.Units
	if commune != "" {
		all, units = false, []string{commune}
	}
	if !all && len(units) == 0 {
		return out, nil
	}
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return MachineOverview{}, err
	}
	var rows []store.MachineCountRow
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.MachineCountsIn(ctx, tx, all, units)
		return e
	}); err != nil {
		return MachineOverview{}, err
	}
	byCoop := map[uuid.UUID][]store.MachineCountRow{}
	order := []uuid.UUID{}
	totals := map[[2]string]int{}
	for _, r := range rows {
		if _, ok := byCoop[r.CooperativeRef]; !ok {
			order = append(order, r.CooperativeRef)
		}
		byCoop[r.CooperativeRef] = append(byCoop[r.CooperativeRef], r)
		totals[[2]string{r.CommuneCode, r.MachineType}] += r.Quantity
	}
	tctx := tenant.WithID(ctx, a.p.TenantID)
	for _, ref := range order {
		rs := byCoop[ref]
		ls, total := lines(rs, cat)
		cm := CoopMachines{CooperativeRef: ref, CommuneCode: rs[0].CommuneCode, Lines: ls, Total: total}
		if s.Orgs != nil {
			if o, err := s.Orgs.Get(tctx, ref); err == nil {
				cm.Name = o.Name
			}
		}
		out.Cooperatives = append(out.Cooperatives, cm)
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

// CoopMachineCounts trả khai báo hiện hành của một HTX.
func (s *Service) CoopMachineCounts(ctx context.Context, ref uuid.UUID) (CoopMachines, error) {
	a, err := s.knActor(ctx, false)
	if err != nil {
		return CoopMachines{}, err
	}
	o, err := s.coopIn(ctx, a, ref, false)
	if err != nil {
		return CoopMachines{}, err
	}
	return s.coopLines(ctx, a.p.TenantID, o)
}

func (s *Service) coopLines(ctx context.Context, tid uuid.UUID, o partyport.Org) (CoopMachines, error) {
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return CoopMachines{}, err
	}
	var rows []store.MachineCountRow
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		rows, e = store.MachineCountsOf(ctx, tx, o.Ref, false)
		return e
	}); err != nil {
		return CoopMachines{}, err
	}
	ls, total := lines(rows, cat)
	return CoopMachines{CooperativeRef: o.Ref, Name: o.Name, CommuneCode: o.AdminUnitCode, Lines: ls, Total: total}, nil
}

// PutMachineCountsInput thay toàn bộ khai báo của HTX.
type PutMachineCountsInput struct {
	CooperativeRef uuid.UUID
	Lines          []domain.CountLine
	Reason         string
	Department     string // chỉ Admin: phòng ban của bản ghi (mặc định trong_trot)
	IdemKey        string
}

// PutMachineCounts thay toàn bộ khai báo của HTX: mỗi loại đổi = một sự kiện (set/removed) trên sổ, bảng chiếu
// cập nhật cùng giao dịch. Gọi lặp cùng Idempotency-Key → khai báo hiện tại, không ghi thêm dòng.
func (s *Service) PutMachineCounts(ctx context.Context, in PutMachineCountsInput) (CoopMachines, error) {
	a, err := s.knActor(ctx, true)
	if err != nil {
		return CoopMachines{}, err
	}
	if len(in.Lines) > domain.MaxMachineCountLines {
		return CoopMachines{}, problem.Validation.WithField("lines", "tối đa 100 dòng")
	}
	for i := range in.Lines {
		in.Lines[i].MachineType = strings.TrimSpace(in.Lines[i].MachineType)
		in.Lines[i].Note = strings.TrimSpace(in.Lines[i].Note)
		l := in.Lines[i]
		if l.MachineType == "" {
			return CoopMachines{}, problem.Validation.WithField("lines.machine_type", "bắt buộc")
		}
		if l.Quantity < 0 || l.Quantity > domain.MaxMachineQuantity {
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
	dept := a.k.Department
	if a.k.Role == authz.KNAdmin {
		dept = strings.TrimSpace(in.Department)
		if dept == "" {
			dept = DefaultDepartment
		}
	}
	if !deptRe.MatchString(dept) {
		return CoopMachines{}, problem.Validation.WithField("department", "mã phòng ban không hợp lệ")
	}
	o, err := s.coopIn(ctx, a, in.CooperativeRef, true)
	if err != nil {
		return CoopMachines{}, err
	}
	if !a.k.Scope.Allows(o.AdminUnitCode) {
		return CoopMachines{}, ErrOutOfScope
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
	if a.k.Support != nil {
		if a.k.Support.OnBehalfOf != uuid.Nil {
			v := a.k.Support.OnBehalfOf
			ob = &v
		}
		id, r := a.k.Support.SessionID, strings.TrimSpace(a.k.Support.Reason)
		sid, sr = &id, &r
	}
	var reason *string
	if r := strings.TrimSpace(in.Reason); r != "" {
		reason = &r
	}
	now := s.now()
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockCoopMachines(ctx, tx, o.Ref); err != nil {
			return err
		}
		if _, done, err := store.RequestRecorded(ctx, tx, in.IdemKey); err != nil || done {
			return err
		}
		rows, err := store.MachineCountsOf(ctx, tx, o.Ref, true)
		if err != nil {
			return err
		}
		cur := map[string]domain.CountLine{}
		for _, r := range rows {
			if r.Active {
				cur[r.MachineType] = domain.CountLine{MachineType: r.MachineType, Quantity: r.Quantity, Note: r.Note}
			}
		}
		for _, ch := range domain.PlanCounts(cur, in.Lines) {
			e := store.MachineCountEvent{ID: ids.New(), TenantID: a.p.TenantID, CooperativeRef: o.Ref, CommuneCode: o.AdminUnitCode,
				MachineType: ch.MachineType, Action: ch.Action, Quantity: ch.Quantity, PrevQuantity: ch.PrevQuantity, PrevNote: ch.PrevNote,
				DepartmentCode: dept, Reason: reason, RequestKey: in.IdemKey, OnBehalfOf: ob, SupportSessionID: sid, SupportReason: sr,
				EventTime: now, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey + "|" + ch.MachineType}
			if ch.Action == domain.CountSet && ch.Note != "" {
				n := ch.Note
				e.Note = &n
			}
			if err := store.InsertMachineCountEvent(ctx, tx, &e); err != nil {
				return err
			}
			if err := store.UpsertMachineCount(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err = ledger.Classify(err); err != nil && !errors.Is(err, ledger.ErrDuplicate) {
		return CoopMachines{}, err
	}
	return s.coopLines(ctx, a.p.TenantID, o)
}

// MachineCountLogEntry là một dòng lịch sử số lượng (Log chuẩn plan §1.5).
type MachineCountLogEntry struct {
	store.MachineCountEvent
	TypeName string
}

// MachineCountLog trả lịch sử thay đổi số lượng của HTX theo seq.
func (s *Service) MachineCountLog(ctx context.Context, ref uuid.UUID, after int64, limit int) ([]MachineCountLogEntry, error) {
	a, err := s.knActor(ctx, false)
	if err != nil {
		return nil, err
	}
	if _, err := s.coopIn(ctx, a, ref, false); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cat, err := s.typeCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var evs []store.MachineCountEvent
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		evs, e = store.MachineCountEvents(ctx, tx, ref, after, limit)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]MachineCountLogEntry, 0, len(evs))
	for _, e := range evs {
		out = append(out, MachineCountLogEntry{MachineCountEvent: e, TypeName: cat[e.MachineType].Name})
	}
	return out, nil
}

// MachineCounts thực hiện partyport.CoopMachineCounts cho cảnh báo vô hiệu hóa HTX (FN-XA-03.03).
type MachineCounts struct{ Svc *Service }

var _ partyport.CoopMachineCounts = MachineCounts{}

// CoopMachineTotal trả tổng số máy đang khai báo của HTX. Tenant lấy từ context.
func (m MachineCounts) CoopMachineTotal(ctx context.Context, coop uuid.UUID) (int, error) {
	var n int
	err := tenant.FromCtxTx(ctx, m.Svc.DB, func(tx pgx.Tx) error {
		var e error
		n, e = store.CoopMachineTotal(ctx, tx, coop)
		return e
	})
	return n, err
}
