package app

// KN v7.7 gói P2: phòng ban theo đơn vị (FN-AD-04.03), khu vực (FN-AD-04.02), cây 3 cấp, menu theo
// cấu hình (UR-KT-02). Mọi thay đổi là một dòng sổ mang ảnh chụp trước → sau (AC-CHUNG); bảng chiếu
// cập nhật trong cùng giao dịch (R-21); không xóa cứng (RB-1).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi KN (docs/errors.md, mục pf/party).
var (
	ErrDepartmentNotFound  = problem.New(http.StatusNotFound, "party.department_not_found", "Không tìm thấy phòng ban")
	ErrDepartmentCodeTaken = problem.New(http.StatusConflict, "party.department_code_taken", "Mã phòng ban đã có trong đơn vị (mã không tái sử dụng)")
	ErrDepartmentState     = problem.New(http.StatusConflict, "party.department_state", "Trạng thái phòng ban không cho phép thao tác này")
	ErrDepartmentInUse     = problem.New(http.StatusConflict, "party.department_in_use",
		"Phòng ban đã có tài khoản hoặc dữ liệu — không xóa được, hãy chuyển Ngưng hoạt động")
	ErrDepartmentNotOpen = problem.New(http.StatusConflict, "party.department_not_open", "Phòng ban chưa ở trạng thái Đang áp dụng")
	ErrDepartmentUnit    = problem.New(http.StatusUnprocessableEntity, "party.department_unit_invalid",
		"Phòng ban chỉ khai báo cho Nút cấp 1 (TTKNQG) hoặc cấp 2 (TTKN tỉnh) đang hoạt động")
	ErrRegionNotFound  = problem.New(http.StatusNotFound, "party.region_not_found", "Không tìm thấy khu vực")
	ErrRegionCodeTaken = problem.New(http.StatusConflict, "party.region_code_taken", "Mã khu vực đã có trong tỉnh")
	ErrRegionState     = problem.New(http.StatusConflict, "party.region_state", "Trạng thái khu vực không cho phép thao tác này")
	ErrRegionInUse     = problem.New(http.StatusConflict, "party.region_in_use", "Khu vực còn xã được gán — gỡ hoặc chuyển các xã trước khi ngừng dùng")
	ErrRegionNode      = problem.New(http.StatusUnprocessableEntity, "party.region_node_invalid",
		"Khu vực chỉ gán cho Nút xã (extension_team) đang hoạt động, với khu vực đang dùng của đúng tỉnh")
	ErrMenuAudience = problem.New(http.StatusForbidden, "party.menu_audience_forbidden", "Người gọi không có vai trò của menu yêu cầu")
	ErrNoSeed       = problem.New(http.StatusNotFound, "party.seed_unavailable", "Không có dữ liệu nền cho đơn vị/tỉnh này")
)

// Vai quản trị KN (FN-AD-04.02, 04.03 — chỉ Admin). registry_admin giữ quyền quản trị cây như G1.
var knAdminRoles = []string{authz.RolePlatformAdmin, port.RoleRegistryAdmin}

// KN là use case KN v7.7 của pf/party.
type KN struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
	Reg   *Registry // cây tổ chức: phạm vi, tạo Nút xã, đơn vị hành chính, thống kê thửa

	// Cổng tùy chọn do gốc lắp ráp nối (module khác, qua port).
	Usage   []port.DepartmentUsage // P1 (tài khoản), P3/P4/P5/P7 (bản ghi có department_code)
	Impact  []port.OrgImpactSource // P1 (tài khoản), P4 (kỳ chưa chốt)
	Forms   port.FormAvailability  // P3: menu "Báo cáo đột xuất"
	Session port.SessionContext    // P1: ngữ cảnh phiên
	Names   port.ActorNames        // tên người thao tác cho Log
}

var (
	_ port.Departments = (*KN)(nil)
	_ port.Regions     = (*KN)(nil)
)

func (k *KN) now() time.Time { return k.Clock.Now().UTC() }

func optReason(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func snapshotJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err) // ảnh chụp chỉ gồm chuỗi, số, bool
	}
	return raw
}

// LogEntry là một dòng khung Log của màn Detail (kn-v7.7-plan §1.5).
type LogEntry struct {
	Seq          int64
	Event        string
	ActorRef     uuid.UUID
	ActorDisplay string
	At           time.Time // event_time
	RecordedAt   time.Time
	Changes      []domain.FieldChange
	Reason       *string
}

