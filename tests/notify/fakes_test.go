package notify_test

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"sync"
	"time"
)

// Mã đơn vị hành chính chính thức (seed masterdata, sau sắp xếp 2025):
// tỉnh An Giang 91 gồm phường Bình Đức 30292, Mỹ Thới 30301; Cần Thơ 92 gồm Cái Khế 31120.
const (
	prov91   = "91"
	ward1    = "30292"
	ward2    = "30301"
	prov92   = "92"
	ward92   = "31120"
	fcmToken = "fcm-token-thu-nghiem-0123456789abcdef"
)

// fakeScope trả phạm vi theo org_ref của người gọi.
type fakeScope struct {
	mu     sync.Mutex
	scopes map[uuid.UUID]authz.Scope
}

func (f *fakeScope) set(org uuid.UUID, s authz.Scope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes[org] = s
}

func (f *fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scopes[p.OrgRef], nil
}

// fakeDevices là sổ thiết bị giả của pf/sync.
type fakeDevices struct {
	mu sync.Mutex
	m  map[[2]uuid.UUID]syncport.DeviceInfo
}

func (f *fakeDevices) add(tid uuid.UUID, d syncport.DeviceInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[[2]uuid.UUID{tid, d.ID}] = d
}

func (f *fakeDevices) Device(_ context.Context, tid, id uuid.UUID) (syncport.DeviceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.m[[2]uuid.UUID{tid, id}]
	if !ok {
		return syncport.DeviceInfo{}, syncport.ErrDeviceNotFound
	}
	return d, nil
}

// fakeStaff là danh bạ cán bộ giả (theo tenant).
type fakeStaff struct {
	mu    sync.Mutex
	staff map[uuid.UUID][]notifyport.StaffRef
}

func (f *fakeStaff) add(tid uuid.UUID, s notifyport.StaffRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.staff[tid] = append(f.staff[tid], s)
}

func (f *fakeStaff) ActiveStaff(_ context.Context, tid uuid.UUID, units []string) ([]notifyport.StaffRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []notifyport.StaffRef
	for _, s := range f.staff[tid] {
		for _, u := range units {
			if s.UnitCode == u || s.ProvinceCode == u {
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
}

// fakeUnits: cây tỉnh → xã tối thiểu.
type fakeUnits struct{}

var tree = map[string][]string{prov91: {prov91, ward1, ward2}, prov92: {prov92, ward92}}

func (fakeUnits) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	return partyport.AdminUnit{Code: code}, nil
}

func (fakeUnits) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	if d, ok := tree[code]; ok {
		return d, nil
	}
	for _, kids := range tree {
		for _, k := range kids {
			if k == code {
				return []string{code}, nil
			}
		}
	}
	return nil, partyport.ErrUnitNotFound
}

func (fakeUnits) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

// fakePusher ghi lại thông điệp; token trong invalid → ErrTokenInvalid, trong flaky → lỗi tạm.
type fakePusher struct {
	mu      sync.Mutex
	sent    []notify.PushMessage
	bodies  [][]byte
	invalid map[string]bool
	flaky   map[string]int
}

func (p *fakePusher) Name() string { return "fake" }

func (p *fakePusher) Send(_ context.Context, m notify.PushMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.invalid[m.Token] {
		return notify.ErrTokenInvalid
	}
	if p.flaky[m.Token] > 0 {
		p.flaky[m.Token]--
		return context.DeadlineExceeded
	}
	body, err := notify.BuildFCMRequest(m)
	if err != nil {
		return err
	}
	p.sent = append(p.sent, m)
	p.bodies = append(p.bodies, body)
	return nil
}

func (p *fakePusher) messages() []notify.PushMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]notify.PushMessage(nil), p.sent...)
}
