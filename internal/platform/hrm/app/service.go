// Package app là phần cài đặt của pf/hrm (xem tài liệu gói gốc internal/platform/hrm).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch riêng của module chạy SET LOCAL ROLE r_hrm (ADR-0028). Module mới không có vai đăng nhập cũ: pool
// là vai đăng nhập của đơn vị (app_platform), là thành viên r_hrm.
var DBModule = db.Module("pf/hrm")

// Service cài đặt port.Catalog và các thao tác HTTP của danh mục nhân sự.
type Service struct {
	DB     tenant.Beginner // đã bọc db.ForModule(…, DBModule)
	Clock  clock.Clock
	Access port.Access
	// Rec: hồ sơ nhân sự (W11.22) — nil cho tới UseRecords; khi nil mọi route hồ sơ trả hrm.records_disabled.
	Rec *Records
	// Rep: cấu hình báo cáo Sở W11.28 (UseEgress, tiêu đề cơ quan).
	Rep ReportsConfig
}

var _ port.Catalog = (*Service)(nil)

var errNoPool = errors.New("hrm: chưa nối pool cho giao dịch của module")

// NewService dựng Service; cấu hình quyền thiếu vai ghi → lỗi.
func NewService(b tenant.Beginner, clk clock.Clock, acc port.Access) (*Service, error) {
	if clk == nil {
		clk = clock.System{}
	}
	if len(acc.ManageBy) == 0 {
		return nil, fmt.Errorf("hrm: cấu hình quyền thiếu vai ghi danh mục")
	}
	return &Service{DB: b, Clock: clk, Access: acc}, nil
}

// caller là người gọi đã kiểm quyền.
type caller struct {
	ref, tenant uuid.UUID
	manage      bool
}

// resolve kiểm người gọi: vai ManageBy (Admin — mọi thao tác) hoặc vai ReadBy trong phòng ban chủ (chỉ xem). Ngữ cảnh KN
// (X-AG-Context) thắng vai realm. Ngữ cảnh chỉ xem (ViewOnly, Cấp TW) không ghi.
func (s *Service) resolve(ctx context.Context, write bool) (caller, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return caller{}, problem.Unauthenticated
	}
	c := caller{ref: p.ActorRef, tenant: p.TenantID}
	read := false
	if p.KN != nil {
		r := p.KN.Role
		c.manage = slices.Contains(s.Access.ManageBy, r) && !p.KN.ReadOnly()
		read = c.manage || slices.Contains(s.Access.ManageBy, r) ||
			(slices.Contains(s.Access.ReadBy, r) && (s.Access.Department == "" || p.KN.Department == s.Access.Department))
	} else {
		for _, r := range p.Roles {
			if slices.Contains(s.Access.ManageBy, r) {
				c.manage, read = true, true
			}
		}
	}
	switch {
	case !read:
		return c, problem.Forbidden.WithDetail("danh mục nhân sự: chỉ Admin và Phòng Hành chính – Tổng hợp")
	case write && !c.manage:
		return c, problem.Forbidden.WithDetail("danh mục nhân sự: chỉ Admin được thêm, sửa, ngừng sử dụng (FN-19)")
	}
	return c, nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return errNoPool
	}
	return db.WithModuleTenantTx(ctx, s.DB, DBModule, tid, fn)
}

func (s *Service) ctxTx(ctx context.Context, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return errNoPool
	}
	return db.WithContextTx(ctx, s.DB, DBModule, fn)
}

// day chuẩn hóa ngày hỏi: zero → hôm nay (giờ VN); khác → ngày lịch của at.
func (s *Service) day(at time.Time) time.Time {
	if at.IsZero() {
		return domain.BusinessDay(s.Clock.Now())
	}
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
}

