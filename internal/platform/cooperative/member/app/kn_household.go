package app

// KN v7.7 FN-XA-03.03 — nông hộ của HTX trên các xã phụ trách (hợp đồng P7 §1): danh sách, thêm, sửa diện tích
// canh tác (sự kiện bù trên sổ kn_profile_event + bảng chiếu, cùng giao dịch — RB-1, R-21), khung Log. Phần HTX
// của màn (/v1/kn/cooperatives, /{ref}, /deactivate, /reactivate, /log) ở lại pf/party — Nút và sổ org_event là
// của pf/party; hồ sơ HTX đi qua partyport.CoopProfiles (profiles.go).
//
// Quyền theo ngữ cảnh phiên (authz.RequireKN, BRD §5.1): Cấp Xã thêm/sửa trong xã phụ trách (k.Scope); Cấp
// Tỉnh, Cấp Quốc gia xem; Admin mọi thứ (chế độ hỗ trợ: lý do ≥ 10 ký tự, lưu on_behalf_of). Dữ liệu cá nhân
// (chủ hộ) chỉ đi vào kho định danh; API trả nhãn đã che + SĐT che (RB-3).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
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

func (s *Service) display(ctx context.Context, ref uuid.UUID) *KNPerson {
	if ref == uuid.Nil {
		return nil
	}
	if s.Vault == nil {
		return &KNPerson{Unavailable: true}
	}
	d, err := s.Vault.Label(ctx, port.IdentityContact, ref)
	if err != nil {
		return &KNPerson{Unavailable: true}
	}
	return &KNPerson{Label: d.Label, MaskedPhone: d.MaskedPhone}
}

// ---------------------------------------------------------------------------
// Nông hộ
// ---------------------------------------------------------------------------

// KNHousehold là một nông hộ thành viên trên tab "Nông hộ".
type KNHousehold struct {
	domain.Household
	Holder KNPerson
}

// KNHouseholdPage là một trang nông hộ.
type KNHouseholdPage struct {
	Items []KNHousehold
	Next  *uuid.UUID
}

func (s *Service) holder(ctx context.Context, ref uuid.UUID) KNPerson {
	if p := s.display(ctx, ref); p != nil {
		return *p
	}
	return KNPerson{Unavailable: true}
}

// ListKNHouseholds liệt kê nông hộ đang là thành viên của HTX trong phạm vi.
func (s *Service) ListKNHouseholds(ctx context.Context, coopRef, after uuid.UUID, limit int) (KNHouseholdPage, error) {
	c, err := collectCaller(ctx, false)
	if err != nil {
		return KNHouseholdPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if _, err := s.readCoop(ctx, c.p.TenantID, c.scope(), coopRef); err != nil {
		return KNHouseholdPage{}, err
	}
	var items []domain.Household
	err = tenant.WithTenantTx(ctx, s.DB, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListHouseholdsByCooperative(ctx, tx, coopRef, after, limit+1)
		return e
	})
	if err != nil {
		return KNHouseholdPage{}, err
	}
	pg := KNHouseholdPage{Items: make([]KNHousehold, 0, len(items))}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Ref
		pg.Next = &last
	}
	for _, h := range items {
		pg.Items = append(pg.Items, KNHousehold{Household: h, Holder: s.holder(ctx, h.Ref)})
	}
	return pg, nil
}

// KNCreateHouseholdInput là dữ liệu thêm nông hộ vào HTX.
type KNCreateHouseholdInput struct {
	CooperativeRef   uuid.UUID
	FullName         string `pii:"true"`
	Phone            string `pii:"true"`
	CultivatedAreaHa *decimal.Decimal
}

// CreateKNHousehold thêm nông hộ vào HTX (lõi createHousehold: chủ hộ vào kho định danh, chỉ giữ ref).
// SĐT đã có trong kho → 409 party.household_phone_taken. Gọi lặp: middleware Idempotency-Key phát lại.
func (s *Service) CreateKNHousehold(ctx context.Context, in KNCreateHouseholdInput) (KNHousehold, error) {
	c, err := collectCaller(ctx, true)
	if err != nil {
		return KNHousehold{}, err
	}
	sc := c.scope()
	coop, err := s.readCoop(ctx, c.p.TenantID, sc, in.CooperativeRef)
	if err != nil {
		return KNHousehold{}, err
	}
	res, err := s.createHousehold(ctx, c.p, sc, CreateHouseholdInput{
		Identity: FarmerIdentity{FullName: in.FullName, Phone: in.Phone}, CooperativeRef: coop.Ref, CultivatedAreaHa: in.CultivatedAreaHa,
		Source: port.HouseholdSourceKNApp, RejectDuplicate: true, KNScoped: true,
	})
	if err != nil {
		return KNHousehold{}, err
	}
	return KNHousehold{Household: res.Household, Holder: s.holder(ctx, res.Household.Ref)}, nil
}

