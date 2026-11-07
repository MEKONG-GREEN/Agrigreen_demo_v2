package app_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"
)

// Hash every uncompressed OOXML part, including styles, relationships and data.
// ZIP metadata is not document content; core creation/modified timestamps are wall-clock metadata.
func standalone267Parts(t *testing.T, body []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	stamp := regexp.MustCompile(`(<dcterms:(?:created|modified)[^>]*>)[^<]*(</dcterms:(?:created|modified)>)`)
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "docProps/core.xml" {
			raw = stamp.ReplaceAll(raw, []byte("${1}CLOCK${2}"))
		}
		hash := sha256.Sum256(raw)
		parts[f.Name] = hex.EncodeToString(hash[:])
	}
	return parts
}

func TestStandalone267AllFiveExportBaseline(t *testing.T) {
	got := map[string]map[string]string{}
	for _, kind := range []string{"week", "month", "quarter", "year", "program"} {
		for _, legacy := range []bool{false, true} {
			draft := wordDraft(kind, map[string]string{"place": "An Giang", "report_date": "2026-12-30", "document_no": "27", "signer": "Người ký kiểm thử", "signer_title": "GIÁM ĐỐC"})
			mode := "without-external"
			if legacy {
				mode = "frozen-legacy"
				var err error
				draft.Data, err = domain.WithExternal(draft.Data, []domain.ExternalFigure{{EntryID: uuid.MustParse("01920000-0000-7000-8000-0000000000bc"), IndicatorID: uuid.MustParse("01920000-0000-7000-8000-0000000000bd"), Version: 1, Code: "legacy267", Name: "Chỉ tiêu lịch sử", Value: "123.5", Unit: "ha", Agency: "Cơ quan cũ", Document: "Nguồn đã lưu", SourceDate: "2026-12-29", At: time.Date(2026, 12, 29, 1, 0, 0, 0, time.UTC)}})
				if err != nil {
					t.Fatal(err)
				}
			}
			original, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			var decoded domain.Draft
			if err := json.Unmarshal(original, &decoded); err != nil {
				t.Fatal(err)
			}
			roundtrip, _ := json.Marshal(decoded)
			if !bytes.Equal(original, roundtrip) {
				t.Fatal("legacy decoder altered payload")
			}
			xlsx, err := app.RenderXLSX(decoded, domain.Filter{}, 3)
			if err != nil {
				t.Fatal(err)
			}
			docx := render(t, decoded, app.WordData(decoded, domain.Filter{}, 3))
			prefix := kind + "/" + mode
			got[prefix+".xlsx"] = standalone267Parts(t, xlsx)
			got[prefix+".docx"] = standalone267Parts(t, docx)
			after, _ := json.Marshal(decoded)
			if !bytes.Equal(after, original) {
				t.Fatal("renderer mutated frozen payload")
			}
		}
	}
	path := filepath.Join("testdata", "standalone267-exports.json")
	if os.Getenv("AG_CAPTURE_267_EXPORT_BASELINE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		for key, parts := range got {
			if !reflect.DeepEqual(parts, want[key]) {
				t.Errorf("OOXML changed from original source: %s", key)
			}
		}
	}
}
