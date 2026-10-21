package app

// W11.07 (D3 C5/H0, Q-068): danh mục đơn vị nội bộ — Ban Giám đốc, văn phòng, phòng (trỏ phòng ban P2), trạm,
// trại, tăng cường. Đề xuất của dev ở Q-068 là "mở rộng department với loại tram/trai"; hiện thực là danh mục
// CẠNH department (phòng trỏ mã phòng ban) để menu, ngữ cảnh đăng nhập và số tài khoản của phòng ban (W11.06)
// không đổi; đặt ở pf/accounts vì đơn vị nội bộ là thuộc tính của NGƯỜI (đơn vị công tác, HS-37) và chỉ có một
// migration cho gói. Admin quản lý; mọi thay đổi là dòng sổ trước → sau; không xóa cứng — "ngừng" có lý do.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/seeddata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi đơn vị nội bộ (docs/errors.md, mục pf/accounts).
var (
	ErrInternalUnitNotFound  = problem.New(http.StatusNotFound, "accounts.internal_unit_not_found", "Không tìm thấy đơn vị nội bộ")
	ErrInternalUnitCodeTaken = problem.New(http.StatusConflict, "accounts.internal_unit_code_taken", "Mã đơn vị nội bộ đã có trong đơn vị (mã không tái sử dụng)")
	ErrInternalUnitState     = problem.New(http.StatusConflict, "accounts.internal_unit_state", "Trạng thái đơn vị nội bộ không cho phép thao tác này")
	ErrInternalUnitOrg       = problem.New(http.StatusUnprocessableEntity, "accounts.internal_unit_org_invalid",
		"Đơn vị nội bộ chỉ khai báo cho Nút cấp 1 (TTKNQG) hoặc cấp 2 (TTKN tỉnh) đang hoạt động")
	ErrInternalUnitDept = problem.New(http.StatusUnprocessableEntity, "accounts.internal_unit_department_invalid",
		"Phòng ban của đơn vị nội bộ loại phòng phải là phòng ban (chưa xóa) của cùng đơn vị")
)

var _ port.InternalUnits = (*Service)(nil)

// UnitInternalUnits thực hiện port.InternalUnits.
func (s *Service) UnitInternalUnits(ctx context.Context, unitRef uuid.UUID, includeInactive bool) ([]port.InternalUnit, error) {
	var out []port.InternalUnit
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListInternalUnits(ctx, tx, unitRef, includeInactive)
		return e
	})
	return out, err
}

// InternalUnitByCode thực hiện port.InternalUnits.
func (s *Service) InternalUnitByCode(ctx context.Context, unitRef uuid.UUID, code string) (port.InternalUnit, error) {
	var u *port.InternalUnit
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		u, e = store.InternalUnitByCode(ctx, tx, unitRef, code)
		return e
	})
	if err != nil {
		return port.InternalUnit{}, err
	}
	if u == nil {
		return port.InternalUnit{}, port.ErrInternalUnitNotFound
	}
	return *u, nil
}

// InternalUnitInput là dữ liệu tạo/sửa đơn vị nội bộ.
type InternalUnitInput struct {
	OrgRef         uuid.UUID
	Code           string
	Kind           string
	Name           string
	DepartmentCode string
	SortOrder      int
}

// checkUnitRefs kiểm Nút (cấp 1/2, đang hoạt động) và phòng ban (chưa xóa, cùng đơn vị) qua cổng pf/party.
func (s *Service) checkUnitRefs(ctx context.Context, u port.InternalUnit, checkOrg bool) error {
	if s.Orgs == nil || s.Departments == nil {
		return ErrDependency.WithDetail("chưa nối pf/party")
	}
	if checkOrg {
		o, err := s.Orgs.Get(ctx, u.OrgRef)
		if errors.Is(err, partyport.ErrOrgNotFound) {
			return ErrInternalUnitOrg
		}
		if err != nil {
			return err
		}
		if !o.Active || (o.Kind != partyport.OrgTTKNNational && o.Kind != partyport.OrgTTKNProvince) {
			return ErrInternalUnitOrg
		}
	}
	if u.DepartmentCode == "" {
		return nil
	}
	d, err := s.Departments.DepartmentByCode(ctx, u.OrgRef, u.DepartmentCode)
	if errors.Is(err, partyport.ErrDepartmentNotFound) || (err == nil && d.Retired) {
		return ErrInternalUnitDept.With("department_code", u.DepartmentCode)
	}
	return err
}