func householdSnapshot(area *decimal.Decimal) []byte {
	m := map[string]any{"cultivated_area_ha": nil}
	if area != nil {
		m["cultivated_area_ha"] = area.String()
	}
	return snapshotJSON(m)
}

// readHousehold đọc nông hộ trong phạm vi (xã của hộ); ngoài phạm vi → 404.
func readHousehold(ctx context.Context, tx pgx.Tx, sc authz.Scope, ref uuid.UUID) (*domain.Household, error) {
	h, err := store.HouseholdByRef(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	if h == nil || !visible(sc, h.CommuneCode) {
		return nil, ErrHouseholdNotFound
	}
	return h, nil
}

// UpdateKNHousehold sửa diện tích canh tác của nông hộ (sự kiện bù trên kn_profile_event + bảng chiếu).
func (s *Service) UpdateKNHousehold(ctx context.Context, ref uuid.UUID, area *string, reason, key string) (KNHousehold, error) {
	c, err := collectCaller(ctx, true)
	if err != nil {
		return KNHousehold{}, err
	}
	if area == nil {
		return KNHousehold{}, problem.Validation.WithField("cultivated_area_ha", "bắt buộc (chuỗi rỗng = xóa giá trị)")
	}
	nArea, _, err := parseOptArea("cultivated_area_ha", area)
	if err != nil {
		return KNHousehold{}, err
	}
	if utf8.RuneCountInString(strings.TrimSpace(reason)) > 500 {
		return KNHousehold{}, problem.Validation.WithField("reason", "tối đa 500 ký tự")
	}
	if key == "" {
		key = "kn-household-" + ids.New().String()
	}
	if prev, err := s.profileReplay(ctx, c.p.TenantID, key); err != nil {
		return KNHousehold{}, err
	} else if prev != nil {
		ref = prev.SubjectRef
	} else {
		sc := c.scope()
		err = tenant.WithTenantTx(ctx, s.DB, c.p.TenantID, func(tx pgx.Tx) error {
			if err := store.LockHousehold(ctx, tx, ref); err != nil {
				return err
			}
			h, err := readHousehold(ctx, tx, sc, ref)
			if err != nil {
				return err
			}
			if !sc.Allows(h.CommuneCode) {
				return ErrOutOfScope
			}
			before, after := householdSnapshot(h.CultivatedAreaHa), householdSnapshot(nArea)
			if len(domain.Diff(before, after)) == 0 {
				return nil
			}
			ev := s.newProfileEvent(c, store.ProfileHousehold, h.Ref, h.CommuneCode, before, after, reason, key)
			if err := store.InsertProfileEvent(ctx, tx, &ev); err != nil {
				return err
			}
			return store.UpdateHouseholdArea(ctx, tx, h.Ref, nArea)
		})
		if err = ledger.Classify(err); err != nil && !errors.Is(err, ledger.ErrDuplicate) {
			return KNHousehold{}, err
		}
	}
	var h *domain.Household
	if err := tenant.WithTenantTx(ctx, s.DB, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		h, e = store.HouseholdByRef(ctx, tx, ref)
		return e
	}); err != nil {
		return KNHousehold{}, err
	}
	if h == nil {
		return KNHousehold{}, ErrHouseholdNotFound
	}
	return KNHousehold{Household: *h, Holder: s.holder(ctx, h.Ref)}, nil
}

// ---------------------------------------------------------------------------
// Log
// ---------------------------------------------------------------------------

// KNLogPage là một trang Log đã gộp (org_event + kn_profile_event); Next là vị trí tiếp theo.
type KNLogPage struct {
	Items []LogEntry
	Next  *int64
}

func strp(s string) *string { return &s }

func profileEntry(e store.ProfileEvent) LogEntry {
	r := e.Reason
	if e.SupportReason != nil && r == nil {
		r = e.SupportReason
	}
	return LogEntry{Event: e.Action, ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt, Changes: domain.Diff(e.Before, e.After), Reason: r}
}

