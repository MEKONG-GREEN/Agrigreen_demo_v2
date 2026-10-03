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

// meta là siêu dữ liệu bắt buộc của hợp đồng van (mục 5, R-56).
const meta = `"version":1,"owner":"mkg/hub","proposed_by":"alice","approved_by":"bob","approved_at":"2026-09-29"`

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

func TestEventFieldTypeMustMatchSchema(t *testing.T) {
	root := t.TempDir()
	write(t, root, "contracts/events/pf.party.consent.recorded.v1.json", schema)
	write(t, root, "internal/platform/party/port/p.go", "package port\n\n//ag:event pf.party.consent.recorded v1\ntype E struct {\n\tConsentID int64 `json:\"consent_id\"`\n}\n")
	p, _ := Check(root, "")
	if len(p) != 1 || !strings.Contains(p[0], "consent_id kiểu Go ra JSON integer") {
		t.Fatalf("phải bắt sai kiểu: %v", p)
	}
}

func TestPublicationDefaultDeny(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/mkg/hub/port/p.go", "package port\n\n//ag:publication hub_capacity\ntype P struct {\n\tHubRef string `json:\"hub_ref\"`\n}\n")
	p, _ := Check(root, "")
	if len(p) != 1 || !strings.Contains(p[0], "mặc định từ chối") {
		t.Fatalf("không có hợp đồng phải bị từ chối: %v", p)
	}
	// Dạng cũ (fields không kiểu) vẫn được nhận nếu có đủ siêu dữ liệu phê duyệt.
	write(t, root, "contracts/publication/hub_capacity.json", `{"name":"hub_capacity",`+meta+`,"fields":["hub_ref"]}`)
	if p, _ := Check(root, ""); len(p) != 0 {
		t.Fatalf("có hợp đồng khớp phải qua: %v", p)
	}
	write(t, root, "internal/mkg/hub/port/p.go", "package port\n\n//ag:publication hub_capacity\ntype P struct {\n\tHubRef string `json:\"hub_ref\"`\n\tPrice string `json:\"price\"`\n}\n")
	if p, _ := Check(root, ""); len(p) != 1 || !strings.Contains(p[0], "P.price") {
		t.Fatalf("trường ngoài hợp đồng dạng cũ phải bị bắt: %v", p)
	}
}

// Mục 5 + R-56: hợp đồng thiếu người phê duyệt, hoặc tự phê duyệt, coi như không có.
func TestPublicationRequiresApproval(t *testing.T) {
	root := t.TempDir()
	write(t, root, "internal/mkg/hub/port/p.go", "package port\n\n//ag:publication hub_capacity\ntype P struct {\n\tHubRef string `json:\"hub_ref\"`\n}\n")
	for _, c := range []struct{ body, want string }{
		{`{"name":"hub_capacity","version":1,"owner":"mkg/hub","proposed_by":"alice","approved_at":"2026-09-29","fields":["hub_ref"]}`, "thiếu approved_by"},
		{`{"name":"hub_capacity","version":1,"owner":"mkg/hub","approved_by":"bob","approved_at":"2026-09-29","fields":["hub_ref"]}`, "thiếu proposed_by"},
		{`{"name":"hub_capacity","version":1,"proposed_by":"alice","approved_by":"bob","approved_at":"2026-09-29","fields":["hub_ref"]}`, "thiếu owner"},
		{`{"name":"hub_capacity","version":1,"owner":"mkg/hub","proposed_by":"alice","approved_by":"bob","fields":["hub_ref"]}`, "thiếu approved_at"},
		{`{"name":"hub_capacity","version":1,"owner":"mkg/hub","proposed_by":"alice","approved_by":"ALICE","approved_at":"2026-09-29","fields":["hub_ref"]}`, "approved_by phải khác proposed_by"},
		{`{"name":"hub_capacity","version":1,"owner":"mkg/hub","proposed_by":"alice","approved_by":"bob","approved_at":"2026-13-01","fields":["hub_ref"]}`, "approved_at phải có dạng"},
		{`{"name":"hub_capacity",` + meta + `,"properties":{"hub_ref":{}}}`, "thiếu type"},
		{`{"name":"hub_capacity",` + meta + `,"event":"hub capacity","fields":["hub_ref"]}`, "event phải có dạng"},
	} {
		write(t, root, "contracts/publication/hub_capacity.json", c.body)
		p, _ := Check(root, "")
		joined := strings.Join(p, "\n")
		if !strings.Contains(joined, c.want) || !strings.Contains(joined, "mặc định từ chối") {
			t.Fatalf("%s: muốn %q và từ chối struct, có:\n%s", c.body, c.want, joined)
		}
	}
	write(t, root, "contracts/publication/other.json", `{"name":"hub_capacity",`+meta+`,"fields":["hub_ref"]}`)
	if p, _ := Check(root, ""); !strings.Contains(strings.Join(p, "\n"), "phải trùng tên tệp") {
		t.Fatalf("tên hợp đồng khác tên tệp phải bị bắt: %v", p)
	}
}