// Status là tình trạng danh mục của tenant người gọi.
type Status struct {
	CanManage    bool
	RuleVersion  string
	Day          time.Time
	Counts       map[string]int // loại → số mục đang dùng
	Placeholders int            // số mục/bảng/tham số hiện hành còn là dữ liệu mẫu (Q-070)
	Scales       int
	Rules        int
}

// Status đếm mục đang dùng theo loại, bảng hệ số và tham số có hiệu lực hôm nay.
func (s *Service) Status(ctx context.Context) (Status, error) {
	c, err := s.resolve(ctx, false)
	if err != nil {
		return Status{}, err
	}
	st := Status{CanManage: c.manage, RuleVersion: domain.RuleVersion, Day: s.day(time.Time{}), Counts: map[string]int{}}
	for _, k := range domain.Kinds {
		st.Counts[string(k)] = 0
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		items, e := store.Current(ctx, tx, "")
		if e != nil {
			return e
		}
		for _, it := range items {
			if it.Status == "active" {
				st.Counts[it.Kind]++
				if it.Placeholder {
					st.Placeholders++
				}
			}
		}
		scales, e := store.ScalesAt(ctx, tx, "", st.Day)
		if e != nil {
			return e
		}
		st.Scales = len(scales)
		for _, sc := range scales {
			if sc.Placeholder {
				st.Placeholders++
			}
		}
		for _, k := range domain.RuleKeys {
			r, ok, e := store.RuleAt(ctx, tx, string(k), st.Day)
			if e != nil {
				return e
			}
			if ok {
				st.Rules++
				if r.Placeholder {
					st.Placeholders++
				}
			}
		}
		return nil
	})
	return st, err
}

func toItem(r store.ItemRow) port.Item {
	return port.Item{Kind: r.Kind, Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Active: r.Status == "active",
		Placeholder: r.Placeholder, Version: r.Version, Attrs: r.Attrs, RecordedAt: r.RecordedAt}
}

func parseKind(kind string) (domain.Kind, error) {
	k, ok := domain.ParseKind(kind)
	if !ok {
		return "", port.ErrKindUnknown.WithDetail("loại " + kind)
	}
	return k, nil
}

// List trả mục hiện hành của loại kind; includeRetired = cả mục đã ngừng sử dụng.
func (s *Service) List(ctx context.Context, kind string, includeRetired bool) ([]port.Item, error) {
	k, err := parseKind(kind)
	if err != nil {
		return nil, err
	}
	c, err := s.resolve(ctx, false)
	if err != nil {
		return nil, err
	}
	var out []port.Item
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		rows, e := store.Current(ctx, tx, string(k))
		for _, r := range rows {
			if includeRetired || r.Status == "active" {
				out = append(out, toItem(r))
			}
		}
		return e
	})
	return out, err
}

// History trả mọi phiên bản của một mục (cũ trước); mã chưa có → hrm.item_not_found.
func (s *Service) History(ctx context.Context, kind, code string) ([]port.Item, error) {
	k, err := parseKind(kind)
	if err != nil {
		return nil, err
	}
	c, err := s.resolve(ctx, false)
	if err != nil {
		return nil, err
	}
	code = domain.NormalizeCode(k, code)
	var out []port.Item
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		rows, e := store.History(ctx, tx, string(k), code)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return port.ErrItemNotFound.WithDetail(string(k) + " " + code)
		}
		for _, r := range rows {
			out = append(out, toItem(r))
		}
		return nil
	})
	return out, err
}

// ItemInput là thân yêu cầu thêm/sửa mục.
type ItemInput struct {
	Code      string
	Name      string
	SortOrder int
	Active    bool
	Attrs     domain.Attrs
}

// WriteResult là kết quả một lần ghi.
type WriteResult struct {
	Code    string
	Version int64
}

// Create thêm mục mới (mã chuẩn hóa — AC-03); mã đã có (kể cả đã ngừng) → hrm.item_exists.
func (s *Service) Create(ctx context.Context, kind string, in ItemInput, idem string) (WriteResult, error) {
	return s.write(ctx, kind, in, idem, true)
}

