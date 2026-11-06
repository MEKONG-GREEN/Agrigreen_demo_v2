package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// View là hộ (bản hiện hành) cho API.
type View struct {
	store.Household
	CommuneName  string
	SpeciesNames map[string]string
	OwnerLabel   string // nhãn che bớt (Display)
	OwnerPhone   string // SĐT che một phần (Mục I.8.1)
	OwnerName    string // họ tên đầy đủ — chỉ chi tiết, người sửa được hộ (Resolve, kho ghi nhật ký)
	Location     *store.Checkin
	CanEdit      bool
	CanDeact     bool
	History      []Change // chỉ chi tiết
	ActorName    string
	Warnings     []string // cảnh báo không chặn của lần ghi (sai số GPS, vị trí giả lập)
	Replayed     bool
}

// Change là một dòng lịch sử HCN-09: người sửa, thời điểm, giá trị cũ → mới.
type Change struct {
	Seq       int64
	Event     string
	ActorRef  uuid.UUID
	ActorName string
	At        time.Time
	Reason    string
	Items     []ChangeItem
}

// ChangeItem là một trường đổi. Họ tên/SĐT chỉ ở dạng che bớt (ADR-0056 mục 4).
type ChangeItem struct{ Field, Old, New string }

// Page là một trang danh sách.
type Page struct {
	Items         []View
	Total         int
	Limit, Offset int
}

// Query lọc danh sách.
type Query struct {
	Commune, Status, LivestockType string
	Limit, Offset                  int
}

// List liệt kê hộ trong phạm vi (Cấp Xã: xã phụ trách — AC-06).
func (s *Service) List(ctx context.Context, q Query) (Page, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Page{}, err
	}
	switch q.Status {
	case "", domain.StatusActive, domain.StatusPending, domain.StatusDeactivated:
	default:
		return Page{}, problem.Validation.WithField("status", "active, pending_location hoặc deactivated")
	}
	if q.LivestockType != "" && !domain.ValidType(q.LivestockType) {
		return Page{}, problem.Validation.WithField("livestock_type", "nông hộ, trang trại nhỏ, vừa hoặc công nghiệp")
	}
	if q.Limit <= 0 {
		q.Limit = defaultPage
	}
	if q.Limit > maxPage || q.Offset < 0 || q.Offset > maxOffset {
		return Page{}, problem.Validation.WithField("limit", "1–200; offset 0–100000")
	}
	f := store.Filter{Status: q.Status, LivestockType: q.LivestockType, Limit: q.Limit, Offset: q.Offset}
	switch x := strings.TrimSpace(q.Commune); {
	case x != "" && !c.sees(x):
		return Page{Items: []View{}, Limit: q.Limit, Offset: q.Offset}, nil
	case x != "":
		f.Communes = []string{x}
	case !c.admin() && !c.k.Scope.All:
		f.Communes = append([]string{}, c.k.Scope.Units...)
	}
	var hhs []store.Household
	var total int
	var cks map[uuid.UUID]store.Checkin
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if hhs, total, e = store.List(ctx, tx, f); e != nil {
			return e
		}
		cks, e = store.Checkins(ctx, tx, checkinRefs(hhs))
		return e
	}); err != nil {
		return Page{}, err
	}
	out := make([]View, 0, len(hhs))
	for _, h := range hhs {
		out = append(out, s.view(c, h, cks))
	}
	s.decorate(ctx, out)
	return Page{Items: out, Total: total, Limit: q.Limit, Offset: q.Offset}, nil
}

func checkinRefs(hhs []store.Household) []uuid.UUID {
	out := []uuid.UUID{}
	for _, h := range hhs {
		if h.CheckinRef != nil {
			out = append(out, *h.CheckinRef)
		}
	}
	return out
}

func (s *Service) view(c caller, h store.Household, cks map[uuid.UUID]store.Checkin) View {
	v := View{Household: h, CanEdit: h.Status != domain.StatusDeactivated && c.canWrite(h.CommuneCode),
		CanDeact: h.Status != domain.StatusDeactivated && c.canDeactivate(h.CommuneCode)}
	if h.CheckinRef != nil {
		if ck, ok := cks[*h.CheckinRef]; ok {
			v.Location = &ck
		}
	}
	return v
}

