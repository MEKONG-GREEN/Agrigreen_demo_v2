package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// CursorVersion là phiên bản định dạng con trỏ.
const CursorVersion = 1

// ErrCursorInvalid: con trỏ bị sửa, sai định dạng, hoặc không thuộc thiết bị/tenant này.
var ErrCursorInvalid = errors.New("sync: con trỏ không hợp lệ")

// Cursor là trạng thái pull của một thiết bị. Trên dây là chuỗi mờ
// base64url(JSON) + "." + base64url(HMAC-SHA256(khóa, phần JSON đã mã hóa)).
// Máy KHÔNG được đọc hay sửa nội dung; sửa → HMAC sai → 400 sync.cursor_invalid.
type Cursor struct {
	V      int              `json:"v"`
	Tenant uuid.UUID        `json:"t"`
	Device uuid.UUID        `json:"d"`
	Scope  string           `json:"sc"`           // dấu vân tay phạm vi lúc phát con trỏ
	Last   map[string]int64 `json:"s"`            // entity → cursor lớn nhất đã giao (port.Source)
	PK     *PubKeyCursor    `json:"pk,omitempty"` // trạng thái lượt gửi khóa công khai
}

// PubKeyCursor là vị trí trong một lượt gửi tập khóa công khai của địa bàn.
//
// Kho định danh không có cursor tăng dần cho khóa công khai, nên pf/sync gửi
// lại TOÀN BỘ tập khóa mỗi khi dấu vân tay tập khóa (FP) đổi. Mọi thay đổi của
// một lượt mang cùng Version = Ver; lượt kết thúc bằng bản ghi đánh dấu
// {entity: pubkey, id: 00000000-…, op: tombstone, version: Ver}: máy xóa mọi
// khóa có version < Ver (hộ đã xóa định danh, ra khỏi địa bàn).
type PubKeyCursor struct {
	FP    string `json:"fp"`              // dấu vân tay tập khóa của lượt
	Ver   int64  `json:"ver"`             // phiên bản lượt (mili giây Unix lúc bắt đầu, tăng dần)
	After string `json:"after,omitempty"` // ref cuối cùng đã giao trong lượt
	Done  bool   `json:"done"`            // đã giao bản ghi đánh dấu kết thúc lượt
}

// NewCursor tạo con trỏ trống cho thiết bị.
func NewCursor(tenantID, deviceID uuid.UUID, scopeFP string) Cursor {
	return Cursor{V: CursorVersion, Tenant: tenantID, Device: deviceID, Scope: scopeFP, Last: map[string]int64{}}
}

// Encode ký con trỏ bằng key.
func (c Cursor) Encode(key []byte) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(mac(key, body)), nil
}

// DecodeCursor kiểm chữ ký rồi đọc con trỏ. Không kiểm tenant/thiết bị — người gọi so.
func DecodeCursor(key []byte, s string) (Cursor, error) {
	body, sig, ok := strings.Cut(s, ".")
	if !ok || body == "" || len(s) > 16<<10 {
		return Cursor{}, ErrCursorInvalid
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac(key, body)) {
		return Cursor{}, ErrCursorInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.V != CursorVersion {
		return Cursor{}, ErrCursorInvalid
	}
	if c.Last == nil {
		c.Last = map[string]int64{}
	}
	return c, nil
}

func mac(key []byte, body string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("ag.sync.cursor.v1\n"))
	m.Write([]byte(body))
	return m.Sum(nil)
}

// ScopeFingerprint là dấu vân tay của phạm vi (All hoặc tập mã đơn vị). Phạm vi
// đổi (điều chuyển công tác, sáp nhập xã) → con trỏ cũ hết hiệu lực, pull bắt
// đầu lại từ đầu và trả reset = true để máy làm mới dữ liệu tham chiếu.
func ScopeFingerprint(all bool, units []string) string {
	if all {
		return "all"
	}
	u := slices.Clone(units)
	slices.Sort(u)
	u = slices.Compact(u)
	h := sha256.Sum256([]byte(strings.Join(u, "\n")))
	return hex.EncodeToString(h[:8])
}