// Update ghi phiên bản mới của mục đã có (sửa tên, thứ tự, thuộc tính; ngừng/dùng lại bằng Active); mã chưa có →
// hrm.item_not_found.
func (s *Service) Update(ctx context.Context, kind, code string, in ItemInput, idem string) (WriteResult, error) {
	in.Code = code
	return s.write(ctx, kind, in, idem, false)
}

func (s *Service) write(ctx context.Context, kind string, in ItemInput, idem string, create bool) (WriteResult, error) {
	k, err := parseKind(kind)
	if err != nil {
		return WriteResult{}, err
	}
	c, err := s.resolve(ctx, true)
	if err != nil {
		return WriteResult{}, err
	}
	it := domain.Item{Kind: k, Code: domain.NormalizeCode(k, in.Code), Name: in.Name, SortOrder: in.SortOrder, Active: in.Active,
		Attrs: in.Attrs}
	if create {
		it.Active = true
	}
	if err := domain.Validate(it); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	attrs, err := json.Marshal(it.Attrs)
	if err != nil {
		return WriteResult{}, err
	}
	res := WriteResult{Code: it.Code}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		_, exists, e := store.Latest(ctx, tx, string(k), it.Code)
		if e != nil {
			return e
		}
		if create && exists {
			return port.ErrItemExists.WithDetail(string(k) + " " + it.Code)
		}
		if !create && !exists {
			return port.ErrItemNotFound.WithDetail(string(k) + " " + it.Code)
		}
		if it.Active {
			if e := checkItemRefs(ctx, tx, it); e != nil {
				return e
			}
		}
		key := idemKey(idem, "item:"+string(k)+":"+it.Code)
		res.Version, e = store.InsertItem(ctx, tx, ids.New(), c.tenant, it, attrs, false, s.Clock.Now(), c.ref, key)
		return classify(e)
	})
	return res, err
}

// active trả mục đang dùng của (kind, code); ok = false nếu chưa có hoặc đã ngừng.
func active(ctx context.Context, tx pgx.Tx, k domain.Kind, code string) (store.ItemRow, bool, error) {
	r, ok, err := store.Latest(ctx, tx, string(k), code)
	return r, ok && r.Status == "active", err
}

// checkItemRefs kiểm tham chiếu chéo: nhóm chuyên ngành → trình độ; chuyên ngành → (trình độ, nhóm của đúng trình độ đó).
func checkItemRefs(ctx context.Context, tx pgx.Tx, it domain.Item) error {
	switch it.Kind {
	case domain.KindMajorGroup:
		if _, ok, err := active(ctx, tx, domain.KindEducationLevel, it.Attrs.Level); err != nil || !ok {
			return refErr(err, "trình độ "+it.Attrs.Level)
		}
	case domain.KindMajor:
		for lv, g := range it.Attrs.Groups {
			if _, ok, err := active(ctx, tx, domain.KindEducationLevel, lv); err != nil || !ok {
				return refErr(err, "trình độ "+lv)
			}
			r, ok, err := active(ctx, tx, domain.KindMajorGroup, g)
			if err != nil || !ok {
				return refErr(err, "nhóm chuyên ngành "+g)
			}
			var a domain.Attrs
			if err := json.Unmarshal(r.Attrs, &a); err != nil {
				return err
			}
			if a.Level != lv {
				return port.ErrItemReference.WithDetail(fmt.Sprintf("nhóm %s thuộc trình độ %s, không phải %s", g, a.Level, lv))
			}
		}
	}
	return nil
}

func refErr(err error, what string) error {
	if err != nil {
		return err
	}
	return port.ErrItemReference.WithDetail(what)
}

