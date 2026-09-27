package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const schema = `{"x-event-type":"pf.party.consent.recorded","x-schema-version":1,
 "required":["consent_id"],"properties":{"consent_id":{"type":"string"},"note":{"type":"string"}}}`

func TestEventStructMustMatchSchema(t *testing.T) {
	root := t.TempDir()
	write(t, root, "contracts/events/pf.party.consent.recorded.v1.json", schema)
	write(t, root, "internal/platform/party/port/p.go", "package port\n\n//ag:event pf.party.consent.recorded v1\ntype E struct {\n\tConsentID string `json:\"consent_id\"`\n\tPhone string `json:\"phone\"`\n}\n")
	p, err := Check(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || !strings.Contains(p[0], "phone") {
		t.Fatalf("phải bắt trường ngoài hợp đồng 'phone': %v", p)
	}
}

func TestPublicationDefaultDeny(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/mkg/hub/port/p.go", "package port\n\n//ag:publication hub_capacity\ntype P struct {\n\tHubRef string `json:\"hub_ref\"`\n}\n")
	p, _ := Check(root, "")
	if len(p) != 1 || !strings.Contains(p[0], "mặc định từ chối") {
		t.Fatalf("không có hợp đồng phải bị từ chối: %v", p)
	}
	write(t, root, "contracts/publication/hub_capacity.json", `{"name":"hub_capacity","fields":["hub_ref"]}`)
	if p, _ := Check(root, ""); len(p) != 0 {
		t.Fatalf("có hợp đồng khớp phải qua: %v", p)
	}
}

func TestFileNameMustMatch(t *testing.T) {
	root := t.TempDir()
	write(t, root, "contracts/events/pf.party.consent.recorded.v2.json", schema)
	p, _ := Check(root, "")
	if len(p) == 0 || !strings.Contains(p[0], "không khớp tên tệp") {
		t.Fatalf("phiên bản trong tệp khác tên tệp phải bị bắt: %v", p)
	}
}

func TestRepositoryContractsPass(t *testing.T) {
	p, err := Check("../..", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 0 {
		t.Fatalf("hợp đồng của repo vi phạm: %v", p)
	}
}
