package app

// HTX = Nút kind=cooperative (pf_party, sổ org_event) + hồ sơ ở pf/cooperative (pf_coop.cooperative, W3.3). pf/party
// giữ các màn HTX hướng Nút và ghép hồ sơ qua partyport.CoopProfiles (gốc lắp ráp nối) — không JOIN chéo schema,
// không giao dịch nào chạy qua hai vai DB.
//
// Tạo HTX hai bước (docs/design/cooperative.md mục 8.6):
//  1. Nút + sổ org_event + pf.party.org.created trong giao dịch của pf/party (idem_key = Idempotency-Key);
//  2. CoopProfiles.CreateProfile — lũy đẳng theo Nút, hồ sơ dựng lại từ details của dòng sổ org_event.created.
//
// Bước 2 lỗi (hồ sơ chưa ghi) → 503 party.dependency_unavailable; middleware Idempotency-Key xóa khóa khi 5xx nên
// client gọi lại CÙNG khóa: bước 1 phát lại dòng sổ cũ (không tạo Nút thứ hai), bước 2 ghi hồ sơ còn thiếu và kết
// quả trả như lần gọi không gián đoạn (201). Mã số thuế: kiểm qua TaxCodeOwner TRƯỚC bước 1; nếu hai yêu cầu
// song song cùng mã số thuế lọt qua, chỉ mục duy nhất của hồ sơ chặn bên thua ở bước 2 → 409 party.tax_code_taken
// (Nút của bên thua còn, chưa có hồ sơ — việc đối soát báo Nút HTX không hồ sơ).

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Cooperative là HTX kèm hồ sơ. Nút chưa có hồ sơ (tạo HTX dừng giữa hai bước) có Profile rỗng.
type Cooperative struct {
	store.OrgState
	Profile domain.CooperativeProfile
	// UpdatedAt: lần cập nhật cuối của Nút hoặc hồ sơ (cột updated_at của màn KN).
	UpdatedAt time.Time
	// HouseholdCount: nông hộ đang là thành viên (chỉ màn KN đọc).
	HouseholdCount int
}

func (r *Registry) coopProfiles() (port.CoopProfiles, error) {
	if r.Profiles == nil {
		return nil, ErrDependency.WithDetail("chưa nối hồ sơ HTX của pf/cooperative")
	}
	return r.Profiles, nil
}

func toDomainProfile(p port.CoopProfile) domain.CooperativeProfile {
	d := domain.CooperativeProfile{TaxCode: p.TaxCode, AreaHa: p.AreaHa, EstablishedOn: p.EstablishedOn, Address: p.Address, Confirmed: p.Confirmed}
	if p.Lat != nil && p.Lon != nil {
		d.Location = &domain.Point{Lat: *p.Lat, Lon: *p.Lon}
	}
	return d
}

func toPortProfile(d domain.CooperativeProfile) port.CoopProfile {
	p := port.CoopProfile{TaxCode: d.TaxCode, AreaHa: d.AreaHa, EstablishedOn: d.EstablishedOn, Address: strings.TrimSpace(d.Address)}
	if d.Location != nil {
		lat, lon := d.Location.Lat, d.Location.Lon
		p.Lat, p.Lon = &lat, &lon
	}
	return p
}

// profileFromDetails dựng lại hồ sơ từ details của dòng sổ org_event.created (profileDetails).
func profileFromDetails(d map[string]any) domain.CooperativeProfile {
	str := func(k string) string {
		s, _ := d[k].(string)
		return s
	}
	dec := func(k string) *decimal.Decimal {
		v, err := decimal.NewFromString(str(k))
		if err != nil {
			return nil
		}
		return &v
	}
	p := domain.CooperativeProfile{TaxCode: str("tax_code"), AreaHa: dec("area_ha"), Address: str("address")}
	if s := str("established_on"); s != "" {
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			p.EstablishedOn = &t
		}
	}
	if lat, lon := dec("lat"), dec("lon"); lat != nil && lon != nil {
		p.Location = &domain.Point{Lat: *lat, Lon: *lon}
	}
	return p
}

