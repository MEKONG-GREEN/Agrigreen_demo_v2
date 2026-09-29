package handler_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// fakeFarmers là liên kết tài khoản nông dân ↔ nông hộ (pf/accounts).
type fakeFarmers struct {
	mu    sync.Mutex
	links map[uuid.UUID][]accport.HouseholdLink
}

func (f *fakeFarmers) link(actor uuid.UUID, l accport.HouseholdLink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[actor] = append(f.links[actor], l)
}

func (f *fakeFarmers) HouseholdsForSubject(_ context.Context, subject uuid.UUID) ([]accport.HouseholdLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.links[subject]), nil
}

// memFarmerSource là FarmerSource trong bộ nhớ: mỗi thay đổi gắn một nông hộ (hh) hoặc là
// dữ liệu công khai (hh = Nil); staff lọc theo đơn vị như memSource.
type memFarmerSource struct {
	mu       sync.Mutex
	entities []string
	farmer   []string
	rows     map[string][]farmerRow
	calls    atomic.Int64
	seen     []port.FarmerScope
}

type farmerRow struct {
	hh   uuid.UUID
	unit string
	ch   port.Change
}

func (s *memFarmerSource) add(entity string, hh uuid.UUID, unit string) uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := ids.New()
	data, _ := json.Marshal(map[string]any{"household_ref": hh.String()})
	s.rows[entity] = append(s.rows[entity], farmerRow{hh: hh, unit: unit, ch: port.Change{Entity: entity, ID: id, Version: 1, Op: port.OpUpsert, Data: data}})
	return id
}

func (s *memFarmerSource) Entities() []string       { return s.entities }
func (s *memFarmerSource) FarmerEntities() []string { return s.farmer }

func (s *memFarmerSource) page(entity string, after int64, limit int, keep func(farmerRow) bool) ([]port.Change, int64) {
	var out []port.Change
	last := after
	for i, r := range s.rows[entity] {
		cur := int64(i + 1)
		if cur <= after || !keep(r) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, r.ch)
		last = cur
	}
	return out, last
}

func (s *memFarmerSource) Changes(_ context.Context, sc authz.Scope, entity string, after int64, limit int) ([]port.Change, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, last := s.page(entity, after, limit, func(r farmerRow) bool { return sc.Allows(r.unit) })
	return out, last, nil
}

func (s *memFarmerSource) FarmerChanges(_ context.Context, fs port.FarmerScope, entity string, after int64, limit int) ([]port.Change, int64, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, fs)
	out, last := s.page(entity, after, limit, func(r farmerRow) bool { return r.hh == uuid.Nil || slices.Contains(fs.Households, r.hh) })
	return out, last, nil
}

func count(pg pullPage) map[string]int {
	got := map[string]int{}
	for _, ch := range pg.Changes {
		got[ch.Entity]++
	}
	return got
}

func pubkeyRefs(t *testing.T, pg pullPage) []string {
	t.Helper()
	var out []string
	for _, ch := range pg.Changes {
		if ch.Entity != port.EntityPubKey || ch.Op != port.OpUpsert {
			continue
		}
		var d struct {
			Ref       string `json:"ref"`
			PublicKey string `json:"public_key"`
		}
		if err := json.Unmarshal(ch.Data, &d); err != nil {
			t.Fatal(err)
		}
		if b, err := base64.StdEncoding.DecodeString(d.PublicKey); err != nil || len(b) != 32 {
			t.Fatalf("public_key phải là base64 32 byte: %q", d.PublicKey)
		}
		out = append(out, d.Ref)
	}
	slices.Sort(out)
	return out
}

