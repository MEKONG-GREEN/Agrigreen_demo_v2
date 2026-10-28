package app

// W11.22 (pf 000150, vault 000004) — HỒ SƠ NHÂN SỰ (BRD HCTH v3.0 FN-07, FN-08, FN-20). Xem tài liệu domain/staff.go,
// domain/perm.go. Dữ liệu cá nhân chỉ đi qua bộ nhớ tới/từ kho định danh (RB-3, R-04): định danh (họ tên, SĐT, địa
// chỉ) + ngăn hrm.profile với mục đích hrm.profile; thẻ Thống kê BC Sở (TK-01..06) ở ngăn hrm.sensitive với mục đích
// hrm.sensitive. Mọi lần xem chi tiết, xem thẻ nhạy cảm, xuất danh sách ghi sổ pf_hrm.record_access TRƯỚC khi đọc kho.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// vaultArea là mã địa bàn của chủ thể hồ sơ nhân sự ở kho: khóa công khai không phân phối xuống thiết bị theo xã.
const vaultArea = "hrm"

// Records là phần cấu hình hồ sơ nhân sự của Service (UseRecords).
type Records struct {
	Vault    client.Client
	Profiles client.Profiles
	Units    port.Units
	Enabled  bool
}

// UseRecords nối kho định danh, danh mục đơn vị nội bộ và cờ bật hồ sơ (port.EnvRecords).
func (s *Service) UseRecords(v client.Client, u port.Units, enabled bool) {
	r := &Records{Vault: v, Units: u, Enabled: enabled}
	if v != nil {
		r.Profiles, _ = client.AsProfiles(v)
	}
	s.Rec = r
}

// rc là người gọi hồ sơ đã giải quyền.
type rc struct {
	ref, tenant, unitRef uuid.UUID
	group                string
	unit                 string // đơn vị nội bộ của người gọi (KNContext.InternalUnit)
	readOnly             bool
	m                    domain.Matrix
}

func (s *Service) recordSession(ctx context.Context) (port.SessionContext, error) {
	if s.Access.Session == nil {
		return port.SessionContext{}, problem.Forbidden.WithDetail("hồ sơ nhân sự: cần ngữ cảnh phiên Khuyến nông")
	}
	k, err := s.Access.Session(ctx)
	if err != nil {
		return port.SessionContext{}, problem.Forbidden.WithDetail("hồ sơ nhân sự: cần ngữ cảnh phiên Khuyến nông").WithCause(err)
	}
	return k, nil
}

// group suy nhóm vai của ma trận từ ngữ cảnh phiên KN (không lấy từ client).
func (s *Service) group(k port.SessionContext) string {
	switch {
	case slices.Contains(s.Access.ManageBy, k.Role):
		return domain.GroupAdmin
	case slices.Contains(s.Access.ReadBy, k.Role) && k.Department == s.Access.Department && !k.ViewOnly:
		return domain.GroupHCTH
	case k.Role == "kn_tinh" && k.Manager:
		return domain.GroupManager
	case k.Role == "kn_tinh":
		return domain.GroupTinh
	case k.Role == "kn_xa":
		return domain.GroupXa
	}
	return domain.GroupTW
}

// recCaller kiểm cờ bật, ngữ cảnh KN và nạp ma trận hiệu lực (nhóm ← dòng nhóm ← dòng người dùng).
func (s *Service) recCaller(ctx context.Context) (rc, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return rc{}, problem.Unauthenticated
	}
	if s.Rec == nil || !s.Rec.Enabled {
		return rc{}, port.ErrRecordsDisabled
	}
	if s.Rec.Vault == nil || s.Rec.Profiles == nil {
		return rc{}, client.ProblemProfilesUnsupported
	}
	k, err := s.recordSession(ctx)
	if err != nil {
		return rc{}, err
	}
	c := rc{ref: p.ActorRef, tenant: p.TenantID, unitRef: k.UnitRef, group: s.group(k), unit: k.InternalUnit,
		readOnly: k.ReadOnly}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		m, e := s.matrixOf(ctx, tx, c.group, c.ref)
		c.m = m
		return e
	})
	return c, err
}