// decorate điền tên xã, tên vật nuôi, nhãn chủ hộ (che bớt — tối đa maxOwnerLabels lời gọi kho). Lỗi tra cứu không chặn đọc.
func (s *Service) decorate(ctx context.Context, vs []View) {
	species := map[string]string{}
	if s.Catalogs != nil {
		if items, err := s.Catalogs.ListAt(ctx, knport.CatalogCNTSSpecies, s.today()); err == nil {
			for _, it := range items {
				species[it.Code] = it.Name
			}
		}
	}
	communes := map[string]string{}
	for i := range vs {
		vs[i].SpeciesNames = species
		code := vs[i].CommuneCode
		if _, ok := communes[code]; !ok && s.Units != nil {
			if u, err := s.Units.Get(ctx, code, s.today()); err == nil {
				communes[code] = u.Name
			} else {
				communes[code] = ""
			}
		}
		vs[i].CommuneName = communes[code]
		if s.Vault == nil || i >= maxOwnerLabels {
			continue
		}
		d, err := s.Vault.Display(ctx, client.PurposeContact, vs[i].PersonRef)
		if err != nil && !errors.Is(err, client.ErrErased) {
			continue
		}
		vs[i].OwnerLabel, vs[i].OwnerPhone = d.Label, d.MaskedPhone
	}
}

// Get đọc chi tiết hộ kèm lịch sử HCN-09.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (View, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return View{}, err
	}
	var vs []store.Household
	var cks map[uuid.UUID]store.Checkin
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.Versions(ctx, tx, ref); e != nil || len(vs) == 0 {
			return e
		}
		cks, e = store.Checkins(ctx, tx, checkinRefs(vs))
		return e
	}); err != nil {
		return View{}, err
	}
	if len(vs) == 0 || !c.sees(vs[len(vs)-1].CommuneCode) {
		return View{}, ErrNotFound
	}
	v := s.view(c, vs[len(vs)-1], cks)
	one := []View{v}
	s.decorate(ctx, one)
	v = one[0]
	if v.CanEdit && s.Vault != nil {
		// Người sửa được hộ xem họ tên đầy đủ (kho ghi nhật ký mỗi lần); SĐT vẫn che (ADR-0056 mục 3).
		if id, err := s.Vault.Resolve(ctx, client.PurposeFieldSupport, v.PersonRef); err == nil {
			v.OwnerName = id.FullName
		}
	}
	v.History = s.history(ctx, vs, cks, v.SpeciesNames)
	actors := make([]uuid.UUID, 0, len(vs))
	for _, h := range vs {
		actors = append(actors, h.ActorRef)
	}
	names := s.names(ctx, actors)
	for i := range v.History {
		v.History[i].ActorName = names[v.History[i].ActorRef]
	}
	v.ActorName = names[v.ActorRef]
	return v, nil
}

// Input là thân thêm/sửa hộ.
type Input struct {
	domain.Fields
	OwnerName  string  // HCN-02 (thêm: bắt buộc; sửa: rỗng = không đổi)
	OwnerPhone *string // HCN-03 (sửa: nil = giữ; "" = bỏ)
	Checkin    *CheckinInput
	// ConfirmDistinct: cán bộ xác nhận là hộ khác khi có cảnh báo trùng (luồng ngoại lệ FN-10).
	ConfirmDistinct bool
	BaseVersion     *int64
	IdemKey         string
}

func (in Input) phone() string {
	if in.OwnerPhone == nil {
		return ""
	}
	return strings.TrimSpace(*in.OwnerPhone)
}

// checkOwner kiểm HCN-02, HCN-03 (chưa gọi kho).
func checkOwner(name string, phone *string) error {
	p := ErrInvalid
	bad := false
	if !domain.ValidOwnerName(name) {
		p, bad = p.WithField(domain.FieldOwnerName, "bắt buộc, tối đa 200 ký tự"), true
	}
	if phone != nil && strings.TrimSpace(*phone) != "" && !domain.ValidPhoneDigits(strings.TrimSpace(*phone)) {
		p, bad = p.WithField(domain.FieldPhone, "chỉ nhập chữ số"), true
	}
	if bad {
		return p
	}
	return nil
}

// duplicates trả mã hộ Đang dùng cùng xã + ấp có họ tên chủ hộ trùng (đã chuẩn hóa) — luồng ngoại lệ FN-10. So tối đa
// maxDupScan hộ ghi gần nhất; mỗi hộ một lần Resolve (kho ghi nhật ký, mục đích extension.contact). Gọi NGOÀI giao dịch
// ghi.
func (s *Service) duplicates(ctx context.Context, c caller, f domain.Fields, name string, except uuid.UUID) ([]string, error) {
	var hhs []store.Household
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		hhs, e = store.ActiveInCommune(ctx, tx, f.CommuneCode, maxDupScan)
		return e
	}); err != nil {
		return nil, err
	}
	var out []string
	for _, h := range hhs {
		if h.Ref == except || !domain.SameHamlet(h.Hamlet, f.Hamlet) {
			continue
		}
		id, err := s.Vault.Resolve(ctx, client.PurposeContact, h.PersonRef)
		if errors.Is(err, client.ErrErased) || errors.Is(err, client.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if domain.SameName(id.FullName, name) {
			out = append(out, h.Code)
		}
	}
	return out, nil
}

