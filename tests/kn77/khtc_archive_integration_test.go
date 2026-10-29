package kn77_test

import (
	"bytes"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"testing"
)

type archiveResult struct {
	Items []struct {
		ID         uuid.UUID `json:"id"`
		DocumentNo string    `json:"document_no"`
		Final      bool      `json:"final"`
	} `json:"items"`
	HasMore bool `json:"has_more"`
}

func TestW1169_ExportArchiveImmutableReadOnly(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	read := func(query string) archiveResult {
		t.Helper()
		var out archiveResult
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?"+query, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		f.ok(c, b, &out)
		return out
	}
	if got := read("year=2026"); len(got.Items) != 0 {
		t.Fatal("draft leaked", got)
	}
	d := f.refresh(p.ID, 0)
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"document_no": "12/BC-TTKN"}})
	f.ok(c, b, &d)
	x1 := f.export(p.ID, d.Version, true)
	frozen := f.download(p.ID, x1.ID)
	c, b = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"document_no": "13/BC-TTKN"}})
	f.ok(c, b, &d)
	x2 := f.export(p.ID, d.Version, true)
	got := read("year=2026")
	if len(got.Items) != 1 || got.Items[0].ID.String() != x2.ID || got.Items[0].DocumentNo != "13/BC-TTKN" {
		t.Fatalf("latest %+v", got)
	}
	got = read("year=2026&versions=true&limit=1")
	if len(got.Items) != 1 || !got.HasMore {
		t.Fatal("pagination", got)
	}
	got = read("year=2026&versions=true&limit=1&offset=1")
	if len(got.Items) != 1 || got.HasMore || got.Items[0].ID.String() != x1.ID || got.Items[0].DocumentNo != "12/BC-TTKN" {
		t.Fatal("frozen metadata", got)
	}
	if got = read("year=2026&versions=true&q=12%2FBC"); len(got.Items) != 1 || got.Items[0].ID.String() != x1.ID {
		t.Fatal("search", got)
	}
	for _, q := range []string{"year=2025", "year=2026&kind=year", "year=2026&q=%25"} {
		if got = read(q); len(got.Items) != 0 {
			t.Fatal("filter", q, got)
		}
	}
	c, b = f.do(http.MethodGet, reportPath(p.ID)+"/exports/"+x1.ID+"/download", authz.KNTW, khtcDept, f.khtcUser, "", nil)
	f.ok(c, b, nil)
	if !bytes.Equal(b, frozen) {
		t.Fatal("TW download changed")
	}
	for _, role := range []string{authz.KNXa, authz.KNTinh} {
		c, b = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026", role, dTT, f.khtcUser, "", nil)
		if c != 403 {
			t.Fatalf("scope HTTP %d %s", c, b)
		}
	}
	c, b = f.do(http.MethodPost, reportPath(p.ID)+"/draft", authz.KNTW, khtcDept, f.khtcUser, "", map[string]any{"base_version": d.Version})
	if c != 403 {
		t.Fatalf("TW wrote %d %s", c, b)
	}
	for _, key := range []string{"year", "limit", "offset"} {
		for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 10000), "999999"} {
			path := "/v1/extension/khtc/exported-reports?year=2026&" + key + "=" + raw
			if key == "year" {
				path = "/v1/extension/khtc/exported-reports?year=" + raw
			}
			c, b = f.do(http.MethodGet, path, authz.KNTW, khtcDept, f.khtcUser, "", nil)
			if c != 422 {
				t.Fatalf("unbounded %s %d %s", key, c, b)
			}
		}
	}
	for _, tail := range []string{"versions=no", "q=" + strings.Repeat("x", 201), "kind=secret", "limit=0", "offset=-1"} {
		c, b = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&"+tail, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		if c != 422 {
			t.Fatalf("invalid %s %d %s", tail, c, b)
		}
	}
	// A final export is explicitly marked and remains discoverable alongside old immutable versions.
	f.submit(p.ID, "10")
	d = f.refresh(p.ID, d.Version)
	finalExport := f.export(p.ID, d.Version, false)
	c, b = f.call(http.MethodPost, p.ID, "/exports/"+finalExport.ID+"/final", map[string]any{})
	f.ok(c, b, nil)
	got = read("year=2026")
	if len(got.Items) != 1 || !got.Items[0].Final || got.Items[0].ID.String() != finalExport.ID {
		t.Fatal("final selection", got)
	}
	got = read("year=2026&versions=true")
	if len(got.Items) != 3 {
		t.Fatal("all versions after final", got)
	}
	// A different selected tenant cannot discover metadata or bytes from this centre.
	old := f.tid
	f.tid = ids.New()
	got = read("year=2026&versions=true")
	if len(got.Items) != 0 {
		t.Fatal("tenant leak", got)
	}
	c, b = f.do(http.MethodGet, reportPath(p.ID)+"/exports/"+x1.ID+"/download", authz.KNTW, khtcDept, f.khtcUser, "", nil)
	if c != 404 {
		t.Fatalf("cross-tenant download %d %s", c, b)
	}
	f.tid = old
	var raw map[string]any
	c, b = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026", authz.KNTW, khtcDept, f.khtcUser, "", nil)
	f.ok(c, b, &raw)
	encoded, _ := json.Marshal(raw)
	if bytes.Contains(encoded, []byte("draft_id")) || bytes.Contains(encoded, []byte("content")) {
		t.Fatal("archive contains draft/content")
	}
}

// Kỳ đã hủy vẫn giữ tệp lịch sử, nhưng không được xuất hiện trong kho kể cả khi xem mọi phiên bản.
func TestW1169_ExportArchiveExcludesCancelledPeriod(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, true)
	if !x.Provisional {
		t.Fatal("expected provisional export before submissions")
	}
	read := func() archiveResult {
		t.Helper()
		var out archiveResult
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&versions=true",
			authz.KNTW, khtcDept, f.khtcUser, "", nil)
		f.ok(c, b, &out)
		return out
	}
	if got := read(); len(got.Items) != 1 || got.Items[0].ID.String() != x.ID || got.Items[0].Final {
		t.Fatalf("export missing before cancellation: %+v", got)
	}
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "",
		map[string]any{"reason": "Hủy kỳ báo cáo mở nhầm"})
	f.ok(c, b, nil)
	if state := f.get(p.ID); state.State != "cancelled" {
		t.Fatalf("period not cancelled: %+v", state)
	}
	if got := read(); len(got.Items) != 0 || got.HasMore {
		t.Fatalf("cancelled period leaked into archive: %+v", got)
	}
}
