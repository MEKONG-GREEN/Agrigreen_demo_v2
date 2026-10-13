package handler_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// fakeUnits là danh mục đơn vị hành chính giả (masterdata đang được làm song song).
type fakeUnits struct {
	mu    sync.RWMutex
	units map[string]partyport.AdminUnit
}

// add thêm đơn vị (KN v7.7: tỉnh An Giang 91 và 102 xã).
func (f *fakeUnits) add(code, name string, lvl partyport.Level, parent string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.units[code] = partyport.AdminUnit{Code: code, Name: name, Level: lvl, ParentCode: parent}
}

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]partyport.AdminUnit{}}
	add := func(code, name string, lvl partyport.Level, parent string) {
		f.units[code] = partyport.AdminUnit{Code: code, Name: name, Level: lvl, ParentCode: parent}
	}
	add("89", "An Giang", partyport.LevelProvince, "")
	add("30001", "Xã A", partyport.LevelCommune, "89")
	add("30002", "Xã B", partyport.LevelCommune, "89")
	add("92", "Cần Thơ", partyport.LevelProvince, "")
	add("31001", "Xã C", partyport.LevelCommune, "92")
	return f
}

func (f *fakeUnits) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	u, ok := f.units[code]
	if !ok {
		return partyport.AdminUnit{}, partyport.ErrUnitNotFound
	}
	return u, nil
}

func (f *fakeUnits) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if _, ok := f.units[code]; !ok {
		return nil, partyport.ErrUnitNotFound
	}
	out := []string{code}
	for c, u := range f.units {
		if u.ParentCode == code {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeUnits) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

// fakeVault là kho định danh giả trong bộ nhớ: chống trùng theo SĐT/CCCD trong tenant.
type fakeVault struct {
	mu       sync.Mutex
	subjects map[uuid.UUID]client.Identity
	index    map[string]uuid.UUID // tenant|khóa mù → ref
	purposes []client.Purpose
}

func newFakeVault() *fakeVault {
	return &fakeVault{subjects: map[uuid.UUID]client.Identity{}, index: map[string]uuid.UUID{}}
}

var errNotImplemented = errors.New("fakeVault: không dùng trong kiểm thử")

func (v *fakeVault) Create(ctx context.Context, purpose client.Purpose, id client.Identity) (client.Created, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purposes = append(v.purposes, purpose)
	if purpose == "" {
		return client.Created{}, client.ErrNoPurpose
	}
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return client.Created{}, err
	}
	keys := []string{tid.String() + "|p:" + id.Phone}
	if id.NationalID != "" {
		keys = append(keys, tid.String()+"|n:"+id.NationalID)
	}
	for _, k := range keys {
		if ref, ok := v.index[k]; ok {
			return client.Created{Ref: ref}, client.ErrDuplicate
		}
	}
	ref := ids.New()
	v.subjects[ref] = id
	for _, k := range keys {
		v.index[k] = ref
	}
	return client.Created{Ref: ref, PublicKey: make([]byte, 32)}, nil
}

func (v *fakeVault) Display(_ context.Context, purpose client.Purpose, ref uuid.UUID) (client.Display, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purposes = append(v.purposes, purpose)
	id, ok := v.subjects[ref]
	if !ok {
		return client.Display{}, client.ErrNotFound
	}
	parts := strings.Fields(id.FullName)
	label := parts[0]
	if len(parts) > 1 {
		label += " " + string([]rune(parts[len(parts)-1])[0]) + "."
	}
	masked := "xxx"
	if len(id.Phone) >= 3 {
		masked = "xxx " + id.Phone[len(id.Phone)-3:]
	}
	return client.Display{Ref: ref, Label: label, MaskedPhone: masked}, nil
}

func (v *fakeVault) Resolve(_ context.Context, purpose client.Purpose, ref uuid.UUID) (client.Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purposes = append(v.purposes, purpose)
	id, ok := v.subjects[ref]
	if !ok {
		return client.Identity{}, client.ProblemNotFound
	}
	return id, nil
}

// Lookup tra chỉ mục mù theo SĐT (giống kho thật: giá trị không phải SĐT/CCCD → 422).
func (v *fakeVault) Lookup(ctx context.Context, purpose client.Purpose, value string) (uuid.UUID, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purposes = append(v.purposes, purpose)
	value = strings.TrimSpace(value)
	if len(value) < 9 || strings.Trim(value, "+0123456789") != "" {
		return uuid.Nil, problem.Validation.WithField("value", "phải là SĐT Việt Nam hoặc CCCD/CMND")
	}
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if ref, ok := v.index[tid.String()+"|p:"+value]; ok {
		return ref, nil
	}
	return uuid.Nil, client.ProblemNotFound
}