func (s *Service) matrixOf(ctx context.Context, tx pgx.Tx, group string, user uuid.UUID) (domain.Matrix, error) {
	rows, err := store.PermRules(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	var role, usr domain.Matrix
	for _, r := range rows {
		var target *domain.Matrix
		switch {
		case r.Kind == domain.GranteeRole && r.Grantee == group:
			target = &role
		case r.Kind == domain.GranteeUser && r.Grantee == user.String():
			target = &usr
		default:
			continue
		}
		m, _, e := domain.ParseMatrix(r.Kind, r.Grantee, r.Perms)
		if e != nil {
			return nil, e
		}
		*target = m
	}
	return domain.EffectiveMatrix(group, role, usr), nil
}

// need kiểm người gọi có nhóm quyền p với phạm vi khác none (và được ghi nếu write).
func (c rc) need(p domain.Perm, write bool) error {
	if c.m[p] == domain.ScopeNone || c.m[p] == "" {
		return problem.Forbidden.WithDetail("hồ sơ nhân sự: không có quyền " + string(p) + " (FN-20)")
	}
	if write && c.readOnly {
		return problem.Forbidden.WithDetail("ngữ cảnh chỉ xem không ghi được hồ sơ")
	}
	return nil
}

// on kiểm phạm vi của nhóm quyền p trên một hồ sơ (bản thân: tài khoản gắn hồ sơ; đơn vị: đơn vị công tác hoặc đơn vị
// quản lý cán bộ xã HS-37 tại ngày hỏi bằng đơn vị nội bộ của người gọi).
func (c rc) on(p domain.Perm, st store.StaffRow, snap domain.Snapshot) bool {
	self := st.AccountRef != nil && *st.AccountRef == c.ref
	unit := c.unit != "" && (snap.Unit == c.unit || snap.MgmtUnit == c.unit)
	return c.m[p].Allows(self, unit)
}

// staffFile là hồ sơ đã nạp: phiên bản đầu hiện hành + mọi dòng quá trình.
type staffFile struct {
	head    store.StaffRow
	history []store.StaffRow
	events  []domain.Event
}

func loadFile(ctx context.Context, tx pgx.Tx, id uuid.UUID) (staffFile, error) {
	h, err := store.StaffHistory(ctx, tx, id)
	if err != nil {
		return staffFile{}, err
	}
	if len(h) == 0 {
		return staffFile{}, port.ErrStaffNotFound
	}
	rows, err := store.Events(ctx, tx, id)
	if err != nil {
		return staffFile{}, err
	}
	f := staffFile{head: h[len(h)-1], history: h}
	for _, r := range rows {
		f.events = append(f.events, r.Event)
	}
	return f, nil
}

// degreeRanker trả hàm thứ hạng trình độ (thuộc tính rank của danh mục education_level).
func degreeRanker(ctx context.Context, tx pgx.Tx) (func(string) int, error) {
	rows, err := store.Current(ctx, tx, string(domain.KindEducationLevel))
	if err != nil {
		return nil, err
	}
	rank := map[string]int{}
	for _, r := range rows {
		var a struct {
			Rank int `json:"rank"`
		}
		_ = jsonUnmarshal(r.Attrs, &a)
		rank[r.Code] = a.Rank
	}
	return func(level string) int {
		if level == "" {
			return -1
		}
		return rank[level]
	}, nil
}

// --- Danh sách, xuất (FN-07 B1, B5). ---

// ListFilter là bộ lọc danh sách hồ sơ (không chứa dữ liệu cá nhân ngoài Q — SĐT tra qua chỉ mục mù của kho).
type ListFilter struct {
	At       time.Time // ngày chốt (zero → hôm nay)
	Type     string
	Unit     string // đơn vị công tác hoặc đơn vị quản lý (cán bộ xã)
	Status   string // active | left | none
	Position string
	Grade    string
	Q        string // mã hồ sơ (tiền tố) hoặc số điện thoại
	Offset   int
	Limit    int
}

// StaffItem là một dòng danh sách.
type StaffItem struct {
	ID          uuid.UUID
	Code        string
	Type        domain.StaffType
	Label       string // nhãn che bớt của kho ("Trần T. M.") — danh sách không đọc định danh đầy đủ
	MaskedPhone string
	Snap        domain.Snapshot
	Coefficient string
	Steps       int
}

// ListResult là một trang danh sách.
type ListResult struct {
	Day   time.Time
	Total int
	Items []StaffItem
	Scope domain.Scope
}

type listed struct {
	head store.StaffRow
	snap domain.Snapshot
}

// filterStaff nạp mọi hồ sơ, chiếu tại ngày, lọc theo phạm vi của nhóm quyền p và bộ lọc.
func (s *Service) filterStaff(ctx context.Context, c rc, p domain.Perm, f ListFilter) (time.Time, []listed, map[string]store.ScaleRow, error) {
	day := s.day(f.At)
	var subject uuid.UUID
	if q := strings.TrimSpace(f.Q); rePhoneQuery.MatchString(q) {
		ref, err := s.Rec.Vault.Lookup(ctx, client.PurposeHRMProfile, q)
		switch {
		case errors.Is(err, client.ErrNotFound):
			return day, nil, nil, nil
		case err != nil:
			return day, nil, nil, err
		}
		subject = ref
	}
	var out []listed
	scales := map[string]store.ScaleRow{}
	err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		heads, e := store.CurrentStaff(ctx, tx)
		if e != nil {
			return e
		}
		evs, e := store.Events(ctx, tx, uuid.Nil)
		if e != nil {
			return e
		}
		rank, e := degreeRanker(ctx, tx)
		if e != nil {
			return e
		}
		sc, e := store.ScalesAt(ctx, tx, "", day)
		if e != nil {
			return e
		}
		for _, x := range sc {
			scales[x.GradeCode] = x
		}
		by := map[uuid.UUID][]domain.Event{}
		for _, r := range evs {
			by[r.StaffID] = append(by[r.StaffID], r.Event)
		}
		q := strings.ToUpper(strings.TrimSpace(f.Q))
		for _, h := range heads {
			snap := domain.Project(by[h.StaffID], day, rank)
			switch {
			case !c.on(p, h, snap):
			case f.Type != "" && string(h.Type) != f.Type:
			case f.Unit != "" && snap.Unit != f.Unit && snap.MgmtUnit != f.Unit:
			case f.Status != "" && snap.Status != f.Status:
			case f.Position != "" && snap.Position != f.Position:
			case f.Grade != "" && snap.Grade != f.Grade:
			case subject != uuid.Nil && h.SubjectRef != subject:
			case subject == uuid.Nil && q != "" && !strings.HasPrefix(h.Code, q):
			default:
				out = append(out, listed{h, snap})
			}
		}
		return nil
	})
	return day, out, scales, err
}

func coefficient(scales map[string]store.ScaleRow, grade string, step int) (string, int) {
	sc, ok := scales[grade]
	if !ok {
		return "", 0
	}
	if step >= 1 && step <= len(sc.Coefficients) {
		return sc.Coefficients[step-1].StringFixed(2), len(sc.Coefficients)
	}
	return "", len(sc.Coefficients)
}

// ListStaff trả một trang hồ sơ trong phạm vi xem của người gọi, chiếu tại ngày chốt. Nhãn che bớt lấy từ kho (Display —
// kho ghi sổ mỗi lần); danh sách không ghi nhật ký xem chi tiết.
func (s *Service) ListStaff(ctx context.Context, f ListFilter) (ListResult, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return ListResult{}, err
	}
	if err := c.need(domain.PermView, false); err != nil {
		return ListResult{}, err
	}
	day, rows, scales, err := s.filterStaff(ctx, c, domain.PermView, f)
	if err != nil {
		return ListResult{}, err
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	res := ListResult{Day: day, Total: len(rows), Scope: c.m[domain.PermView], Items: []StaffItem{}}
	if f.Offset < 0 || f.Offset >= len(rows) {
		return res, nil
	}
	end := min(f.Offset+f.Limit, len(rows))
	for _, r := range rows[f.Offset:end] {
		it := StaffItem{ID: r.head.StaffID, Code: r.head.Code, Type: r.head.Type, Snap: r.snap}
		it.Coefficient, it.Steps = coefficient(scales, r.snap.Grade, r.snap.Step)
		if it.Label, it.MaskedPhone, err = s.label(ctx, r.head.SubjectRef); err != nil {
			return ListResult{}, err
		}
		res.Items = append(res.Items, it)
	}
	return res, nil
}

// ExportRow là một dòng tệp xuất (không có trường nhạy cảm TK — BR-03).
type ExportRow = StaffItem

// ExportStaff trả mọi hồ sơ đang lọc trong phạm vi xuất của người gọi (tối đa 5.000) và ghi MỘT dòng nhật ký xuất (mã các hồ
// sơ, bộ lọc) TRƯỚC khi đọc nhãn từ kho.
func (s *Service) ExportStaff(ctx context.Context, f ListFilter) (time.Time, []ExportRow, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return time.Time{}, nil, err
	}
	if err := c.need(domain.PermExport, false); err != nil {
		return time.Time{}, nil, err
	}
	day, rows, scales, err := s.filterStaff(ctx, c, domain.PermExport, f)
	if err != nil {
		return day, nil, err
	}
	if len(rows) > 5000 {
		return day, nil, problem.Validation.WithDetail("xuất tối đa 5.000 hồ sơ — thu hẹp bộ lọc")
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.head.StaffID)
	}
	q := map[string]string{"at": day.Format(time.DateOnly), "type": f.Type, "unit": f.Unit, "status": f.Status, "position": f.Position,
		"grade": f.Grade}
	if f.Q != "" {
		q["q"] = "(có)" // không ghi giá trị tra (có thể là SĐT)
	}
	for k, v := range q {
		if v == "" {
			delete(q, k)
		}
	}
	if err := s.audit(ctx, c, store.Access{Action: "export", StaffIDs: ids, ItemCount: len(ids), Scope: string(c.m[domain.PermExport]), Query: q}); err != nil {
		return day, nil, err
	}
	out := make([]ExportRow, 0, len(rows))
	for _, r := range rows {
		it := StaffItem{ID: r.head.StaffID, Code: r.head.Code, Type: r.head.Type, Snap: r.snap}
		it.Coefficient, it.Steps = coefficient(scales, r.snap.Grade, r.snap.Step)
		if it.Label, it.MaskedPhone, err = s.label(ctx, r.head.SubjectRef); err != nil {
			return day, nil, err
		}
		out = append(out, it)
	}
	return day, out, nil
}