func duplicateProblem(codes []string) error {
	return ErrDuplicate.WithField("confirm_distinct", "trùng hộ "+strings.Join(codes, ", ")).With("duplicates", codes)
}

// Create thêm hộ từ màn danh sách (H1–H3): bắt buộc check-in tại nhà hộ trong cùng yêu cầu (AC-05), trạng thái Đang dùng.
func (s *Service) Create(ctx context.Context, in Input) (View, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return View{}, err
	}
	key := idemKey(in.IdemKey)
	var prev *store.Household
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return View{}, err
	}
	if prev != nil {
		v, err := s.Get(ctx, prev.Ref)
		v.Replayed = true
		return v, err
	}
	name := domain.NormalizeOwnerName(in.OwnerName)
	if err := checkOwner(name, in.OwnerPhone); err != nil {
		return View{}, err
	}
	if err := s.checkFields(ctx, c, &in.Fields); err != nil {
		return View{}, err
	}
	if in.Checkin == nil {
		return View{}, ErrNeedCheckin.WithField("checkin", "chưa check-in tại nhà hộ (AC-05)")
	}
	ck, warn, err := s.checkin(ctx, *in.Checkin)
	if err != nil {
		return View{}, err
	}
	if !in.ConfirmDistinct {
		codes, err := s.duplicates(ctx, c, in.Fields, name, uuid.Nil)
		if err != nil {
			return View{}, err
		}
		if len(codes) > 0 {
			return View{}, duplicateProblem(codes)
		}
	}
	cr, err := s.Vault.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold, FullName: name,
		Phone: in.phone(), AreaCode: in.CommuneCode})
	if err != nil && !(errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil) {
		return View{}, err
	}
	ref, now := ids.New(), s.now()
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if p, e := store.ByIdem(ctx, tx, key); e != nil || p != nil {
			if p != nil {
				ref = p.Ref
			}
			return e
		}
		code, e := store.NextCode(ctx, tx, c.p.TenantID)
		if e != nil {
			return e
		}
		if e := store.InsertKey(ctx, tx, c.p.TenantID, ref, code); e != nil {
			return e
		}
		row := store.Checkin{ID: ids.New(), HouseholdRef: ref, Checkin: ck, ActorRef: c.p.ActorRef}
		if e := store.InsertCheckin(ctx, tx, c.p.TenantID, row); e != nil {
			return e
		}
		h := store.Household{Meta: c.meta(now, key), Ref: ref, Code: code, Event: domain.EventCreated, Status: domain.StatusActive,
			PersonRef: cr.Ref, Fields: in.Fields, CheckinRef: &row.ID}
		return store.Insert(ctx, tx, c.p.TenantID, &h)
	})
	if dup(err) {
		return s.Create(ctx, in) // lần gửi song song cùng khóa đã ghi → trả bản đã ghi
	}
	if err != nil {
		return View{}, err
	}
	v, err := s.Get(ctx, ref)
	v.Warnings = warn
	return v, err
}

// load đọc bản hiện hành, kiểm phạm vi, trạng thái, phiên bản (ngoài khóa).
func (s *Service) load(ctx context.Context, c caller, ref uuid.UUID, base *int64) (*store.Household, error) {
	var h *store.Household
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		h, e = store.Latest(ctx, tx, ref)
		return e
	}); err != nil {
		return nil, err
	}
	return h, check(c, h, base)
}

// check: hộ có, trong phạm vi, chưa vô hiệu hóa, đúng phiên bản.
func check(c caller, h *store.Household, base *int64) error {
	switch {
	case h == nil || !c.sees(h.CommuneCode):
		return ErrNotFound
	case h.Status == domain.StatusDeactivated:
		return ErrDeactivated
	case base == nil || *base != h.Seq:
		return ErrConflict
	}
	return nil
}