// testdata/ok: trường lồng nhau, mảng struct, struct nhúng, kiểu có tên, payload
// van chứng minh được qua literal / biến / tham số / interface Publication.
func TestTestdataOK(t *testing.T) {
	p, err := Check(filepath.Join("testdata", "ok"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 0 {
		t.Fatalf("ca hợp lệ không được có vi phạm:\n%s", strings.Join(p, "\n"))
	}
}

// testdata/bad: mỗi vi phạm của cổng van công bố phải bị bắt.
func TestTestdataBad(t *testing.T) {
	p, err := Check(filepath.Join("testdata", "bad"), "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(p, "\n")
	for _, want := range []string{
		// siêu dữ liệu hợp đồng
		"no_meta.json: thiếu owner", "no_meta.json: thiếu proposed_by", "no_meta.json: thiếu approved_by", "no_meta.json: thiếu approved_at",
		"self_ok.json: approved_by phải khác proposed_by", "self_ok.json: approved_at phải có dạng YYYY-MM-DD",
		"SelfApproved chú thích ag:publication self_ok nhưng không có hợp đồng đã phê duyệt",
		// kiểu và cấu trúc lồng nhau
		"SitePublishedV1.location.lat kiểu Go ra JSON string", "SitePublishedV1.location.alt không có trong hợp đồng công bố depot_site",
		"SitePublishedV1.zones[].code kiểu Go ra JSON boolean", "SitePublishedV1.capacity_kg kiểu Go ra JSON integer",
		"SitePublishedV1.tags[] kiểu Go ra JSON integer", "SitePublishedV1.owner_phone không có trong hợp đồng công bố depot_site",
		"SitePublishedV1 thiếu trường required code",
		// liên kết hợp đồng ↔ sự kiện
		"chú thích ag:event mkg.depot.site.published v2 nhưng hợp đồng depot_site khai event mkg.depot.site.published v1",
		"schema sự kiện mkg.depot.site.published v1 có trường debug_trace ngoài hợp đồng van",
		// quy tắc vùng mkg
		"ThingHappenedV1 là payload sự kiện của vùng mkg nhưng không có //ag:publication",
		"Rogue cài đặt PublicationName nhưng không có //ag:publication",
		"SitePublishedV1.PublicationName phải là `return \"depot_site\"`",
		"có kiểu costRow không có //ag:publication — mặc định từ chối",
		"có kiểu ThingHappenedV1 không có //ag:publication",
		"không chứng minh được là hợp đồng van",
		"gán .Payload của outbox.Event trực tiếp",
		"outbox.Event phải dùng literal có tên trường",
		"//ag:publication chỉ dùng trong internal/mkg",
		// sự kiện nội bộ MKG (ADR-0018)
		"pf.party.thing.done.v1.json: sự kiện nội bộ phải thuộc vùng mkg",
		"mkg.depot.thing.happened.v1.json: mkg.depot.thing.happened đã là sự kiện nền tảng",
		"MovedV1.kg kiểu Go ra JSON integer, schema nội bộ mkg.depot.moved.done v1",
		"NoSchemaV1 chú thích ag:internal-event mkg.depot.ghost.happened v1 nhưng không có schema",
		"DualV1 không được vừa là sự kiện nội bộ vừa là sự kiện nền tảng / payload van",
		"//ag:internal-event chỉ dùng trong internal/mkg",
		"Payload MovedV1 là sự kiện nội bộ MKG (//ag:internal-event) — không được phát lên exchange nền tảng",
		"Payload của outbox.Event nội bộ (KindInternal) có kiểu SitePublishedV1 không có //ag:internal-event",
		"Kind của outbox.Event trong vùng mkg phải là hằng",
		"gán .Kind của outbox.Event trực tiếp",
		"outbox.KindInternal chỉ dùng trong internal/mkg",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("thiếu vi phạm %q", want)
		}
	}
	if t.Failed() {
		t.Log(joined)
	}
	// Integer khớp number (lng int) và struct khác outbox.Event có trường Payload không bị bắt nhầm.
	for _, not := range []string{"location.lng", "o.Payload"} {
		if strings.Contains(joined, not) {
			t.Errorf("báo nhầm %q", not)
		}
	}
}

// G5 (ADR-0022 mục 9): sản phẩm dữ liệu Public API — mặc định từ chối, đúng kiểu, chỉ vùng nền tảng,
// tag OpenAPI, không dữ liệu cá nhân.
func TestDataProductContracts_G5(t *testing.T) {
	const prod = `{"name":"x_prod","version":1,"owner":"pf/apiclient","sources":["pf/geo"],"data_owner":"bên cấp",
	"grant_kind":"dp_x_prod","scopes":["dp:x_prod:read"],"route":"GET /v1/data/x","status":"draft",
	"proposed_by":"a","approved_by":"b","approved_at":"2026-10-01","purpose":"p","personal_data":false,
	"type":"object","required":["code"],"properties":{"code":{"type":"string"},"rolls":{"type":"integer"}}}`
	root := t.TempDir()
	write(t, root, "internal/platform/apiclient/port/p.go", "package port\n\n//ag:dataproduct x_prod\ntype X struct {\n\tCode string `json:\"code\"`\n\tRolls int64 `json:\"rolls\"`\n}\n")
	p, _ := Check(root, "")
	if len(p) != 1 || !strings.Contains(p[0], "mặc định từ chối") {
		t.Fatalf("không có hợp đồng phải bị từ chối: %v", p)
	}
	write(t, root, "contracts/dataproduct/x_prod.json", prod)
	p, _ = Check(root, "")
	if len(p) != 1 || !strings.Contains(p[0], "thiếu contracts/openapi/public.yaml") {
		t.Fatalf("thiếu public.yaml phải bị bắt: %v", p)
	}
	write(t, root, "contracts/openapi/public.yaml", "openapi: 3.1.0\ntags:\n  - name: other\n")
	if p, _ = Check(root, ""); len(p) != 1 || !strings.Contains(p[0], `không có tag "x_prod"`) {
		t.Fatalf("thiếu tag phải bị bắt: %v", p)
	}
	write(t, root, "contracts/openapi/public.yaml", "openapi: 3.1.0\ntags:\n  - name: x_prod\n")
	if p, _ = Check(root, ""); len(p) != 0 {
		t.Fatalf("hợp đồng khớp phải qua: %v", p)
	}
	// Trường ngoài hợp đồng + sai kiểu.
	write(t, root, "internal/platform/apiclient/port/p.go", "package port\n\n//ag:dataproduct x_prod\ntype X struct {\n\tCode string `json:\"code\"`\n\tRolls string `json:\"rolls\"`\n\tHousehold string `json:\"household_ref\"`\n}\n")
	joined := func() string { p, _ := Check(root, ""); return strings.Join(p, "\n") }
	if j := joined(); !strings.Contains(j, "X.household_ref không có trong hợp đồng sản phẩm dữ liệu x_prod") || !strings.Contains(j, "X.rolls kiểu Go ra JSON string") {
		t.Fatalf("trường lạ / sai kiểu phải bị bắt:\n%s", j)
	}
	// Chỉ vùng nền tảng.
	write(t, root, "internal/platform/apiclient/port/p.go", "package port\n")
	write(t, root, "internal/mkg/hub/port/p.go", "package port\n\n//ag:dataproduct x_prod\ntype X struct {\n\tCode string `json:\"code\"`\n}\n")
	if j := joined(); !strings.Contains(j, "//ag:dataproduct chỉ dùng trong internal/platform") {
		t.Fatalf("chú thích ở vùng mkg phải bị bắt:\n%s", j)
	}
	write(t, root, "internal/mkg/hub/port/p.go", "package port\n")
	// Dữ liệu cá nhân / tự phê duyệt → hợp đồng coi như không có.
	write(t, root, "contracts/dataproduct/x_prod.json", strings.Replace(strings.Replace(prod, `"personal_data":false`, `"personal_data":true`, 1), `"approved_by":"b"`, `"approved_by":"a"`, 1))
	if j := joined(); !strings.Contains(j, "personal_data phải khai và bằng false") || !strings.Contains(j, "approved_by phải khác proposed_by") {
		t.Fatalf("siêu dữ liệu sai phải bị bắt:\n%s", j)
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

// ADR-0018: hai lần phát cùng một payload nội bộ trên exchange nền tảng phải bị
// bắt ở cả hai dạng (không ghi Kind, KindPlatform tường minh).
func TestInternalEventNeverOnPlatformExchange(t *testing.T) {
	p, err := Check(filepath.Join("testdata", "bad"), "")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range p {
		if strings.Contains(x, "Payload MovedV1 là sự kiện nội bộ MKG") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("muốn 2 vi phạm phát MovedV1 lên exchange nền tảng, có %d:\n%s", n, strings.Join(p, "\n"))
	}
}

// Mọi struct có dòng chỉ thị //ag:event trong repo đều được cổng nhận ra (không
// bỏ sót vì cách viết chú thích).
func TestRepositoryAnnotationsAllSeen(t *testing.T) {
	ix, err := buildIndex("../..")
	if err != nil {
		t.Fatal(err)
	}
	directives, seen := 0, 0
	for _, sf := range ix.files {
		for _, cg := range sf.file.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//ag:event ") {
					directives++
				}
			}
		}
	}
	for _, types := range ix.types {
		for _, d := range types {
			if eventTagRe.MatchString(d.doc) {
				seen++
			}
		}
	}
	if directives == 0 || directives != seen {
		t.Fatalf("có %d chỉ thị //ag:event nhưng cổng nhận %d struct", directives, seen)
	}
}