func (k *KN) toLog(ctx context.Context, evs []store.KNEvent, changes func(store.KNEvent) []domain.FieldChange) []LogEntry {
	out := make([]LogEntry, 0, len(evs))
	refs := make([]uuid.UUID, 0, len(evs))
	for _, e := range evs {
		out = append(out, LogEntry{Seq: e.Seq, Event: e.Action, ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt,
			Changes: changes(e), Reason: e.Reason})
		refs = append(refs, e.ActorRef)
	}
	if k.Names != nil && len(refs) > 0 {
		if names, err := k.Names.DisplayNames(ctx, refs); err == nil {
			for i := range out {
				out[i].ActorDisplay = names[out[i].ActorRef]
			}
		}
	}
	return out
}

func snapshotChanges(e store.KNEvent) []domain.FieldChange { return domain.Diff(e.Before, e.After) }

func clampLog(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

func authenticated(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Phòng ban — cổng đọc cho module khác (port.Departments)
// ---------------------------------------------------------------------------

// DepartmentUnit thực hiện port.Departments: Nút cấp 1/2 gần nhất (chính nó hoặc tổ tiên).
func (k *KN) DepartmentUnit(ctx context.Context, orgRef uuid.UUID) (uuid.UUID, error) {
	var out uuid.UUID
	err := tenant.FromCtxTx(ctx, k.DB, func(tx pgx.Tx) error {
		var e error
		out, e = departmentUnitTx(ctx, tx, orgRef)
		return e
	})
	return out, err
}

func departmentUnitTx(ctx context.Context, tx pgx.Tx, orgRef uuid.UUID) (uuid.UUID, error) {
	cur := orgRef
	for range 16 { // cây 3 cấp (+ HTX) — giới hạn chống vòng
		if cur == uuid.Nil {
			break
		}
		s, err := store.OrgByRef(ctx, tx, cur, false)
		if err != nil {
			return uuid.Nil, err
		}
		if s == nil {
			break
		}
		if s.Kind == port.OrgTTKNNational || s.Kind == port.OrgTTKNProvince {
			return s.Ref, nil
		}
		cur = s.ParentRef
	}
	return uuid.Nil, port.ErrOrgNotFound
}

// UnitDepartments thực hiện port.Departments.
func (k *KN) UnitDepartments(ctx context.Context, unitRef uuid.UUID) ([]port.Department, error) {
	var out []port.Department
	err := tenant.FromCtxTx(ctx, k.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListDepartments(ctx, tx, unitRef, false)
		return e
	})
	return out, err
}

// DepartmentByCode thực hiện port.Departments.
func (k *KN) DepartmentByCode(ctx context.Context, unitRef uuid.UUID, code string) (port.Department, error) {
	var d *port.Department
	err := tenant.FromCtxTx(ctx, k.DB, func(tx pgx.Tx) error {
		var e error
		d, e = store.DepartmentByCode(ctx, tx, unitRef, code)
		return e
	})
	if err != nil {
		return port.Department{}, err
	}
	if d == nil || d.Retired {
		return port.Department{}, port.ErrDepartmentNotFound
	}
	return *d, nil
}

// ---------------------------------------------------------------------------
// Phòng ban — quản trị (FN-AD-04.03)
// ---------------------------------------------------------------------------

// DepartmentInput là dữ liệu tạo/sửa phòng ban.
type DepartmentInput struct {
	OrgRef      uuid.UUID
	Code        string
	Name        string
	Description string
	SortOrder   int
	Status      string // tạo: rỗng = not_open
	MenuProfile string
}

// DepartmentView là phòng ban kèm số tài khoản/bản ghi (nil khi chưa nối nguồn đếm).
type DepartmentView struct {
	port.Department
	Usage *port.Usage
}

// usage cộng số tài khoản/bản ghi từ mọi nguồn; known = false khi chưa nối nguồn nào.
func (k *KN) usage(ctx context.Context, unit uuid.UUID, code string) (port.Usage, bool, error) {
	var u port.Usage
	for _, src := range k.Usage {
		n, err := src.DepartmentUsage(ctx, unit, code)
		if err != nil {
			return port.Usage{}, false, err
		}
		u.Accounts += n.Accounts
		u.Records += n.Records
	}
	return u, len(k.Usage) > 0, nil
}

func (k *KN) checkUnit(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	s, err := store.OrgByRef(ctx, tx, ref, false)
	if err != nil {
		return err
	}
	if s == nil || !s.Active || (s.Kind != port.OrgTTKNNational && s.Kind != port.OrgTTKNProvince) {
		return ErrDepartmentUnit
	}
	return nil
}

// CreateDepartment khai báo một phòng ban cho đơn vị. Gọi lặp cùng Idempotency-Key → như lần đầu.
func (k *KN) CreateDepartment(ctx context.Context, in DepartmentInput, idem string) (port.Department, bool, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return port.Department{}, false, err
	}
	if idem == "" {
		return port.Department{}, false, problem.IdempotencyRequired
	}
	d, err := domain.NormalizeDepartment(port.Department{OrgRef: in.OrgRef, Code: in.Code, Name: in.Name, Description: in.Description,
		SortOrder: in.SortOrder, Status: in.Status, MenuProfile: in.MenuProfile})
	if err != nil {
		return port.Department{}, false, fieldProblem(err)
	}
	if d.OrgRef == uuid.Nil {
		return port.Department{}, false, problem.Validation.WithField("org_ref", "bắt buộc — Nút cấp 1 hoặc cấp 2")
	}
	return k.createDepartment(ctx, p, d, "dept:create:"+idem)
}

