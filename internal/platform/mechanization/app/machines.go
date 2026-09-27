package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MachineInput là dữ liệu hồ sơ máy (FN-05 B2–B3). Không có Mã máy (hệ thống tự
// sinh) và không có Khâu (tự gán theo chủng loại).
type MachineInput struct {
	CooperativeRef uuid.UUID
	MachineType    string
	OwnerID        uuid.UUID
	Brand, Model   string
	SerialNo       string
	ChassisNo      string
	YearMade       *int
	Fuel           string
	PowerHP        *decimal.Decimal
	CapacityHa     *decimal.Decimal
	OwnedSince     time.Time
	Status         domain.MachineStatus
	IdemKey        string
}

// MachineResult là kết quả một thao tác ghi sổ máy.
type MachineResult struct {
	Machine  domain.Machine
	Event    domain.MachineEvent
	Replayed bool
}

// machineType tra chủng loại đang dùng và khâu tự gán (FN-05 BR-03, AC-03).
func (s *Service) machineType(ctx context.Context, code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", problem.Validation.WithField("machine_type", "Chủng loại máy là trường bắt buộc")
	}
	it, err := s.catalogItem(ctx, mdport.CatalogMachineType, code)
	if err != nil {
		return "", err
	}
	if it == nil || !it.Active {
		return "", problem.Validation.WithField("machine_type", "Chủng loại máy không có trong danh mục chuẩn (hoặc đã ngừng sử dụng)")
	}
	stage := strings.TrimSpace(it.Attrs["farm_stage"])
	if stage == "" {
		return "", problem.Validation.WithField("machine_type", "Chủng loại máy chưa gắn khâu sản xuất trong danh mục")
	}
	return stage, nil
}

func (s *Service) machineReplay(ctx context.Context, tid uuid.UUID, key string) (*MachineResult, error) {
	var res *MachineResult
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		ev, err := store.MachineEventByIdem(ctx, tx, key)
		if err != nil || ev == nil {
			return err
		}
		cur, err := store.MachineByRef(ctx, tx, ev.Machine.Ref, false)
		if err != nil {
			return err
		}
		res = &MachineResult{Machine: ev.Machine, Event: *ev, Replayed: true}
		if cur != nil {
			res.Machine = *cur
		}
		return nil
	})
	return res, err
}

// identTaken kiểm trùng Số máy/SN, Số khung trong tenant (FN-05 BR-01), bỏ qua máy self.
func (s *Service) identTaken(ctx context.Context, tx pgx.Tx, serial, chassis string, self uuid.UUID) error {
	if serial != "" {
		m, err := store.MachineBySerial(ctx, tx, serial)
		if err != nil {
			return err
		}
		if m != nil && m.Ref != self {
			return s.takenProblem(ctx, ErrSerialTaken, "serial_no", "Số máy/SN này đã tồn tại trên hệ thống", *m)
		}
	}
	if chassis != "" {
		m, err := store.MachineByChassis(ctx, tx, chassis)
		if err != nil {
			return err
		}
		if m != nil && m.Ref != self {
			return s.takenProblem(ctx, ErrChassisTaken, "chassis_no", "Số khung này đã tồn tại trên hệ thống", *m)
		}
	}
	return nil
}

func (s *Service) takenProblem(ctx context.Context, base *problem.Problem, field, msg string, m domain.Machine) error {
	if o, err := s.Orgs.Get(ctx, m.CooperativeRef); err == nil && o.Name != "" {
		msg += ", thuộc " + o.Name
	}
	return base.WithField(field, msg).With("existing_code", m.Code)
}

// CreateMachine thêm máy với Mã máy tự sinh MAY-<tỉnh của HTX>-00001 (FN-05 BR-02).
func (s *Service) CreateMachine(ctx context.Context, in MachineInput) (MachineResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return MachineResult{}, err
	}
	return s.createMachine(ctx, p, sc, in, domain.SourceManual)
}