// label đọc nhãn che bớt từ kho (Display, mục đích hrm.profile). Người gọi không có vai kho (403) → nhãn rỗng, không
// lỗi: danh sách vẫn dùng được theo mã hồ sơ. Chủ thể đã xóa định danh → nhãn của kho.
func (s *Service) label(ctx context.Context, ref uuid.UUID) (string, string, error) {
	d, err := s.Rec.Vault.Display(ctx, client.PurposeHRMProfile, ref)
	var p *problem.Problem
	switch {
	case err == nil, errors.Is(err, client.ErrErased):
		return d.Label, d.MaskedPhone, nil
	case errors.As(err, &p) && p.Status == http.StatusForbidden:
		return "", "", nil
	}
	return "", "", err
}

func (s *Service) audit(ctx context.Context, c rc, a store.Access) error {
	a.ID, a.Actor = ids.New(), c.ref
	if a.Scope == "" || a.Scope == string(domain.ScopeNone) {
		a.Scope = string(domain.ScopeSelf)
	}
	return s.tx(ctx, c.tenant, func(tx pgx.Tx) error { return store.InsertAccess(ctx, tx, c.tenant, a, s.Clock.Now()) })
}

// --- Chi tiết (FN-07 B3). ---

// Profile là định danh + ngăn hrm.profile — CHỈ trong bộ nhớ, trả cho người có quyền xem.
type Profile struct {
	FullName       string `pii:"true"`
	Phone          string `pii:"true"`
	Address        string `pii:"true"`
	BirthDate      string `pii:"true"`
	BirthPrecision string
	Gender         string `pii:"true"`
	Email          string `pii:"true"`
	Note           string `pii:"true"`
}

// Detail là hồ sơ chi tiết tại một ngày.
type Detail struct {
	Day         time.Time
	Head        store.StaffRow
	Created     store.StaffRow
	Profile     Profile
	Age         *int
	Snap        domain.Snapshot
	Coefficient string
	Steps       int
	Events      []domain.Event // mọi dòng (kể cả đã thay/hủy) — thẻ quá trình + lịch sử sửa
	History     []store.StaffRow
	Can         map[domain.Perm]bool
	Erased      bool
}

// GetStaff trả hồ sơ chi tiết; GHI NHẬT KÝ XEM (record_access 'view') trước khi đọc kho định danh.
func (s *Service) GetStaff(ctx context.Context, id uuid.UUID, at time.Time) (Detail, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return Detail{}, err
	}
	if err := c.need(domain.PermView, false); err != nil {
		return Detail{}, err
	}
	d := Detail{Day: s.day(at), Can: map[domain.Perm]bool{}}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		f, e := loadFile(ctx, tx, id)
		if e != nil {
			return e
		}
		rank, e := degreeRanker(ctx, tx)
		if e != nil {
			return e
		}
		d.Head, d.Created, d.History, d.Events = f.head, f.history[0], f.history, f.events
		d.Snap = domain.Project(f.events, d.Day, rank)
		today := domain.Project(f.events, s.day(time.Time{}), rank)
		if !c.on(domain.PermView, f.head, today) && !c.on(domain.PermView, f.head, d.Snap) {
			return port.ErrStaffNotFound // không lộ sự tồn tại của hồ sơ ngoài phạm vi
		}
		for _, p := range domain.Perms {
			d.Can[p] = c.m[p] != domain.ScopeNone && c.on(p, f.head, today) && (!c.readOnly || p == domain.PermView || p == domain.PermAudit ||
				p == domain.PermExport)
		}
		d.Can[domain.PermSensitive] = d.Can[domain.PermSensitive] && f.head.Type.HasSensitiveTab()
		sc, e := store.ScalesAt(ctx, tx, d.Snap.Grade, d.Day)
		if e != nil {
			return e
		}
		scales := map[string]store.ScaleRow{}
		for _, x := range sc {
			scales[x.GradeCode] = x
		}
		d.Coefficient, d.Steps = coefficient(scales, d.Snap.Grade, d.Snap.Step)
		return store.InsertAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view", StaffID: &id, ItemCount: 1,
			Scope: string(c.m[domain.PermView]), Actor: c.ref, Query: map[string]string{"at": d.Day.Format(time.DateOnly)}}, s.Clock.Now())
	})
	if err != nil {
		return Detail{}, err
	}
	id0, err := s.Rec.Vault.Resolve(ctx, client.PurposeHRMProfile, d.Head.SubjectRef)
	if errors.Is(err, client.ErrErased) {
		d.Erased = true
		return d, nil
	}
	if err != nil {
		return Detail{}, err
	}
	prof, err := s.Rec.Profiles.GetSection(ctx, client.PurposeHRMProfile, d.Head.SubjectRef, client.SectionHRMProfile)
	if err != nil {
		return Detail{}, err
	}
	d.Profile = Profile{FullName: id0.FullName, Phone: localPhone(id0.Phone), Address: id0.Address, BirthDate: prof["birth_date"],
		BirthPrecision: prof["birth_precision"], Gender: prof["gender"], Email: prof["email"], Note: prof["note"]}
	if b, _, err := domain.ParseDate(d.Profile.BirthDate); err == nil {
		a := ageAt(b, d.Day)
		d.Age = &a
	}
	return d, nil
}

func localPhone(p string) string {
	if strings.HasPrefix(p, "84") && len(p) == 11 {
		return "0" + p[2:]
	}
	return p
}

func ageAt(birth, at time.Time) int {
	a := at.Year() - birth.Year()
	if at.YearDay() < birth.YearDay() {
		a--
	}
	return a
}