func (k *KN) createDepartment(ctx context.Context, p authz.Principal, d port.Department, key string) (port.Department, bool, error) {
	now := k.now()
	var out port.Department
	replayed := false
	err := tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.DeptEventByIdem(ctx, tx, key); err != nil || prev != nil {
			if err != nil {
				return err
			}
			cur, err := store.DepartmentByID(ctx, tx, prev.EntityID, false)
			if err != nil || cur == nil {
				return notFoundOr(err, ErrDepartmentNotFound)
			}
			out, replayed = *cur, true
			return nil
		}
		if err := k.checkUnit(ctx, tx, d.OrgRef); err != nil {
			return err
		}
		if cur, err := store.DepartmentByCode(ctx, tx, d.OrgRef, d.Code); err != nil {
			return err
		} else if cur != nil {
			return ErrDepartmentCodeTaken
		}
		d.ID, d.Retired, d.UpdatedBy, d.UpdatedAt = ids.New(), false, p.ActorRef, now
		e := store.KNEvent{ID: ids.New(), TenantID: p.TenantID, EntityID: d.ID, OrgRef: d.OrgRef, Code: d.Code, Action: domain.DeptCreated,
			Status: d.Status, After: snapshotJSON(domain.DeptSnapshotOf(d)), EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertDeptEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = d
		return store.PutDepartment(ctx, tx, p.TenantID, d, e)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return k.createDepartment(ctx, p, d, key) // bên thua của hai request song song đọc lại
	}
	return out, replayed, err
}

// writeDept ghi một sự kiện phòng ban dưới khóa hàng: mutate trả trạng thái mới (nil = không đổi).
func (k *KN) writeDept(ctx context.Context, id uuid.UUID, key, action string, reason *string,
	mutate func(tx pgx.Tx, cur port.Department) (*port.Department, error)) (port.Department, bool, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return port.Department{}, false, err
	}
	now := k.now()
	var out port.Department
	replayed := false
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.DeptEventByIdem(ctx, tx, key); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if prev.EntityID != id {
				return problem.IdempotencyConflict
			}
			cur, err := store.DepartmentByID(ctx, tx, id, false)
			if err != nil || cur == nil {
				return notFoundOr(err, ErrDepartmentNotFound)
			}
			out, replayed = *cur, true
			return nil
		}
		cur, err := store.DepartmentByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrDepartmentNotFound
		}
		next, err := mutate(tx, *cur)
		if err != nil {
			return err
		}
		if next == nil {
			out = *cur
			return nil
		}
		next.UpdatedBy, next.UpdatedAt = p.ActorRef, now
		e := store.KNEvent{ID: ids.New(), TenantID: p.TenantID, EntityID: id, OrgRef: cur.OrgRef, Code: cur.Code, Action: action,
			Status: next.Status, Before: snapshotJSON(domain.DeptSnapshotOf(*cur)), After: snapshotJSON(domain.DeptSnapshotOf(*next)),
			Reason: reason, EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertDeptEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = *next
		return store.PutDepartment(ctx, tx, p.TenantID, *next, e)
	})
	return out, replayed, err
}

