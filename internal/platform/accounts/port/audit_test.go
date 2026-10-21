package port_test

import (
	"context"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

func auditNoRows(context.Context, uuid.UUID, port.AuditReadQuery) ([]port.AuditRecord, error) {
	return nil, nil
}
func TestAuditRegistryCopiesAndSanitizedValues(t *testing.T) {
	fields := map[string]port.AuditValueKind{"enabled": port.AuditBoolean, "roles": port.AuditCodes}
	src := port.AuditSource{Key: "example.events", Module: "example", Fields: fields, Read: auditNoRows}
	reg, err := port.NewAuditRegistry(src)
	if err != nil {
		t.Fatal(err)
	}
	fields["raw"] = port.AuditCodes
	delete(fields, "enabled")
	out := reg.Sources()
	delete(out[0].Fields, "enabled")
	out[0].Key = "changed"
	if reg.Sources()[0].Fields["enabled"] != port.AuditBoolean || reg.Sources()[0].Key != "example.events" {
		t.Fatal("registration aliases caller data")
	}
	now := time.Now().UTC()
	yes := true
	r := port.AuditRecord{EventRef: ids.New(), EntityRef: ids.New(), ActorRef: ids.New(), OccurredAt: now, RecordedAt: now, Action: "created", Outcome: "unknown", Changes: []port.AuditChange{{Field: "enabled", After: &port.AuditValue{Kind: port.AuditBoolean, Boolean: &yes}}}}
	if err := reg.ValidateRecord(src.Key, r); err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.Changes = []port.AuditChange{{Field: "raw", After: &port.AuditValue{Kind: port.AuditCodes, Codes: []string{"private"}}}}
	if reg.ValidateRecord(src.Key, bad) == nil {
		t.Fatal("unregistered field admitted")
	}
	bad = r
	bad.Changes = []port.AuditChange{{Field: "roles", After: &port.AuditValue{Kind: port.AuditCodes, Codes: []string{"person@example.test"}}}}
	if reg.ValidateRecord(src.Key, bad) == nil {
		t.Fatal("free text admitted as code")
	}
	bad = r
	bad.Changes = []port.AuditChange{{Field: "enabled", After: &port.AuditValue{Kind: port.AuditBoolean}}}
	if reg.ValidateRecord(src.Key, bad) == nil {
		t.Fatal("missing typed value admitted")
	}
	bad = r
	bad.Outcome = ""
	if reg.ValidateRecord(src.Key, bad) == nil {
		t.Fatal("outcome inferred")
	}
}
func TestAuditRegistryRejectsMalformedSources(t *testing.T) {
	valid := port.AuditSource{Key: "example.events", Module: "example", Read: auditNoRows}
	cases := [][]port.AuditSource{nil, {valid, valid}, {{Key: "bad source", Module: "example", Read: auditNoRows}}, {{Key: "example.events", Module: "example"}}, {{Key: "example.events", Module: "example", Read: auditNoRows, Fields: map[string]port.AuditValueKind{"raw": "text"}}}}
	for i, cs := range cases {
		if _, err := port.NewAuditRegistry(cs...); err == nil {
			t.Fatalf("case%d accepted", i)
		}
	}
}