// jsonUnmarshal giải JSON (thuộc tính danh mục đã kiểm khi ghi); lỗi bỏ qua ở nơi gọi đọc trường tùy chọn.
func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// stableKey: có khóa của request → khóa ổn định (gọi lặp đụng khóa trùng → 409, không ghi đôi); không có → ngẫu nhiên.
func stableKey(idem, suffix string) string {
	if idem == "" {
		return "hrm:" + suffix + ":" + ids.New().String()
	}
	return idem + ":" + suffix
}

func idemKey(idem, suffix string) string {
	if idem == "" {
		return "hrm:" + suffix
	}
	return idem + ":" + suffix
}

// ScaleView là bảng hệ số có hiệu lực của một ngạch kèm thông tin ngạch.
type ScaleView struct {
	Grade port.Item
	Scale *store.ScaleRow // nil nếu chưa có bảng hiệu lực tại ngày hỏi
}

// Scales trả mọi ngạch đang dùng kèm bảng hệ số có hiệu lực tại ngày at (zero → hôm nay).
func (s *Service) Scales(ctx context.Context, at time.Time) (time.Time, []ScaleView, error) {
	c, err := s.resolve(ctx, false)
	if err != nil {
		return time.Time{}, nil, err
	}
	day := s.day(at)
	var out []ScaleView
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		grades, e := store.Current(ctx, tx, string(domain.KindGrade))
		if e != nil {
			return e
		}
		scales, e := store.ScalesAt(ctx, tx, "", day)
		if e != nil {
			return e
		}
		by := map[string]store.ScaleRow{}
		for _, sc := range scales {
			by[sc.GradeCode] = sc
		}
		for _, g := range grades {
			if g.Status != "active" {
				continue
			}
			v := ScaleView{Grade: toItem(g)}
			if sc, ok := by[g.Code]; ok {
				v.Scale = &sc
			}
			out = append(out, v)
		}
		return nil
	})
	return day, out, err
}

// ScaleHistory trả mọi phiên bản bảng hệ số của ngạch (theo ngày hiệu lực); ngạch chưa có → hrm.item_not_found.
func (s *Service) ScaleHistory(ctx context.Context, grade string) ([]store.ScaleRow, error) {
	c, err := s.resolve(ctx, false)
	if err != nil {
		return nil, err
	}
	grade = domain.NormalizeCode(domain.KindGrade, grade)
	var out []store.ScaleRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, ok, e := store.Latest(ctx, tx, string(domain.KindGrade), grade); e != nil || !ok {
			if e != nil {
				return e
			}
			return port.ErrItemNotFound.WithDetail("ngạch " + grade)
		}
		var e error
		out, e = store.Scales(ctx, tx, grade)
		return e
	})
	return out, err
}

// AddScale ghi phiên bản bảng hệ số mới của ngạch (BR-02: có ngày hiệu lực; bảng cũ giữ nguyên cho ngày trước đó).
// Ngạch phải đang dùng.
func (s *Service) AddScale(ctx context.Context, sc domain.GradeScale, idem string) (WriteResult, error) {
	c, err := s.resolve(ctx, true)
	if err != nil {
		return WriteResult{}, err
	}
	sc.GradeCode = domain.NormalizeCode(domain.KindGrade, sc.GradeCode)
	if err := domain.ValidateScale(sc); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	res := WriteResult{Code: sc.GradeCode}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, ok, e := active(ctx, tx, domain.KindGrade, sc.GradeCode); e != nil || !ok {
			return refErr(e, "ngạch "+sc.GradeCode)
		}
		var e error
		key := idemKey(idem, "scale:"+sc.GradeCode)
		res.Version, e = store.InsertScale(ctx, tx, ids.New(), c.tenant, sc, false, s.Clock.Now(), c.ref, key)
		return classify(e)
	})
	return res, err
}

// RuleView là tham số có hiệu lực tại một ngày kèm mọi phiên bản.
type RuleView struct {
	Key      string
	Day      time.Time
	Current  *store.RuleRow
	Versions []store.RuleRow
}

