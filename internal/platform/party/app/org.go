package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// OrgResult là kết quả một thao tác ghi sổ tổ chức.
type OrgResult struct {
	Org      port.Org
	Event    domain.OrgEvent
	Replayed bool
	// ActiveHouseholds: số nông hộ đang là thành viên (cảnh báo phạm vi ảnh hưởng khi vô hiệu hóa HTX).
	ActiveHouseholds int
	// RepresentativeDuplicate: người đại diện đã có trong kho định danh, dùng lại mã cũ.
	RepresentativeDuplicate bool
}

// Representative là định danh người đại diện HTX gửi kèm khi tạo — chỉ đi thẳng vào kho định danh.
type Representative struct {
	FullName string `pii:"true"`
	Phone    string `pii:"true"`
}

// CreateOrgInput là dữ liệu vào của CreateOrg.
type CreateOrgInput struct {
	Kind              port.OrgKind
	Code              string // bỏ trống với HTX: hệ thống tự sinh (CGH BR-01)
	Name              string
	AdminUnitCode     string
	ParentRef         uuid.UUID
	RepresentativeRef uuid.UUID
	Representative    *Representative
	Cooperative       *domain.CooperativeProfile
	IdemKey           string
	// UniqueName: chặn tạo HTX trùng tên (không phân biệt hoa thường) — nhập hàng loạt
	// và sao chép hồ sơ HTX (HTX US-HTX-02/03). Tạo đơn lẻ G1 giữ hành vi cũ.
	UniqueName bool
}

// MoveOrgInput: di chuyển một nút (kèm cả nhánh con) sang cha mới — sáp nhập (KN US-ADM-07/08).
type MoveOrgInput struct {
	Ref              uuid.UUID
	NewParentRef     uuid.UUID // Nil = đưa lên gốc
	NewAdminUnitCode string    // rỗng = giữ mã đơn vị hiện tại
	Reason           string
	IdemKey          string
}

// StatusInput: vô hiệu hóa / kích hoạt lại / đổi tên.
type StatusInput struct {
	Ref        uuid.UUID
	ExpectKind port.OrgKind // rỗng = mọi loại; khác loại → 404
	Reason     string
	Name       string // chỉ dùng khi đổi tên
	IdemKey    string
}

func (r *Registry) now() time.Time { return r.Clock.Now().UTC() }

// unitInfo tra đơn vị hành chính và kiểm cấp theo loại tổ chức.
func (r *Registry) unitInfo(ctx context.Context, kind port.OrgKind, code, field string) (*port.AdminUnit, error) {
	if code == "" {
		return nil, nil
	}
	if r.Units == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := r.Units.Get(ctx, code, r.now())
	if errors.Is(err, port.ErrUnitNotFound) {
		return nil, problem.Validation.WithField(field, "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	if err != nil {
		return nil, err
	}
	switch want := domain.UnitLevel(kind); want {
	case "province", "commune":
		if string(u.Level) != want {
			return nil, problem.Validation.WithField(field, "loại tổ chức này phải gắn đơn vị cấp "+want)
		}
	}
	return &u, nil
}

// unitWithin kiểm đơn vị của nút con nằm trong đơn vị của nút cha.
func unitWithin(child *port.AdminUnit, parent *store.OrgState) bool {
	if parent == nil || parent.AdminUnitCode == "" || child == nil {
		return true
	}
	return child.Code == parent.AdminUnitCode || child.ParentCode == parent.AdminUnitCode
}

func (r *Registry) requireWriteScope(ctx context.Context, p authz.Principal, unitCodes ...string) error {
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return err
	}
	for _, u := range unitCodes {
		if u == "" && !sc.All {
			return ErrOutOfScope.WithDetail("tổ chức cấp quốc gia chỉ quản trị viên toàn quốc được ghi")
		}
		if u != "" && !sc.Allows(u) {
			return ErrOutOfScope
		}
	}
	return nil
}

// classifyOrgErr quy lỗi ràng buộc duy nhất về mã lỗi nghiệp vụ.
func classifyOrgErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "org_tenant_id_code_key":
			return ErrOrgCodeTaken
		case "cooperative_tax_code":
			return ErrTaxCodeTaken
		}
	}
	return ledger.Classify(err)
}