func unitJSON(u port.InternalUnit) []byte {
	b, err := json.Marshal(domain.InternalUnitSnapshotOf(u))
	if err != nil {
		panic(err) // chỉ chuỗi, số, bool
	}
	return b
}

// CreateInternalUnit khai báo một đơn vị nội bộ (Admin). Gọi lặp cùng Idempotency-Key → như lần đầu.
func (s *Service) CreateInternalUnit(ctx context.Context, in InternalUnitInput, idem string) (port.InternalUnit, bool, error) {
	if err := requireKey(idem); err != nil {
		return port.InternalUnit{}, false, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return port.InternalUnit{}, false, err
	}
	if in.OrgRef == uuid.Nil {
		return port.InternalUnit{}, false, problem.Validation.WithField("org_ref", "bắt buộc — Nút cấp 1 hoặc cấp 2")
	}
	u, err := domain.NormalizeInternalUnit(port.InternalUnit{OrgRef: in.OrgRef, Code: in.Code, Kind: in.Kind, Name: in.Name,
		DepartmentCode: in.DepartmentCode, SortOrder: in.SortOrder, Active: true})
	if err != nil {
		return port.InternalUnit{}, false, fieldProblem(err)
	}
	return s.createInternalUnit(ctx, a.p, u, "iunit:create:"+idem)
}

func (s *Service) createInternalUnit(ctx context.Context, p authz.Principal, u port.InternalUnit, key string) (port.InternalUnit, bool, error) {
	if prev, ok, err := s.unitReplay(ctx, p.TenantID, key, uuid.Nil); err != nil || ok {
		return prev, ok, err
	}
	if err := s.checkUnitRefs(ctx, u, true); err != nil {
		return port.InternalUnit{}, false, err
	}
	now := s.now()
	var out port.InternalUnit
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if cur, err := store.InternalUnitByCode(ctx, tx, u.OrgRef, u.Code); err != nil {
			return err
		} else if cur != nil {
			return ErrInternalUnitCodeTaken
		}
		u.ID, u.UpdatedBy, u.UpdatedAt = ids.New(), p.ActorRef, now
		e := store.UnitEvent{ID: ids.New(), TenantID: p.TenantID, Action: domain.UnitCreated, U: u, After: unitJSON(u), EventTime: now,
			ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertUnitEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = u
		return store.PutInternalUnit(ctx, tx, e)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return s.createInternalUnit(ctx, p, u, key) // bên thua của hai request song song đọc lại
	}
	return out, false, err
}

// unitReplay: dòng sổ đã ghi với key → trạng thái hiện hành (id khác Nil phải khớp).
func (s *Service) unitReplay(ctx context.Context, tid uuid.UUID, key string, id uuid.UUID) (port.InternalUnit, bool, error) {
	var out port.InternalUnit
	found := false
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		prev, ok, err := store.UnitEventByIdem(ctx, tx, key)
		if err != nil || !ok {
			return err
		}
		if id != uuid.Nil && prev != id {
			return problem.IdempotencyConflict
		}
		cur, err := store.InternalUnitByID(ctx, tx, prev, false)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrInternalUnitNotFound
		}
		out, found = *cur, true
		return nil
	})
	return out, found, err
}

// writeInternalUnit ghi một sự kiện đơn vị nội bộ dưới khóa hàng: mutate trả trạng thái mới (nil = không đổi).
func (s *Service) writeInternalUnit(ctx context.Context, id uuid.UUID, key, action string, reason *string,
	mutate func(cur port.InternalUnit) (*port.InternalUnit, error)) (port.InternalUnit, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return port.InternalUnit{}, err
	}
	if prev, ok, err := s.unitReplay(ctx, a.p.TenantID, key, id); err != nil || ok {
		return prev, err
	}
	var cur *port.InternalUnit
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		cur, e = store.InternalUnitByID(ctx, tx, id, false)
		return e
	}); err != nil {
		return port.InternalUnit{}, err
	}
	if cur == nil {
		return port.InternalUnit{}, ErrInternalUnitNotFound
	}
	next, err := mutate(*cur)
	if err != nil || next == nil {
		return *cur, err
	}
	if err := s.checkUnitRefs(ctx, *next, false); err != nil {
		return port.InternalUnit{}, err
	}
	now := s.now()
	var out port.InternalUnit
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		locked, err := store.InternalUnitByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if locked == nil || domain.InternalUnitSnapshotOf(*locked) != domain.InternalUnitSnapshotOf(*cur) {
			return problem.Conflict.WithDetail("đơn vị nội bộ vừa được sửa — tải lại")
		}
		n := *next
		n.UpdatedBy, n.UpdatedAt = a.p.ActorRef, now
		e := store.UnitEvent{ID: ids.New(), TenantID: a.p.TenantID, Action: action, U: n, Before: unitJSON(*cur), After: unitJSON(n),
			Reason: reason, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertUnitEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = n
		return store.PutInternalUnit(ctx, tx, e)
	})
	return out, err
}

