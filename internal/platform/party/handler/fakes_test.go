package handler_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// fakeUnits là danh mục đơn vị hành chính giả (masterdata đang được làm song song).
type fakeUnits struct{ units map[string]mdport.AdminUnit }

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]mdport.AdminUnit{}}
	add := func(code, name string, lvl mdport.Level, parent string) {
		f.units[code] = mdport.AdminUnit{Code: code, Name: name, Level: lvl, ParentCode: parent}
	}
	add("89", "An Giang", mdport.LevelProvince, "")
	add("30001", "Xã A", mdport.LevelCommune, "89")
	add("30002", "Xã B", mdport.LevelCommune, "89")
	add("92", "Cần Thơ", mdport.LevelProvince, "")
	add("31001", "Xã C", mdport.LevelCommune, "92")
	return f
}

func (f *fakeUnits) Get(_ context.Context, code string, _ time.Time) (mdport.AdminUnit, error) {
	u, ok := f.units[code]
	if !ok {
		return mdport.AdminUnit{}, mdport.ErrUnitNotFound
	}
	return u, nil
}

func (f *fakeUnits) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	if _, ok := f.units[code]; !ok {
		return nil, mdport.ErrUnitNotFound
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

func (v *fakeVault) Resolve(context.Context, client.Purpose, uuid.UUID) (client.Identity, error) {
	return client.Identity{}, errNotImplemented
}

func (v *fakeVault) Lookup(context.Context, client.Purpose, string) (uuid.UUID, error) {
	return uuid.Nil, errNotImplemented
}

func (v *fakeVault) Unwrap(context.Context, client.Purpose, uuid.UUID, []byte) ([]byte, error) {
	return nil, errNotImplemented
}

func (v *fakeVault) Erase(context.Context, uuid.UUID, string) error { return errNotImplemented }

func (v *fakeVault) PubKeys(context.Context, []string) ([]client.PubKey, error) {
	return nil, errNotImplemented
}

func (v *fakeVault) usedPurposes() []client.Purpose {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]client.Purpose(nil), v.purposes...)
}

var _ client.Client = (*fakeVault)(nil)