// Sensitive trả thẻ Thống kê BC Sở (TK-01..06); GHI NHẬT KÝ 'view_sensitive' trước khi đọc kho (mục đích hrm.sensitive).
func (s *Service) Sensitive(ctx context.Context, id uuid.UUID) (client.Fields, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.need(domain.PermSensitive, false); err != nil {
		return nil, err
	}
	var head store.StaffRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		f, e := loadFile(ctx, tx, id)
		if e != nil {
			return e
		}
		head = f.head
		if !c.on(domain.PermSensitive, f.head, domain.Project(f.events, s.day(time.Time{}), nil)) {
			return port.ErrStaffNotFound
		}
		if !f.head.Type.HasSensitiveTab() {
			return port.ErrNotApplicable.WithDetail("thẻ Thống kê BC Sở chỉ áp dụng viên chức, hợp đồng lao động")
		}
		return store.InsertAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view_sensitive", StaffID: &id, ItemCount: 1,
			Scope: string(c.m[domain.PermSensitive]), Actor: c.ref}, s.Clock.Now())
	})
	if err != nil {
		return nil, err
	}
	return s.Rec.Profiles.GetSection(ctx, client.PurposeHRMSensitive, head.SubjectRef, client.SectionHRMSensitive)
}

// --- Ghi (FN-07 B2, B4; FN-08). ---

// ProfileInput là trường định danh/hồ sơ; con trỏ nil = không đổi (sửa) — tạo mới đòi họ tên, giới tính, ngày sinh, SĐT.
type ProfileInput struct {
	FullName  *string `pii:"true"`
	Phone     *string `pii:"true"`
	Address   *string `pii:"true"`
	BirthDate *string `pii:"true"`
	Gender    *string
	Email     *string `pii:"true"`
	Note      *string `pii:"true"`
}

// CreateInput là hồ sơ mới.
type CreateInput struct {
	Type       string
	Profile    ProfileInput
	Sensitive  client.Fields // tùy chọn (TK-01..06), cần quyền sensitive.view
	AccountRef *uuid.UUID
	Events     []domain.EventInput // quá trình ban đầu: đơn vị + chức vụ bắt buộc; ngạch, vào làm …
}

// WriteStaffResult là kết quả ghi hồ sơ.
type WriteStaffResult struct {
	ID       uuid.UUID
	Code     string
	Version  int64
	Warnings []string
}

var (
	// rePhoneQuery: chuỗi tra là SĐT (tra qua chỉ mục mù của kho); khác → tiền tố mã hồ sơ.
	rePhoneQuery = regexp.MustCompile(`^\+?[0-9][0-9 .-]{7,14}$`)
	reEmail      = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	reNoNote     = regexp.MustCompile(`[()\[\]0-9]`)
)

// checkProfile kiểm dạng trường (FN-07 luồng ngoại lệ, BR-05); trả cảnh báo (tuổi dưới 18). Thông báo không chứa giá trị.
func (s *Service) checkProfile(in ProfileInput, create bool) ([]string, error) {
	v, bad := problem.Validation, false
	var warn []string
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	if create || in.FullName != nil {
		n := str(in.FullName)
		if utf8.RuneCountInString(n) < 2 || utf8.RuneCountInString(n) > 100 || reNoNote.MatchString(n) {
			v, bad = v.WithField("full_name", "bắt buộc, 2–100 ký tự, không chứa ghi chú/số (BR-05)"), true
		}
	}
	if create || in.Phone != nil {
		if str(in.Phone) == "" {
			v, bad = v.WithField("phone", "bắt buộc (HS-12)"), true
		}
	}
	if create || in.Gender != nil {
		if g := str(in.Gender); g != "nam" && g != "nu" {
			v, bad = v.WithField("gender", "nam | nu (HS-03)"), true
		}
	}
	if create || in.BirthDate != nil {
		b, _, err := domain.ParseDate(str(in.BirthDate))
		today := s.day(time.Time{})
		switch {
		case err != nil:
			v, bad = v.WithField("birth_date", "bắt buộc: YYYY-MM-DD, YYYY-MM hoặc YYYY (HS-04)"), true
		case b.After(today):
			v, bad = v.WithField("birth_date", "ngày sinh sau ngày hiện tại"), true
		case ageAt(b, today) < 18:
			warn = append(warn, "Tuổi dưới 18")
		}
	}
	if e := str(in.Email); e != "" {
		if _, err := mail.ParseAddress(e); err != nil || !reEmail.MatchString(e) || len(e) > 254 {
			v, bad = v.WithField("email", "định dạng email (HS-35)"), true
		}
	}
	if utf8.RuneCountInString(str(in.Address)) > 500 {
		v, bad = v.WithField("address", "tối đa 500 ký tự"), true
	}
	if utf8.RuneCountInString(str(in.Note)) > 1000 {
		v, bad = v.WithField("note", "tối đa 1.000 ký tự (HS-32)"), true
	}
	if bad {
		return nil, v
	}
	return warn, nil
}

// profileFields dựng ngăn hrm.profile từ dữ liệu vào (cur: giá trị hiện có, nil khi tạo); trả tên trường đã đổi.
func profileFields(in ProfileInput, cur client.Fields) (client.Fields, []string) {
	out := client.Fields{}
	for k, v := range cur {
		out[k] = v
	}
	var changed []string
	set := func(key string, p *string) {
		if p == nil {
			return
		}
		v := strings.TrimSpace(*p)
		if out[key] != v {
			changed = append(changed, key)
		}
		if v == "" {
			delete(out, key)
		} else {
			out[key] = v
		}
	}
	if in.BirthDate != nil {
		if b, prec, err := domain.ParseDate(strings.TrimSpace(*in.BirthDate)); err == nil {
			f, p := domain.FormatDate(b, prec), string(prec)
			in.BirthDate = &f
			set("birth_precision", &p)
		}
	}
	set("birth_date", in.BirthDate)
	set("gender", in.Gender)
	set("email", in.Email)
	set("note", in.Note)
	slices.Sort(changed)
	return out, slices.Compact(changed)
}

// checkSensitive kiểm thẻ TK theo ngăn kho (tên trường) + mã danh mục dân tộc/tôn giáo + ngày.
func checkSensitive(f client.Fields) (client.Fields, []domain.Ref, error) {
	allowed := map[string]bool{"ethnicity": true, "religion": true, "party_date": true, "hometown": true, "birthplace": true,
		"revolution_date": true}
	out := client.Fields{}
	var refs []domain.Ref
	for k, v := range f {
		v = strings.TrimSpace(v)
		if !allowed[k] {
			return nil, nil, problem.Validation.WithField(k, "không thuộc thẻ Thống kê BC Sở")
		}
		if v == "" {
			continue
		}
		switch k {
		case "ethnicity", "religion":
			kind := domain.KindEthnicity
			if k == "religion" {
				kind = domain.KindReligion
			}
			v = domain.NormalizeCode(kind, v)
			if !domain.ValidCode(kind, v) {
				return nil, nil, problem.Validation.WithField(k, "mã danh mục")
			}
			refs = append(refs, domain.Ref{Kind: kind, Code: v})
		case "party_date", "revolution_date":
			d, p, err := domain.ParseDate(v)
			if err != nil {
				return nil, nil, problem.Validation.WithField(k, "YYYY-MM-DD, YYYY-MM hoặc YYYY")
			}
			v = domain.FormatDate(d, p)
			out[strings.TrimSuffix(k, "_date")+"_precision"] = string(p)
		default:
			if utf8.RuneCountInString(v) > 300 {
				return nil, nil, problem.Validation.WithField(k, "tối đa 300 ký tự")
			}
		}
		out[k] = v
	}
	return out, refs, nil
}