// UpdateInternalUnit sửa loại, tên, phòng ban, thứ tự (PUT). Mã không đổi. Không đổi gì → không ghi sổ.
func (s *Service) UpdateInternalUnit(ctx context.Context, id uuid.UUID, in InternalUnitInput) (port.InternalUnit, error) {
	return s.writeInternalUnit(ctx, id, "iunit:put:"+ids.New().String(), domain.UnitUpdated, nil,
		func(cur port.InternalUnit) (*port.InternalUnit, error) {
			next := cur
			next.Kind, next.Name, next.DepartmentCode, next.SortOrder = in.Kind, in.Name, in.DepartmentCode, in.SortOrder
			n, err := domain.NormalizeInternalUnit(next)
			if err != nil {
				return nil, fieldProblem(err)
			}
			if domain.InternalUnitSnapshotOf(n) == domain.InternalUnitSnapshotOf(cur) {
				return nil, nil
			}
			return &n, nil
		})
}

// SetInternalUnitActive ngừng (lý do ≥ 10 ký tự) hoặc dùng lại một đơn vị nội bộ. Người đã gán giữ nguyên; đơn
// vị ngừng không chọn được cho thuộc tính vai mới.
func (s *Service) SetInternalUnitActive(ctx context.Context, id uuid.UUID, active bool, reason, idem string) (port.InternalUnit, error) {
	if err := requireKey(idem); err != nil {
		return port.InternalUnit{}, err
	}
	action := domain.UnitReactivated
	if !active {
		action = domain.UnitDeactivated
		if err := domain.CheckKNReason("reason", reason); err != nil {
			return port.InternalUnit{}, fieldProblem(err)
		}
	}
	return s.writeInternalUnit(ctx, id, "iunit:active:"+idem, action, optStr(reason),
		func(cur port.InternalUnit) (*port.InternalUnit, error) {
			if cur.Active == active {
				return nil, ErrInternalUnitState.WithDetail("đơn vị nội bộ đã ở trạng thái này")
			}
			next := cur
			next.Active = active
			return &next, nil
		})
}

// ListInternalUnits liệt kê đơn vị nội bộ (mọi người đã xác thực). unit Nil = đơn vị của ngữ cảnh phiên, hoặc đơn
// vị (Nút cấp 1/2) gần nhất của Nút người gọi.
func (s *Service) ListInternalUnits(ctx context.Context, unit uuid.UUID, includeInactive bool) ([]port.InternalUnit, uuid.UUID, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, uuid.Nil, problem.Unauthenticated
	}
	if s.Departments == nil {
		return nil, uuid.Nil, ErrDependency.WithDetail("chưa nối pf/party")
	}
	var ownerUnit uuid.UUID
	var handled bool
	if s.SessionPolicies.DefaultUnit != nil {
		var err error
		ownerUnit, handled, err = s.SessionPolicies.DefaultUnit(ctx)
		if err != nil {
			return nil, uuid.Nil, err
		}
	}
	// Temporary compatibility until the KN owner adapter is wired in a separate lane.
	// A configured resolver is authoritative: it may never fall back to this bridge.
	legacy := s.SessionPolicies.DefaultUnit == nil && p.KN != nil
	if legacy && unit == uuid.Nil {
		unit = p.KN.UnitRef
	}
	if p.SelectedSession != nil && !handled && !legacy {
		// A bound owner is never native, even if a configured resolver declines it.
		return nil, uuid.Nil, problem.Forbidden
	}
	if unit == uuid.Nil && handled {
		unit = ownerUnit
		if unit == uuid.Nil {
			return nil, uuid.Nil, problem.Validation.WithField("org_ref", "ngữ cảnh phiên không gắn đơn vị")
		}
	}
	if unit == uuid.Nil {
		if p.OrgRef == uuid.Nil {
			return nil, uuid.Nil, problem.Validation.WithField("org_ref", "bắt buộc khi người gọi không thuộc Nút nào")
		}
		u, err := s.Departments.DepartmentUnit(ctx, p.OrgRef)
		if err != nil {
			if errors.Is(err, partyport.ErrOrgNotFound) {
				return nil, uuid.Nil, problem.Validation.WithField("org_ref", "Nút người gọi không thuộc đơn vị cấp 1/2")
			}
			return nil, uuid.Nil, err
		}
		unit = u
	}
	var out []port.InternalUnit
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListInternalUnits(ctx, tx, unit, includeInactive)
		return e
	})
	return out, unit, err
}