// lockLatest khóa hộ trong giao dịch ghi và kiểm lại.
func lockLatest(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID, base *int64) (*store.Household, error) {
	if err := store.Lock(ctx, tx, ref); err != nil {
		return nil, err
	}
	h, err := store.Latest(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	return h, check(c, h, base)
}

// Update sửa thông tin hộ (H4) — không đổi vị trí (đổi vị trí là check-in lại). Họ tên/SĐT đổi qua kho định danh (bản sửa
// gắn vào phiên bản, HCN-09).
func (s *Service) Update(ctx context.Context, ref uuid.UUID, in Input) (View, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return View{}, err
	}
	if in.BaseVersion == nil {
		return View{}, errBase
	}
	key := idemKey(in.IdemKey)
	if v, done, err := s.replayed(ctx, c, key); done || err != nil {
		return v, err
	}
	cur, err := s.load(ctx, c, ref, in.BaseVersion)
	if err != nil {
		return View{}, err
	}
	if !c.canWrite(cur.CommuneCode) {
		return View{}, problem.Forbidden.WithDetail("chỉ cán bộ xã phụ trách, cán bộ tổng hợp Cấp Tỉnh hoặc Admin sửa hộ")
	}
	if err := s.checkFields(ctx, c, &in.Fields); err != nil {
		return View{}, err
	}
	// Họ tên/SĐT: so với giá trị hiện tại ở kho (Resolve có nhật ký) để biết trường nào đổi.
	changedName, changedPhone := false, false
	var amend client.Identity
	name := domain.NormalizeOwnerName(in.OwnerName)
	if name != "" || in.OwnerPhone != nil {
		id, err := s.Vault.Resolve(ctx, client.PurposeContact, cur.PersonRef)
		if err != nil {
			return View{}, err
		}
		if name == "" {
			name = id.FullName
		}
		if err := checkOwner(name, in.OwnerPhone); err != nil {
			return View{}, err
		}
		amend = client.Identity{FullName: name, Phone: id.Phone}
		if in.OwnerPhone != nil {
			amend.Phone = in.phone()
		}
		changedName = name != id.FullName
		changedPhone = !samePhone(amend.Phone, id.Phone)
	}
	fields := domain.Changed(cur.Fields, in.Fields)
	if changedName {
		fields = append(fields, domain.FieldOwnerName)
	}
	if changedPhone {
		fields = append(fields, domain.FieldPhone)
	}
	if len(fields) == 0 {
		return View{}, problem.Validation.WithDetail("không có thay đổi")
	}
	if !in.ConfirmDistinct && (changedName || cur.CommuneCode != in.CommuneCode || !domain.SameHamlet(cur.Hamlet, in.Hamlet)) {
		ownerName := name
		if ownerName == "" {
			id, err := s.Vault.Resolve(ctx, client.PurposeContact, cur.PersonRef)
			if err != nil {
				return View{}, err
			}
			ownerName = id.FullName
		}
		codes, err := s.duplicates(ctx, c, in.Fields, ownerName, ref)
		if err != nil {
			return View{}, err
		}
		if len(codes) > 0 {
			return View{}, duplicateProblem(codes)
		}
	}
	var rev *uuid.UUID
	if changedName || changedPhone {
		rs, ok := client.AsRevisions(s.Vault)
		if !ok {
			return View{}, ErrDependency.WithDetail("kho định danh chưa hỗ trợ lịch sử họ tên, số điện thoại")
		}
		r, err := rs.AmendRevision(ctx, client.PurposeContact, cur.PersonRef, amend)
		if errors.Is(err, client.ErrDuplicate) {
			return View{}, ErrPhoneTaken.WithField(domain.FieldPhone, "số điện thoại đã thuộc chủ thể khác")
		}
		if err != nil {
			return View{}, err
		}
		rev = &r
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		h, e := lockLatest(ctx, tx, c, ref, in.BaseVersion)
		if e != nil {
			return e
		}
		n := *h
		n.Meta, n.Event, n.Fields, n.ChangedFields, n.PIIRevisionRef, n.Reason = c.meta(s.now(), key), domain.EventUpdated, in.Fields,
			fields, rev, ""
		n.OriginOutbreakRef = nil
		return store.Insert(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, ref)
}

func samePhone(a, b string) bool {
	return normalizePhone(a) == normalizePhone(b)
}

// normalizePhone đưa SĐT về dạng 84… như kho (để so trước/sau); không hợp lệ → giữ nguyên.
func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	switch {
	case strings.HasPrefix(p, "0"):
		return "84" + p[1:]
	case strings.HasPrefix(p, "+84"):
		return p[1:]
	}
	return p
}

