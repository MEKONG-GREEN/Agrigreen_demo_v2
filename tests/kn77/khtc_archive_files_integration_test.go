package kn77_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestW1169ArchiveSnapshotPreviewZIP(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"document_no": "12/BC-TTKN"}})
	f.ok(c, b, &d)
	x := f.export(p.ID, d.Version, true)
	frozen := f.download(p.ID, x.ID)
	c, b = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"document_no": "99/BC-TTKN"}})
	f.ok(c, b, &d)
	preview := reportPath(p.ID) + "/exports/" + x.ID + "/preview"
	c, b = f.do(http.MethodGet, preview, authz.KNTW, khtcDept, f.khtcUser, "", nil)
	f.ok(c, b, nil)
	if !bytes.Contains(b, []byte("12/BC-TTKN")) || bytes.Contains(b, []byte("99/BC-TTKN")) {
		t.Fatalf("preview read live draft: %s", b)
	}
	var data struct {
		ExportID string `json:"export_id"`
		Version  int64  `json:"version"`
	}
	if err := json.Unmarshal(b, &data); err != nil || data.ExportID != x.ID {
		t.Fatalf("snapshot identity %s %v", b, err)
	}
	body := map[string]any{"items": []map[string]string{{"period_id": p.ID, "export_id": x.ID}}}
	c, b = f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", authz.KNTW, khtcDept, f.khtcUser, ids.New().String(), body)
	f.ok(c, b, nil)
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(z.File) != 1 {
		t.Fatalf("zip: %v", err)
	}
	r, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, frozen) {
		t.Fatal("ZIP changed exported bytes", err)
	}
	if strings.Contains(z.File[0].Name, "..") {
		t.Fatal("unsafe filename")
	}
	for _, items := range []any{[]any{}, []any{body["items"].([]map[string]string)[0], body["items"].([]map[string]string)[0]}} {
		c, b = f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", authz.KNTW, khtcDept, f.khtcUser, ids.New().String(), map[string]any{"items": items})
		if c != 422 {
			t.Fatalf("invalid selection %d %s", c, b)
		}
	}
	for _, path := range []string{preview} {
		c, b = f.do(http.MethodGet, path, authz.KNXa, dTT, f.khtcUser, "", nil)
		if c != 403 {
			t.Fatalf("commune preview %d %s", c, b)
		}
	}
	f.tid = ids.New()
	c, b = f.do(http.MethodGet, preview, authz.KNTW, khtcDept, f.khtcUser, "", nil)
	if c != 404 {
		t.Fatalf("cross-tenant preview %d %s", c, b)
	}
	c, b = f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", authz.KNTW, khtcDept, f.khtcUser, ids.New().String(), body)
	if c != 404 {
		t.Fatalf("cross-tenant zip %d %s", c, b)
	}
}