func (s *Service) createMachine(ctx context.Context, p authz.Principal, sc authz.Scope, in MachineInput, source string) (MachineResult, error) {
	if err := requireIdem(in.IdemKey); err != nil {
		return MachineResult{}, err
	}
	ci, err := s.activeCoop(ctx, sc, in.CooperativeRef, "cooperative_ref")
	if err != nil {
		return MachineResult{}, err
	}
	stage, err := s.machineType(ctx, in.MachineType)
	if err != nil {
		return MachineResult{}, err
	}
	if in.OwnerID == uuid.Nil {
		return MachineResult{}, problem.Validation.WithField("owner_id", "Vui lòng chọn chủ sở hữu (tạo ở Hồ sơ chủ sở hữu trước)")
	}
	if in.Status == "" {
		in.Status = domain.StatusActive
	}
	now := s.now()
	m := domain.Machine{
		Ref: ids.New(), MachineType: strings.TrimSpace(in.MachineType), FarmStage: stage, CooperativeRef: ci.Ref, OwnerID: in.OwnerID,
		Brand: strings.TrimSpace(in.Brand), Model: strings.TrimSpace(in.Model), SerialNo: domain.NormalizeIdent(in.SerialNo),
		ChassisNo: domain.NormalizeIdent(in.ChassisNo), YearMade: in.YearMade, Fuel: strings.TrimSpace(in.Fuel), PowerHP: in.PowerHP,
		CapacityHa: in.CapacityHa, OwnedSince: dayOf(in.OwnedSince), Status: in.Status, StatusSource: source, StatusAt: now, Active: true,
	}
	if in.OwnedSince.IsZero() {
		m.OwnedSince = time.Time{}
	}
	if err := domain.ValidateMachine(m, s.today()); err != nil {
		return MachineResult{}, fieldProblem(err)
	}
	if prev, err := s.machineReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return MachineResult{}, err
	}
	var res MachineResult
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		r, err := s.insertMachineTx(ctx, tx, p, m, ci.ProvinceLabel, source, in.IdemKey, now)
		res = r
		return err
	})
	return res, classify(err)
}

// insertMachineTx ghi máy mới trong giao dịch đang mở: kiểm chủ sở hữu, trùng định danh, cấp mã.
func (s *Service) insertMachineTx(ctx context.Context, tx pgx.Tx, p authz.Principal, m domain.Machine, provinceLabel, source, key string, now time.Time) (MachineResult, error) {
	o, err := store.OwnerByID(ctx, tx, m.OwnerID, false)
	if err != nil {
		return MachineResult{}, err
	}
	if o == nil || !o.Active {
		return MachineResult{}, problem.Validation.WithField("owner_id", "Chủ sở hữu không tồn tại hoặc đã vô hiệu hóa — tạo ở tab \"Chủ sở hữu\" trước")
	}
	if err := s.identTaken(ctx, tx, m.SerialNo, m.ChassisNo, uuid.Nil); err != nil {
		return MachineResult{}, err
	}
	code, err := store.NextCode(ctx, tx, p.TenantID, domain.PrefixMachine, provinceLabel)
	if err != nil {
		return MachineResult{}, err
	}
	m.Code = code
	ev := domain.MachineEvent{
		ID: ids.New(), TenantID: p.TenantID, Machine: m, Action: domain.MachineCreated, Source: source, EventTime: now,
		ActorRef: p.ActorRef, IdemKey: key,
	}
	if err := store.InsertMachineEvent(ctx, tx, &ev); err != nil {
		return MachineResult{}, err
	}
	m.LastSeq = ev.Seq
	if err := store.InsertMachine(ctx, tx, p.TenantID, m); err != nil {
		return MachineResult{}, err
	}
	return MachineResult{Machine: m, Event: ev}, nil
}