// replay đọc lại kết quả của lần ghi trước cùng idem_key.
func (r *Registry) replay(ctx context.Context, tid uuid.UUID, key string) (*OrgResult, error) {
	var res *OrgResult
	err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		var e error
		res, e = replayTx(ctx, tx, key)
		return e
	})
	return res, err
}

func replayTx(ctx context.Context, tx pgx.Tx, key string) (*OrgResult, error) {
	ev, err := store.OrgEventByIdemKey(ctx, tx, key)
	if err != nil || ev == nil {
		return nil, err
	}
	cur, err := store.OrgByRef(ctx, tx, ev.Org.Ref, false)
	if err != nil {
		return nil, err
	}
	res := &OrgResult{Org: ev.Org, Event: *ev, Replayed: true}
	if cur != nil {
		res.Org = cur.Org
	}
	return res, nil
}

// write chạy một thao tác ghi sổ có chống lặp theo idem_key.
func (r *Registry) write(ctx context.Context, tid uuid.UUID, key string, fn func(tx pgx.Tx) (OrgResult, error)) (OrgResult, error) {
	var res OrgResult
	err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		// A concurrent retry must observe the winner before name/code allocation.
		// Keep this lock in the source transaction; no extra pool connection.
		if err := store.LockOrgIdempotency(ctx, tx, key); err != nil {
			return err
		}
		if prev, err := replayTx(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		var err error
		res, err = fn(tx)
		return err
	})
	err = classifyOrgErr(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		// Hai request cùng khóa chạy song song: bên thua đọc lại bản ghi của bên thắng.
		if prev, rerr := r.replay(ctx, tid, key); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

func newEvent(p authz.Principal, o port.Org, action domain.OrgAction, now time.Time, key string) domain.OrgEvent {
	return domain.OrgEvent{
		ID: ids.New(), TenantID: p.TenantID, Org: o, Action: action, EventTime: now, ActorRef: p.ActorRef, IdemKey: key,
	}
}

// CreateOrg tạo tổ chức: sổ + bảng hiện tại trong một giao dịch; kind = cooperative thêm bước 2 ghi hồ sơ HTX ở
// pf/cooperative (coop.go).
func (r *Registry) CreateOrg(ctx context.Context, in CreateOrgInput) (OrgResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgResult{}, err
	}
	if err := requireNodeCreator(p, in.Kind); err != nil {
		return OrgResult{}, err
	}
	return r.createOrg(ctx, p, in, func(unit string) error { return r.requireWriteScope(ctx, p, unit) })
}

