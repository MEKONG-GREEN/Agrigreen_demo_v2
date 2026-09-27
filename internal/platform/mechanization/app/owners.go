package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// OwnerIdentity là họ tên/SĐT chủ sở hữu — CHỈ đi qua bộ nhớ để gửi vào kho định danh (R-04).
type OwnerIdentity struct {
	Name  string `pii:"true"`
	Phone string `pii:"true"`
}

// CreateOwnerInput là dữ liệu vào của CreateOwner (FN-04 B2). Không có Mã CSH: hệ thống tự sinh (BR-01).
type CreateOwnerInput struct {
	Type           domain.OwnerType
	CooperativeRef uuid.UUID
	Identity       OwnerIdentity
	IdemKey        string
}

// OwnerResult là kết quả một thao tác ghi sổ chủ sở hữu.
type OwnerResult struct {
	Owner    domain.Owner
	Event    domain.OwnerEvent
	Replayed bool
	// Machines là máy bị chuyển Inactive cùng chủ sở hữu (FN-04 BR-05).
	Machines []domain.Machine
}

func (s *Service) ownerReplay(ctx context.Context, tid uuid.UUID, key string) (*OwnerResult, error) {
	var res *OwnerResult
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		ev, err := store.OwnerEventByIdem(ctx, tx, key)
		if err != nil || ev == nil {
			return err
		}
		cur, err := store.OwnerByID(ctx, tx, ev.Owner.ID, false)
		if err != nil {
			return err
		}
		res = &OwnerResult{Owner: ev.Owner, Event: *ev, Replayed: true}
		if cur != nil {
			res.Owner = *cur
		}
		return nil
	})
	return res, err
}

// CreateOwner tạo chủ sở hữu với Mã CSH tự sinh (FN-04). Cá nhân: họ tên, SĐT
// vào kho định danh; chống trùng theo SĐT qua Lookup (chỉ mục mù).
func (s *Service) CreateOwner(ctx context.Context, in CreateOwnerInput) (OwnerResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return OwnerResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return OwnerResult{}, err
	}
	if _, ok := domain.OwnerTypeLabels[in.Type]; !ok {
		return OwnerResult{}, problem.Validation.WithField("owner_type",
			"Loại chủ sở hữu phải là một trong: Thành viên HTX / HTX / Doanh nghiệp / Đơn vị khác")
	}
	name, phone := strings.TrimSpace(in.Identity.Name), strings.TrimSpace(in.Identity.Phone)
	if in.Type.NeedsCooperative() && in.CooperativeRef == uuid.Nil {
		return OwnerResult{}, problem.Validation.WithField("cooperative_ref", "Chủ sở hữu loại Thành viên HTX / HTX phải liên kết đúng một HTX")
	}
	if in.Type != domain.OwnerCoop {
		if name == "" {
			return OwnerResult{}, problem.Validation.WithField("name", "Tên chủ sở hữu là trường bắt buộc")
		}
		if phone == "" {
			return OwnerResult{}, problem.Validation.WithField("phone", "SĐT không đúng định dạng, VD: 09xxxxxxxx")
		}
	}
	var ci *coopInfo
	if in.CooperativeRef != uuid.Nil {
		if ci, err = s.activeCoop(ctx, sc, in.CooperativeRef, "cooperative_ref"); err != nil {
			return OwnerResult{}, err
		}
	} else if !sc.All {
		return OwnerResult{}, ErrOutOfScope.WithDetail("chủ sở hữu không gắn HTX chỉ quản trị viên toàn vùng được tạo")
	}
	if prev, err := s.ownerReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return OwnerResult{}, err
	}

	o := domain.Owner{ID: ids.New(), Type: in.Type, CooperativeRef: in.CooperativeRef, Active: true}
	label, area := domain.NoProvince, domain.NoProvince
	if ci != nil {
		label = ci.ProvinceLabel
		if ci.CommuneCode != "" {
			area = ci.CommuneCode
		}
	}
	switch in.Type {
	case domain.OwnerCoop:
		o.CooperativeRef = ci.Ref
	case domain.OwnerEnterprise, domain.OwnerOther:
		o.DisplayName = name // tên tổ chức, không phải dữ liệu cá nhân
	}
	if in.Type != domain.OwnerCoop {
		if o.PersonRef, err = s.ownerPerson(ctx, p.TenantID, name, phone, area); err != nil {
			return OwnerResult{}, err
		}
	}

	var res OwnerResult
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if in.Type == domain.OwnerCoop {
			if dup, err := store.OwnerOfCoop(ctx, tx, o.CooperativeRef); err != nil {
				return err
			} else if dup != nil {
				return ErrOwnerDuplicate.With("existing_code", dup.Code).WithDetail("HTX này đã có hồ sơ chủ sở hữu loại HTX")
			}
		}
		code, err := store.NextCode(ctx, tx, p.TenantID, domain.PrefixOwner, label)
		if err != nil {
			return err
		}
		o.Code = code
		ev := domain.OwnerEvent{
			ID: ids.New(), TenantID: p.TenantID, Owner: o, Action: domain.OwnerCreated, EventTime: s.now(), ActorRef: p.ActorRef,
			IdemKey: in.IdemKey,
		}
		if err := store.InsertOwnerEvent(ctx, tx, &ev); err != nil {
			return err
		}
		o.LastSeq = ev.Seq
		if err := store.InsertOwner(ctx, tx, p.TenantID, o); err != nil {
			return err
		}
		res = OwnerResult{Owner: o, Event: ev}
		return nil
	})
	return res, classify(err)
}

