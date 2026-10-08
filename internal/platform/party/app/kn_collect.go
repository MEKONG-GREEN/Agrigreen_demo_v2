package app

// KN v7.7 FN-XA-03.03 — Quản lý HTX & nông hộ trên các xã phụ trách (hợp đồng P7 §1).
//
// Quyền theo ngữ cảnh phiên (authz.RequireKN, BRD §5.1): Cấp Xã thêm/sửa/vô hiệu hóa trong xã phụ trách
// (k.Scope); Cấp Tỉnh xem trong tỉnh; Cấp Quốc gia xem; Admin mọi thứ (chế độ hỗ trợ: lý do ≥ 10 ký tự, lưu
// on_behalf_of). HTX/nông hộ là dữ liệu chủ dùng chung các phòng ban — không lọc phòng ban (tạm, chờ khách).
// Tạo/vô hiệu hóa/kích hoạt lại HTX dùng lõi của sổ org_event (G1, tạo HTX hai bước — coop.go); "sửa" hồ sơ HTX
// là sự kiện bù trên sổ kn_profile_event + bảng chiếu hồ sơ trong MỘT giao dịch ở pf/cooperative
// (partyport.CoopProfiles.UpdateProfile), rồi tên/người đại diện hiện hành của Nút ở pf_party (W3.3).
// Nông hộ của màn (/v1/kn/cooperatives/{ref}/households, /v1/kn/households/*) ở pf/cooperative (W3.3).
// Dữ liệu cá nhân (người đại diện) chỉ đi vào kho định danh; API trả nhãn đã che + SĐT che (RB-3).

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi FN-XA-03.03 (docs/errors.md, mục pf/party).
var (
	ErrKNReadOnly      = problem.New(http.StatusForbidden, "party.read_only", "Vai trò của ngữ cảnh phiên chỉ được xem dữ liệu HTX, nông hộ")
	ErrKNSupportReason = problem.New(http.StatusUnprocessableEntity, "party.support_reason_required",
		"Chế độ hỗ trợ: thao tác ghi bắt buộc lý do (ít nhất 10 ký tự)")
)

// MinKNReason là độ dài tối thiểu của lý do kích hoạt lại HTX và lý do chế độ hỗ trợ.
const MinKNReason = 10

// knCaller là người gọi trong ngữ cảnh phiên KN.
type knCaller struct {
	p authz.Principal
	k authz.KNContext
}

func (c knCaller) scope() authz.Scope { return c.k.Scope }

// support trả on_behalf_of, phiên và lý do của chế độ hỗ trợ (nil khi không hỗ trợ).
func (c knCaller) support() (*uuid.UUID, *uuid.UUID, *string) {
	if c.k.Support == nil {
		return nil, nil, nil
	}
	var ob *uuid.UUID
	if c.k.Support.OnBehalfOf != uuid.Nil {
		v := c.k.Support.OnBehalfOf
		ob = &v
	}
	sid, r := c.k.Support.SessionID, strings.TrimSpace(c.k.Support.Reason)
	return ob, &sid, &r
}