// createOrg là lõi của CreateOrg sau bước kiểm vai: allow kiểm phạm vi ghi của đơn vị hành chính
// (G1: cây ScopeResolver; KN v7.7 FN-XA-03.03: xã phụ trách của ngữ cảnh phiên).
func (r *Registry) createOrg(ctx context.Context, p authz.Principal, in CreateOrgInput, allow func(unit string) error) (OrgResult, error) {
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	now := r.now()
	in.Name, in.Code, in.AdminUnitCode = strings.TrimSpace(in.Name), strings.TrimSpace(in.Code), strings.TrimSpace(in.AdminUnitCode)
	isCoop := in.Kind == port.OrgCooperative
	if isCoop && in.Code != "" {
		return OrgResult{}, problem.Validation.WithField("code", "Mã HTX do hệ thống tự sinh, không nhập tay (CGH BR-01)")
	}
	if err := domain.ValidateOrgBasics(in.Kind, in.Code, in.Name, in.AdminUnitCode, isCoop); err != nil {
		return OrgResult{}, fieldProblem(err)
	}
	var profile domain.CooperativeProfile
	if isCoop {
		if in.Cooperative != nil {
			profile = *in.Cooperative
		}
		if profile.TaxCode != "" {
			tc, err := domain.NormalizeTaxCode(profile.TaxCode)
			if err != nil {
				return OrgResult{}, problem.Validation.WithField("cooperative.tax_code", err.Error())
			}
			profile.TaxCode = tc
		}
		if err := profile.Validate(now); err != nil {
			return OrgResult{}, fieldProblem(err)
		}
	} else if in.Cooperative != nil {
		return OrgResult{}, problem.Validation.WithField("cooperative", "chỉ dùng với kind = cooperative")
	}
	if in.Representative != nil && in.RepresentativeRef != uuid.Nil {
		return OrgResult{}, problem.Validation.WithField("representative", "chỉ gửi representative hoặc representative_ref")
	}
	unit, err := r.unitInfo(ctx, in.Kind, in.AdminUnitCode, "admin_unit_code")
	if err != nil {
		return OrgResult{}, err
	}
	if err := allow(in.AdminUnitCode); err != nil {
		return OrgResult{}, err
	}

	// Gọi lặp: trả kết quả cũ trước khi chạm kho định danh (kèm bước 2 nếu lần trước dừng giữa hai bước).
	if prev, err := r.replay(ctx, p.TenantID, in.IdemKey); err != nil {
		return OrgResult{}, err
	} else if prev != nil {
		return r.completeCoop(ctx, p.TenantID, *prev)
	}
	if isCoop {
		if _, err := r.coopProfiles(); err != nil {
			return OrgResult{}, err
		}
		if profile.TaxCode != "" {
			if err := r.taxCodeTaken(ctx, p.TenantID, profile.TaxCode, uuid.Nil); err != nil {
				return OrgResult{}, err
			}
		}
	}
	repRef, repDup := in.RepresentativeRef, false
	if in.Representative != nil {
		repRef, repDup, err = r.createRepresentative(ctx, *in.Representative, in.AdminUnitCode)
		if err != nil {
			return OrgResult{}, err
		}
	}

	res, err := r.write(ctx, p.TenantID, in.IdemKey, func(tx pgx.Tx) (OrgResult, error) {
		var parent *store.OrgState
		var parentKind *port.OrgKind
		if in.ParentRef != uuid.Nil {
			var err error
			if parent, err = store.OrgByRef(ctx, tx, in.ParentRef, false); err != nil {
				return OrgResult{}, err
			}
			if parent == nil || !parent.Active {
				return OrgResult{}, ErrInvalidHierarchy.WithDetail("tổ chức cấp trên không tồn tại hoặc đã ngừng hoạt động")
			}
			parentKind = &parent.Kind
		}
		if !domain.AllowedParent(in.Kind, parentKind) {
			return OrgResult{}, ErrInvalidHierarchy.WithDetail(domain.ErrParent.Error())
		}
		if !unitWithin(unit, parent) {
			return OrgResult{}, ErrInvalidHierarchy.WithDetail("đơn vị hành chính không thuộc địa bàn của tổ chức cấp trên")
		}
		code := in.Code
		if isCoop && in.UniqueName {
			if err := store.LockCooperativeName(ctx, tx, in.Name); err != nil {
				return OrgResult{}, err
			}
			owner, err := store.CooperativeNameOwner(ctx, tx, in.Name)
			if err != nil {
				return OrgResult{}, err
			}
			if owner != uuid.Nil {
				return OrgResult{}, ErrCoopNameTaken.With("existing_ref", owner.String())
			}
		}
		if isCoop {
			var err error
			if code, err = store.NextCooperativeCode(ctx, tx); err != nil {
				return OrgResult{}, err
			}
		} else if taken, err := store.OrgCodeTaken(ctx, tx, code); err != nil {
			return OrgResult{}, err
		} else if taken {
			return OrgResult{}, ErrOrgCodeTaken
		}
		o := port.Org{
			Ref: ids.New(), Kind: in.Kind, Code: code, Name: in.Name, AdminUnitCode: in.AdminUnitCode,
			ParentRef: in.ParentRef, RepresentativeRef: repRef, Active: true,
		}
		ev := newEvent(p, o, domain.ActionCreated, now, in.IdemKey)
		if isCoop {
			ev.Details = profileDetails(profile)
		}
		if err := store.InsertOrgEvent(ctx, tx, &ev); err != nil {
			return OrgResult{}, err
		}
		if err := store.InsertOrg(ctx, tx, p.TenantID, o, ev); err != nil {
			return OrgResult{}, err
		}
		// P0-HTX-KREF: the INSERT trigger already projected k_ref in this tx.
		// Keep the event for remote projections and share-scope rebuilding.
		if err := enqueueStatusEvent(ctx, tx, ev, 0); err != nil {
			return OrgResult{}, err
		}
		return OrgResult{Org: o, Event: ev, RepresentativeDuplicate: repDup}, nil
	})
	if err != nil {
		return OrgResult{}, err
	}
	// Bước 2 (W3.3): hồ sơ HTX ở pf/cooperative, dựng từ details của dòng sổ org_event.created.
	return r.completeCoop(ctx, p.TenantID, res)
}

