package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// RuleValue là phiên bản quy tắc ghi sổ giá trị (daily_value.rule_version).
const RuleValue = "metrics.value.v1"

// RuleLayer là phiên bản quy tắc chuyển sự kiện nguồn → dòng nhật ký lớp.
const RuleLayer = "metrics.layer.v1"

// SystemActor là actor_ref của job/consumer (không phải người dùng).
var SystemActor = uuid.MustParse("00000000-0000-7000-8000-00000000a0a5")

// InputsHash là băm tất định của một giá trị: định nghĩa (key, phiên bản, băm tệp),
// phạm vi, ngày, giá trị chuẩn hóa, watermark. Cùng đầu vào → cùng băm → job không
// ghi thêm (idempotent); khác → bản ghi bù (supersedes).
func InputsHash(d Def, sc port.Scope, day clock.Date, v decimal.Decimal, wm port.Watermark) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%d\n%x\n%s\n%s\n%s\n%s\n%s\n%s", d.Key, d.Version, d.SHA256, ScopeUnit(sc), sc.Dimension, day,
		v.String(), wm.LastEventID, wm.RecordedAt.UTC().Format(time.RFC3339Nano))
	h := sha256.Sum256([]byte(b.String()))
	return h[:]
}

// ScopeUnit là đơn vị ghi sổ của phạm vi: rỗng = toàn tenant; một mã = đơn vị đó.
// Phạm vi nhiều đơn vị không ghi sổ (chỉ tính theo yêu cầu).
func ScopeUnit(sc port.Scope) string {
	if len(sc.Units) == 1 {
		return sc.Units[0]
	}
	return ""
}

// ValidOp cho biết op hợp lệ.
func ValidOp(op port.LayerOp) bool {
	switch op {
	case port.LayerCreate, port.LayerUpdate, port.LayerRetire, port.LayerRestore:
		return true
	}
	return false
}

// Fold áp các thay đổi (đã xếp theo recorded_at, ID) lên trạng thái base.
func Fold(base []port.LayerFeature, changes []port.LayerChange) []port.LayerFeature {
	state := map[uuid.UUID]port.LayerFeature{}
	for _, f := range base {
		state[f.FeatureRef] = f
	}
	for _, c := range changes {
		f := state[c.FeatureRef]
		f.FeatureRef = c.FeatureRef
		if len(c.After) > 0 && !bytes.Equal(bytes.TrimSpace(c.After), []byte("null")) {
			f.Attributes = c.After
		}
		f.Retired = c.Op == port.LayerRetire
		state[c.FeatureRef] = f
	}
	out := make([]port.LayerFeature, 0, len(state))
	for _, f := range state {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FeatureRef.String() < out[j].FeatureRef.String() })
	return out
}

// Later cho biết (t1, id1) > (t2, id2) theo thứ tự nhật ký.
func Later(t1 time.Time, id1 uuid.UUID, t2 time.Time, id2 uuid.UUID) bool {
	if !t1.Equal(t2) {
		return t1.After(t2)
	}
	return bytes.Compare(id1[:], id2[:]) > 0
}

// FeaturesSHA256 là băm của danh sách đối tượng (tái lập ảnh chụp).
func FeaturesSHA256(fs []port.LayerFeature) ([]byte, error) {
	b, err := json.Marshal(featuresJSON(fs))
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	return h[:], nil
}

// FeatureJSON là dạng lưu của một đối tượng trong ảnh chụp.
type FeatureJSON struct {
	FeatureRef uuid.UUID       `json:"feature_ref"`
	Attributes json.RawMessage `json:"attributes"`
	Retired    bool            `json:"retired"`
}

func featuresJSON(fs []port.LayerFeature) []FeatureJSON {
	out := make([]FeatureJSON, 0, len(fs))
	for _, f := range fs {
		a := f.Attributes
		if len(a) == 0 {
			a = json.RawMessage("null")
		}
		// Chuẩn hóa JSON để băm không phụ thuộc định dạng jsonb trả về.
		var v any
		if json.Unmarshal(a, &v) == nil {
			if nb, err := json.Marshal(v); err == nil {
				a = nb
			}
		}
		out = append(out, FeatureJSON{FeatureRef: f.FeatureRef, Attributes: a, Retired: f.Retired})
	}
	return out
}

// EncodeFeatures mã hóa danh sách đối tượng để lưu.
func EncodeFeatures(fs []port.LayerFeature) ([]byte, error) { return json.Marshal(featuresJSON(fs)) }

// DecodeFeatures đọc danh sách đối tượng đã lưu.
func DecodeFeatures(b []byte) ([]port.LayerFeature, error) {
	var raw []FeatureJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]port.LayerFeature, 0, len(raw))
	for _, r := range raw {
		out = append(out, port.LayerFeature{FeatureRef: r.FeatureRef, Attributes: r.Attributes, Retired: r.Retired})
	}
	return out, nil
}

// Normalize chuẩn hóa danh sách (cùng dạng như sau lưu/đọc) để so sánh.
func Normalize(fs []port.LayerFeature) []port.LayerFeature {
	b, _ := EncodeFeatures(fs)
	out, _ := DecodeFeatures(b)
	return out
}
