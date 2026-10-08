package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// defaultDutyTTL: trạng thái tự khai hết hạn sau 12 giờ nếu chưa có tham số.
var defaultDutyTTL = decimal.NewFromInt(12)

// UpsertStaff ghi một cán bộ vào danh bạ. TTKN tỉnh/QG ghi cho cán bộ trong phạm
// vi; cán bộ xã chỉ tự khai cho chính mình trong xã phụ trách.
func (s *Service) UpsertStaff(ctx context.Context, st domain.Staff) (domain.Staff, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return domain.Staff{}, err
	}
	if !isSupervisor(p) && st.ActorRef != p.ActorRef {
		return domain.Staff{}, problem.Forbidden.WithDetail("cán bộ xã chỉ tự khai thông tin của mình")
	}
	if err := domain.ValidateStaff(&st); err != nil {
		return domain.Staff{}, fieldProblem(err)
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.Staff{}, err
	}
	if !sc.Allows(st.UnitCode) {
		return domain.Staff{}, ErrOutOfScope
	}
	prov, _, err := s.provinceOf(ctx, st.UnitCode, "unit_code")
	if err != nil {
		return domain.Staff{}, err
	}
	st.ProvinceCode = prov
	var out *domain.Staff
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.StaffByActor(ctx, tx, st.ActorRef); err != nil {
			return err
		} else if prev != nil && !visible(sc, prev.UnitCode, prev.ProvinceCode) {
			// Cán bộ đang thuộc địa bàn khác: chỉ cấp quản lý địa bàn đó chuyển được.
			return ErrOutOfScope.WithDetail("cán bộ đang thuộc đơn vị ngoài phạm vi")
		}
		if err := store.UpsertStaff(ctx, tx, p.TenantID, st, p.ActorRef); err != nil {
			return err
		}
		var e error
		out, e = store.StaffByActor(ctx, tx, st.ActorRef)
		return e
	})
	if err != nil {
		return domain.Staff{}, err
	}
	return *out, nil
}

// SetDutyStatus: cán bộ tự cập nhật "Đang trực"/"Tạm nghỉ" (US-DIR-01). Hết hạn sau
// extension.duty_status_ttl_hours → "không xác định".
func (s *Service) SetDutyStatus(ctx context.Context, status string) (domain.ManualStatus, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return domain.ManualStatus{}, err
	}
	if status != domain.DutyOn && status != domain.DutyOff {
		return domain.ManualStatus{}, problem.Validation.WithField("status", "on_duty hoặc off_duty")
	}
	now := s.now()
	ttl, err := s.paramDecimal(ctx, port.ParamDutyStatusTTLHours, now, defaultDutyTTL)
	if err != nil {
		return domain.ManualStatus{}, err
	}
	if !ttl.IsPositive() || ttl.GreaterThan(decimal.NewFromInt(168)) {
		ttl = defaultDutyTTL
	}
	m := domain.ManualStatus{Status: status, SetAt: now, ValidUntil: domain.DueAt(now, ttl)}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		st, err := store.StaffByActor(ctx, tx, p.ActorRef)
		if err != nil {
			return err
		}
		if st == nil || !st.Active {
			return ErrStaffNotFound
		}
		return store.InsertDutyStatus(ctx, tx, p.TenantID, ids.New(), p.ActorRef, status, m.SetAt, m.ValidUntil)
	})
	return m, err
}

// AddShift: TTKN tỉnh khai báo ca trực cho cán bộ trong tỉnh (US-DIR-03).
func (s *Service) AddShift(ctx context.Context, actor uuid.UUID, starts, ends time.Time) (domain.Shift, error) {
	p, err := authz.Require(ctx, supervisors...)
	if err != nil {
		return domain.Shift{}, err
	}
	if !ends.After(starts) {
		return domain.Shift{}, problem.Validation.WithField("ends_at", "phải sau starts_at")
	}
	if ends.Sub(starts) > 7*24*time.Hour {
		return domain.Shift{}, problem.Validation.WithField("ends_at", "một ca tối đa 7 ngày")
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.Shift{}, err
	}
	sh := domain.Shift{ID: ids.New(), ActorRef: actor, StartsAt: starts.UTC(), EndsAt: ends.UTC()}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		st, err := store.StaffByActor(ctx, tx, actor)
		if err != nil {
			return err
		}
		if st == nil || !st.Active || !visible(sc, st.UnitCode, st.ProvinceCode) {
			return ErrStaffNotFound
		}
		return store.InsertShift(ctx, tx, p.TenantID, sh, p.ActorRef)
	})
	return sh, err
}

// RosterEntry là một dòng danh bạ trực.
type RosterEntry struct {
	Staff  domain.Staff
	Status string
	Source string
	Until  *time.Time
}

// Roster là danh bạ trực của một đơn vị.
type Roster struct {
	Unit     string
	Items    []RosterEntry
	OnDuty   int
	Fallback []RosterEntry // US-DIR-02 AC: không ai đang trực → cán bộ TTKN tỉnh phụ trách gần nhất
}