func (r *Registry) createRepresentative(ctx context.Context, rep Representative, area string) (uuid.UUID, bool, error) {
	if r.Vault == nil {
		return uuid.Nil, false, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	if strings.TrimSpace(rep.FullName) == "" || strings.TrimSpace(rep.Phone) == "" {
		return uuid.Nil, false, problem.Validation.WithField("representative", "họ tên và SĐT người đại diện là bắt buộc")
	}
	created, err := r.Vault.Create(ctx, client.PurposeRegistryAdmin, client.Identity{
		Kind: client.SubjectPerson, FullName: strings.TrimSpace(rep.FullName), Phone: strings.TrimSpace(rep.Phone), AreaCode: area,
	})
	switch {
	case errors.Is(err, client.ErrDuplicate) && created.Ref != uuid.Nil:
		return created.Ref, true, nil
	case err != nil:
		return uuid.Nil, false, vaultError(err)
	}
	return created.Ref, false, nil
}

func profileDetails(p domain.CooperativeProfile) map[string]any {
	d := map[string]any{}
	if p.TaxCode != "" {
		d["tax_code"] = p.TaxCode
	}
	if p.AreaHa != nil {
		d["area_ha"] = p.AreaHa.String()
	}
	if p.EstablishedOn != nil {
		d["established_on"] = p.EstablishedOn.Format(time.DateOnly)
	}
	if p.Location != nil {
		d["lat"], d["lon"] = p.Location.Lat.String(), p.Location.Lon.String()
	}
	if a := strings.TrimSpace(p.Address); a != "" {
		d["address"] = a
	}
	return d
}

// MoveOrg di chuyển một nút kèm toàn bộ nhánh con sang cha mới, giữ lịch sử trong sổ.
func (r *Registry) MoveOrg(ctx context.Context, in MoveOrgInput) (OrgResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgResult{}, err
	}
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	if err := domain.ValidateMoveReason(in.Reason); err != nil {
		return OrgResult{}, fieldProblem(err)
	}
	now := r.now()
	if prev, err := r.replay(ctx, p.TenantID, in.IdemKey); err != nil {
		return OrgResult{}, err
	} else if prev != nil {
		return *prev, nil
	}
	// Đọc trạng thái trước để kiểm đơn vị hành chính (gọi masterdata ngoài giao dịch).
	var before *store.OrgState
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		before, e = store.OrgByRef(ctx, tx, in.Ref, false)
		return e
	}); err != nil {
		return OrgResult{}, err
	}
	if before == nil {
		return OrgResult{}, ErrOrgNotFound
	}
	newUnit := strings.TrimSpace(in.NewAdminUnitCode)
	if newUnit == "" {
		newUnit = before.AdminUnitCode
	}
	unit, err := r.unitInfo(ctx, before.Kind, newUnit, "new_admin_unit_code")
	if err != nil {
		return OrgResult{}, err
	}
	if err := r.requireWriteScope(ctx, p, before.AdminUnitCode, newUnit); err != nil {
		return OrgResult{}, err
	}

	return r.write(ctx, p.TenantID, in.IdemKey, func(tx pgx.Tx) (OrgResult, error) {
		cur, err := store.OrgByRef(ctx, tx, in.Ref, true)
		if err != nil {
			return OrgResult{}, err
		}
		if cur == nil {
			return OrgResult{}, ErrOrgNotFound
		}
		// KN v7.7 FN-AD-04.02: không di chuyển Nút cấp 1 (TTKNQG), cấp 2 (TTKN tỉnh) sang cha khác.
		// Đổi mã đơn vị hành chính tại chỗ (sáp nhập ĐVHC, KN US-ADM-07/08) vẫn được phép.
		if (cur.Kind == port.OrgTTKNNational || cur.Kind == port.OrgTTKNProvince) && cur.ParentRef != in.NewParentRef {
			return OrgResult{}, ErrInvalidHierarchy.WithDetail("không di chuyển Nút cấp 1 (TTKNQG), cấp 2 (TTKN tỉnh) sang Nút cha khác (FN-AD-04.02)")
		}
		var parent *store.OrgState
		var parentKind *port.OrgKind
		if in.NewParentRef != uuid.Nil {
			if parent, err = store.OrgByRef(ctx, tx, in.NewParentRef, false); err != nil {
				return OrgResult{}, err
			}
			if parent == nil || !parent.Active {
				return OrgResult{}, ErrInvalidHierarchy.WithDetail("tổ chức cấp trên mới không tồn tại hoặc đã ngừng hoạt động")
			}
			inside, err := store.IsInSubtree(ctx, tx, cur.Ref, parent.Ref)
			if err != nil {
				return OrgResult{}, err
			}
			if inside {
				return OrgResult{}, ErrInvalidHierarchy.WithDetail("không thể di chuyển một nút vào chính nhánh con của nó")
			}
			parentKind = &parent.Kind
		}
		if !domain.AllowedParent(cur.Kind, parentKind) {
			return OrgResult{}, ErrInvalidHierarchy.WithDetail(domain.ErrParent.Error())
		}
		if !unitWithin(unit, parent) {
			return OrgResult{}, ErrInvalidHierarchy.WithDetail("đơn vị hành chính không thuộc địa bàn của tổ chức cấp trên mới")
		}
		if cur.ParentRef == in.NewParentRef && cur.AdminUnitCode == newUnit {
			return OrgResult{}, ErrOrgState.WithDetail("tổ chức đã ở vị trí này")
		}
		o := cur.Org
		o.ParentRef, o.AdminUnitCode = in.NewParentRef, newUnit
		ev := newEvent(p, o, domain.ActionMoved, now, in.IdemKey)
		ev.PrevParentRef, ev.PrevAdminUnit, ev.Reason = cur.ParentRef, cur.AdminUnitCode, strings.TrimSpace(in.Reason)
		if err := store.InsertOrgEvent(ctx, tx, &ev); err != nil {
			return OrgResult{}, err
		}
		if err := store.UpdateOrg(ctx, tx, o, cur.StatusReason, ev); err != nil {
			return OrgResult{}, err
		}
		// W2.5: pf.party.org.moved → worker pf.kref dựng lại bao đóng của cả nhánh (ADR-0029).
		if err := enqueueStatusEvent(ctx, tx, ev, 0); err != nil {
			return OrgResult{}, err
		}
		// KN v7.7: Nút xã sang tỉnh khác thì khu vực của tỉnh cũ không còn đúng — bỏ gán (bản ghi bù).
		if err := clearRegionOnMove(ctx, tx, p, o, in.Reason, now, in.IdemKey); err != nil {
			return OrgResult{}, err
		}
		return OrgResult{Org: o, Event: ev}, nil
	})
}