// collectCaller kiểm ngữ cảnh phiên. write: chỉ Cấp Xã và Admin; Cấp Tỉnh, Cấp Quốc gia → party.read_only.
func collectCaller(ctx context.Context, write bool) (knCaller, error) {
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return knCaller{}, err
	}
	c := knCaller{p: p, k: k}
	if !write {
		return c, nil
	}
	if k.ReadOnly() || k.Role == authz.KNTinh {
		return c, ErrKNReadOnly
	}
	if k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(k.Support.Reason)) < MinKNReason {
		return c, ErrKNSupportReason
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Đọc HTX
// ---------------------------------------------------------------------------

// KNPerson là nhãn hiển thị của người đại diện / chủ hộ (đã che, RB-3).
type KNPerson struct {
	Label       string `pii:"true"`
	MaskedPhone string `pii:"true"`
	Unavailable bool
}

// KNCooperative là một HTX của màn "Danh sách HTX".
type KNCooperative struct {
	Cooperative
	Representative *KNPerson
}

// KNCoopQuery là tham số liệt kê.
type KNCoopQuery struct {
	Commune string
	Status  string // active (mặc định) | inactive | all
	Q       string
	AreaMin *decimal.Decimal
	AreaMax *decimal.Decimal
	After   uuid.UUID
	Limit   int
}

// KNCoopPage là một trang HTX.
type KNCoopPage struct {
	Items []KNCooperative
	Next  *uuid.UUID
}

func (k *KN) display(ctx context.Context, ref uuid.UUID) *KNPerson {
	if ref == uuid.Nil {
		return nil
	}
	if k.Reg.Vault == nil {
		return &KNPerson{Unavailable: true}
	}
	d, err := k.Reg.Vault.Display(ctx, client.PurposeContact, ref)
	if err != nil {
		return &KNPerson{Unavailable: true}
	}
	return &KNPerson{Label: d.Label, MaskedPhone: d.MaskedPhone}
}

func (k *KN) withRep(ctx context.Context, c Cooperative) KNCooperative {
	return KNCooperative{Cooperative: c, Representative: k.display(ctx, c.RepresentativeRef)}
}

// ListKNCooperatives liệt kê HTX trong phạm vi ngữ cảnh phiên (mặc định mọi xã phụ trách, HTX đang hoạt động).
func (k *KN) ListKNCooperatives(ctx context.Context, q KNCoopQuery) (KNCoopPage, error) {
	c, err := collectCaller(ctx, false)
	if err != nil {
		return KNCoopPage{}, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	switch q.Status {
	case "":
		q.Status = "active"
	case "active", "inactive", "all":
	default:
		return KNCoopPage{}, problem.Validation.WithField("status", "active | inactive | all")
	}
	if q.AreaMin != nil && q.AreaMax != nil && q.AreaMin.GreaterThan(*q.AreaMax) {
		return KNCoopPage{}, problem.Validation.WithField("area_min", "phải ≤ area_max")
	}
	sc := c.scope()
	q.Commune = strings.TrimSpace(q.Commune)
	if q.Commune != "" && !sc.Allows(q.Commune) {
		return KNCoopPage{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if sc.Empty() {
		return KNCoopPage{Items: []KNCooperative{}}, nil
	}
	qRefs, only, err := k.Reg.coopFilterRefs(ctx, c.p.TenantID, q.Q, q.AreaMin, q.AreaMax)
	if err != nil {
		return KNCoopPage{}, err
	}
	var orgs []store.CoopOrg
	err = tenant.WithTenantTx(ctx, k.DB, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		orgs, e = store.ListCooperativeOrgs(ctx, tx, store.CoopOrgFilter{All: sc.All, Units: sc.Units, Commune: q.Commune, Status: q.Status,
			Q: q.Q, QRefs: qRefs, Only: only, After: q.After, Limit: q.Limit + 1})
		return e
	})
	if err != nil {
		return KNCoopPage{}, err
	}
	pg := KNCoopPage{Items: make([]KNCooperative, 0, len(orgs))}
	if len(orgs) > q.Limit {
		orgs = orgs[:q.Limit]
		last := orgs[q.Limit-1].Ref
		pg.Next = &last
	}
	rows, err := k.Reg.cooperatives(ctx, c.p.TenantID, orgs, true)
	if err != nil {
		return KNCoopPage{}, err
	}
	for _, r := range rows {
		pg.Items = append(pg.Items, k.withRep(ctx, r))
	}
	return pg, nil
}

// readCoop đọc HTX trong phạm vi đọc; ngoài phạm vi hoặc không phải HTX → 404 (không lộ sự tồn tại).
func (k *KN) readCoop(ctx context.Context, tid uuid.UUID, sc authz.Scope, ref uuid.UUID) (*Cooperative, error) {
	cur, err := k.Reg.cooperativeByRef(ctx, tid, ref, true)
	if err != nil {
		return nil, err
	}
	if cur == nil || !visible(sc, cur.AdminUnitCode) {
		return nil, ErrOrgNotFound
	}
	return cur, nil
}

// GetKNCooperative đọc một HTX.
func (k *KN) GetKNCooperative(ctx context.Context, ref uuid.UUID) (KNCooperative, error) {
	c, err := collectCaller(ctx, false)
	if err != nil {
		return KNCooperative{}, err
	}
	cur, err := k.readCoop(ctx, c.p.TenantID, c.scope(), ref)
	if err != nil {
		return KNCooperative{}, err
	}
	return k.withRep(ctx, *cur), nil
}

func (k *KN) coopAfter(ctx context.Context, tid, ref uuid.UUID) (KNCooperative, error) {
	cur, err := k.Reg.cooperativeByRef(ctx, tid, ref, true)
	if err != nil {
		return KNCooperative{}, err
	}
	if cur == nil {
		return KNCooperative{}, ErrOrgNotFound
	}
	return k.withRep(ctx, *cur), nil
}

// ---------------------------------------------------------------------------
// Ghi HTX
// ---------------------------------------------------------------------------

// KNCreateCoopInput là dữ liệu tạo HTX từ màn Cấp Xã.
type KNCreateCoopInput struct {
	Name           string
	CommuneCode    string
	AreaHa         *decimal.Decimal
	TaxCode        string
	HeadOffice     string
	Representative *Representative
	IdemKey        string
}

// CreateKNCooperative tạo HTX trong xã phụ trách (lõi CreateOrg của sổ org_event, chặn trùng tên).
// Nút cha mặc định: Nút xã của xã, không có thì TTKN tỉnh (cây 3 cấp FN-AD-04.02).
func (k *KN) CreateKNCooperative(ctx context.Context, in KNCreateCoopInput) (KNCooperative, bool, error) {
	c, err := collectCaller(ctx, true)
	if err != nil {
		return KNCooperative{}, false, err
	}
	commune := strings.TrimSpace(in.CommuneCode)
	if commune == "" {
		return KNCooperative{}, false, problem.Validation.WithField("commune_code", "bắt buộc")
	}
	sc := c.scope()
	if !sc.Allows(commune) {
		return KNCooperative{}, false, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	unit, err := k.Reg.unitInfo(ctx, port.OrgCooperative, commune, "commune_code")
	if err != nil {
		return KNCooperative{}, false, err
	}
	var parent uuid.UUID
	if err := tenant.WithTenantTx(ctx, k.DB, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		parent, e = store.CommuneParent(ctx, tx, commune, unit.ParentCode)
		return e
	}); err != nil {
		return KNCooperative{}, false, err
	}
	res, err := k.Reg.createOrg(ctx, c.p, CreateOrgInput{
		Kind: port.OrgCooperative, Name: in.Name, AdminUnitCode: commune, ParentRef: parent, Representative: in.Representative,
		Cooperative: &domain.CooperativeProfile{AreaHa: in.AreaHa, TaxCode: in.TaxCode, Address: strings.TrimSpace(in.HeadOffice)},
		IdemKey:     in.IdemKey, UniqueName: true,
	}, func(u string) error {
		if !sc.Allows(u) {
			return ErrOutOfScope
		}
		return nil
	})
	if err != nil {
		return KNCooperative{}, false, err
	}
	out, err := k.coopAfter(ctx, c.p.TenantID, res.Org.Ref)
	return out, res.Replayed, err
}

// KNUpdateCoopInput: trường nil = giữ nguyên; chuỗi rỗng ở TaxCode/HeadOffice/AreaHa = xóa giá trị.
type KNUpdateCoopInput struct {
	Ref            uuid.UUID
	Name           *string
	AreaHa         *string
	TaxCode        *string
	HeadOffice     *string
	Representative *Representative
	Reason         string
	IdemKey        string
}

func coopSnapshot(name string, area *decimal.Decimal, tax, head string, rep uuid.UUID) []byte {
	m := map[string]any{"name": name, "area_ha": nil, "tax_code": nil, "head_office": nil, "representative_ref": nil}
	if area != nil {
		m["area_ha"] = area.String()
	}
	if tax != "" {
		m["tax_code"] = tax
	}
	if head != "" {
		m["head_office"] = head
	}
	if rep != uuid.Nil {
		m["representative_ref"] = rep.String()
	}
	return snapshotJSON(m)
}

func parseOptArea(field string, s *string) (*decimal.Decimal, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil, true, nil
	}
	d, err := decimal.NewFromString(v)
	if err != nil || !domain.ValidArea(d) {
		return nil, true, problem.Validation.WithField(field, domain.ErrArea.Error())
	}
	return &d, true, nil
}

func (k *KN) profileReplay(ctx context.Context, tid uuid.UUID, key string) (*port.CoopProfileEvent, error) {
	ps, err := k.Reg.coopProfiles()
	if err != nil {
		return nil, err
	}
	ev, err := ps.ProfileEventByIdem(tenant.WithID(ctx, tid), key)
	if err != nil {
		return nil, ErrDependency.WithDetail("không đọc được sổ hồ sơ HTX").WithCause(err)
	}
	return ev, nil
}

// applyOrgSnapshot là bước 2 của sửa hồ sơ HTX: ghi tên và người đại diện hiện hành của Nút (pf_party) theo ảnh
// chụp After của dòng sổ kn_profile_event (đã ghi ở bước 1, pf/cooperative). Lũy đẳng — gọi lặp cùng khóa hoàn
// tất một lần sửa dừng giữa hai bước.
func (k *KN) applyOrgSnapshot(ctx context.Context, tid, ref uuid.UUID, after []byte) error {
	var snap struct {
		Name string  `json:"name"`
		Rep  *string `json:"representative_ref"`
	}
	if err := json.Unmarshal(after, &snap); err != nil || strings.TrimSpace(snap.Name) == "" {
		return err
	}
	rep := uuid.Nil
	if snap.Rep != nil {
		if u, err := uuid.Parse(*snap.Rep); err == nil {
			rep = u
		}
	}
	return tenant.WithTenantTx(ctx, k.DB, tid, func(tx pgx.Tx) error {
		cur, err := store.OrgByRef(ctx, tx, ref, true)
		if err != nil || cur == nil {
			return err
		}
		if cur.Name == snap.Name && cur.RepresentativeRef == rep {
			return nil
		}
		if !strings.EqualFold(snap.Name, cur.Name) {
			if err := store.LockCooperativeName(ctx, tx, snap.Name); err != nil {
				return err
			}
			owner, err := store.CooperativeNameOwner(ctx, tx, snap.Name)
			if err != nil {
				return err
			}
			if owner != uuid.Nil && owner != cur.Ref {
				return ErrCoopNameTaken.With("existing_ref", owner.String())
			}
		}
		return store.UpdateOrgProfile(ctx, tx, cur.Ref, snap.Name, rep)
	})
}

// UpdateKNCooperative sửa hồ sơ HTX bằng một sự kiện bù trên kn_profile_event (Log cũ → mới; người đại diện
// mới đi vào kho định danh, sổ chỉ ghi representative_ref cũ → mới). Gọi lặp cùng khóa → kết quả hiện tại,
// không ghi thêm dòng. Không có thay đổi → trả hiện trạng, không ghi sổ.
//
// W3.3 — hai bước, không giao dịch nào chạy qua hai vai: (1) pf/cooperative ghi dòng sổ (ảnh chụp đầy đủ, gồm cả
// tên và người đại diện) + bảng chiếu hồ sơ trong một giao dịch; (2) pf/party ghi tên/người đại diện hiện hành
// của Nút theo ảnh chụp After. Bước 2 lỗi → gọi lại cùng Idempotency-Key: bước 1 phát lại dòng sổ, bước 2 chạy lại.
func (k *KN) UpdateKNCooperative(ctx context.Context, in KNUpdateCoopInput) (KNCooperative, error) {
	c, err := collectCaller(ctx, true)
	if err != nil {
		return KNCooperative{}, err
	}
	if in.IdemKey == "" {
		in.IdemKey = "kn-coop-" + ids.New().String()
	}
	if prev, err := k.profileReplay(ctx, c.p.TenantID, in.IdemKey); err != nil {
		return KNCooperative{}, err
	} else if prev != nil {
		if err := k.applyOrgSnapshot(ctx, c.p.TenantID, prev.SubjectRef, prev.After); err != nil {
			return KNCooperative{}, err
		}
		return k.coopAfter(ctx, c.p.TenantID, prev.SubjectRef)
	}
	area, areaSet, err := parseOptArea("area_ha", in.AreaHa)
	if err != nil {
		return KNCooperative{}, err
	}
	var name, tax, head string
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if n := utf8.RuneCountInString(name); n == 0 || n > 200 {
			return KNCooperative{}, problem.Validation.WithField("name", domain.ErrName.Error())
		}
	}
	if in.TaxCode != nil && strings.TrimSpace(*in.TaxCode) != "" {
		if tax, err = domain.NormalizeTaxCode(*in.TaxCode); err != nil {
			return KNCooperative{}, problem.Validation.WithField("tax_code", err.Error())
		}
	}
	if in.HeadOffice != nil {
		head = strings.TrimSpace(*in.HeadOffice)
		if utf8.RuneCountInString(head) > 500 {
			return KNCooperative{}, problem.Validation.WithField("head_office", "tối đa 500 ký tự")
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) > 500 {
		return KNCooperative{}, problem.Validation.WithField("reason", "tối đa 500 ký tự")
	}
	ps, err := k.Reg.coopProfiles()
	if err != nil {
		return KNCooperative{}, err
	}
	sc := c.scope()
	// Kiểm phạm vi trước khi chạm kho định danh.
	pre, err := k.readCoop(ctx, c.p.TenantID, sc, in.Ref)
	if err != nil {
		return KNCooperative{}, err
	}
	repRef := uuid.Nil
	if in.Representative != nil {
		if repRef, _, err = k.Reg.createRepresentative(ctx, *in.Representative, pre.AdminUnitCode); err != nil {
			return KNCooperative{}, err
		}
	}
	cur, err := k.readCoop(ctx, c.p.TenantID, sc, in.Ref)
	if err != nil {
		return KNCooperative{}, err
	}
	if !sc.Allows(cur.AdminUnitCode) {
		return KNCooperative{}, ErrOutOfScope
	}
	if !cur.Active {
		return KNCooperative{}, ErrOrgState.WithDetail("HTX đã bị vô hiệu hóa — kích hoạt lại trước khi sửa")
	}
	nName, nArea, nTax, nHead, nRep := cur.Name, cur.Profile.AreaHa, cur.Profile.TaxCode, cur.Profile.Address, cur.RepresentativeRef
	if in.Name != nil {
		nName = name
	}
	if areaSet {
		nArea = area
	}
	if in.TaxCode != nil {
		nTax = tax
	}
	if in.HeadOffice != nil {
		nHead = head
	}
	if repRef != uuid.Nil {
		nRep = repRef
	}
	before := coopSnapshot(cur.Name, cur.Profile.AreaHa, cur.Profile.TaxCode, cur.Profile.Address, cur.RepresentativeRef)
	after := coopSnapshot(nName, nArea, nTax, nHead, nRep)
	if len(domain.Diff(before, after)) == 0 {
		return k.withRep(ctx, *cur), nil
	}
	if !strings.EqualFold(nName, cur.Name) {
		var owner uuid.UUID
		if err := tenant.WithTenantTx(ctx, k.DB, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			owner, e = store.CooperativeNameOwner(ctx, tx, nName)
			return e
		}); err != nil {
			return KNCooperative{}, err
		}
		if owner != uuid.Nil && owner != cur.Ref {
			return KNCooperative{}, ErrCoopNameTaken.With("existing_ref", owner.String())
		}
	}
	if nTax != "" && nTax != cur.Profile.TaxCode {
		if err := k.Reg.taxCodeTaken(ctx, c.p.TenantID, nTax, cur.Ref); err != nil {
			return KNCooperative{}, err
		}
	}
	ob, sid, sr := c.support()
	if err := ps.UpdateProfile(tenant.WithID(ctx, c.p.TenantID), port.CoopProfileChange{
		Ref: cur.Ref, CommuneCode: cur.AdminUnitCode, AreaHa: nArea, TaxCode: nTax, Address: nHead, Before: before, After: after,
		Reason: optReason(in.Reason), OnBehalfOf: ob, SupportSessionID: sid, SupportReason: sr, EventTime: k.now(),
		ActorRef: c.p.ActorRef, IdemKey: in.IdemKey,
	}); err != nil {
		return KNCooperative{}, k.Reg.profileError(ctx, c.p.TenantID, nTax, cur.Ref, err)
	}
	if err := k.applyOrgSnapshot(ctx, c.p.TenantID, cur.Ref, after); err != nil {
		return KNCooperative{}, err
	}
	return k.coopAfter(ctx, c.p.TenantID, in.Ref)
}

// KNDeactivationWarnings là dữ liệu liên kết còn lại của HTX lúc vô hiệu hóa (AC: không xóa được HTX có dữ
// liệu liên kết — chỉ vô hiệu hóa, kèm cảnh báo). Plots/Machines nil khi chưa nối pf/geo / pf/mechanization.
type KNDeactivationWarnings struct {
	Households int
	Plots      *int64
	Machines   *int
}

// DeactivateKNCooperative vô hiệu hóa HTX (thay xóa cứng; lõi changeStatus của sổ org_event, phát
// pf.party.org.deactivated). Lý do ≥ 20 ký tự theo ràng buộc sổ org_event (US-HTXSTATUS-01 AC-3).
func (k *KN) DeactivateKNCooperative(ctx context.Context, ref uuid.UUID, reason, key string) (KNCooperative, KNDeactivationWarnings, bool, error) {
	c, err := collectCaller(ctx, true)
	if err != nil {
		return KNCooperative{}, KNDeactivationWarnings{}, false, err
	}
	if err := domain.ValidateDeactivateReason(reason); err != nil {
		return KNCooperative{}, KNDeactivationWarnings{}, false, fieldProblem(err)
	}
	res, err := k.Reg.changeStatusIn(ctx, c.p, c.scope(), StatusInput{Ref: ref, ExpectKind: port.OrgCooperative, Reason: reason, IdemKey: key},
		domain.ActionDeactivated, func(cur *store.OrgState) error {
			if !cur.Active {
				return ErrOrgState.WithDetail("HTX đã bị vô hiệu hóa")
			}
			return nil
		})
	if err != nil {
		return KNCooperative{}, KNDeactivationWarnings{}, false, err
	}
	out, err := k.coopAfter(ctx, c.p.TenantID, ref)
	if err != nil {
		return KNCooperative{}, KNDeactivationWarnings{}, false, err
	}
	w := KNDeactivationWarnings{Households: res.ActiveHouseholds}
	if res.Replayed {
		w.Households = out.HouseholdCount
	}
	tctx := tenant.WithID(ctx, c.p.TenantID)
	if k.Reg.Plots != nil {
		if st, err := k.Reg.Plots.DigitizedByCooperative(tctx, []uuid.UUID{ref}); err == nil {
			n := st[ref].Plots
			w.Plots = &n
		}
	}
	if k.Reg.Machines != nil {
		if n, err := k.Reg.Machines.CoopMachineTotal(tctx, ref); err == nil {
			w.Machines = &n
		}
	}
	return out, w, res.Replayed, nil
}

// ReactivateKNCooperative kích hoạt lại HTX (lý do ≥ 10 ký tự), phát pf.party.org.reactivated.
func (k *KN) ReactivateKNCooperative(ctx context.Context, ref uuid.UUID, reason, key string) (KNCooperative, bool, error) {
	c, err := collectCaller(ctx, true)
	if err != nil {
		return KNCooperative{}, false, err
	}
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < MinKNReason {
		return KNCooperative{}, false, problem.Validation.WithField("reason", "lý do bắt buộc, ít nhất 10 ký tự")
	}
	res, err := k.Reg.changeStatusIn(ctx, c.p, c.scope(), StatusInput{Ref: ref, ExpectKind: port.OrgCooperative, Reason: reason, IdemKey: key},
		domain.ActionReactivated, func(cur *store.OrgState) error {
			if cur.Active {
				return ErrOrgState.WithDetail("HTX đang hoạt động")
			}
			return nil
		})
	if err != nil {
		return KNCooperative{}, false, err
	}
	out, err := k.coopAfter(ctx, c.p.TenantID, ref)
	return out, res.Replayed, err
}

// ---------------------------------------------------------------------------
// Log
// ---------------------------------------------------------------------------

// KNLogPage là một trang Log đã gộp (org_event ở pf/party + kn_profile_event ở pf/cooperative); Next là vị trí tiếp theo.
type KNLogPage struct {
	Items []LogEntry
	Next  *int64
}

func strp(s string) *string { return &s }

func detailStr(d map[string]any, key string) *string {
	if v, ok := d[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return &s
		}
	}
	return nil
}

