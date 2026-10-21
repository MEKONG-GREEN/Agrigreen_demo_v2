package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

func TestIdempotencyBindsCurrentOwnerSessionPayload(t *testing.T) {
	type projectionState struct {
		payload []byte
		invalid bool
	}
	var state atomic.Pointer[projectionState]
	allowed := &projectionState{payload: []byte(`{"targets":["one"],"read_only":false}`)}
	state.Store(allowed)
	registry, err := authz.NewSessionRegistry(authz.SessionRegistration{Profile: "fixture", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		if p.KN == nil {
			return authz.Session{}, false, nil
		}
		current := state.Load()
		if current.invalid {
			return authz.Session{}, true, nil
		}
		binding, err := authz.NewSession("fixture", p.KN.ID, authz.SessionScope{NodeID: p.KN.OrgRef,
			Department: p.KN.Department, UnitCodes: p.KN.Communes, RequiresNode: p.KN.Role == authz.KNXa})
		return binding.WithPayload(current.payload), true, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	f := newKNFixture(t, registry)
	_, actor, created := f.createKN("Cán bộ kiểm dấu quyền", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	selected := f.ctxID(actor, asgIDs(created)[port.KNXa], deptTT)
	idem := key()
	call := func(k, contextID string) resp {
		headers := map[string]string{idempotency.Header: k}
		if contextID != "" {
			headers[authz.ContextHeader] = contextID
		}
		return f.req(f.app, actor, http.MethodPost, "/v1/probe/owner/write", map[string]any{"note": "same request"}, headers)
	}
	first := call(idem, selected)
	if first.status != http.StatusOK || first.hdr.Get(idempotency.ReplayedHeader) != "" {
		t.Fatalf("initial selected request: %d %v", first.status, first.body)
	}
	initial := f.lastProbe()
	legacy, err := json.Marshal(initial.KN)
	if err != nil {
		t.Fatal(err)
	}
	if repeat := call(idem, selected); repeat.status != http.StatusOK || repeat.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("same owner projection did not replay: %d %v", repeat.status, repeat.body)
	}
	for _, data := range [][]byte{
		[]byte(`{"targets":["two"],"read_only":false}`),
		[]byte(`{"targets":[],"read_only":true}`),
	} {
		state.Store(&projectionState{payload: data})
		changed := call(idem, selected)
		if changed.status != http.StatusUnprocessableEntity || changed.body["code"] != "idempotency_key_reused" || changed.hdr.Get(idempotency.ReplayedHeader) != "" {
			t.Fatalf("changed owner rights reused old authorization: %d %v", changed.status, changed.body)
		}
	}
	if len(f.probe) != 1 {
		t.Fatal("cache conflict ran the handler")
	}
	// Invalid owner results are rejected before cached responses are considered.
	state.Store(&projectionState{invalid: true})
	denied := call(idem, selected)
	if denied.status != http.StatusForbidden || denied.body["code"] != "accounts.context_invalid" || denied.hdr.Get(idempotency.ReplayedHeader) != "" {
		t.Fatalf("invalid owner projection reused cache: %d %v", denied.status, denied.body)
	}
	state.Store(allowed)
	if restored := call(idem, selected); restored.status != http.StatusOK || restored.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("restored exact authorization no longer replays: %d %v", restored.status, restored.body)
	}
	// A fresh key with changed owner state reaches the handler with the same
	// authenticated identity and legacy context, proving only owner rights changed.
	changedPayload := []byte(`{"targets":["two"],"read_only":false}`)
	state.Store(&projectionState{payload: changedPayload})
	if fresh := call(key(), selected); fresh.status != http.StatusOK {
		t.Fatalf("fresh key failed under changed owner projection: %d %v", fresh.status, fresh.body)
	}
	current := f.lastProbe()
	currentLegacy, err := json.Marshal(current.KN)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legacy, currentLegacy) || current.ActorRef != initial.ActorRef || current.TenantID != initial.TenantID || current.OrgRef != initial.OrgRef ||
		current.MFA != initial.MFA || !reflect.DeepEqual(current.Roles, initial.Roles) || current.SelectedSession == nil || !bytes.Equal(current.SelectedSession.Payload(), changedPayload) {
		t.Fatal("stamping changed identity, legacy authorization or owner payload")
	}
	// Native requests do not acquire a selected session or depend on owner state.
	nativeKey := key()
	if native := call(nativeKey, ""); native.status != http.StatusOK || f.lastProbe().SelectedSession != nil {
		t.Fatalf("native request acquired owner authorization: %d %v", native.status, native.body)
	}
	state.Store(&projectionState{invalid: true})
	if native := call(nativeKey, ""); native.status != http.StatusOK || native.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("native replay changed with unrelated owner state: %d %v", native.status, native.body)
	}
	if len(f.probe) != 3 {
		t.Fatalf("unexpected handler executions: %d", len(f.probe))
	}
}
