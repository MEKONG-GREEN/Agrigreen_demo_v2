package handler_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/google/uuid"
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