func profileEntry(e port.CoopProfileEvent) LogEntry {
	r := e.Reason
	if e.SupportReason != nil && r == nil {
		r = e.SupportReason
	}
	return LogEntry{Event: e.Action, ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt, Changes: domain.Diff(e.Before, e.After), Reason: r}
}

func (k *KN) pageLog(ctx context.Context, all []LogEntry, after int64, limit int) KNLogPage {
	sort.SliceStable(all, func(i, j int) bool { return all[i].RecordedAt.Before(all[j].RecordedAt) })
	for i := range all {
		all[i].Seq = int64(i + 1)
	}
	limit = clampLog(limit)
	if after < 0 || after > int64(len(all)) {
		after = int64(len(all))
	}
	end := min(int(after)+limit, len(all))
	items := all[after:end]
	pg := KNLogPage{Items: items}
	if end < len(all) {
		n := int64(end)
		pg.Next = &n
	}
	if k.Names != nil && len(items) > 0 {
		refs := make([]uuid.UUID, 0, len(items))
		for _, e := range items {
			refs = append(refs, e.ActorRef)
		}
		if names, err := k.Names.DisplayNames(ctx, refs); err == nil {
			for i := range pg.Items {
				pg.Items[i].ActorDisplay = names[pg.Items[i].ActorRef]
			}
		}
	}
	return pg
}

