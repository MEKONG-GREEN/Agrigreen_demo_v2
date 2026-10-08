package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
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

func (f fakeParams) At(_ context.Context, key string, at time.Time) (params.ParamValue, error) {
	if key != port.ParamConflictSLAHours || f.hours == nil {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: *f.hours, Unit: "h", Version: 1, EffectiveFrom: at}, nil
}

// fakeFiles thay pf/evidence (W6.5 — sync/handler chỉ import port/ của module khác, R-02): lưu theo
// (tenant, file_ref), ghi lại đầu vào để kiểm route /v1/files. Quy tắc tệp (phong bì, băm, chủ thể,
// Open(purpose), đồng ý, sổ truy cập) kiểm ở internal/platform/evidence/app.
type fakeFiles struct {
	mu       sync.Mutex
	files    map[[2]uuid.UUID]evport.File
	blobs    map[[2]uuid.UUID][]byte
	attached []evport.AttachInput
	purposes []string
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{files: map[[2]uuid.UUID]evport.File{}, blobs: map[[2]uuid.UUID][]byte{}}
}

func (f *fakeFiles) Attach(ctx context.Context, in evport.AttachInput) (evport.File, bool, error) {
	p, _ := authz.FromContext(ctx)
	ref := ids.New()
	if in.FileRef != nil {
		r, err := uuid.Parse(*in.FileRef)
		if err != nil {
			return evport.File{}, false, problem.Validation.WithField("file_ref", "phải là UUIDv7")
		}
		ref = r
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached = append(f.attached, in)
	k := [2]uuid.UUID{p.TenantID, ref}
	if old, ok := f.files[k]; ok {
		if old.SHA256Ciphertext != in.Meta.SHA256Ciphertext {
			return evport.File{}, false, problem.New(http.StatusConflict, "sync.file_ref_taken", "file_ref đã dùng")
		}
		return old, false, nil
	}
	file := evport.File{TenantID: p.TenantID, Ref: ref, SHA256Ciphertext: in.Meta.SHA256Ciphertext, Size: int64(len(in.Blob)),
		ContentType: in.Meta.ContentType, UploadedBy: p.ActorRef, DeviceID: in.DeviceID, Subject: in.Subject, CreatedAt: time.Now()}
	f.files[k], f.blobs[k] = file, in.Blob
	return file, true, nil
}

func (f *fakeFiles) find(ctx context.Context, ref uuid.UUID, purpose string) (evport.File, []byte, error) {
	p, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purposes = append(f.purposes, purpose)
	if purpose == "" {
		return evport.File{}, nil, problem.New(http.StatusBadRequest, "sync.purpose_required", "Thiếu mục đích")
	}
	k := [2]uuid.UUID{p.TenantID, ref}
	file, ok := f.files[k]
	if !ok {
		return evport.File{}, nil, problem.New(http.StatusNotFound, "sync.file_not_found", "Không tìm thấy tệp")
	}
	return file, f.blobs[k], nil
}

func (f *fakeFiles) Open(ctx context.Context, ref uuid.UUID, purpose string) (evport.Opened, error) {
	file, _, err := f.find(ctx, ref, purpose)
	return evport.Opened{File: file, URL: "http://objstore.test/" + ref.String(), ExpiresAt: time.Now().Add(5 * time.Minute)}, err
}

func (f *fakeFiles) OpenContent(ctx context.Context, ref uuid.UUID, purpose string) (string, []byte, error) {
	file, blob, err := f.find(ctx, ref, purpose)
	return file.ContentType, blob, err
}

// memSource là Source trong bộ nhớ: mỗi thay đổi có cursor = vị trí + 1 và mã
// đơn vị để lọc theo phạm vi.
type memSource struct {
	mu       sync.Mutex
	entities []string
	changes  map[string][]memChange
	calls    atomic.Int64
	forbid   map[string]bool // entity → trả 403 (người gọi không có vai đọc)
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
	if s.forbid[entity] {
		return nil, after, problem.Forbidden
	}
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