// prepared là một sự kiện đã kiểm.
type prepared struct {
	ev   domain.Event
	refs []domain.Ref
}

// checkEvents kiểm sự kiện trong giao dịch: danh mục đang dùng, bậc ≤ số bậc của bảng hệ số tại ngày hiệu lực, chức vụ
// lãnh đạo phải có ngày bổ nhiệm (HS-20), đơn vị nội bộ có trong danh mục (khi nối), FN-08 vào làm/nghỉ việc, sự kiện
// bậc cần ngạch hiệu lực.
func (s *Service) checkEvents(ctx context.Context, tx pgx.Tx, c rc, t domain.StaffType, existing []domain.Event, add []prepared) error {
	units, err := s.unitSet(ctx, c)
	if err != nil {
		return err
	}
	all := slices.Clone(existing)
	for _, p := range add {
		for _, r := range p.refs {
			if _, ok, e := active(ctx, tx, r.Kind, r.Code); e != nil || !ok {
				return refErr(e, string(r.Kind)+" "+r.Code)
			}
		}
		ev := p.ev
		switch ev.Kind {
		case domain.EventPosition:
			it, _, e := active(ctx, tx, domain.KindPosition, ev.Payload["position"])
			if e != nil {
				return e
			}
			var a domain.Attrs
			_ = jsonUnmarshal(it.Attrs, &a)
			if a.Leader && ev.Payload["appointed_on"] == "" {
				return problem.Validation.WithField("appointed_on", "chức vụ lãnh đạo phải có ngày quyết định bổ nhiệm (HS-20)")
			}
			if ev.Payload["allowance"] == "" && a.Allowance != nil {
				ev.Payload["allowance"] = a.Allowance.StringFixed(2) // HS-19 gợi ý từ danh mục, sửa được
			}
		case domain.EventUnit:
			for _, k := range []string{"unit", "mgmt_unit"} {
				if u := ev.Payload[k]; u != "" && units != nil && !units[u] {
					return problem.Validation.WithField(k, "đơn vị nội bộ không có hoặc đã ngừng")
				}
			}
		case domain.EventGrade, domain.EventStep:
			grade := ev.Payload["grade"]
			if ev.Kind == domain.EventStep {
				grade = domain.Project(all, ev.ValidFrom, nil).Grade
				if grade == "" {
					return port.ErrStepOutOfRange.WithDetail("chưa có ngạch hiệu lực tại ngày nâng bậc")
				}
			}
			sc, e := store.ScalesAt(ctx, tx, grade, ev.ValidFrom)
			if e != nil {
				return e
			}
			step, _ := strconv.Atoi(ev.Payload["step"])
			if len(sc) == 0 || step > len(sc[0].Coefficients) {
				return port.ErrStepOutOfRange.WithDetail(fmt.Sprintf("ngạch %s tại %s", grade, ev.ValidFrom.Format(time.DateOnly)))
			}
			if r := ev.Payload["reserved_coeff"]; r != "" {
				if d, _ := decimal.NewFromString(r); d.IsZero() {
					delete(ev.Payload, "reserved_coeff")
				}
			}
		}
		all = append(all, ev)
	}
	if err := domain.ValidateWork(all); err != nil {
		return problem.Validation.WithDetail(err.Error())
	}
	return nil
}

// unitSet trả tập mã đơn vị nội bộ đang dùng của đơn vị người gọi; nil = không kiểm (chưa nối hoặc Admin không Nút).
// P0-POOL-KN-2 (#139): unitSet được gọi TRONG giao dịch (checkEvents, checkDocRefs) — dùng bản đã nạp trước giao dịch
// (preloadUnits) nếu có; danh mục đơn vị nội bộ là port của pf/accounts, mở giao dịch riêng trên CHUNG pool.
func (s *Service) unitSet(ctx context.Context, c rc) (map[string]bool, error) {
	if s.Rec.Units == nil || c.unitRef == uuid.Nil {
		return nil, nil
	}
	if m, ok := ctx.Value(unitsKey{}).(unitsMemo); ok && m.ref == c.unitRef {
		return m.set, m.err
	}
	return s.loadUnitSet(ctx, c.unitRef)
}

type unitsKey struct{}

// unitsMemo là tập đơn vị nội bộ (và lỗi đọc) đã nạp NGOÀI giao dịch cho đơn vị ref của người gọi.
type unitsMemo struct {
	ref uuid.UUID
	set map[string]bool
	err error
}

// preloadUnits (P0-POOL-KN-2) nạp tập đơn vị nội bộ của đơn vị người gọi TRƯỚC giao dịch ghi; lỗi đọc chỉ trả khi thân
// giao dịch thực sự cần (unitSet) — như trước. Không nối danh mục / không có ngữ cảnh → ctx giữ nguyên.
func (s *Service) preloadUnits(ctx context.Context) context.Context {
	if s.Rec == nil || s.Rec.Units == nil {
		return ctx
	}
	k, err := s.recordSession(ctx)
	if err != nil || k.UnitRef == uuid.Nil {
		return ctx
	}
	set, err := s.loadUnitSet(ctx, k.UnitRef)
	return context.WithValue(ctx, unitsKey{}, unitsMemo{ref: k.UnitRef, set: set, err: err})
}

func (s *Service) loadUnitSet(ctx context.Context, unitRef uuid.UUID) (map[string]bool, error) {
	us, err := s.Rec.Units.InternalUnits(ctx, unitRef)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, u := range us {
		if u.Active {
			out[u.Code] = true
		}
	}
	return out, nil
}

func normEvents(t domain.StaffType, in []domain.EventInput) ([]prepared, error) {
	out := make([]prepared, 0, len(in))
	for i, e := range in {
		ev, refs, err := domain.NormalizeEvent(t, e)
		if err != nil {
			return nil, problem.Validation.WithField(fmt.Sprintf("events[%d]", i), err.Error())
		}
		out = append(out, prepared{ev, refs})
	}
	return out, nil
}