// App HTX (HTX US-SEASON-01, US-LOG-01, US-SUPPORT-01): tài khoản nông dân (vai farmer,
// token KHÔNG có org_ref → phạm vi pf/party rỗng) pull được các entity của FarmerSource
// theo nông hộ liên kết — dữ liệu của nông hộ khác cùng xã không đi xuống; Source không
// phục vụ nông dân (plot/task của memSource) không bao giờ được gọi. Khóa công khai: chỉ
// của nông hộ mình (R-40, không cần vai vault_pubkey_sync). Đổi liên kết → reset.
func TestFarmerPullByLinkedHouseholds(t *testing.T) {
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	fsrc := &memFarmerSource{entities: []string{"season", "sop_version", "staff_only"}, farmer: []string{"season", "sop_version"}, rows: map[string][]farmerRow{}}
	f := newFixture(t, opts{extra: []port.Source{fsrc}, farmers: farmers})

	// Nông hộ trong kho định danh (household_ref = ref của chủ thể), cùng xã unitA.
	reg := f.as(f.tenant, uuid.Nil, client.RoleRegistrar)
	mk := func(phone string) uuid.UUID {
		c, err := f.vault.Create(reg.ctx(), client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold, FullName: "Hộ " + phone, Phone: phone, AreaCode: unitA})
		if err != nil {
			t.Fatal(err)
		}
		return c.Ref
	}
	hh1, hh2 := mk("0901234561"), mk("0901234562")
	coop := ids.New()

	fsrc.add("season", hh1, unitA)
	fsrc.add("season", hh1, unitA)
	fsrc.add("season", hh2, unitA) // hộ khác cùng xã
	fsrc.add("sop_version", uuid.Nil, unitA)
	fsrc.add("staff_only", hh1, unitA) // entity không có trong FarmerEntities
	f.source.add("plot", unitA, 3)
	f.source.add("task", unitA, 2)
	before := f.source.calls.Load()

	farmer := f.as(f.tenant, uuid.Nil, accport.RoleFarmer)
	farmer.register()
	farmers.link(farmer.actor, accport.HouseholdLink{HouseholdRef: hh1, CooperativeRef: coop, CommuneCode: unitA})

	pg, r := farmer.pull("", 100)
	if r.status != http.StatusOK {
		t.Fatalf("pull nông dân: %d %s", r.status, r.raw)
	}
	got := count(pg)
	if got["season"] != 2 || got["sop_version"] != 1 || got["plot"] != 0 || got["task"] != 0 || got["staff_only"] != 0 || got[port.EntityPubKey] != 2 {
		t.Fatalf("nông dân chỉ nhận dữ liệu nông hộ mình + công khai + khóa hộ mình: %v", got)
	}
	if refs := pubkeyRefs(t, pg); len(refs) != 1 || refs[0] != hh1.String() {
		t.Fatalf("khóa công khai chỉ của hộ liên kết: %v", refs)
	}
	if f.source.calls.Load() != before {
		t.Fatal("Source không phục vụ nông dân không được gọi")
	}
	fsrc.mu.Lock()
	sc := fsrc.seen[len(fsrc.seen)-1]
	fsrc.mu.Unlock()
	if len(sc.Households) != 1 || sc.Households[0] != hh1 || len(sc.Cooperatives) != 1 || sc.Cooperatives[0] != coop || len(sc.Communes) != 1 || sc.Communes[0] != unitA {
		t.Fatalf("FarmerScope: %+v", sc)
	}

	// Pull tiếp từ con trỏ: không có gì mới.
	pg2, _ := farmer.pull(pg.NextCursor, 100)
	if len(pg2.Changes) != 0 || pg2.Reset {
		t.Fatalf("pull lặp: %+v", pg2)
	}

	// Liên kết thêm nông hộ hh2 → phạm vi đổi → reset, nhận lại đủ kèm dữ liệu và khóa hh2.
	farmers.link(farmer.actor, accport.HouseholdLink{HouseholdRef: hh2, CooperativeRef: coop, CommuneCode: unitA})
	pg3, _ := farmer.pull(pg2.NextCursor, 100)
	if !pg3.Reset || count(pg3)["season"] != 3 {
		t.Fatalf("đổi liên kết → reset: reset=%v %v", pg3.Reset, count(pg3))
	}
	if refs := pubkeyRefs(t, pg3); len(refs) != 2 {
		t.Fatalf("khóa của 2 hộ liên kết: %v", refs)
	}

	// Cán bộ (có org_ref, phạm vi unitA) vẫn đi đường cũ: nhận plot/task/staff_only theo xã.
	officer := f.officer()
	officer.register()
	op, _ := officer.pull("", 100)
	if g := count(op); g["plot"] != 3 || g["task"] != 2 || g["staff_only"] != 1 || g["season"] != 3 {
		t.Fatalf("pull cán bộ không đổi: %v", g)
	}

	// Tài khoản không có vai farmer, không org_ref → không nhận gì (mặc định chặn).
	nobody := f.as(f.tenant, uuid.Nil, "kn_officer")
	nobody.register()
	farmers.link(nobody.actor, accport.HouseholdLink{HouseholdRef: hh1, CommuneCode: unitA})
	if np, _ := nobody.pull("", 100); len(np.Changes) != 0 {
		t.Fatalf("không vai farmer: %v", count(np))
	}

	// Nông dân tenant khác (cùng liên kết giả) không thấy khóa của tenant này.
	otherT := f.as(ids.New(), uuid.Nil, accport.RoleFarmer)
	otherT.register()
	farmers.link(otherT.actor, accport.HouseholdLink{HouseholdRef: hh1, CommuneCode: unitA})
	opg, _ := otherT.pull("", 100)
	if refs := pubkeyRefs(t, opg); len(refs) != 0 {
		t.Fatalf("khóa chéo tenant: %v", refs)
	}
}

// Chưa nối pf/accounts (UseFarmers) → thiết bị nông dân không nhận gì, như trước.
func TestFarmerPullWithoutAccountsIsClosed(t *testing.T) {
	fsrc := &memFarmerSource{entities: []string{"season"}, farmer: []string{"season"}, rows: map[string][]farmerRow{}}
	f := newFixture(t, opts{extra: []port.Source{fsrc}})
	fsrc.add("season", uuid.Nil, unitA)
	farmer := f.as(f.tenant, uuid.Nil, accport.RoleFarmer)
	farmer.register()
	pg, r := farmer.pull("", 100)
	if r.status != http.StatusOK || len(pg.Changes) != 0 || fsrc.calls.Load() != 0 {
		t.Fatalf("chưa nối pf/accounts: %d %v", r.status, count(pg))
	}
}