// RenameOrg đổi tên tổ chức (giữ lịch sử).
func (r *Registry) RenameOrg(ctx context.Context, in StatusInput) (OrgResult, error) {
	name := strings.TrimSpace(in.Name)
	return r.changeStatus(ctx, in, domain.ActionRenamed, func(cur *store.OrgState) error {
		if err := domain.ValidateOrgBasics(cur.Kind, cur.Code, name, cur.AdminUnitCode, true); err != nil {
			return fieldProblem(err)
		}
		if name == cur.Name {
			return ErrOrgState.WithDetail("tên mới trùng tên hiện tại")
		}
		return nil
	})
}

// Deactivate vô hiệu hóa tổ chức (soft-delete, US-HTXSTATUS-01): lý do ≥ 20 ký tự,
// phát pf.party.org.deactivated để khóa tài khoản thuộc tổ chức.
func (r *Registry) Deactivate(ctx context.Context, in StatusInput) (OrgResult, error) {
	if err := domain.ValidateDeactivateReason(in.Reason); err != nil {
		if _, perr := authz.Require(ctx, adminRoles...); perr != nil {
			return OrgResult{}, perr
		}
		return OrgResult{}, fieldProblem(err)
	}
	return r.changeStatus(ctx, in, domain.ActionDeactivated, func(cur *store.OrgState) error {
		if !cur.Active {
			return ErrOrgState.WithDetail("tổ chức đã bị vô hiệu hóa")
		}
		return nil
	})
}

