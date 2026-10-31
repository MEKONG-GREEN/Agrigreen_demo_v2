package kn77_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/xuri/excelize/v2"
)

type fixedReportDTO struct {
	Version int64 `json:"version"`
	Content struct {
		TemplateSnapshot *port.KHTCTemplateSnapshot `json:"template_snapshot"`
		TemplateTerminal *string                    `json:"template_terminal"`
		Texts            map[string]string          `json:"texts"`
	} `json:"content"`
	Document    json.RawMessage `json:"template_document"`
	LockedAdmin []string        `json:"locked_admin"`
}

func fixedReport(t *testing.T, f *reportFx, pid string) fixedReportDTO {
	t.Helper()
	var out fixedReportDTO
	c, b := f.call(http.MethodGet, pid, "", nil)
	f.ok(c, b, &out)
	return out
}
func TestW1165_ComposerPinsPublishedLegacyAndExportHistory(t *testing.T) {
	f := newReportFx(t)
	withExporter(t, f)
	legacyPeriod := f.open(40, dTT, dTTTT)
	old := f.refresh(legacyPeriod.ID, 0)
	legacy := fixedReport(t, f, legacyPeriod.ID)
	if legacy.Content.TemplateSnapshot == nil || legacy.Content.TemplateSnapshot.Mode != "legacy" || legacy.Content.TemplateTerminal == nil {
		t.Fatal("new legacy draft lacks explicit frame")
	}
	definition := templateDefinition(t, f, "mau_1")
	definition.Nodes = append(definition.Nodes, templateNodeDTO{ID: "customer_memo", Parent: "B", Order: 100, Title: "Mục tự biên tập", Repeat: "none", Body: "{memo}", TextSlot: true, Bindings: []templateBindingDTO{{Variable: "memo", Source: "text", Field: "narrative"}}, LockedAdmin: []string{"signer"}})
	v1 := templateSave(t, f, "mau_1", 0, definition)
	templatePublish(t, f, "mau_1", v1, "2026-10-07")
	p := f.open(41, dTT, dTTTT)
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, 0)
	pinned := fixedReport(t, f, p.ID)
	if pinned.Content.TemplateSnapshot == nil || pinned.Content.TemplateSnapshot.ID != v1.ID || pinned.Content.TemplateSnapshot.WordKey != "khtc.fixed" || pinned.Content.TemplateSnapshot.ExcelRule != "khtc-xlsx/fixed-v1" {
		t.Fatal("wrong published source", pinned)
	}
	if len(pinned.LockedAdmin) != 1 || pinned.LockedAdmin[0] != "signer" {
		t.Fatal("missing lock projection")
	}
	for _, body := range []map[string]any{
		{"base_version": d.Version, "admin": map[string]string{"signer": "forged"}},
		{"base_version": d.Version, "texts": map[string]string{"plan.value": "999"}},
	} {
		c, b := f.call(http.MethodPost, p.ID, "/draft", body)
		if c != http.StatusUnprocessableEntity {
			t.Fatalf("locked write: %d %s", c, b)
		}
	}
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "template_snapshot": pinned.Content.TemplateSnapshot})
	if c != http.StatusBadRequest {
		t.Fatalf("snapshot accepted from client: %d %s", c, b)
	}
	c, b = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "texts": map[string]string{"customer_memo": "NỘI DUNG RIÊNG ĐÃ GHIM"}})
	f.ok(c, b, &d)
	saved := fixedReport(t, f, p.ID)
	snapshotBytes, _ := json.Marshal(saved.Content.TemplateSnapshot)
	if !strings.Contains(string(saved.Document), "NỘI DUNG RIÊNG ĐÃ GHIM") {
		t.Fatal("saved narrative absent from composer")
	}
	x := f.export(p.ID, d.Version, true)
	xlsx := f.download(p.ID, x.ID)
	file, err := excelize.OpenReader(bytes.NewReader(xlsx))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := file.GetRows("Báo cáo")
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		for _, cell := range r {
			if cell == "NỘI DUNG RIÊNG ĐÃ GHIM" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Excel ignored pinned definition")
	}
	w1 := f.word(p.ID, x.ID, "")
	c, wordBefore := f.call(http.MethodGet, p.ID, "/words/"+w1.ID+"/download", nil)
	f.ok(c, wordBefore, nil)
	document, err := docx.Extract(wordBefore)
	if err != nil || !strings.Contains(document.Text(), "NỘI DUNG RIÊNG ĐÃ GHIM") {
		t.Fatal("Word ignored pinned definition", err)
	}
	changed := cloneTemplateDefinition(t, definition)
	for i := range changed.Nodes {
		if changed.Nodes[i].ID == "customer_memo" {
			changed.Nodes[i].Body = "MẪU TƯƠNG LAI"
		}
	}
	v2 := templateSave(t, f, "mau_1", 1, changed)
	templatePublish(t, f, "mau_1", v2, "2026-10-14")
	var restored templateVersionDTO
	c, b = f.do(http.MethodPost, templatePath+"mau_1/restore", authz.KNAdmin, "", f.khtcUser, "", map[string]any{"base_version": 2, "version_id": v1.ID})
	f.ok(c, b, &restored)
	templatePublish(t, f, "mau_1", restored, "2026-10-21")
	refreshed := f.refresh(p.ID, d.Version)
	after := fixedReport(t, f, p.ID)
	afterBytes, _ := json.Marshal(after.Content.TemplateSnapshot)
	if !bytes.Equal(snapshotBytes, afterBytes) || after.Content.Texts["customer_memo"] != saved.Content.Texts["customer_memo"] {
		t.Fatal("refresh rebound or erased narrative")
	}
	if refreshed.Version <= d.Version {
		t.Fatal("refresh did not create revision")
	}
	legacyAfter := f.refresh(legacyPeriod.ID, old.Version)
	_ = legacyAfter
	if got := fixedReport(t, f, legacyPeriod.ID); got.Content.TemplateSnapshot.Mode != "legacy" || got.Content.TemplateSnapshot.Hash != legacy.Content.TemplateSnapshot.Hash {
		t.Fatal("first publication displaced legacy draft")
	}
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	if got := fixedReport(t, f, p.ID); got.Content.TemplateTerminal == nil || *got.Content.TemplateTerminal != "" {
		t.Fatal("live terminal rewrote saved revision")
	}
	w2 := f.word(p.ID, x.ID, "")
	c, wordAfter := f.call(http.MethodGet, p.ID, "/words/"+w2.ID+"/download", nil)
	f.ok(c, wordAfter, nil)
	if !bytes.Equal(wordBefore, wordAfter) || w1.BodyHash != w2.BodyHash || !bytes.Equal(xlsx, f.download(p.ID, x.ID)) {
		t.Fatal("publication/restore/close changed historical export bytes")
	}
	next := f.open(42, dTT, dTTTT)
	_ = f.refresh(next.ID, 0)
	if got := fixedReport(t, f, next.ID); got.Content.TemplateSnapshot.ID != v2.ID {
		t.Fatal("next period did not select effective published version")
	}
}
