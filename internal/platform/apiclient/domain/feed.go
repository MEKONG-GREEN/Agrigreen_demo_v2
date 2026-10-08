package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// Cursor feed (R-INT-06, phong bì feed_page): chuỗi MỜ = base64url không đệm của "seq:<n>" — đối tác không
// được dựa vào dạng bên trong. seq 0 = trước mục đầu tiên.

const cursorPrefix = "seq:"

// ErrCursor: cursor sai dạng.
var ErrCursor = errors.New("apiclient: cursor feed sai dạng")

// EncodeCursor mã hóa seq thành cursor mờ.
func EncodeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.FormatInt(seq, 10)))
}

// DecodeCursor giải cursor mờ thành seq (≥ 0).
func DecodeCursor(s string) (int64, error) {
	if s == "" || len(s) > 200 {
		return 0, ErrCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, ErrCursor
	}
	v, ok := strings.CutPrefix(string(raw), cursorPrefix)
	if !ok {
		return 0, ErrCursor
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != v {
		return 0, ErrCursor
	}
	return n, nil
}

// KeyedRecord là một bản ghi đã phát của tập dữ liệu: khóa thực thể + JSON CHUẨN (khóa sắp — json.Marshal của
// map) để băm và so phiên bản.
type KeyedRecord struct {
	EntityKey string
	Record    json.RawMessage
}

// CanonicalRecord trả JSON chuẩn của bản ghi (khóa đối tượng sắp tăng ở mọi cấp, số giữ nguyên dạng).
func CanonicalRecord(raw []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// RecordSHA256 trả băm hex của JSON chuẩn.
func RecordSHA256(canonical []byte) string {
	s := sha256.Sum256(canonical)
	return hex.EncodeToString(s[:])
}

type snapshotLine struct {
	EntityKey string          `json:"entity_key"`
	Record    json.RawMessage `json:"record"`
}

// Snapshot dựng ảnh chụp NDJSON của một phiên bản: mỗi dòng {"entity_key":…,"record":…} theo entity_key tăng.
// Tổng kiểm R-INT-10: record_count = số dòng, sha256 = băm của chính tệp — đối tác tải ảnh chụp tự kiểm được.
func Snapshot(recs []KeyedRecord) (body []byte, count int64, sha string, err error) {
	sorted := slices.Clone(recs)
	slices.SortFunc(sorted, func(a, b KeyedRecord) int { return strings.Compare(a.EntityKey, b.EntityKey) })
	var buf bytes.Buffer
	for i, r := range sorted {
		if i > 0 && sorted[i-1].EntityKey == r.EntityKey {
			return nil, 0, "", errors.New("apiclient: ảnh chụp trùng entity_key " + r.EntityKey)
		}
		line, err := json.Marshal(snapshotLine(r))
		if err != nil {
			return nil, 0, "", err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	s := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), int64(len(sorted)), hex.EncodeToString(s[:]), nil
}