// MachinePatch là thay đổi hồ sơ máy; nil = giữ nguyên. Mã máy không bao giờ đổi
// (ADR-0009: nhãn hiển thị cấp một lần, kể cả khi máy chuyển sang HTX tỉnh khác).
type MachinePatch struct {
	Ref            uuid.UUID
	CooperativeRef *uuid.UUID
	MachineType    *string
	OwnerID        *uuid.UUID
	Brand, Model   *string
	SerialNo       *string
	ChassisNo      *string
	YearMade       *int
	Fuel           *string
	PowerHP        *decimal.Decimal
	CapacityHa     *decimal.Decimal
	OwnedSince     *time.Time
	IdemKey        string
}

// UpdateMachine sửa hồ sơ máy (FN-05 B7): ghi một dòng "updated" mang ảnh chụp sau sửa.
func (s *Service) UpdateMachine(ctx context.Context, in MachinePatch) (MachineResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return MachineResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return MachineResult{}, err
	}
	cur, err := s.visibleMachine(ctx, p, sc, in.Ref)
	if err != nil {
		return MachineResult{}, err
	}
	next := cur
	if in.CooperativeRef != nil && *in.CooperativeRef != cur.CooperativeRef {
		ci, err := s.activeCoop(ctx, sc, *in.CooperativeRef, "cooperative_ref")
		if err != nil {
			return MachineResult{}, err
		}
		next.CooperativeRef = ci.Ref
	}
	if in.MachineType != nil && strings.TrimSpace(*in.MachineType) != cur.MachineType {
		stage, err := s.machineType(ctx, *in.MachineType)
		if err != nil {
			return MachineResult{}, err
		}
		next.MachineType, next.FarmStage = strings.TrimSpace(*in.MachineType), stage
	}
	set := func(dst *string, v *string, norm func(string) string) {
		if v != nil {
			*dst = norm(*v)
		}
	}
	set(&next.Brand, in.Brand, strings.TrimSpace)
	set(&next.Model, in.Model, strings.TrimSpace)
	set(&next.Fuel, in.Fuel, strings.TrimSpace)
	set(&next.SerialNo, in.SerialNo, domain.NormalizeIdent)
	set(&next.ChassisNo, in.ChassisNo, domain.NormalizeIdent)
	if in.OwnerID != nil {
		next.OwnerID = *in.OwnerID
	}
	if in.YearMade != nil {
		next.YearMade = in.YearMade
	}
	if in.PowerHP != nil {
		next.PowerHP = in.PowerHP
	}
	if in.CapacityHa != nil {
		next.CapacityHa = in.CapacityHa
	}
	if in.OwnedSince != nil {
		next.OwnedSince = dayOf(*in.OwnedSince)
	}
	if err := domain.ValidateMachine(next, s.today()); err != nil {
		return MachineResult{}, fieldProblem(err)
	}
	if next.DeactivatedOn != nil && next.DeactivatedOn.Before(next.OwnedSince) {
		return MachineResult{}, problem.Validation.WithField("owned_since", domain.ErrDeactBefore.Error())
	}
	if prev, err := s.machineReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return MachineResult{}, err
	}
	var res MachineResult
	now := s.now()
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		locked, err := store.MachineByRef(ctx, tx, cur.Ref, true)
		if err != nil {
			return err
		}
		if locked == nil || locked.LastSeq != cur.LastSeq {
			return ErrState.WithDetail("hồ sơ máy vừa được người khác sửa, vui lòng tải lại")
		}
		if sameMachine(*locked, next) {
			return ErrState.WithDetail("không có thay đổi")
		}
		if next.OwnerID != cur.OwnerID {
			o, err := store.OwnerByID(ctx, tx, next.OwnerID, false)
			if err != nil {
				return err
			}
			if o == nil || !o.Active {
				return problem.Validation.WithField("owner_id", "Chủ sở hữu không tồn tại hoặc đã vô hiệu hóa")
			}
		}
		if err := s.identTaken(ctx, tx, next.SerialNo, next.ChassisNo, cur.Ref); err != nil {
			return err
		}
		r, err := writeMachineTx(ctx, tx, p, next, domain.MachineUpdated, domain.SourceManual, "", now, in.IdemKey)
		res = r
		return err
	})
	return res, classify(err)
}