func (v *fakeVault) Unwrap(context.Context, client.Purpose, uuid.UUID, []byte) ([]byte, error) {
	return nil, errNotImplemented
}

func (v *fakeVault) Erase(context.Context, uuid.UUID, string) error { return errNotImplemented }

func (v *fakeVault) PubKeys(context.Context, []string) ([]client.PubKey, error) {
	return nil, errNotImplemented
}

func (v *fakeVault) HouseholdPubKeys(context.Context, []uuid.UUID) ([]client.PubKey, error) {
	return nil, errNotImplemented
}

func (v *fakeVault) usedPurposes() []client.Purpose {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]client.Purpose(nil), v.purposes...)
}

var _ client.Client = (*fakeVault)(nil)

// fakeCoopProfiles là hồ sơ HTX giả của pf/cooperative (partyport.CoopProfiles, W3.3) trong bộ nhớ, theo tenant:
// mã số thuế duy nhất trong tenant, sổ hồ sơ KN chống lặp theo idem_key. failCreate > 0: lần gọi CreateProfile kế
// tiếp lỗi (giả lập bước 2 của tạo HTX dừng giữa chừng).
type fakeCoopProfiles struct {
	mu         sync.Mutex
	profiles   map[uuid.UUID]map[uuid.UUID]partyport.CoopProfile // tenant → Nút → hồ sơ
	households map[uuid.UUID]int64                               // Nút → số hộ thành viên
	events     map[uuid.UUID][]partyport.CoopProfileEvent        // tenant → sổ
	idem       map[string]int                                    // tenant|khóa → vị trí+1 trong sổ
	changes    map[uuid.UUID][]partyport.CoopProfileChange       // tenant → các lần sửa đã ghi
	failCreate int
	failErr    error // lỗi của lần CreateProfile bị ép lỗi; nil = errFakeProfileDown
	creates    int
}

func newFakeCoopProfiles() *fakeCoopProfiles {
	return &fakeCoopProfiles{profiles: map[uuid.UUID]map[uuid.UUID]partyport.CoopProfile{}, households: map[uuid.UUID]int64{},
		events: map[uuid.UUID][]partyport.CoopProfileEvent{}, idem: map[string]int{},
		changes: map[uuid.UUID][]partyport.CoopProfileChange{}}
}

var errFakeProfileDown = errors.New("fakeCoopProfiles: pf/cooperative không phản hồi")

func (f *fakeCoopProfiles) tenantMap(ctx context.Context) (uuid.UUID, map[uuid.UUID]partyport.CoopProfile, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return uuid.Nil, nil, err
	}
	m := f.profiles[tid]
	if m == nil {
		m = map[uuid.UUID]partyport.CoopProfile{}
		f.profiles[tid] = m
	}
	return tid, m, nil
}

// failNext ép n lần CreateProfile kế tiếp lỗi err (nil = pf/cooperative không phản hồi).
func (f *fakeCoopProfiles) failNext(n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCreate, f.failErr = n, err
}

// setHouseholds đặt số hộ thành viên đang mở của HTX.
func (f *fakeCoopProfiles) setHouseholds(ref uuid.UUID, n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.households[ref] = n
}

// count trả số hồ sơ của tenant.
func (f *fakeCoopProfiles) count(tid uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.profiles[tid])
}

// profile đọc hồ sơ của Nút.
func (f *fakeCoopProfiles) profile(tid, ref uuid.UUID) (partyport.CoopProfile, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[tid][ref]
	return p, ok
}

// eventCount trả số dòng sổ hồ sơ KN của tenant (kèm điều kiện tùy chọn).
func (f *fakeCoopProfiles) eventCount(tid uuid.UUID, keep func(partyport.CoopProfileEvent) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events[tid] {
		if keep == nil || keep(e) {
			n++
		}
	}
	return n
}

// supportCount đếm lần sửa ghi ở chế độ hỗ trợ thay actor (on_behalf_of, có lý do hỗ trợ).
func (f *fakeCoopProfiles) supportCount(tid, actor uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.changes[tid] {
		if c.OnBehalfOf != nil && *c.OnBehalfOf == actor && c.SupportReason != nil {
			n++
		}
	}
	return n
}