// UpdateDepartment sửa tên, mô tả, thứ tự, bộ menu (PUT). Không đổi gì → không ghi sổ.
func (k *KN) UpdateDepartment(ctx context.Context, id uuid.UUID, in DepartmentInput) (port.Department, error) {
	d, _, err := k.writeDept(ctx, id, "dept:put:"+ids.New().String(), domain.DeptUpdated, nil,
		func(_ pgx.Tx, cur port.Department) (*port.Department, error) {
			if cur.Retired {
				return nil, ErrDepartmentState.WithDetail("phòng ban đã xóa")
			}
			next := cur
			next.Name, next.Description, next.SortOrder, next.MenuProfile = in.Name, in.Description, in.SortOrder, in.MenuProfile
			if next.MenuProfile == "" {
				next.MenuProfile = cur.MenuProfile
			}
			n, err := domain.NormalizeDepartment(next)
			if err != nil {
				return nil, fieldProblem(err)
			}
			if domain.DeptSnapshotOf(n) == domain.DeptSnapshotOf(cur) {
				return nil, nil
			}
			return &n, nil
		})
	return d, err
}

// StatusResult là kết quả đổi trạng thái kèm cảnh báo (FN-AD-04.03: ngưng phòng ban có tài
// khoản/dữ liệu → cảnh báo, dữ liệu cũ giữ nguyên).
type StatusResult struct {
	Department port.Department
	Replayed   bool
	Usage      *port.Usage
	Warnings   []string
}

// ChangeDepartmentStatus đổi Đang áp dụng / Chưa mở / Ngưng hoạt động. Có hiệu lực ngay ở bước 2
// đăng nhập và menu (đọc trực tiếp bảng hiện hành).
func (k *KN) ChangeDepartmentStatus(ctx context.Context, id uuid.UUID, status, reason, idem string) (StatusResult, error) {
	if _, err := authz.Require(ctx, knAdminRoles...); err != nil {
		return StatusResult{}, err
	}
	if idem == "" {
		return StatusResult{}, problem.IdempotencyRequired
	}
	status = strings.TrimSpace(status)
	if !domain.ValidStatus(status) {
		return StatusResult{}, problem.Validation.WithField("status", domain.ErrDeptStatus.Error())
	}
	if status == port.DeptSuspended || strings.TrimSpace(reason) != "" {
		if err := domain.ValidateReason("reason", reason); err != nil {
			return StatusResult{}, fieldProblem(err)
		}
	}
	d, replayed, err := k.writeDept(ctx, id, "dept:status:"+idem, domain.DeptStatusChanged, optReason(reason),
		func(_ pgx.Tx, cur port.Department) (*port.Department, error) {
			if cur.Retired {
				return nil, ErrDepartmentState.WithDetail("phòng ban đã xóa")
			}
			if cur.Status == status {
				return nil, ErrDepartmentState.WithDetail("phòng ban đã ở trạng thái " + domain.StatusLabel(status))
			}
			next := cur
			next.Status = status
			return &next, nil
		})
	if err != nil {
		return StatusResult{}, err
	}
	res := StatusResult{Department: d, Replayed: replayed}
	if status != port.DeptActive {
		u, known, err := k.usage(ctx, d.OrgRef, d.Code)
		if err != nil {
			return StatusResult{}, err
		}
		if known {
			res.Usage = &u
			if u.Accounts > 0 || u.Records > 0 {
				res.Warnings = append(res.Warnings, "Phòng ban còn tài khoản hoặc dữ liệu — dữ liệu cũ giữ nguyên; tài khoản không chọn được phòng ban này khi đăng nhập")
			}
		} else {
			res.Warnings = append(res.Warnings, "Chưa nối nguồn đếm tài khoản/dữ liệu của phòng ban — kiểm tra trước khi ngưng")
		}
	}
	return res, nil
}