// ownerPerson trả mã tham chiếu kho định danh cho (họ tên, SĐT): trùng SĐT với
// chủ sở hữu đã có → 409; người đã có ở kho (vd nông hộ) → dùng lại mã.
func (s *Service) ownerPerson(ctx context.Context, tid uuid.UUID, name, phone, area string) (uuid.UUID, error) {
	if s.Vault == nil {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	ref, err := s.Vault.Lookup(ctx, client.PurposeDedup, phone)
	switch {
	case err == nil:
	case errors.Is(err, client.ErrNotFound):
		created, cerr := s.Vault.Create(ctx, port.PurposeMachineOwner, client.Identity{
			Kind: client.SubjectPerson, FullName: name, Phone: phone, AreaCode: area,
		})
		switch {
		case cerr == nil || (errors.Is(cerr, client.ErrDuplicate) && created.Ref != uuid.Nil):
			ref = created.Ref
		default:
			return uuid.Nil, vaultErr(cerr, "phone")
		}
	default:
		return uuid.Nil, vaultErr(err, "phone")
	}
	var dup *domain.Owner
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		dup, e = store.OwnerByPerson(ctx, tx, ref)
		return e
	}); err != nil {
		return uuid.Nil, err
	}
	if dup != nil {
		return uuid.Nil, ErrOwnerDuplicate.With("existing_code", dup.Code).WithDetail("Chủ sở hữu trùng định danh (SĐT) đã tồn tại: " + dup.Code)
	}
	return ref, nil
}

// vaultErr giữ lỗi kiểm dữ liệu của kho (vd SĐT sai dạng), còn lại là 503.
func vaultErr(err error, field string) error {
	var pr *problem.Problem
	if errors.As(err, &pr) && pr.Status == 422 {
		return problem.Validation.WithField(field, "SĐT không đúng định dạng, VD: 09xxxxxxxx")
	}
	return ErrDependency.WithDetail("kho định danh từ chối hoặc không phản hồi").WithCause(err)
}

// OwnerStatusInput: vô hiệu hóa / kích hoạt lại chủ sở hữu.
type OwnerStatusInput struct {
	ID            uuid.UUID
	Reason        string
	WithMachines  bool       // FN-04 BR-05: xác nhận vô hiệu hóa kèm máy
	DeactivatedOn *time.Time // ngày vô hiệu hóa của máy kèm theo; nil = hôm nay
	IdemKey       string
}

// DeactivateOwner vô hiệu hóa chủ sở hữu (FN-04 BR-05): còn máy đang hoạt động thì
// chặn, trừ khi xác nhận vô hiệu hóa kèm máy (máy chuyển Inactive, giữ lịch sử).
func (s *Service) DeactivateOwner(ctx context.Context, in OwnerStatusInput) (OwnerResult, error) {
	return s.ownerStatus(ctx, in, domain.OwnerDeactivated)
}