// Reactivate kích hoạt lại tổ chức (US-HTXSTATUS-02), phát pf.party.org.reactivated.
func (r *Registry) Reactivate(ctx context.Context, in StatusInput) (OrgResult, error) {
	return r.changeStatus(ctx, in, domain.ActionReactivated, func(cur *store.OrgState) error {
		if cur.Active {
			return ErrOrgState.WithDetail("tổ chức đang hoạt động")
		}
		return nil
	})
}

func (r *Registry) changeStatus(ctx context.Context, in StatusInput, action domain.OrgAction, check func(*store.OrgState) error) (OrgResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgResult{}, err
	}
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return OrgResult{}, err
	}
	return r.changeStatusIn(ctx, p, sc, in, action, check)
}

// changeStatusIn là lõi của changeStatus sau bước kiểm vai và tính phạm vi (G1: ScopeResolver; KN v7.7
// FN-XA-03.03: phạm vi của ngữ cảnh phiên).
func (r *Registry) changeStatusIn(ctx context.Context, p authz.Principal, sc authz.Scope, in StatusInput, action domain.OrgAction,
	check func(*store.OrgState) error) (OrgResult, error) {
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	now := r.now()
	// Nông hộ đang là thành viên (pf/cooperative) đọc TRƯỚC giao dịch của sổ tổ chức — chỉ là số cảnh báo trong
	// payload pf.party.org.deactivated, không phải điều kiện chặn.
	households := 0
	if action == domain.ActionDeactivated && (in.ExpectKind == port.OrgCooperative || in.ExpectKind == "") && r.Profiles != nil {
		n, err := r.activeHouseholds(ctx, p.TenantID, in.Ref)
		if err != nil {
			return OrgResult{}, err
		}
		households = n
	}
	return r.write(ctx, p.TenantID, in.IdemKey, func(tx pgx.Tx) (OrgResult, error) {
		cur, err := store.OrgByRef(ctx, tx, in.Ref, true)
		if err != nil {
			return OrgResult{}, err
		}
		if cur == nil || (in.ExpectKind != "" && cur.Kind != in.ExpectKind) || !visible(sc, cur.AdminUnitCode) {
			return OrgResult{}, ErrOrgNotFound
		}
		if cur.AdminUnitCode == "" && !sc.All {
			return OrgResult{}, ErrOutOfScope
		}
		if err := check(cur); err != nil {
			return OrgResult{}, err
		}
		o := cur.Org
		statusReason := cur.StatusReason
		if cur.Kind != port.OrgCooperative {
			households = 0
		}
		switch action {
		case domain.ActionRenamed:
			o.Name = strings.TrimSpace(in.Name)
		case domain.ActionDeactivated:
			n, err := store.CountActiveChildren(ctx, tx, cur.Ref)
			if err != nil {
				return OrgResult{}, err
			}
			if n > 0 {
				return OrgResult{}, ErrActiveChildren.With("active_children", n)
			}
			o.Active, statusReason = false, strings.TrimSpace(in.Reason)
		case domain.ActionReactivated:
			if cur.ParentRef != uuid.Nil {
				parent, err := store.OrgByRef(ctx, tx, cur.ParentRef, false)
				if err != nil {
					return OrgResult{}, err
				}
				if parent != nil && !parent.Active {
					return OrgResult{}, ErrOrgState.WithDetail("tổ chức cấp trên đang bị vô hiệu hóa")
				}
			}
			o.Active, statusReason = true, ""
		}
		ev := newEvent(p, o, action, now, in.IdemKey)
		ev.Reason = strings.TrimSpace(in.Reason)
		if err := store.InsertOrgEvent(ctx, tx, &ev); err != nil {
			return OrgResult{}, err
		}
		if err := store.UpdateOrg(ctx, tx, o, statusReason, ev); err != nil {
			return OrgResult{}, err
		}
		if err := enqueueStatusEvent(ctx, tx, ev, households); err != nil {
			return OrgResult{}, err
		}
		return OrgResult{Org: o, Event: ev, ActiveHouseholds: households}, nil
	})
}

