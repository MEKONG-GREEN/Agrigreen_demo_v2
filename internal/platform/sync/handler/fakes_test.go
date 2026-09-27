package handler_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// fakeScope trả phạm vi theo org_ref của người gọi (party đang được làm song song).
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

// fakeParams trả sync.conflict_sla_hours nếu đã đặt.
type fakeParams struct{ hours *decimal.Decimal }

func (f fakeParams) At(_ context.Context, key string, at time.Time) (mdport.ParamValue, error) {
	if key != port.ParamConflictSLAHours || f.hours == nil {
		return mdport.ParamValue{}, mdport.ErrParamNotFound
	}
	return mdport.ParamValue{Key: key, Value: *f.hours, Unit: "h", Version: 1, EffectiveFrom: at}, nil
}

// fakeConsent từ chối các chủ thể trong denied.
type fakeConsent struct {
	mu     sync.Mutex
	denied map[uuid.UUID]bool
}

func (f *fakeConsent) HasConsent(_ context.Context, subject uuid.UUID, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.denied[subject], nil
}

// memSource là Source trong bộ nhớ: mỗi thay đổi có cursor = vị trí + 1 và mã
// đơn vị để lọc theo phạm vi.
type memSource struct {
	mu       sync.Mutex
	entities []string
	changes  map[string][]memChange
	calls    atomic.Int64
}

type memChange struct {
	unit string
	ch   port.Change
}

func newMemSource(entities ...string) *memSource {
	return &memSource{entities: entities, changes: map[string][]memChange{}}
}

func (s *memSource) add(entity, unit string, n int) []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uuid.UUID
	for range n {
		id := ids.New()
		data, _ := json.Marshal(map[string]any{"unit": unit, "n": len(s.changes[entity]) + 1})
		s.changes[entity] = append(s.changes[entity], memChange{unit: unit,
			ch: port.Change{Entity: entity, ID: id, Version: 1, Op: port.OpUpsert, Data: data}})
		out = append(out, id)
	}
	return out
}

func (s *memSource) Entities() []string { return s.entities }

func (s *memSource) Changes(_ context.Context, scope authz.Scope, entity string, after int64, limit int) ([]port.Change, int64, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []port.Change
	last := after
	for i, c := range s.changes[entity] {
		cur := int64(i + 1)
		if cur <= after || !scope.Allows(c.unit) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, c.ch)
		last = cur
	}
	return out, last, nil
}

// memSink là Sink trong bộ nhớ, idempotent theo idem_key. Hành vi theo payload.behave:
// "conflict" → xung đột (conflict_id cố định theo payload.conflict_key), "reject" →
// rejected, "panic" → hoảng, còn lại → accepted.
type memSink struct {
	mu        sync.Mutex
	entities  []string
	seen      map[string]port.Result
	conflicts map[string]uuid.UUID
	calls     atomic.Int64
}

func newMemSink(entities ...string) *memSink {
	return &memSink{entities: entities, seen: map[string]port.Result{}, conflicts: map[string]uuid.UUID{}}
}

func (s *memSink) Entities() []string { return s.entities }

func (s *memSink) Accept(_ context.Context, _ authz.Scope, rec port.Record) port.Result {
	s.calls.Add(1)
	var p struct {
		Behave      string `json:"behave"`
		ConflictKey string `json:"conflict_key"`
	}
	_ = json.Unmarshal(rec.Payload, &p)
	if p.Behave == "panic" {
		panic("memSink: hoảng theo yêu cầu kiểm thử")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[rec.IdemKey]; ok {
		return port.Result{ID: rec.ID, Status: port.StatusDuplicate}
	}
	var res port.Result
	switch p.Behave {
	case "conflict":
		cid, ok := s.conflicts[p.ConflictKey]
		if !ok {
			cid = ids.New()
			s.conflicts[p.ConflictKey] = cid
		}
		res = port.Result{ID: rec.ID, Status: port.StatusConflict, ConflictID: &cid}
	case "reject":
		return port.Result{ID: rec.ID, Status: port.StatusRejected, Code: "validation_failed",
			Errors: []problem.FieldError{{Field: "payload.area_ha", Reason: "phải dương"}}}
	default:
		res = port.Result{ID: rec.ID, Status: port.StatusAccepted}
	}
	s.seen[rec.IdemKey] = res
	return res
}