// ReactivateOwner kích hoạt lại chủ sở hữu (máy không tự kích hoạt lại).
func (s *Service) ReactivateOwner(ctx context.Context, in OwnerStatusInput) (OwnerResult, error) {
	return s.ownerStatus(ctx, in, domain.OwnerReactivated)
}

func (s *Service) ownerStatus(ctx context.Context, in OwnerStatusInput, action domain.OwnerAction) (OwnerResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return OwnerResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return OwnerResult{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if action == domain.OwnerDeactivated && !domain.ValidReason(reason) {
		return OwnerResult{}, problem.Validation.WithField("reason", domain.ErrReason.Error())
	}
	today := s.today()
	on := today
	if in.DeactivatedOn != nil {
		on = dayOf(*in.DeactivatedOn)
	}
	if prev, err := s.ownerReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return OwnerResult{}, err
	}
	var cur *domain.Owner
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		cur, e = store.OwnerByID(ctx, tx, in.ID, false)
		return e
	}); err != nil {
		return OwnerResult{}, err
	}
	if cur == nil {
		return OwnerResult{}, ErrOwnerNotFound
	}
	if err := s.ownerWritable(ctx, sc, *cur); err != nil {
		return OwnerResult{}, err
	}
	var res OwnerResult
	now := s.now()
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.OwnerByID(ctx, tx, in.ID, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOwnerNotFound
		}
		ev := domain.OwnerEvent{ID: ids.New(), TenantID: p.TenantID, Action: action, Reason: reason, EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		switch action {
		case domain.OwnerDeactivated:
			if !o.Active {
				return ErrState.WithDetail("chủ sở hữu đã bị vô hiệu hóa")
			}
			ms, err := store.ActiveMachines(ctx, tx, uuid.Nil, o.ID)
			if err != nil {
				return err
			}
			if len(ms) > 0 && !in.WithMachines {
				return ErrOwnerHasMachines.With("active_machines", len(ms)).WithDetail(
					"Chủ sở hữu này còn máy đang hoạt động. Vui lòng chuyển chủ hoặc ngừng máy trước, hoặc xác nhận vô hiệu hóa kèm máy.")
			}
			for _, m := range ms {
				if err := domain.ValidateDeactivation(m.OwnedSince, on, today); err != nil {
					return fieldProblem(err)
				}
				mr := "Vô hiệu hóa kèm chủ sở hữu " + o.Code + ": " + reason
				dm, err := deactivateMachineTx(ctx, tx, p, m, on, mr, domain.SourceManual, now, in.IdemKey+":m:"+m.Ref.String())
				if err != nil {
					return err
				}
				res.Machines = append(res.Machines, dm.Machine)
			}
			o.Active, o.StatusReason, ev.MachinesAffected = false, reason, len(ms)
		case domain.OwnerReactivated:
			if o.Active {
				return ErrState.WithDetail("chủ sở hữu đang hoạt động")
			}
			o.Active, o.StatusReason = true, ""
		}
		ev.Owner = *o
		if err := store.InsertOwnerEvent(ctx, tx, &ev); err != nil {
			return err
		}
		o.LastSeq = ev.Seq
		if err := store.UpdateOwnerStatus(ctx, tx, *o); err != nil {
			return err
		}
		res.Owner, res.Event = *o, ev
		return nil
	})
	return res, classify(err)
}

// ownerWritable kiểm phạm vi ghi trên một chủ sở hữu.
func (s *Service) ownerWritable(ctx context.Context, sc authz.Scope, o domain.Owner) error {
	if sc.All {
		return nil
	}
	if o.CooperativeRef == uuid.Nil {
		return ErrOutOfScope
	}
	ci, err := s.coop(ctx, o.CooperativeRef, s.now())
	if err != nil {
		return err
	}
	if !visible(sc, ci) {
		return ErrOutOfScope
	}
	return nil
}

// OwnerView là chủ sở hữu kèm nhãn hiển thị đã che (mục 10.4).
type OwnerView struct {
	domain.Owner
	Machines         int
	Label            string `pii:"true"`
	MaskedPhone      string `pii:"true"`
	LabelUnavailable bool
}