func enqueueStatusEvent(ctx context.Context, tx pgx.Tx, ev domain.OrgEvent, households int) error {
	base := outbox.Event{
		ID: ids.New(), TenantID: ev.TenantID, SchemaVersion: 1, OccurredAt: ev.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/party",
	}
	switch ev.Action {
	case domain.ActionCreated:
		base.Type = port.EventOrgCreated
		base.Payload = port.OrgCreatedV1{
			OrgRef: ev.Org.Ref, Kind: string(ev.Org.Kind), Code: ev.Org.Code, AdminUnitCode: ev.Org.AdminUnitCode,
			ParentRef: refPtr(ev.Org.ParentRef), Active: ev.Org.Active, ActorRef: ev.ActorRef,
			EventTime: ev.EventTime.Format(time.RFC3339), Seq: ev.Seq,
		}
	case domain.ActionMoved:
		base.Type = port.EventOrgMoved
		base.Payload = port.OrgMovedV1{
			OrgRef: ev.Org.Ref, Kind: string(ev.Org.Kind), Code: ev.Org.Code, AdminUnitCode: ev.Org.AdminUnitCode,
			ParentRef: refPtr(ev.Org.ParentRef), PrevParentRef: refPtr(ev.PrevParentRef), PrevAdminUnitCode: ev.PrevAdminUnit,
			Active: ev.Org.Active, Reason: ev.Reason, ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339), Seq: ev.Seq,
		}
	case domain.ActionDeactivated:
		base.Type = port.EventOrgDeactivated
		base.Payload = port.OrgDeactivatedV1{
			OrgRef: ev.Org.Ref, Kind: string(ev.Org.Kind), Code: ev.Org.Code, AdminUnitCode: ev.Org.AdminUnitCode,
			Reason: ev.Reason, ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339), Seq: ev.Seq,
			ActiveHouseholds: households,
		}
	case domain.ActionReactivated:
		base.Type = port.EventOrgReactivated
		base.Payload = port.OrgReactivatedV1{
			OrgRef: ev.Org.Ref, Kind: string(ev.Org.Kind), Code: ev.Org.Code, AdminUnitCode: ev.Org.AdminUnitCode,
			Reason: ev.Reason, ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339), Seq: ev.Seq,
		}
	default:
		return nil
	}
	return outbox.Enqueue(ctx, tx, base)
}

// refPtr đổi uuid.Nil (gốc cây) thành null của payload.
func refPtr(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

// visible cho biết tổ chức gắn đơn vị unit nằm trong phạm vi đọc.
func visible(sc authz.Scope, unit string) bool {
	if sc.All {
		return true
	}
	return unit != "" && sc.Allows(unit)
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// ListQuery là tham số liệt kê.
type ListQuery struct {
	Kind            string
	Q               string
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
	// AdminUnit (tùy chọn): chỉ tổ chức thuộc tỉnh/xã này (G-03), giao với phạm vi người gọi.
	AdminUnit string
	// Confirmation (D2, chỉ danh sách HTX): confirmed | unconfirmed | rỗng = không lọc.
	Confirmation string
}

func (q *ListQuery) normalize() {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	q.Q = strings.TrimSpace(q.Q)
}

// OrgPage là một trang tổ chức.
type OrgPage struct {
	Items []port.Org
	Next  *uuid.UUID
}

// ListOrgs liệt kê tổ chức trong phạm vi của người gọi.
func (r *Registry) ListOrgs(ctx context.Context, q ListQuery) (OrgPage, error) {
	p, err := authz.Require(ctx, readerRoles...)
	if err != nil {
		return OrgPage{}, err
	}
	q.normalize()
	if q.Kind != "" && !domain.KnownKind(port.OrgKind(q.Kind)) {
		return OrgPage{}, problem.Validation.WithField("kind", domain.ErrKind.Error())
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil || sc.Empty() {
		return OrgPage{Items: []port.Org{}}, err
	}
	var items []port.Org
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListOrgs(ctx, tx, store.OrgFilter{
			All: sc.All, Units: sc.Units, Kind: q.Kind, IncludeInactive: q.IncludeInactive, After: q.After, Limit: q.Limit + 1,
		})
		return e
	})
	if err != nil {
		return OrgPage{}, err
	}
	pg := OrgPage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].Ref
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []port.Org{}
	}
	return pg, nil
}