// taxCodeTaken trả party.tax_code_taken nếu mã số thuế đã gắn với HTX khác self.
func (r *Registry) taxCodeTaken(ctx context.Context, tid uuid.UUID, tax string, self uuid.UUID) error {
	ps, err := r.coopProfiles()
	if err != nil {
		return err
	}
	owner, err := ps.TaxCodeOwner(tenant.WithID(ctx, tid), tax)
	if err != nil {
		return ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
	}
	if owner != uuid.Nil && owner != self {
		return ErrTaxCodeTaken.With("existing_ref", owner.String())
	}
	return nil
}

// profileError quy lỗi của bước ghi hồ sơ HTX: mã số thuế trùng → 409 (kèm existing_ref), lỗi khác → 503 để
// client gọi lại cùng Idempotency-Key.
func (r *Registry) profileError(ctx context.Context, tid uuid.UUID, tax string, self uuid.UUID, err error) error {
	if errors.Is(err, port.ErrCoopTaxCodeTaken) {
		if terr := r.taxCodeTaken(ctx, tid, tax, self); terr != nil {
			return terr
		}
		return ErrTaxCodeTaken
	}
	return ErrDependency.WithDetail("chưa ghi được hồ sơ HTX — gọi lại cùng Idempotency-Key").WithCause(err)
}

// completeCoop là bước 2 của tạo HTX: ghi hồ sơ còn thiếu của Nút vừa tạo (hoặc phát lại). Lũy đẳng; kết quả
// phát lại mà vừa ghi được hồ sơ (lần trước dừng giữa hai bước) trả như lần gọi đầu (Replayed = false).
func (r *Registry) completeCoop(ctx context.Context, tid uuid.UUID, res OrgResult) (OrgResult, error) {
	if res.Event.Org.Kind != port.OrgCooperative || res.Event.Action != domain.ActionCreated {
		return res, nil
	}
	ps, err := r.coopProfiles()
	if err != nil {
		return OrgResult{}, err
	}
	tctx := tenant.WithID(ctx, tid)
	have, err := ps.Profiles(tctx, []uuid.UUID{res.Org.Ref})
	if err != nil {
		return OrgResult{}, ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
	}
	if _, ok := have[res.Org.Ref]; ok {
		return res, nil
	}
	prof := profileFromDetails(res.Event.Details)
	if err := ps.CreateProfile(tctx, res.Org.Ref, toPortProfile(prof)); err != nil {
		return OrgResult{}, r.profileError(ctx, tid, prof.TaxCode, res.Org.Ref, err)
	}
	res.Replayed = false
	return res, nil
}

