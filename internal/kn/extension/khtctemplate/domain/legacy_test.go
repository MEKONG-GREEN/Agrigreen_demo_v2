package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

func TestLegacySourceFingerprintAndVersion(t *testing.T) {
	word := doctpl.Template{Key: "khtc.mau_1", Version: 1, Title: "Mẫu nền", Owner: "pf/extension", Body: []doctpl.Block{{Kind: doctpl.KindParagraph, Text: "Nội dung nền"}}}
	pinned, err := LegacySnapshot("mau_1", "khtc-report/g1-v1", json.RawMessage(`[{"Key":"A","Title":"Kết quả","Editable":false}]`), word)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateLegacy(pinned); err != nil {
		t.Fatal(err)
	}
	if pinned.Mode != "legacy" || pinned.WordHash != word.Hash() || pinned.Hash == pinned.WordHash {
		t.Fatal("legacy layout and renderer were not independently identified")
	}
	bad := pinned
	bad.ExcelRule = "other-layout"
	if ValidateLegacy(bad) == nil {
		t.Fatal("Excel renderer drift accepted")
	}
	bad = pinned
	bad.Hash = strings.Repeat("0", 64)
	if ValidateLegacy(bad) == nil {
		t.Fatal("corrupt source hash accepted")
	}
	bad = pinned
	bad.WordVersion = 2
	if ValidateLegacy(bad) == nil {
		t.Fatal("renderer version drift accepted")
	}
	bad = pinned
	bad.Definition = append([]byte(nil), pinned.Definition...)
	bad.Definition[0] = '!'
	if ValidateLegacy(bad) == nil {
		t.Fatal("invalid source descriptor accepted")
	}
}