// RetireDepartment "xóa" phòng ban (sự kiện retired, không xóa cứng). Chặn khi có tài khoản/dữ liệu
// hoặc khi chưa nối nguồn đếm (mặc định chặn).
func (k *KN) RetireDepartment(ctx context.Context, id uuid.UUID, reason, idem string) (port.Department, bool, error) {
	if _, err := authz.Require(ctx, knAdminRoles...); err != nil {
		return port.Department{}, false, err
	}
	if idem == "" {
		return port.Department{}, false, problem.IdempotencyRequired
	}
	if err := domain.ValidateReason("reason", reason); err != nil {
		return port.Department{}, false, fieldProblem(err)
	}
	return k.writeDept(ctx, id, "dept:retire:"+idem, domain.DeptRetired, optReason(reason),
		func(_ pgx.Tx, cur port.Department) (*port.Department, error) {
			if cur.Retired {
				return nil, ErrDepartmentState.WithDetail("phòng ban đã xóa")
			}
			u, known, err := k.usage(ctx, cur.OrgRef, cur.Code)
			if err != nil {
				return nil, err
			}
			if !known {
				return nil, ErrDependency.WithDetail("chưa nối nguồn đếm tài khoản/dữ liệu của phòng ban — không xóa được")
			}
			if u.Accounts > 0 || u.Records > 0 {
				return nil, ErrDepartmentInUse.With("accounts", u.Accounts).With("records", u.Records)
			}
			next := cur
			next.Retired = true
			return &next, nil
		})
}

// GetDepartment đọc một phòng ban kèm số tài khoản/bản ghi (Admin).
func (k *KN) GetDepartment(ctx context.Context, id uuid.UUID) (DepartmentView, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return DepartmentView{}, err
	}
	var d *port.Department
	if err := tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		d, e = store.DepartmentByID(ctx, tx, id, false)
		return e
	}); err != nil {
		return DepartmentView{}, err
	}
	if d == nil {
		return DepartmentView{}, ErrDepartmentNotFound
	}
	v := DepartmentView{Department: *d}
	if u, known, err := k.usage(ctx, d.OrgRef, d.Code); err != nil {
		return DepartmentView{}, err
	} else if known {
		v.Usage = &u
	}
	return v, nil
}

// ListDepartments liệt kê phòng ban của đơn vị. admin = true: Admin API (kèm số tài khoản, có thể
// gồm phòng ban đã xóa); ngược lại Application API: mọi người đã xác thực (bước 2 đăng nhập), đơn vị
// mặc định là đơn vị của Nút người gọi.
func (k *KN) ListDepartments(ctx context.Context, unit uuid.UUID, includeRetired, admin bool) ([]DepartmentView, uuid.UUID, error) {
	var p authz.Principal
	var err error
	if admin {
		p, err = authz.Require(ctx, knAdminRoles...)
	} else {
		p, err = authenticated(ctx)
		includeRetired = false
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	var ds []port.Department
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if unit == uuid.Nil {
			u, err := departmentUnitTx(ctx, tx, p.OrgRef)
			if errors.Is(err, port.ErrOrgNotFound) {
				return problem.Validation.WithField("org_ref", "bắt buộc khi người gọi không thuộc Nút nào")
			}
			if err != nil {
				return err
			}
			unit = u
		}
		var e error
		ds, e = store.ListDepartments(ctx, tx, unit, includeRetired)
		return e
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	out := make([]DepartmentView, 0, len(ds))
	for _, d := range ds {
		v := DepartmentView{Department: d}
		if admin {
			if u, known, err := k.usage(ctx, d.OrgRef, d.Code); err != nil {
				return nil, uuid.Nil, err
			} else if known {
				v.Usage = &u
			}
		}
		out = append(out, v)
	}
	return out, unit, nil
}

// DepartmentLog trả khung Log của phòng ban (người, thời điểm, cũ → mới, lý do), phân trang theo seq.
func (k *KN) DepartmentLog(ctx context.Context, id uuid.UUID, after int64, limit int) ([]LogEntry, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return nil, err
	}
	var evs []store.KNEvent
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if d, err := store.DepartmentByID(ctx, tx, id, false); err != nil || d == nil {
			return notFoundOr(err, ErrDepartmentNotFound)
		}
		var e error
		evs, e = store.DeptEvents(ctx, tx, id, after, clampLog(limit))
		return e
	})
	if err != nil {
		return nil, err
	}
	return k.toLog(ctx, evs, snapshotChanges), nil
}

// notFoundOr trả err nếu có, ngược lại lỗi không tìm thấy nf.
func notFoundOr(err, nf error) error {
	if err != nil {
		return err
	}
	return nf
}