// CreateStaff thêm hồ sơ (FN-07 B2): kiểm mọi thứ trước, rồi tạo chủ thể ở kho (định danh + ngăn), rồi ghi sổ hồ sơ + quá
// trình ban đầu với mã tự sinh. Trùng SĐT → hrm.duplicate_person (kho chống trùng bằng chỉ mục mù).
func (s *Service) CreateStaff(ctx context.Context, in CreateInput, idem string) (WriteStaffResult, error) {
	ctx = s.preloadUnits(ctx) // P0-POOL-KN-2: đơn vị nội bộ đọc ngoài giao dịch
	c, err := s.recCaller(ctx)
	if err != nil {
		return WriteStaffResult{}, err
	}
	if err := c.need(domain.PermEdit, true); err != nil {
		return WriteStaffResult{}, err
	}
	t, ok := domain.ParseStaffType(in.Type)
	if !ok {
		return WriteStaffResult{}, problem.Validation.WithField("type", "vien_chuc | hop_dong | can_bo_xa (HS-15)")
	}
	warn, err := s.checkProfile(in.Profile, true)
	if err != nil {
		return WriteStaffResult{}, err
	}
	evs, err := normEvents(t, in.Events)
	if err != nil {
		return WriteStaffResult{}, err
	}
	kinds := map[domain.EventKind]bool{}
	for _, e := range evs {
		kinds[e.ev.Kind] = true
	}
	if !kinds[domain.EventUnit] || !kinds[domain.EventPosition] {
		return WriteStaffResult{}, problem.Validation.WithField("events", "bắt buộc đơn vị công tác (HS-16) và chức vụ (HS-17)")
	}
	var sens client.Fields
	var sensRefs []domain.Ref
	if len(in.Sensitive) > 0 {
		if err := c.need(domain.PermSensitive, true); err != nil {
			return WriteStaffResult{}, err
		}
		if !t.HasSensitiveTab() {
			return WriteStaffResult{}, port.ErrNotApplicable.WithDetail("thẻ Thống kê BC Sở chỉ áp dụng viên chức, hợp đồng lao động")
		}
		if sens, sensRefs, err = checkSensitive(in.Sensitive); err != nil {
			return WriteStaffResult{}, err
		}
	}
	// Phạm vi ghi khi tạo: toàn Trung tâm, hoặc "đơn vị mình" với đơn vị ban đầu là đơn vị của người gọi.
	probe := store.StaffRow{AccountRef: in.AccountRef}
	if !c.on(domain.PermEdit, probe, domain.Project(eventsOf(evs), s.day(time.Time{}), nil)) && c.m[domain.PermEdit] != domain.ScopeAll {
		return WriteStaffResult{}, problem.Forbidden.WithDetail("chỉ thêm được hồ sơ thuộc phạm vi của mình (FN-20)")
	}
	// Kiểm quá trình + danh mục trước khi chạm kho (giảm chủ thể mồ côi).
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		for _, r := range sensRefs {
			if _, ok, e := active(ctx, tx, r.Kind, r.Code); e != nil || !ok {
				return refErr(e, string(r.Kind)+" "+r.Code)
			}
		}
		return s.checkEvents(ctx, tx, c, t, nil, evs)
	}); err != nil {
		return WriteStaffResult{}, err
	}

	prof, _ := profileFields(in.Profile, nil)
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	created, err := s.Rec.Vault.Create(ctx, client.PurposeHRMProfile, client.Identity{Kind: client.SubjectPerson,
		FullName: str(in.Profile.FullName), Phone: str(in.Profile.Phone), Address: str(in.Profile.Address), AreaCode: vaultArea})
	if errors.Is(err, client.ErrDuplicate) {
		return WriteStaffResult{}, port.ErrDuplicatePerson
	}
	if err != nil {
		return WriteStaffResult{}, err
	}
	if err := s.Rec.Profiles.PutSection(ctx, client.PurposeHRMProfile, created.Ref, client.SectionHRMProfile, prof); err != nil {
		return WriteStaffResult{}, err
	}
	if sens != nil {
		if err := s.Rec.Profiles.PutSection(ctx, client.PurposeHRMSensitive, created.Ref, client.SectionHRMSensitive, sens); err != nil {
			return WriteStaffResult{}, err
		}
	}
	res := WriteStaffResult{ID: ids.New(), Warnings: warn}
	now := s.Clock.Now()
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if e := store.LockCodes(ctx, tx, c.tenant); e != nil {
			return e
		}
		n, e := store.MaxCodeNumber(ctx, tx, t.CodePrefix())
		if e != nil {
			return e
		}
		res.Code = domain.StaffCode(t, n+1)
		row := store.StaffRow{ID: ids.New(), StaffID: res.ID, Code: res.Code, Type: t, SubjectRef: created.Ref, AccountRef: in.AccountRef,
			Change: "created", Actor: c.ref}
		if len(sens) > 0 {
			row.ChangedFields = []string{"sensitive"}
		}
		if res.Version, e = store.InsertStaff(ctx, tx, c.tenant, row, now, stableKey(idem, "staff:create")); e != nil {
			return classify(e)
		}
		for i, p := range evs {
			p.ev.ID, p.ev.Actor = ids.New(), c.ref
			if _, e := store.InsertEvent(ctx, tx, c.tenant, res.ID, p.ev, now, stableKey(idem, fmt.Sprintf("staff:create:ev:%d", i))); e != nil {
				return classify(e)
			}
		}
		return nil
	})
	return res, err
}

func eventsOf(ps []prepared) []domain.Event {
	out := make([]domain.Event, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ev)
	}
	return out
}

// editable nạp hồ sơ và kiểm quyền ghi (profile.edit, phạm vi tại hôm nay).
func (s *Service) editable(ctx context.Context, tx pgx.Tx, c rc, id uuid.UUID, p domain.Perm) (staffFile, error) {
	f, err := loadFile(ctx, tx, id)
	if err != nil {
		return f, err
	}
	snap := domain.Project(f.events, s.day(time.Time{}), nil)
	if !c.on(domain.PermView, f.head, snap) && !c.on(p, f.head, snap) {
		return f, port.ErrStaffNotFound
	}
	if !c.on(p, f.head, snap) {
		return f, problem.Forbidden.WithDetail("hồ sơ ngoài phạm vi sửa của người gọi (FN-20)")
	}
	return f, nil
}