// Rule đọc tham số key tại ngày at (zero → hôm nay).
func (s *Service) Rule(ctx context.Context, key string, at time.Time) (RuleView, error) {
	k, ok := domain.ParseRuleKey(key)
	if !ok {
		return RuleView{}, port.ErrRuleUnknown.WithDetail(key)
	}
	c, err := s.resolve(ctx, false)
	if err != nil {
		return RuleView{}, err
	}
	v := RuleView{Key: string(k), Day: s.day(at)}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		if v.Versions, e = store.Rules(ctx, tx, string(k)); e != nil {
			return e
		}
		cur, ok, e := store.RuleAt(ctx, tx, string(k), v.Day)
		if ok {
			v.Current = &cur
		}
		return e
	})
	return v, err
}

// AddRule ghi phiên bản tham số mới (payload kiểm chặt; mã kỷ luật/khen thưởng phải đang dùng).
func (s *Service) AddRule(ctx context.Context, in domain.RuleVersionInput, idem string) (WriteResult, error) {
	if _, ok := domain.ParseRuleKey(string(in.Key)); !ok {
		return WriteResult{}, port.ErrRuleUnknown.WithDetail(string(in.Key))
	}
	c, err := s.resolve(ctx, true)
	if err != nil {
		return WriteResult{}, err
	}
	if err := domain.ValidateRuleVersion(in); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	canon, refs, err := domain.ParseRule(in.Key, in.Payload)
	if err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	in.Payload = canon
	res := WriteResult{Code: string(in.Key)}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if e := checkRuleRefs(ctx, tx, refs); e != nil {
			return e
		}
		var e error
		res.Version, e = store.InsertRule(ctx, tx, ids.New(), c.tenant, in, false, s.Clock.Now(), c.ref, idemKey(idem, "rule:"+string(in.Key)))
		return classify(e)
	})
	return res, err
}

func checkRuleRefs(ctx context.Context, tx pgx.Tx, refs map[domain.Kind][]string) error {
	for k, codes := range refs {
		for _, code := range codes {
			if _, ok, e := active(ctx, tx, k, code); e != nil || !ok {
				return refErr(e, string(k)+" "+code)
			}
		}
	}
	return nil
}

// InstallResult đếm số phiên bản đã ghi khi cài mặc định.
type InstallResult struct {
	Items, Scales, Rules int
}

// InstallDefaults ghi danh mục, bảng hệ số và tham số mặc định (domain.DefaultItems, DefaultScales, DefaultRules) cho
// tenant người gọi — chỉ mã/ngạch/khóa CHƯA có (đã có, kể cả đã ngừng hoặc đã sửa, giữ nguyên). Admin.
func (s *Service) InstallDefaults(ctx context.Context, idem string) (InstallResult, error) {
	c, err := s.resolve(ctx, true)
	if err != nil {
		return InstallResult{}, err
	}
	now := s.Clock.Now()
	var res InstallResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		cur, e := store.Current(ctx, tx, "")
		if e != nil {
			return e
		}
		have := map[string]bool{}
		for _, r := range cur {
			have[r.Kind+":"+r.Code] = true
		}
		for _, d := range domain.DefaultItems() {
			if have[string(d.Kind)+":"+d.Code] {
				continue
			}
			if e := domain.Validate(d.Item); e != nil {
				return e
			}
			attrs, e := json.Marshal(d.Attrs)
			if e != nil {
				return e
			}
			key := idemKey(idem, "default:item:"+string(d.Kind)+":"+d.Code)
			if _, e := store.InsertItem(ctx, tx, ids.New(), c.tenant, d.Item, attrs, d.Placeholder, now, c.ref, key); e != nil {
				return classify(e)
			}
			res.Items++
		}
		scales, e := store.Scales(ctx, tx, "")
		if e != nil {
			return e
		}
		hasScale := map[string]bool{}
		for _, sc := range scales {
			hasScale[sc.GradeCode] = true
		}
		for _, sc := range domain.DefaultScales() {
			if hasScale[sc.GradeCode] {
				continue
			}
			if _, ok, e := active(ctx, tx, domain.KindGrade, sc.GradeCode); e != nil || !ok {
				if e != nil {
					return e
				}
				continue // ngạch mặc định đã bị ngừng — không cài bảng
			}
			key := idemKey(idem, "default:scale:"+sc.GradeCode)
			if _, e := store.InsertScale(ctx, tx, ids.New(), c.tenant, sc, true, now, c.ref, key); e != nil {
				return classify(e)
			}
			res.Scales++
		}
		for _, r := range domain.DefaultRules() {
			vs, e := store.Rules(ctx, tx, string(r.Key))
			if e != nil {
				return e
			}
			if len(vs) > 0 {
				continue
			}
			canon, refs, e := domain.ParseRule(r.Key, r.Payload)
			if e != nil {
				return e
			}
			if checkRuleRefs(ctx, tx, refs) != nil {
				continue // mã khen thưởng/kỷ luật mẫu đã bị ngừng — để Admin khai
			}
			r.Payload = canon
			if _, e := store.InsertRule(ctx, tx, ids.New(), c.tenant, r, true, now, c.ref, idemKey(idem, "default:rule:"+string(r.Key))); e != nil {
				return classify(e)
			}
			res.Rules++
		}
		return nil
	})
	return res, err
}