func (f *fakeCoopProfiles) Profiles(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]partyport.CoopProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, m, err := f.tenantMap(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]partyport.CoopProfile{}
	for _, r := range refs {
		if p, ok := m[r]; ok {
			out[r] = p
		}
	}
	return out, nil
}

func (f *fakeCoopProfiles) taxOwner(m map[uuid.UUID]partyport.CoopProfile, tax string) uuid.UUID {
	for ref, p := range m {
		if tax != "" && p.TaxCode == tax {
			return ref
		}
	}
	return uuid.Nil
}

func (f *fakeCoopProfiles) CreateProfile(ctx context.Context, ref uuid.UUID, p partyport.CoopProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreate > 0 {
		f.failCreate--
		if f.failErr != nil {
			return f.failErr
		}
		return errFakeProfileDown
	}
	_, m, err := f.tenantMap(ctx)
	if err != nil {
		return err
	}
	if _, ok := m[ref]; ok {
		return nil
	}
	if o := f.taxOwner(m, p.TaxCode); o != uuid.Nil {
		return partyport.ErrCoopTaxCodeTaken
	}
	p.UpdatedAt = time.Now()
	m[ref] = p
	f.creates++
	return nil
}

func (f *fakeCoopProfiles) TaxCodeOwner(ctx context.Context, tax string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, m, err := f.tenantMap(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	return f.taxOwner(m, tax), nil
}

func (f *fakeCoopProfiles) MatchProfiles(ctx context.Context, q partyport.CoopProfileFilter) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, m, err := f.tenantMap(ctx)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for ref, p := range m {
		if q.TaxPrefix != "" && !strings.HasPrefix(p.TaxCode, q.TaxPrefix) {
			continue
		}
		if q.AreaMin != nil && (p.AreaHa == nil || p.AreaHa.LessThan(*q.AreaMin)) {
			continue
		}
		if q.AreaMax != nil && (p.AreaHa == nil || p.AreaHa.GreaterThan(*q.AreaMax)) {
			continue
		}
		if q.Confirmation != "" && p.Confirmed != (q.Confirmation == partyport.CoopConfirmed) {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

func (f *fakeCoopProfiles) ActiveHouseholds(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]int64{}
	for _, c := range coops {
		if n := f.households[c]; n > 0 {
			out[c] = n
		}
	}
	return out, nil
}

func (f *fakeCoopProfiles) UpdateProfile(ctx context.Context, c partyport.CoopProfileChange) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tid, m, err := f.tenantMap(ctx)
	if err != nil {
		return err
	}
	if f.idem[tid.String()+"|"+c.IdemKey] > 0 {
		return nil
	}
	if o := f.taxOwner(m, c.TaxCode); o != uuid.Nil && o != c.Ref {
		return partyport.ErrCoopTaxCodeTaken
	}
	p := m[c.Ref]
	p.AreaHa, p.TaxCode, p.Address, p.UpdatedAt = c.AreaHa, c.TaxCode, c.Address, time.Now()
	m[c.Ref] = p
	f.idem[tid.String()+"|"+c.IdemKey] = len(f.events[tid]) + 1
	f.changes[tid] = append(f.changes[tid], c)
	f.events[tid] = append(f.events[tid], partyport.CoopProfileEvent{SubjectRef: c.Ref, Action: "updated", Before: c.Before, After: c.After,
		Reason: c.Reason, SupportReason: c.SupportReason, ActorRef: c.ActorRef, EventTime: c.EventTime, RecordedAt: time.Now(),
		Seq: int64(len(f.events[tid]) + 1)})
	return nil
}

func (f *fakeCoopProfiles) ProfileEventByIdem(ctx context.Context, key string) (*partyport.CoopProfileEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	i := f.idem[tid.String()+"|"+key]
	if i == 0 {
		return nil, nil
	}
	e := f.events[tid][i-1]
	return &e, nil
}

func (f *fakeCoopProfiles) ProfileEvents(ctx context.Context, ref uuid.UUID) ([]partyport.CoopProfileEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	var out []partyport.CoopProfileEvent
	for _, e := range f.events[tid] {
		if e.SubjectRef == ref {
			out = append(out, e)
		}
	}
	return out, nil
}

var _ partyport.CoopProfiles = (*fakeCoopProfiles)(nil)