// UpdateProfile sửa định danh/ngăn hrm.profile và tài khoản gắn; sổ ghi TÊN trường đã đổi ("Lịch sử sửa").
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, in ProfileInput, account *uuid.UUID, clearAccount bool, idem string) (WriteStaffResult, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return WriteStaffResult{}, err
	}
	if err := c.need(domain.PermEdit, true); err != nil {
		return WriteStaffResult{}, err
	}
	warn, err := s.checkProfile(in, false)
	if err != nil {
		return WriteStaffResult{}, err
	}
	var f staffFile
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) (e error) { f, e = s.editable(ctx, tx, c, id, domain.PermEdit); return e }); err != nil {
		return WriteStaffResult{}, err
	}
	ref := f.head.SubjectRef
	var changed []string
	if in.FullName != nil || in.Phone != nil || in.Address != nil {
		cur, err := s.Rec.Vault.Resolve(ctx, client.PurposeHRMProfile, ref)
		if err != nil {
			return WriteStaffResult{}, err
		}
		next := cur
		pick := func(name string, p *string, dst *string, same func(a, b string) bool) {
			if p == nil {
				return
			}
			v := strings.TrimSpace(*p)
			if !same(*dst, v) {
				changed = append(changed, name)
			}
			*dst = v
		}
		eq := func(a, b string) bool {
			return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
		}
		pick("full_name", in.FullName, &next.FullName, eq)
		pick("phone", in.Phone, &next.Phone, func(a, b string) bool { return localPhone(a) == b || a == b })
		pick("address", in.Address, &next.Address, eq)
		if len(changed) > 0 {
			err := s.Rec.Profiles.Amend(ctx, client.PurposeHRMProfile, ref, next)
			if errors.Is(err, client.ErrDuplicate) {
				return WriteStaffResult{}, port.ErrDuplicatePerson
			}
			if err != nil {
				return WriteStaffResult{}, err
			}
		}
	}
	if in.BirthDate != nil || in.Gender != nil || in.Email != nil || in.Note != nil {
		cur, err := s.Rec.Profiles.GetSection(ctx, client.PurposeHRMProfile, ref, client.SectionHRMProfile)
		if err != nil {
			return WriteStaffResult{}, err
		}
		next, ch := profileFields(in, cur)
		if len(ch) > 0 {
			if err := s.Rec.Profiles.PutSection(ctx, client.PurposeHRMProfile, ref, client.SectionHRMProfile, next); err != nil {
				return WriteStaffResult{}, err
			}
			changed = append(changed, ch...)
		}
	}
	res := WriteStaffResult{ID: id, Code: f.head.Code, Version: f.head.Seq, Warnings: warn}
	newAcc := f.head.AccountRef
	accChanged := false
	if clearAccount && newAcc != nil {
		newAcc, accChanged = nil, true
	} else if account != nil && (newAcc == nil || *newAcc != *account) {
		newAcc, accChanged = account, true
	}
	if len(changed) == 0 && !accChanged {
		return res, nil
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		row := f.head
		row.ID, row.Actor, row.AccountRef, row.Change, row.ChangedFields = ids.New(), c.ref, newAcc, "profile", changed
		if len(changed) == 0 {
			row.Change, row.ChangedFields = "account", []string{"account_ref"}
		} else if accChanged {
			row.ChangedFields = append(row.ChangedFields, "account_ref")
		}
		v, e := store.InsertStaff(ctx, tx, c.tenant, row, s.Clock.Now(), stableKey(idem, "staff:"+id.String()+":"+row.Change))
		res.Version = v
		return classify(e)
	})
	return res, err
}

// UpdateSensitive thay thẻ Thống kê BC Sở (cần sensitive.view + profile.edit); sổ ghi tên trường đã đổi.
func (s *Service) UpdateSensitive(ctx context.Context, id uuid.UUID, fields client.Fields, idem string) (WriteStaffResult, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return WriteStaffResult{}, err
	}
	if err := c.need(domain.PermSensitive, true); err != nil {
		return WriteStaffResult{}, err
	}
	if err := c.need(domain.PermEdit, true); err != nil {
		return WriteStaffResult{}, err
	}
	next, refs, err := checkSensitive(fields)
	if err != nil {
		return WriteStaffResult{}, err
	}
	var f staffFile
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		if f, e = s.editable(ctx, tx, c, id, domain.PermSensitive); e != nil {
			return e
		}
		if !f.head.Type.HasSensitiveTab() {
			return port.ErrNotApplicable.WithDetail("thẻ Thống kê BC Sở chỉ áp dụng viên chức, hợp đồng lao động")
		}
		for _, r := range refs {
			if _, ok, e := active(ctx, tx, r.Kind, r.Code); e != nil || !ok {
				return refErr(e, string(r.Kind)+" "+r.Code)
			}
		}
		return nil
	}); err != nil {
		return WriteStaffResult{}, err
	}
	cur, err := s.Rec.Profiles.GetSection(ctx, client.PurposeHRMSensitive, f.head.SubjectRef, client.SectionHRMSensitive)
	if err != nil {
		return WriteStaffResult{}, err
	}
	var changed []string
	for _, k := range []string{"ethnicity", "religion", "party_date", "hometown", "birthplace", "revolution_date"} {
		if cur[k] != next[k] {
			changed = append(changed, k)
		}
	}
	res := WriteStaffResult{ID: id, Code: f.head.Code, Version: f.head.Seq}
	if len(changed) == 0 {
		return res, nil
	}
	if err := s.Rec.Profiles.PutSection(ctx, client.PurposeHRMSensitive, f.head.SubjectRef, client.SectionHRMSensitive, next); err != nil {
		return WriteStaffResult{}, err
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		row := f.head
		row.ID, row.Actor, row.Change, row.ChangedFields = ids.New(), c.ref, "sensitive", changed
		v, e := store.InsertStaff(ctx, tx, c.tenant, row, s.Clock.Now(), stableKey(idem, "staff:"+id.String()+":"+row.Change))
		res.Version = v
		return classify(e)
	})
	return res, err
}

// EventResult là kết quả ghi một dòng quá trình.
type EventResult struct {
	ID      uuid.UUID
	Version int64
}

// AddEvent thêm một dòng quá trình (FN-07 B4: trường có quá trình yêu cầu ngày hiệu lực; FN-08; B6 bậc; B7 khen thưởng).
// replaces khác Nil: dòng mới THAY dòng đó (sửa — cần lý do ≥ 10 ký tự).
func (s *Service) AddEvent(ctx context.Context, id uuid.UUID, in domain.EventInput, replaces uuid.UUID, reason, idem string) (EventResult, error) {
	return s.writeEvent(ctx, id, &in, replaces, reason, idem)
}

// VoidEvent hủy một dòng quá trình (dòng 'void' trỏ tới nó, có lý do ≥ 10 ký tự). Hồ sơ không xóa (BR-01).
func (s *Service) VoidEvent(ctx context.Context, id, eventID uuid.UUID, reason, idem string) (EventResult, error) {
	return s.writeEvent(ctx, id, nil, eventID, reason, idem)
}