// Items (port.Catalog) đọc mục đang dùng của loại kind cho tenant của ctx (không kiểm vai — bên gọi đã kiểm).
func (s *Service) Items(ctx context.Context, kind string) ([]port.Item, error) {
	k, err := parseKind(kind)
	if err != nil {
		return nil, err
	}
	var out []port.Item
	err = s.ctxTx(ctx, func(tx pgx.Tx) error {
		rows, e := store.Current(ctx, tx, string(k))
		for _, r := range rows {
			if r.Status == "active" {
				out = append(out, toItem(r))
			}
		}
		return e
	})
	return out, err
}

// ScaleAt (port.Catalog) đọc bảng hệ số của ngạch có hiệu lực tại ngày at (giờ VN).
func (s *Service) ScaleAt(ctx context.Context, grade string, at time.Time) (port.Scale, bool, error) {
	var out port.Scale
	found := false
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		rows, e := store.ScalesAt(ctx, tx, domain.NormalizeCode(domain.KindGrade, grade), domain.BusinessDay(at))
		if e != nil || len(rows) == 0 {
			return e
		}
		r := rows[0]
		out, found = port.Scale{GradeCode: r.GradeCode, ValidFrom: r.ValidFrom, Coefficients: r.Coefficients, HoldMonths: r.HoldMonths,
			Placeholder: r.Placeholder, Version: r.Version}, true
		return nil
	})
	return out, found, err
}

// RuleAt (port.Catalog) đọc tham số key có hiệu lực tại ngày at (giờ VN).
func (s *Service) RuleAt(ctx context.Context, key string, at time.Time) (port.Rule, bool, error) {
	if _, ok := domain.ParseRuleKey(key); !ok {
		return port.Rule{}, false, port.ErrRuleUnknown.WithDetail(key)
	}
	var out port.Rule
	found := false
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		r, ok, e := store.RuleAt(ctx, tx, key, domain.BusinessDay(at))
		if ok {
			out, found = port.Rule{Key: r.Key, ValidFrom: r.ValidFrom, Payload: r.Payload, Placeholder: r.Placeholder, Version: r.Version}, true
		}
		return e
	})
	return out, found, err
}

// classify đổi lỗi DB của pf_hrm sang mã lỗi chung.
func classify(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return problem.Conflict.WithDetail("danh mục đang được ghi đồng thời — thử lại").WithCause(err)
	}
	return err
}