func sameMachine(a, b domain.Machine) bool {
	eqDec := func(x, y *decimal.Decimal) bool { return (x == nil) == (y == nil) && (x == nil || x.Equal(*y)) }
	eqInt := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.CooperativeRef == b.CooperativeRef && a.MachineType == b.MachineType && a.OwnerID == b.OwnerID && a.Brand == b.Brand &&
		a.Model == b.Model && a.SerialNo == b.SerialNo && a.ChassisNo == b.ChassisNo && eqInt(a.YearMade, b.YearMade) &&
		a.Fuel == b.Fuel && eqDec(a.PowerHP, b.PowerHP) && eqDec(a.CapacityHa, b.CapacityHa) && a.OwnedSince.Equal(b.OwnedSince)
}

// writeMachineTx ghi một biến động máy (sổ + bảng hiện tại) trong giao dịch đang mở.
func writeMachineTx(ctx context.Context, tx pgx.Tx, p authz.Principal, m domain.Machine, action domain.MachineAction, source, reason string, at time.Time, key string) (MachineResult, error) {
	ev := domain.MachineEvent{
		ID: ids.New(), TenantID: p.TenantID, Machine: m, Action: action, Source: source, Reason: reason, EventTime: at,
		ActorRef: p.ActorRef, IdemKey: key,
	}
	if err := store.InsertMachineEvent(ctx, tx, &ev); err != nil {
		return MachineResult{}, err
	}
	m.LastSeq = ev.Seq
	if err := store.UpdateMachine(ctx, tx, m); err != nil {
		return MachineResult{}, err
	}
	return MachineResult{Machine: m, Event: ev}, nil
}

// deactivateMachineTx chuyển máy sang Inactive kèm Ngày vô hiệu hóa (FN-05 BR-06/BR-10) — KHÔNG xóa.
func deactivateMachineTx(ctx context.Context, tx pgx.Tx, p authz.Principal, m domain.Machine, on time.Time, reason, source string, now time.Time, key string) (MachineResult, error) {
	d := on
	m.Active, m.DeactivatedOn, m.StatusReason = false, &d, reason
	return writeMachineTx(ctx, tx, p, m, domain.MachineDeactivated, source, reason, now, key)
}

// visibleMachine đọc máy trong phạm vi (ngoài phạm vi → 404).
func (s *Service) visibleMachine(ctx context.Context, p authz.Principal, sc authz.Scope, ref uuid.UUID) (domain.Machine, error) {
	var m *domain.Machine
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		m, e = store.MachineByRef(ctx, tx, ref, false)
		return e
	}); err != nil {
		return domain.Machine{}, err
	}
	if m == nil || s.coopVisible(ctx, sc, m.CooperativeRef) != nil {
		return domain.Machine{}, ErrMachineNotFound
	}
	return *m, nil
}

// StatusInput là đổi tình trạng máy (FN-05 B7, QT-03 nhập tay).
type StatusInput struct {
	Ref         uuid.UUID
	Status      domain.MachineStatus
	EffectiveAt *time.Time // thời điểm tình trạng có hiệu lực; nil = bây giờ
	IdemKey     string
}