func (s *Service) pageLog(ctx context.Context, all []LogEntry, after int64, limit int) KNLogPage {
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
	if s.Names != nil && len(items) > 0 {
		refs := make([]uuid.UUID, 0, len(items))
		for _, e := range items {
			refs = append(refs, e.ActorRef)
		}
		if names, err := s.Names.DisplayNames(ctx, refs); err == nil {
			for i := range pg.Items {
				pg.Items[i].ActorDisplay = names[pg.Items[i].ActorRef]
			}
		}
	}
	return pg
}

// HouseholdLog trả Log của nông hộ: dòng tạo (từ hồ sơ) và các lần sửa diện tích (kn_profile_event).
func (s *Service) HouseholdLog(ctx context.Context, ref uuid.UUID, after int64, limit int) (KNLogPage, error) {
	c, err := collectCaller(ctx, false)
	if err != nil {
		return KNLogPage{}, err
	}
	var h *domain.Household
	var meta *store.HouseholdMeta
	var evs []store.ProfileEvent
	err = tenant.WithTenantTx(ctx, s.DB, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if h, e = readHousehold(ctx, tx, c.scope(), ref); e != nil {
			return e
		}
		if meta, e = store.HouseholdMetaByRef(ctx, tx, ref); e != nil {
			return e
		}
		evs, e = store.ProfileEvents(ctx, tx, store.ProfileHousehold, ref)
		return e
	})
	if err != nil {
		return KNLogPage{}, err
	}
	all := make([]LogEntry, 0, len(evs)+1)
	if meta != nil {
		initial := h.CultivatedAreaHa
		if len(evs) > 0 {
			ch := domain.Diff(nil, evs[0].Before)
			initial = nil
			for _, x := range ch {
				if x.Field == "cultivated_area_ha" && x.New != nil {
					d, err := decimal.NewFromString(*x.New)
					if err == nil {
						initial = &d
					}
				}
			}
		}
		changes := []domain.FieldChange{{Field: "commune_code", New: strp(h.CommuneCode)}}
		if initial != nil {
			changes = append(changes, domain.FieldChange{Field: "cultivated_area_ha", New: strp(initial.String())})
		}
		all = append(all, LogEntry{Event: "created", ActorRef: meta.CreatedBy, At: meta.CreatedAt, RecordedAt: meta.CreatedAt, Changes: changes})
	}
	for _, e := range evs {
		all = append(all, profileEntry(e))
	}
	return s.pageLog(ctx, all, after, limit), nil
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

// readCoop đọc Nút HTX trong phạm vi đọc; ngoài phạm vi hoặc không phải HTX → 404 (không lộ sự tồn tại).
func (s *Service) readCoop(ctx context.Context, tid uuid.UUID, sc authz.Scope, ref uuid.UUID) (*partyport.Org, error) {
	cur, err := s.coopOrg(ctx, tid, ref)
	if err != nil {
		return nil, err
	}
	if cur == nil || !visible(sc, cur.AdminUnitCode) {
		return nil, ErrOrgNotFound
	}
	return cur, nil
}

func parseOptArea(field string, v *string) (*decimal.Decimal, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil, true, nil
	}
	d, err := decimal.NewFromString(t)
	if err != nil || !domain.ValidArea(d) {
		return nil, true, problem.Validation.WithField(field, domain.ErrArea.Error())
	}
	return &d, true, nil
}

func (s *Service) profileReplay(ctx context.Context, tid uuid.UUID, key string) (*store.ProfileEvent, error) {
	var ev *store.ProfileEvent
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		ev, e = store.ProfileEventByIdem(ctx, tx, key)
		return e
	})
	return ev, err
}

func (s *Service) newProfileEvent(c knCaller, kind string, ref uuid.UUID, commune string, before, after []byte, reason, key string) store.ProfileEvent {
	ob, sid, sr := c.support()
	return store.ProfileEvent{ID: ids.New(), TenantID: c.p.TenantID, SubjectKind: kind, SubjectRef: ref, CommuneCode: commune, Action: "updated",
		Before: before, After: after, Reason: optReason(reason), OnBehalfOf: ob, SupportSessionID: sid, SupportReason: sr,
		EventTime: s.now(), ActorRef: c.p.ActorRef, IdemKey: key}
}

func optReason(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func snapshotJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err) // ảnh chụp chỉ gồm chuỗi, số, bool
	}
	return raw
}

func clampLog(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
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
