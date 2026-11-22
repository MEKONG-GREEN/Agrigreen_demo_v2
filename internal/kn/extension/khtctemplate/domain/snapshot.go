package domain

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

// PinnedDefinition checks the embedded source without looking up a newer publication.
// A zero definition means explicitly pinned legacy, never a fallback after an error.
func PinnedDefinition(p port.KHTCTemplateSnapshot, key string, start time.Time) (Definition, error) {
	if !Fixed(key) || p.Key != key || start.IsZero() || p.ApplicableFrom.IsZero() || p.ApplicableFrom.After(start) {
		return Definition{}, invalid("template", "Mẫu và kỳ áp dụng không khớp")
	}
	if p.Mode == "legacy" {
		if err := ValidateLegacy(p); err != nil {
			return Definition{}, err
		}
		if p.LegacyUntil != nil && !start.Before(*p.LegacyUntil) {
			return Definition{}, invalid("template", "Kỳ không thuộc hiệu lực mẫu nền đã ghim")
		}
		return Definition{}, nil
	}
	if p.Mode != "published" || p.Rule != RuleVersion || p.ID == uuid.Nil || p.Version < 1 || p.Version > 1000000000 || p.LegacyUntil != nil || len(p.Definition) > MaxDefinitionBytes {
		return Definition{}, invalid("template", "Phiên bản và quy tắc mẫu đã ghim không hợp lệ")
	}
	var d Definition
	decoder := json.NewDecoder(bytes.NewReader(p.Definition))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return Definition{}, invalid("template", "Định nghĩa mẫu đã ghim không hợp lệ")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Definition{}, invalid("template", "Định nghĩa mẫu có dữ liệu thừa")
	}
	if d.Key != key {
		return Definition{}, invalid("template", "Định nghĩa khác mẫu đã chọn")
	}
	if err := Validate(d, nil); err != nil {
		return Definition{}, err
	}
	_, actual, err := d.BytesHash()
	if err != nil {
		return Definition{}, err
	}
	if p.Hash != actual {
		return Definition{}, invalid("template", "Băm định nghĩa mẫu đã ghim không khớp")
	}
	return d, nil
}