// ChangeStatus ghi đổi tình trạng máy vào sổ; tình trạng tại thời điểm T đọc lại từ sổ.
func (s *Service) ChangeStatus(ctx context.Context, in StatusInput) (MachineResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return MachineResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return MachineResult{}, err
	}
	if _, ok := domain.StatusLabels[in.Status]; !ok {
		return MachineResult{}, problem.Validation.WithField("status", domain.ErrStatus.Error())
	}
	now := s.now()
	at := now
	if in.EffectiveAt != nil {
		at = in.EffectiveAt.UTC()
	}
	if at.After(now) {
		return MachineResult{}, problem.Validation.WithField("effective_at", "thời điểm hiệu lực không được ở tương lai")
	}
	cur, err := s.visibleMachine(ctx, p, sc, in.Ref)
	if err != nil {
		return MachineResult{}, err
	}
	if prev, err := s.machineReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return MachineResult{}, err
	}
	var res MachineResult
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		m, err := store.MachineByRef(ctx, tx, cur.Ref, true)
		if err != nil {
			return err
		}
		switch {
		case m == nil:
			return ErrMachineNotFound
		case !m.Active:
			return ErrState.WithDetail("máy đã bị vô hiệu hóa")
		case m.Status == in.Status:
			return ErrState.WithDetail("máy đang ở tình trạng này")
		case at.Before(m.StatusAt):
			return problem.Validation.WithField("effective_at", "thời điểm hiệu lực phải sau lần cập nhật tình trạng gần nhất")
		}
		m.Status, m.StatusSource, m.StatusAt = in.Status, domain.SourceManual, at
		r, err := writeMachineTx(ctx, tx, p, *m, domain.MachineStatusChanged, domain.SourceManual, "", at, in.IdemKey)
		res = r
		return err
	})
	return res, classify(err)
}

// MachineStatusChange là vô hiệu hóa / kích hoạt lại máy.
type MachineStatusChange struct {
	Ref           uuid.UUID
	DeactivatedOn *time.Time // nil = hôm nay
	Reason        string
	IdemKey       string
}

// DeactivateMachine vô hiệu hóa máy (FN-05 BR-06): ẩn khỏi bản đồ, không tính cân
// đối kể từ Ngày vô hiệu hóa, giữ lịch sử.
func (s *Service) DeactivateMachine(ctx context.Context, in MachineStatusChange) (MachineResult, error) {
	return s.machineActive(ctx, in, false)
}

// ReactivateMachine kích hoạt lại máy.
func (s *Service) ReactivateMachine(ctx context.Context, in MachineStatusChange) (MachineResult, error) {
	return s.machineActive(ctx, in, true)
}

func (s *Service) machineActive(ctx context.Context, in MachineStatusChange, activate bool) (MachineResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return MachineResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return MachineResult{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if !activate && !domain.ValidReason(reason) {
		return MachineResult{}, problem.Validation.WithField("reason", domain.ErrReason.Error())
	}
	today := s.today()
	on := today
	if in.DeactivatedOn != nil {
		on = dayOf(*in.DeactivatedOn)
	}
	cur, err := s.visibleMachine(ctx, p, sc, in.Ref)
	if err != nil {
		return MachineResult{}, err
	}
	if prev, err := s.machineReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return MachineResult{}, err
	}
	var res MachineResult
	now := s.now()
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		m, err := store.MachineByRef(ctx, tx, cur.Ref, true)
		if err != nil {
			return err
		}
		if m == nil {
			return ErrMachineNotFound
		}
		if activate {
			if m.Active {
				return ErrState.WithDetail("máy đang hoạt động")
			}
			o, err := store.OwnerByID(ctx, tx, m.OwnerID, false)
			if err != nil {
				return err
			}
			if o == nil || !o.Active {
				return ErrState.WithDetail("chủ sở hữu của máy đang bị vô hiệu hóa — kích hoạt lại chủ sở hữu trước")
			}
			m.Active, m.DeactivatedOn, m.StatusReason = true, nil, ""
			r, err := writeMachineTx(ctx, tx, p, *m, domain.MachineReactivated, domain.SourceManual, reason, now, in.IdemKey)
			res = r
			return err
		}
		if !m.Active {
			return ErrState.WithDetail("máy đã bị vô hiệu hóa")
		}
		if err := domain.ValidateDeactivation(m.OwnedSince, on, today); err != nil {
			return fieldProblem(err)
		}
		r, err := deactivateMachineTx(ctx, tx, p, *m, on, reason, domain.SourceManual, now, in.IdemKey)
		res = r
		return err
	})
	return res, classify(err)
}