func (s *Service) writeEvent(ctx context.Context, id uuid.UUID, in *domain.EventInput, target uuid.UUID, reason, idem string) (EventResult, error) {
	ctx = s.preloadUnits(ctx) // P0-POOL-KN-2: đơn vị nội bộ đọc ngoài giao dịch
	c, err := s.recCaller(ctx)
	if err != nil {
		return EventResult{}, err
	}
	if err := c.need(domain.PermEdit, true); err != nil {
		return EventResult{}, err
	}
	reason = strings.TrimSpace(reason)
	if target != uuid.Nil && (utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 500) {
		return EventResult{}, problem.Validation.WithField("reason", "sửa/hủy dòng quá trình cần lý do 10–500 ký tự")
	}
	res := EventResult{ID: ids.New()}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		f, e := s.editable(ctx, tx, c, id, domain.PermEdit)
		if e != nil {
			return e
		}
		eff := domain.Effective(f.events)
		var old *domain.Event
		if target != uuid.Nil {
			for i := range eff {
				if eff[i].ID == target {
					old = &eff[i]
				}
			}
			if old == nil {
				return port.ErrEventNotFound
			}
		}
		rest := slices.DeleteFunc(slices.Clone(eff), func(e domain.Event) bool { return old != nil && e.ID == old.ID })
		var ev domain.Event
		if in != nil {
			ps, e := normEvents(f.head.Type, []domain.EventInput{*in})
			if e != nil {
				return e
			}
			if old != nil && ps[0].ev.Kind != old.Kind {
				return problem.Validation.WithField("kind", "dòng thay phải cùng loại với dòng bị thay")
			}
			if e := s.checkEvents(ctx, tx, c, f.head.Type, rest, ps); e != nil {
				return e
			}
			ev = ps[0].ev
		} else {
			if e := domain.ValidateWork(rest); e != nil {
				return problem.Validation.WithDetail(e.Error())
			}
			ev = domain.Event{Kind: old.Kind, ValidFrom: old.ValidFrom, Precision: old.Precision, ValidTo: old.ValidTo, Payload: map[string]string{},
				Void: true}
		}
		ev.ID, ev.Actor = res.ID, c.ref
		if old != nil {
			ev.Supersedes, ev.Reason = &old.ID, reason
		}
		res.Version, e = store.InsertEvent(ctx, tx, c.tenant, id, ev, s.Clock.Now(), stableKey(idem, "staff:"+id.String()+":ev"))
		return classify(e)
	})
	return res, err
}

// AccessLog trả nhật ký xem/xuất của hồ sơ (FN-07 NFR: mỗi lần xem chi tiết, xuất), mới trước.
func (s *Service) AccessLog(ctx context.Context, id uuid.UUID, limit int) ([]store.Access, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.need(domain.PermAudit, false); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []store.Access
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		f, e := loadFile(ctx, tx, id)
		if e != nil {
			return e
		}
		if !c.on(domain.PermAudit, f.head, domain.Project(f.events, s.day(time.Time{}), nil)) {
			return port.ErrStaffNotFound
		}
		out, e = store.AccessOf(ctx, tx, id, limit)
		return e
	})
	return out, err
}

// --- Quyền của người gọi, đơn vị, ma trận (FN-20). ---

// MyAccess là quyền hồ sơ hiệu lực của người gọi.
type MyAccess struct {
	Enabled bool
	Group   string
	Unit    string
	Perms   domain.Matrix
}

// MyAccess trả quyền hồ sơ của người gọi (không lỗi khi tắt — để màn hình hiện thông báo).
func (s *Service) MyAccess(ctx context.Context) (MyAccess, error) {
	c, err := s.recCaller(ctx)
	var p *problem.Problem
	if errors.As(err, &p) && p.Code == port.ErrRecordsDisabled.Code {
		return MyAccess{Enabled: false, Perms: domain.Matrix{}}, nil
	}
	if err != nil {
		return MyAccess{}, err
	}
	return MyAccess{Enabled: true, Group: c.group, Unit: c.unit, Perms: c.m}, nil
}

// Units trả đơn vị nội bộ của đơn vị người gọi (bộ lọc, chọn đơn vị công tác); rỗng nếu chưa nối/không có Nút.
func (s *Service) Units(ctx context.Context) ([]port.Unit, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return nil, err
	}
	if c.m[domain.PermView] == domain.ScopeNone {
		return nil, problem.Forbidden
	}
	if s.Rec.Units == nil || c.unitRef == uuid.Nil {
		return []port.Unit{}, nil
	}
	return s.Rec.Units.InternalUnits(ctx, c.unitRef)
}

// PermEntry là một dòng ma trận hiển thị.
type PermEntry struct {
	Kind, Grantee string
	Perms         domain.Matrix
	Default       domain.Matrix // nhóm: mặc định; người dùng: nil
	Version       int64
	Note          string
	RecordedAt    time.Time
	Actor         uuid.UUID
}

// admin kiểm người gọi là Admin của module (FN-20: chỉ Admin phân quyền).
func (s *Service) admin(ctx context.Context, write bool) (rc, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return c, err
	}
	if c.group != domain.GroupAdmin || (write && c.readOnly) {
		return c, problem.Forbidden.WithDetail("phân quyền hồ sơ: chỉ Admin (FN-20)")
	}
	return c, nil
}

// Permissions trả ma trận: mọi nhóm (mặc định + dòng đã ghi) rồi các dòng theo người dùng; history = mọi phiên bản.
func (s *Service) Permissions(ctx context.Context, history bool) ([]PermEntry, error) {
	c, err := s.admin(ctx, false)
	if err != nil {
		return nil, err
	}
	var rows []store.PermRow
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) (e error) { rows, e = store.PermRules(ctx, tx, history); return e }); err != nil {
		return nil, err
	}
	var out []PermEntry
	seen := map[string]bool{}
	for _, r := range rows {
		m, _, e := domain.ParseMatrix(r.Kind, r.Grantee, r.Perms)
		if e != nil {
			return nil, e
		}
		pe := PermEntry{Kind: r.Kind, Grantee: r.Grantee, Perms: m, Version: r.Version, Note: r.Note, RecordedAt: r.RecordedAt, Actor: r.Actor}
		if r.Kind == domain.GranteeRole {
			pe.Default = domain.DefaultMatrix(r.Grantee)
			if !history {
				pe.Perms = domain.EffectiveMatrix(r.Grantee, m, nil)
			}
			seen[r.Grantee] = true
		}
		out = append(out, pe)
	}
	if history {
		return out, nil
	}
	for _, g := range domain.Groups {
		if !seen[g] {
			out = append(out, PermEntry{Kind: domain.GranteeRole, Grantee: g, Perms: domain.DefaultMatrix(g), Default: domain.DefaultMatrix(g)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == domain.GranteeRole
		}
		if out[i].Kind == domain.GranteeRole {
			return slices.Index(domain.Groups, out[i].Grantee) < slices.Index(domain.Groups, out[j].Grantee)
		}
		return out[i].Grantee < out[j].Grantee
	})
	return out, nil
}

// SetPermission ghi phiên bản mới của một dòng ma trận (nhóm hoặc người dùng). Admin cố định (hrm.permission_locked).
func (s *Service) SetPermission(ctx context.Context, kind, grantee string, perms []byte, note, idem string) (int64, error) {
	c, err := s.admin(ctx, true)
	if err != nil {
		return 0, err
	}
	if kind == domain.GranteeRole && grantee == domain.GroupAdmin {
		return 0, port.ErrPermissionLocked
	}
	_, canon, err := domain.ParseMatrix(kind, grantee, perms)
	if err != nil {
		return 0, problem.Validation.WithDetail(err.Error())
	}
	if utf8.RuneCountInString(note) > 500 {
		return 0, problem.Validation.WithField("note", "tối đa 500 ký tự")
	}
	var v int64
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		id := ids.New()
		var e error
		v, e = store.InsertPerm(ctx, tx, id, c.tenant, kind, grantee, canon, note, s.Clock.Now(), c.ref,
			stableKey(idem, "perm:"+kind+":"+grantee))
		return classify(e)
	})
	return v, err
}