// CoopPage là một trang HTX.
type CoopPage struct {
	Items []Cooperative
	Next  *uuid.UUID
}

// ListCooperatives liệt kê HTX trong phạm vi, tìm theo tên/mã (Nút) hoặc mã số thuế (hồ sơ ở pf/cooperative).
func (r *Registry) ListCooperatives(ctx context.Context, q ListQuery) (CoopPage, error) {
	p, err := authz.Require(ctx, readerRoles...)
	if err != nil {
		return CoopPage{}, err
	}
	q.normalize()
	sc, err := r.scopeFor(ctx, p)
	if err == nil && !sc.Empty() {
		sc, err = r.narrowScope(ctx, sc, q.AdminUnit)
	}
	if err != nil || sc.Empty() {
		return CoopPage{Items: []Cooperative{}}, err
	}
	qRefs, only, err := r.coopFilterRefs(ctx, p.TenantID, q.Q, nil, nil, q.Confirmation)
	if err != nil {
		return CoopPage{}, err
	}
	var orgs []store.CoopOrg
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		orgs, e = store.ListCooperativeOrgs(ctx, tx, store.CoopOrgFilter{
			All: sc.All, Units: sc.Units, Q: q.Q, QRefs: qRefs, Only: only, IncludeInactive: q.IncludeInactive, After: q.After, Limit: q.Limit + 1,
		})
		return e
	})
	if err != nil {
		return CoopPage{}, err
	}
	var pg CoopPage
	if len(orgs) > q.Limit {
		orgs = orgs[:q.Limit]
		last := orgs[q.Limit-1].Ref
		pg.Next = &last
	}
	if pg.Items, err = r.cooperatives(ctx, p.TenantID, orgs, false); err != nil {
		return CoopPage{}, err
	}
	return pg, nil
}

// GetCooperative đọc một HTX; ngoài phạm vi → 404 (không lộ sự tồn tại).
func (r *Registry) GetCooperative(ctx context.Context, ref uuid.UUID) (Cooperative, error) {
	p, err := authz.Require(ctx, readerRoles...)
	if err != nil {
		return Cooperative{}, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return Cooperative{}, err
	}
	c, err := r.cooperativeByRef(ctx, p.TenantID, ref, false)
	if err != nil {
		return Cooperative{}, err
	}
	if c == nil || !visible(sc, c.AdminUnitCode) {
		return Cooperative{}, ErrOrgNotFound
	}
	return *c, nil
}

// OrgHistory trả nhật ký biến động chỉ đọc của một tổ chức (US-HTXSTATUS-03).
func (r *Registry) OrgHistory(ctx context.Context, ref uuid.UUID) ([]domain.OrgEvent, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return nil, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return nil, err
	}
	var evs []domain.OrgEvent
	var cur *store.OrgState
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if cur, e = store.OrgByRef(ctx, tx, ref, false); e != nil || cur == nil {
			return e
		}
		evs, e = store.OrgEvents(ctx, tx, ref)
		return e
	})
	if err != nil {
		return nil, err
	}
	if cur == nil || !visible(sc, cur.AdminUnitCode) {
		return nil, ErrOrgNotFound
	}
	return evs, nil
}