// MachineQuery là tham số liệt kê máy (FN-05 B1 bộ lọc).
type MachineQuery struct {
	CooperativeRef  uuid.UUID
	OwnerID         uuid.UUID
	FarmStage       string
	MachineType     string
	Status          string
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
}

// MachinePage là một trang máy.
type MachinePage struct {
	Items []domain.Machine
	Next  *uuid.UUID
}

// ListMachines liệt kê máy. Phạm vi hẹp (không phải toàn vùng) phải lọc theo một HTX trong phạm vi.
func (s *Service) ListMachines(ctx context.Context, q MachineQuery) (MachinePage, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return MachinePage{}, err
	}
	q.Limit = normLimit(q.Limit)
	if !sc.All && (q.CooperativeRef == uuid.Nil || s.coopVisible(ctx, sc, q.CooperativeRef) != nil) {
		return MachinePage{Items: []domain.Machine{}}, nil
	}
	var items []domain.Machine
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListMachines(ctx, tx, store.MachineFilter{
			CooperativeRef: q.CooperativeRef, OwnerID: q.OwnerID, FarmStage: q.FarmStage, MachineType: q.MachineType, Status: q.Status,
			IncludeInactive: q.IncludeInactive, After: q.After, Limit: q.Limit + 1,
		})
		return e
	}); err != nil {
		return MachinePage{}, err
	}
	pg := MachinePage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].Ref
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []domain.Machine{}
	}
	return pg, nil
}

// GetMachine đọc một máy.
func (s *Service) GetMachine(ctx context.Context, ref uuid.UUID) (domain.Machine, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return domain.Machine{}, err
	}
	return s.visibleMachine(ctx, p, sc, ref)
}

// MachineHistory trả Lịch sử biến động của máy (FN-05 B8, AC-04).
func (s *Service) MachineHistory(ctx context.Context, ref uuid.UUID) ([]domain.MachineEvent, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return nil, err
	}
	if _, err := s.visibleMachine(ctx, p, sc, ref); err != nil {
		return nil, err
	}
	var evs []domain.MachineEvent
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		evs, e = store.MachineEvents(ctx, tx, ref)
		return e
	})
	return evs, err
}

// MachineCount là số máy của một HTX tại ngày T (US-MAC-03).
type MachineCount struct {
	At      time.Time
	Total   int // máy thuộc HTX tại T (BR-09)
	Active  int // trong đó "Hoạt động" (tính vào năng lực, BR-04)
	ByStage []StageCount
}

// StageCount là số máy theo khâu × chủng loại.
type StageCount struct {
	FarmStage   string
	MachineType string
	Total       int
	ByStatus    map[string]int
}

// MachineCountAt tính lại số máy của HTX tại một ngày trong quá khứ (FN-05 BR-09, AC-05).
func (s *Service) MachineCountAt(ctx context.Context, coop uuid.UUID, at time.Time) (MachineCount, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return MachineCount{}, err
	}
	if err := s.coopVisible(ctx, sc, coop); err != nil {
		return MachineCount{}, err
	}
	day := dayOf(at)
	out := MachineCount{At: day, ByStage: []StageCount{}}
	var rows []store.CountRow
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.CountsAt(ctx, tx, day, dayStart(day.AddDate(0, 0, 1)), []uuid.UUID{coop})
		return e
	}); err != nil {
		return MachineCount{}, err
	}
	for _, r := range rows {
		out.Total += r.Total
		out.Active += r.ByStatus[string(domain.StatusActive)]
		out.ByStage = append(out.ByStage, StageCount{FarmStage: r.FarmStage, MachineType: r.MachineType, Total: r.Total, ByStatus: r.ByStatus})
	}
	return out, nil
}