// replayed: khóa idem đã ghi → trả bản hiện hành (gửi lại cùng thao tác).
func (s *Service) replayed(ctx context.Context, c caller, key string) (View, bool, error) {
	var prev *store.Household
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ByIdem(ctx, tx, key)
		return e
	}); err != nil || prev == nil {
		return View{}, false, err
	}
	v, err := s.Get(ctx, prev.Ref)
	v.Replayed = true
	return v, true, err
}

// CheckinAt check-in tại nhà hộ (H2, H4, BR-08): hộ Chờ bổ sung vị trí → Đang dùng; hộ Đang dùng → đổi vị trí, bắt buộc lý do.
func (s *Service) CheckinAt(ctx context.Context, ref uuid.UUID, base *int64, in CheckinInput, reason, idem string) (View, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return View{}, err
	}
	if base == nil {
		return View{}, errBase
	}
	key := idemKey(idem)
	if v, done, err := s.replayed(ctx, c, key); done || err != nil {
		return v, err
	}
	cur, err := s.load(ctx, c, ref, base)
	if err != nil {
		return View{}, err
	}
	if !c.canWrite(cur.CommuneCode) {
		return View{}, problem.Forbidden.WithDetail("chỉ cán bộ xã phụ trách, cán bộ tổng hợp Cấp Tỉnh hoặc Admin check-in hộ")
	}
	reason = strings.TrimSpace(reason)
	if cur.Status == domain.StatusActive && !domain.ValidReason(reason) {
		return View{}, ErrInvalid.WithField("reason", "đổi vị trí phải nhập lý do (5–1000 ký tự)")
	}
	if reason != "" && (!domain.ValidReason(reason) || exdomainPII(reason)) {
		return View{}, ErrInvalid.WithField("reason", "5–1000 ký tự, không chứa SĐT/CCCD")
	}
	ck, warn, err := s.checkin(ctx, in)
	if err != nil {
		return View{}, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		h, e := lockLatest(ctx, tx, c, ref, base)
		if e != nil {
			return e
		}
		row := store.Checkin{ID: ids.New(), HouseholdRef: ref, Checkin: ck, ActorRef: c.p.ActorRef, Reason: reason}
		if e := store.InsertCheckin(ctx, tx, c.p.TenantID, row); e != nil {
			return e
		}
		n := *h
		n.Meta, n.CheckinRef, n.PIIRevisionRef, n.OriginOutbreakRef, n.Reason = c.meta(s.now(), key), &row.ID, nil, nil, reason
		if h.Status == domain.StatusPending {
			n.Event, n.Status, n.ChangedFields = domain.EventCheckedIn, domain.StatusActive, []string{domain.FieldLocation, domain.FieldStatus}
		} else {
			n.Event, n.ChangedFields = domain.EventRelocated, []string{domain.FieldLocation}
		}
		return store.Insert(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return View{}, err
	}
	v, err := s.Get(ctx, ref)
	v.Warnings = warn
	return v, err
}

// Deactivate vô hiệu hóa hộ (BR-04): chỉ cán bộ tổng hợp Cấp Tỉnh, Admin; bắt buộc lý do; không kích hoạt lại.
func (s *Service) Deactivate(ctx context.Context, ref uuid.UUID, base *int64, reason, idem string) (View, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return View{}, err
	}
	if base == nil {
		return View{}, errBase
	}
	key := idemKey(idem)
	if v, done, err := s.replayed(ctx, c, key); done || err != nil {
		return v, err
	}
	cur, err := s.load(ctx, c, ref, base)
	if err != nil {
		return View{}, err
	}
	if !c.canDeactivate(cur.CommuneCode) {
		return View{}, problem.Forbidden.WithDetail("chỉ cán bộ tổng hợp Cấp Tỉnh (hoặc Admin) vô hiệu hóa hộ (BR-04)")
	}
	reason = strings.TrimSpace(reason)
	if !domain.ValidReason(reason) || exdomainPII(reason) {
		return View{}, ErrInvalid.WithField("reason", "bắt buộc 5–1000 ký tự, không chứa SĐT/CCCD")
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		h, e := lockLatest(ctx, tx, c, ref, base)
		if e != nil {
			return e
		}
		n := *h
		n.Meta, n.Event, n.Status, n.ChangedFields, n.PIIRevisionRef, n.OriginOutbreakRef, n.Reason = c.meta(s.now(), key),
			domain.EventDeactivated, domain.StatusDeactivated, []string{domain.FieldStatus}, nil, nil, reason
		return store.Insert(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, ref)
}