// CooperativeLog trả Log của HTX: tạo, vô hiệu hóa/kích hoạt lại, đổi vị trí (org_event) và sửa hồ sơ
// (kn_profile_event, pf/cooperative) — người, thời điểm, giá trị cũ → mới, lý do (AC-CHUNG). Không có dữ liệu cá nhân.
func (k *KN) CooperativeLog(ctx context.Context, ref uuid.UUID, after int64, limit int) (KNLogPage, error) {
	c, err := collectCaller(ctx, false)
	if err != nil {
		return KNLogPage{}, err
	}
	if _, err := k.readCoop(ctx, c.p.TenantID, c.scope(), ref); err != nil {
		return KNLogPage{}, err
	}
	var orgEvs []domain.OrgEvent
	err = tenant.WithTenantTx(ctx, k.DB, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		orgEvs, e = store.OrgEvents(ctx, tx, ref)
		return e
	})
	if err != nil {
		return KNLogPage{}, err
	}
	ps, err := k.Reg.coopProfiles()
	if err != nil {
		return KNLogPage{}, err
	}
	profEvs, err := ps.ProfileEvents(tenant.WithID(ctx, c.p.TenantID), ref)
	if err != nil {
		return KNLogPage{}, ErrDependency.WithDetail("không đọc được sổ hồ sơ HTX").WithCause(err)
	}
	all := make([]LogEntry, 0, len(orgEvs)+len(profEvs))
	var name string
	for _, e := range orgEvs {
		le := LogEntry{Event: string(e.Action), ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt, Changes: []domain.FieldChange{}}
		if e.Reason != "" {
			le.Reason = strp(e.Reason)
		}
		switch e.Action {
		case domain.ActionCreated:
			le.Changes = append(le.Changes, domain.FieldChange{Field: "name", New: strp(e.Org.Name)},
				domain.FieldChange{Field: "code", New: strp(e.Org.Code)}, domain.FieldChange{Field: "commune_code", New: strp(e.Org.AdminUnitCode)})
			for _, f := range []string{"area_ha", "tax_code", "address"} {
				if v := detailStr(e.Details, f); v != nil {
					fld := f
					if f == "address" {
						fld = "head_office"
					}
					le.Changes = append(le.Changes, domain.FieldChange{Field: fld, New: v})
				}
			}
			if e.Org.RepresentativeRef != uuid.Nil {
				le.Changes = append(le.Changes, domain.FieldChange{Field: "representative_ref", New: strp(e.Org.RepresentativeRef.String())})
			}
		case domain.ActionRenamed:
			le.Changes = append(le.Changes, domain.FieldChange{Field: "name", Old: strp(name), New: strp(e.Org.Name)})
		case domain.ActionMoved:
			le.Changes = append(le.Changes, domain.FieldChange{Field: "commune_code", Old: strp(e.PrevAdminUnit), New: strp(e.Org.AdminUnitCode)})
		case domain.ActionDeactivated:
			le.Changes = append(le.Changes, domain.FieldChange{Field: "active", Old: strp("true"), New: strp("false")})
		case domain.ActionReactivated:
			le.Changes = append(le.Changes, domain.FieldChange{Field: "active", Old: strp("false"), New: strp("true")})
		}
		name = e.Org.Name
		all = append(all, le)
	}
	for _, e := range profEvs {
		all = append(all, profileEntry(e))
	}
	return k.pageLog(ctx, all, after, limit), nil
}

// ParseLogCursor đọc cursor vị trí của Log đã gộp.
func ParseLogCursor(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, problem.Validation.WithField("cursor", "phải là số ≥ 0")
	}
	return n, nil
}