// cooperatives ghép hồ sơ (và tùy chọn số hộ thành viên) vào các Nút HTX đã đọc.
func (r *Registry) cooperatives(ctx context.Context, tid uuid.UUID, orgs []store.CoopOrg, withHouseholds bool) ([]Cooperative, error) {
	out := make([]Cooperative, 0, len(orgs))
	if len(orgs) == 0 {
		return out, nil
	}
	ps, err := r.coopProfiles()
	if err != nil {
		return nil, err
	}
	refs := make([]uuid.UUID, 0, len(orgs))
	for _, o := range orgs {
		refs = append(refs, o.Ref)
	}
	tctx := tenant.WithID(ctx, tid)
	profs, err := ps.Profiles(tctx, refs)
	if err != nil {
		return nil, ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
	}
	var members map[uuid.UUID]int64
	if withHouseholds {
		if members, err = ps.ActiveHouseholds(tctx, refs); err != nil {
			return nil, ErrDependency.WithDetail("không đọc được nông hộ của HTX").WithCause(err)
		}
	}
	for _, o := range orgs {
		c := Cooperative{OrgState: o.OrgState, UpdatedAt: o.UpdatedAt, HouseholdCount: int(members[o.Ref])}
		if p, ok := profs[o.Ref]; ok {
			c.Profile = toDomainProfile(p)
			if p.UpdatedAt.After(c.UpdatedAt) {
				c.UpdatedAt = p.UpdatedAt
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// cooperativeByRef đọc một HTX kèm hồ sơ; nil nếu không có Nút HTX.
func (r *Registry) cooperativeByRef(ctx context.Context, tid, ref uuid.UUID, withHouseholds bool) (*Cooperative, error) {
	var o *store.CoopOrg
	if err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		var e error
		o, e = store.CoopOrgByRef(ctx, tx, ref)
		return e
	}); err != nil || o == nil {
		return nil, err
	}
	cs, err := r.cooperatives(ctx, tid, []store.CoopOrg{*o}, withHouseholds)
	if err != nil {
		return nil, err
	}
	return &cs[0], nil
}

// coopFilterRefs quy điều kiện trên hồ sơ (mã số thuế theo từ khóa q, khoảng diện tích) thành danh sách Nút:
// qRefs (khớp q theo mã số thuế), only (nil = không lọc diện tích).
//
// confirmation (D2 — ADR-0052): port.CoopConfirmed | port.CoopUnconfirmed | rỗng; khác rỗng → only giao với Nút có
// trạng thái xác nhận tương ứng (HTX tạm "Chưa xác nhận" vẫn có trong danh sách mặc định — chỉ lọc khi yêu cầu).
func (r *Registry) coopFilterRefs(ctx context.Context, tid uuid.UUID, q string, areaMin, areaMax *decimal.Decimal,
	confirmation string) (qRefs, only []uuid.UUID, err error) {
	q = strings.TrimSpace(q)
	switch confirmation {
	case "", port.CoopConfirmed, port.CoopUnconfirmed:
	default:
		return nil, nil, problem.Validation.WithField("confirmation", "confirmed | unconfirmed")
	}
	if q == "" && areaMin == nil && areaMax == nil && confirmation == "" {
		return nil, nil, nil
	}
	ps, err := r.coopProfiles()
	if err != nil {
		return nil, nil, err
	}
	tctx := tenant.WithID(ctx, tid)
	if q != "" {
		if qRefs, err = ps.MatchProfiles(tctx, port.CoopProfileFilter{TaxPrefix: q}); err != nil {
			return nil, nil, ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
		}
	}
	if areaMin != nil || areaMax != nil {
		if only, err = ps.MatchProfiles(tctx, port.CoopProfileFilter{AreaMin: areaMin, AreaMax: areaMax}); err != nil {
			return nil, nil, ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
		}
		if only == nil {
			only = []uuid.UUID{}
		}
	}
	if confirmation != "" {
		conf, err := ps.MatchProfiles(tctx, port.CoopProfileFilter{Confirmation: confirmation})
		if err != nil {
			return nil, nil, ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
		}
		only = intersectRefs(only, conf)
	}
	return qRefs, only, nil
}

// activeHouseholds đếm nông hộ đang là thành viên của một HTX (cảnh báo phạm vi ảnh hưởng khi vô hiệu hóa).
func (r *Registry) activeHouseholds(ctx context.Context, tid, ref uuid.UUID) (int, error) {
	ps, err := r.coopProfiles()
	if err != nil {
		return 0, err
	}
	m, err := ps.ActiveHouseholds(tenant.WithID(ctx, tid), []uuid.UUID{ref})
	if err != nil {
		return 0, ErrDependency.WithDetail("không đọc được nông hộ của HTX").WithCause(err)
	}
	return int(m[ref]), nil
}

// intersectRefs giao hai tập Nút; a nil = không giới hạn (trả b, không bao giờ nil).
func intersectRefs(a, b []uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	if a == nil {
		return append(out, b...)
	}
	in := make(map[uuid.UUID]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	for _, x := range a {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}