// UnitLogEntry là một dòng khung Log của đơn vị nội bộ.
type UnitLogEntry struct {
	Seq      int64
	Event    string
	ActorRef uuid.UUID
	At       time.Time
	Changes  []domain.FieldChange
	Reason   *string
}

// InternalUnitLog trả khung Log của đơn vị nội bộ (Admin), phân trang theo seq.
func (s *Service) InternalUnitLog(ctx context.Context, id uuid.UUID, after int64, limit int) ([]UnitLogEntry, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return nil, err
	}
	var rows []store.UnitLogRow
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if u, err := store.InternalUnitByID(ctx, tx, id, false); err != nil || u == nil {
			if err != nil {
				return err
			}
			return ErrInternalUnitNotFound
		}
		var e error
		rows, e = store.UnitEvents(ctx, tx, id, after, limit)
		return e
	})
	out := make([]UnitLogEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, UnitLogEntry{Seq: r.Seq, Event: "internal_unit." + r.Action, ActorRef: r.ActorRef, At: r.EventTime,
			Changes: domain.Diff(r.Before, r.After), Reason: r.Reason})
	}
	return out, err
}

type unitSeedFile struct {
	Format string `json:"format"`
	Status string `json:"status"`
	Source string `json:"source"`
	Units  []struct {
		Code           string `json:"code"`
		Kind           string `json:"kind"`
		Name           string `json:"name"`
		DepartmentCode string `json:"department_code"`
		SortOrder      int    `json:"sort_order"`
	} `json:"units"`
}

// UnitSeedResult tổng kết một lần nạp đơn vị nội bộ.
type UnitSeedResult struct {
	Created  []port.InternalUnit
	Existing int
	Status   string
}

// SeedInternalUnits khai báo đơn vị nội bộ mặc định (Ban Giám đốc + 5 phòng gắn phòng ban P2) cho đơn vị; mã đã
// có giữ nguyên. Phòng ban chưa khai báo → đơn vị loại phòng tạo không gắn phòng ban. Gọi lặp an toàn. Trạm, trại
// do Admin thêm (tên, số lượng chờ khách — Q-068).
func (s *Service) SeedInternalUnits(ctx context.Context, unit uuid.UUID, idem string) (UnitSeedResult, error) {
	if err := requireKey(idem); err != nil {
		return UnitSeedResult{}, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return UnitSeedResult{}, err
	}
	if unit == uuid.Nil {
		return UnitSeedResult{}, problem.Validation.WithField("org_ref", "bắt buộc — Nút cấp 1 hoặc cấp 2")
	}
	var f unitSeedFile
	dec := json.NewDecoder(bytes.NewReader(seeddata.InternalUnits))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil || f.Format != "agrigreen.internal-units/v1" {
		return UnitSeedResult{}, fmt.Errorf("accounts: dữ liệu nhúng internal-units lỗi: %w", err)
	}
	depts, err := s.Departments.UnitDepartments(ctx, unit)
	if err != nil {
		return UnitSeedResult{}, err
	}
	res := UnitSeedResult{Created: []port.InternalUnit{}, Status: f.Status}
	for _, su := range f.Units {
		dept := strings.TrimSpace(su.DepartmentCode)
		if dept != "" && !slices.ContainsFunc(depts, func(d partyport.Department) bool { return d.Code == dept && !d.Retired }) {
			dept = ""
		}
		u, err := domain.NormalizeInternalUnit(port.InternalUnit{OrgRef: unit, Code: su.Code, Kind: su.Kind, Name: su.Name,
			DepartmentCode: dept, SortOrder: su.SortOrder, Active: true})
		if err != nil {
			return res, fmt.Errorf("accounts: đơn vị nội bộ nhúng %s: %w", su.Code, err)
		}
		out, replayed, err := s.createInternalUnit(ctx, a.p, u, "seed:iunit:"+unit.String()+":"+u.Code)
		switch {
		case errors.Is(err, ErrInternalUnitCodeTaken):
			res.Existing++
		case err != nil:
			return res, err
		case replayed:
			res.Existing++
		default:
			res.Created = append(res.Created, out)
		}
	}
	return res, nil
}