// GetRoster tra danh bạ trực theo địa bàn (US-DIR-02). unit rỗng = toàn bộ phạm vi.
func (s *Service) GetRoster(ctx context.Context, unit string) (Roster, error) {
	p, err := authz.Require(ctx, rosterReader...)
	if err != nil {
		return Roster{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return Roster{}, err
	}
	unit = strings.TrimSpace(unit)
	if unit != "" && !sc.Allows(unit) {
		return Roster{}, ErrOutOfScope
	}
	if unit == "" && sc.Empty() {
		return Roster{Items: []RosterEntry{}, Fallback: []RosterEntry{}}, nil
	}
	province := ""
	if unit != "" {
		if province, _, err = s.provinceOf(ctx, unit, "unit"); err != nil {
			return Roster{}, err
		}
	}
	now := s.now()
	out := Roster{Unit: unit, Items: []RosterEntry{}, Fallback: []RosterEntry{}}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		all, codes := sc.All && unit == "", sc.Units
		if unit != "" {
			codes = []string{unit}
		}
		staff, err := store.StaffInUnits(ctx, tx, all, codes)
		if err != nil {
			return err
		}
		entries, err := dutyEntries(ctx, tx, staff, now)
		if err != nil {
			return err
		}
		out.Items = entries
		for _, e := range entries {
			if e.Status == domain.DutyOn {
				out.OnDuty++
			}
		}
		if out.OnDuty == 0 && province != "" && province != unit {
			prov, err := store.StaffInUnits(ctx, tx, false, []string{province})
			if err != nil {
				return err
			}
			var provLevel []domain.Staff
			for _, st := range prov {
				if st.UnitCode == province {
					provLevel = append(provLevel, st)
				}
			}
			if out.Fallback, err = dutyEntries(ctx, tx, provLevel, now); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func dutyEntries(ctx context.Context, tx pgx.Tx, staff []domain.Staff, now time.Time) ([]RosterEntry, error) {
	out := []RosterEntry{}
	if len(staff) == 0 {
		return out, nil
	}
	actors := make([]uuid.UUID, len(staff))
	for i, st := range staff {
		actors[i] = st.ActorRef
	}
	manual, err := store.LatestDutyStatus(ctx, tx, actors, now)
	if err != nil {
		return nil, err
	}
	shifts, err := store.ShiftsCovering(ctx, tx, actors, now)
	if err != nil {
		return nil, err
	}
	for _, st := range staff {
		var m *domain.ManualStatus
		if v, ok := manual[st.ActorRef]; ok {
			m = &v
		}
		status, source, until := domain.DutyAt(now, m, shifts[st.ActorRef])
		out = append(out, RosterEntry{Staff: st, Status: status, Source: source, Until: until})
	}
	return out, nil
}

// StaffPhone trả số điện thoại của một cán bộ trong danh bạ trực để "gọi/nhắn 1 chạm"
// (US-DIR-02). Người đọc phải thấy được cán bộ đó: cùng địa bàn trong phạm vi, hoặc
// cán bộ cấp tỉnh là nơi dự phòng (fallback) của một xã trong phạm vi. Số đọc từ
// pf/accounts lúc gọi, không lưu, không ghi log (R-14).
func (s *Service) StaffPhone(ctx context.Context, actor uuid.UUID) (string, error) {
	p, err := authz.Require(ctx, rosterReader...)
	if err != nil {
		return "", err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return "", err
	}
	var st *domain.Staff
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		st, e = store.StaffByActor(ctx, tx, actor)
		return e
	})
	if err != nil {
		return "", err
	}
	if st == nil || !st.Active || !s.contactVisible(ctx, sc, *st) {
		return "", ErrStaffNotFound // không lộ sự tồn tại của cán bộ ngoài phạm vi
	}
	if s.Contacts == nil {
		return "", ErrDependency.WithDetail("chưa nối pf/accounts (Keycloak Admin REST)")
	}
	phone, err := s.Contacts.PhoneOf(ctx, p.TenantID, actor)
	if errors.Is(err, accountsport.ErrNoPhone) {
		return "", ErrNoContact
	}
	if err != nil {
		return "", ErrDependency.WithDetail("không tra được số liên hệ")
	}
	return phone, nil
}

// contactVisible: cán bộ thuộc phạm vi, hoặc là cán bộ cấp tỉnh (dự phòng khi xã
// không ai trực) của tỉnh chứa một đơn vị trong phạm vi.
func (s *Service) contactVisible(ctx context.Context, sc authz.Scope, st domain.Staff) bool {
	if visible(sc, st.UnitCode, st.ProvinceCode) {
		return true
	}
	if st.UnitCode != st.ProvinceCode {
		return false
	}
	for _, u := range sc.Units {
		if prov, _, err := s.provinceOf(ctx, u, "unit"); err == nil && prov == st.ProvinceCode {
			return true
		}
	}
	return false
}
