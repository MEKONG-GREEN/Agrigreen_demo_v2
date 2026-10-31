package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

// LegacyDefinition identifies the existing layout and Word renderer exactly. It
// is pinned for a new legacy draft rather than rebound when Admin publishes later.
type LegacyDefinition struct {
	Source   string          `json:"source"`
	Key      string          `json:"key"`
	Rule     string          `json:"rule"`
	Sections json.RawMessage `json:"sections"`
	Word     doctpl.Template `json:"word"`
}

func LegacySnapshot(key, rule string, sections json.RawMessage, word doctpl.Template) (port.KHTCTemplateSnapshot, error) {
	if !Fixed(key) || rule == "" || !json.Valid(sections) || word.Key != "khtc."+key || word.Version != 1 {
		return port.KHTCTemplateSnapshot{}, fmt.Errorf("invalid builtin KHTC template")
	}
	if err := doctpl.Validate(word); err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	definition := LegacyDefinition{Source: "builtin:khtc-report", Key: key, Rule: rule, Sections: append(json.RawMessage(nil), sections...), Word: word}
	raw, err := json.Marshal(definition)
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	raw, err = canonicalLegacyJSON(raw)
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	sum := sha256.Sum256(raw)
	return port.KHTCTemplateSnapshot{Mode: "legacy", Rule: rule, Key: key, Version: 1, Hash: hex.EncodeToString(sum[:]), Definition: raw, ApplicableFrom: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), WordKey: word.Key, WordVersion: word.Version, WordHash: word.Hash(), ExcelRule: rule}, nil
}

func ValidateLegacy(pinned port.KHTCTemplateSnapshot) error {
	if pinned.Mode != "legacy" || !Fixed(pinned.Key) || pinned.Version != 1 || pinned.Rule == "" || pinned.ExcelRule != pinned.Rule || pinned.WordVersion != 1 || pinned.WordKey != "khtc."+pinned.Key || len(pinned.Definition) > MaxDefinitionBytes {
		return invalid("template", "Nguồn mẫu nền không hợp lệ")
	}
	var definition LegacyDefinition
	if err := json.Unmarshal(pinned.Definition, &definition); err != nil {
		return invalid("template", "Không đọc được nguồn mẫu nền")
	}
	if definition.Source != "builtin:khtc-report" || definition.Key != pinned.Key || definition.Rule != pinned.Rule || definition.Word.Key != pinned.WordKey || definition.Word.Version != pinned.WordVersion || definition.Word.Hash() != pinned.WordHash {
		return invalid("template", "Phiên bản hoặc băm bộ dựng nền không khớp")
	}
	var sections []json.RawMessage
	if err := json.Unmarshal(definition.Sections, &sections); err != nil || len(sections) == 0 {
		return invalid("template", "Khung mục nguồn nền không hợp lệ")
	}
	if err := doctpl.Validate(definition.Word); err != nil {
		return invalid("template", "Định nghĩa bộ dựng nền không hợp lệ")
	}
	canonical, err := canonicalLegacyJSON(pinned.Definition)
	if err != nil {
		return invalid("template", "Không đọc được nguồn mẫu nền")
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != pinned.Hash {
		return invalid("template", "Băm nguồn mẫu nền không khớp")
	}
	return nil
}

// JSONB preserves JSON values rather than whitespace/object order. Use a
// canonical JSON value (with exact numbers) for the immutable source digest.
func canonicalLegacyJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid legacy JSON suffix")
	}
	return json.Marshal(value)
}