// label trả nhãn hiển thị tối thiểu của chủ sở hữu: tổ chức → tên; cá nhân → nhãn đã che từ kho.
func (s *Service) label(ctx context.Context, o domain.Owner, coopName string) (label, masked string, unavailable bool) {
	switch {
	case o.Type == domain.OwnerCoop:
		return coopName, "", false
	case o.PersonRef == uuid.Nil || s.Vault == nil:
		return o.DisplayName, "", o.DisplayName == ""
	}
	d, err := s.Vault.Display(ctx, port.PurposeMachineOwner, o.PersonRef)
	if err != nil && !errors.Is(err, client.ErrErased) {
		return o.DisplayName, "", o.DisplayName == ""
	}
	if o.DisplayName != "" {
		return o.DisplayName, d.MaskedPhone, false
	}
	return d.Label, d.MaskedPhone, false
}

// OwnerQuery là tham số liệt kê chủ sở hữu.
type OwnerQuery struct {
	CooperativeRef  uuid.UUID
	Type            string
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
}

// OwnerPage là một trang chủ sở hữu.
type OwnerPage struct {
	Items []OwnerView
	Next  *uuid.UUID
}

func normLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// ListOwners liệt kê chủ sở hữu (FN-04 B1) kèm nhãn đã che và số máy đang hoạt động.
func (s *Service) ListOwners(ctx context.Context, q OwnerQuery) (OwnerPage, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return OwnerPage{}, err
	}
	q.Limit = normLimit(q.Limit)
	if !sc.All {
		// Phạm vi hẹp: chỉ liệt kê theo một HTX trong phạm vi.
		if q.CooperativeRef == uuid.Nil {
			return OwnerPage{Items: []OwnerView{}}, nil
		}
		if err := s.coopVisible(ctx, sc, q.CooperativeRef); err != nil {
			return OwnerPage{Items: []OwnerView{}}, nil //nolint:nilerr // ngoài phạm vi → trang rỗng
		}
	}
	var rows []store.OwnerRow
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ListOwners(ctx, tx, store.OwnerFilter{
			CooperativeRef: q.CooperativeRef, Type: q.Type, IncludeInactive: q.IncludeInactive, After: q.After, Limit: q.Limit + 1,
		})
		return e
	}); err != nil {
		return OwnerPage{}, err
	}
	pg := OwnerPage{Items: []OwnerView{}}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[q.Limit-1].ID
		pg.Next = &last
	}
	names := map[uuid.UUID]string{}
	for _, r := range rows {
		pg.Items = append(pg.Items, s.ownerView(ctx, r.Owner, r.Machines, names))
	}
	return pg, nil
}

func (s *Service) ownerView(ctx context.Context, o domain.Owner, machines int, names map[uuid.UUID]string) OwnerView {
	coopName := ""
	if o.Type == domain.OwnerCoop {
		if n, ok := names[o.CooperativeRef]; ok {
			coopName = n
		} else if org, err := s.Orgs.Get(ctx, o.CooperativeRef); err == nil {
			coopName, names[o.CooperativeRef] = org.Name, org.Name
		}
	}
	v := OwnerView{Owner: o, Machines: machines}
	v.Label, v.MaskedPhone, v.LabelUnavailable = s.label(ctx, o, coopName)
	return v
}

// GetOwner đọc một chủ sở hữu.
func (s *Service) GetOwner(ctx context.Context, id uuid.UUID) (OwnerView, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return OwnerView{}, err
	}
	var o *domain.Owner
	var machines []domain.Machine
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if o, e = store.OwnerByID(ctx, tx, id, false); e != nil || o == nil {
			return e
		}
		machines, e = store.ListMachines(ctx, tx, store.MachineFilter{OwnerID: id, Limit: 10000})
		return e
	}); err != nil {
		return OwnerView{}, err
	}
	if o == nil || s.ownerWritable(ctx, sc, *o) != nil {
		return OwnerView{}, ErrOwnerNotFound
	}
	return s.ownerView(ctx, *o, len(machines), map[uuid.UUID]string{}), nil
}

// OwnerHistory trả Lịch sử biến động của chủ sở hữu (FN-04 B7, AC-03).
func (s *Service) OwnerHistory(ctx context.Context, id uuid.UUID) ([]domain.OwnerEvent, error) {
	if _, err := s.GetOwner(ctx, id); err != nil {
		return nil, err
	}
	p, _ := authz.FromContext(ctx)
	var evs []domain.OwnerEvent
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		evs, e = store.OwnerEvents(ctx, tx, id)
		return e
	})
	return evs, err
}
