package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func TestCursorRoundTripAndTamper(t *testing.T) {
	c := NewCursor(ids.New(), ids.New(), ScopeFingerprint(false, []string{"30001", "89"}))
	c.Last["plot"] = 42
	s, err := c.Encode(key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCursor(key, s)
	if err != nil || got.Last["plot"] != 42 || got.Device != c.Device {
		t.Fatalf("giải mã: %v %+v", err, got)
	}
	// Sửa nội dung JSON (tăng cursor) rồi mã hóa lại phần thân, giữ chữ ký cũ.
	body, sig, _ := strings.Cut(s, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	forged := strings.Replace(string(raw), `"plot":42`, `"plot":0`, 1)
	if _, err := DecodeCursor(key, base64.RawURLEncoding.EncodeToString([]byte(forged))+"."+sig); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("con trỏ bị sửa phải bị từ chối, có %v", err)
	}
	if _, err := DecodeCursor([]byte("khac-khoa-khac-khoa-khac-khoa-xx"), s); !errors.Is(err, ErrCursorInvalid) {
		t.Fatal("khóa khác phải bị từ chối")
	}
	for _, bad := range []string{"", "abc", "abc.def", s + "x"} {
		if _, err := DecodeCursor(key, bad); !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("%q phải bị từ chối", bad)
		}
	}
}

func TestScopeFingerprintOrderInsensitive(t *testing.T) {
	if ScopeFingerprint(false, []string{"a", "b"}) != ScopeFingerprint(false, []string{"b", "a", "a"}) {
		t.Fatal("dấu vân tay phải không phụ thuộc thứ tự")
	}
	if ScopeFingerprint(true, nil) == ScopeFingerprint(false, nil) {
		t.Fatal("All khác rỗng")
	}
}

func TestParseRecord(t *testing.T) {
	now := time.Date(2026, 12, 3, 2, 0, 0, 0, time.UTC)
	dev := ids.New()
	ok := RawRecord{
		ID: ids.New().String(), IdemKey: "dev:" + dev.String() + ":7", Entity: "field_report", Op: port.OpCreate,
		EventTime: "2026-12-03T08:15:00+07:00", Payload: json.RawMessage(`{"a":1}`),
	}
	rec, errs := ParseRecord(ok, dev, now)
	if len(errs) > 0 || !rec.EventTime.Equal(now.Add(-45*time.Minute)) {
		t.Fatalf("bản ghi hợp lệ: %v %v", errs, rec.EventTime)
	}
	reason := "vẽ lại ranh giới cho đúng bờ"
	sup := ids.New().String()
	cases := map[string]func(r *RawRecord){
		"id v4":                    func(r *RawRecord) { r.ID = uuid.New().String() },
		"idem thiết bị khác":       func(r *RawRecord) { r.IdemKey = "dev:" + ids.New().String() + ":1" },
		"idem sai dạng":            func(r *RawRecord) { r.IdemKey = "abc" },
		"op update":                func(r *RawRecord) { r.Op = "update" },
		"correct thiếu lý do":      func(r *RawRecord) { r.Op, r.SupersedesID = port.OpCorrect, &sup },
		"create có supersedes":     func(r *RawRecord) { r.SupersedesID = &sup },
		"event_time tương lai":     func(r *RawRecord) { r.EventTime = now.Add(6 * time.Minute).Format(time.RFC3339) },
		"event_time không múi giờ": func(r *RawRecord) { r.EventTime = "2026-12-03T08:15:00" },
		"payload mảng":             func(r *RawRecord) { r.Payload = json.RawMessage(`[1]`) },
	}
	for name, mut := range cases {
		r := ok
		mut(&r)
		if _, errs := ParseRecord(r, dev, now); len(errs) == 0 {
			t.Errorf("%s: phải bị từ chối", name)
		}
	}
	r := ok
	r.Op, r.SupersedesID, r.CorrectionReason = port.OpCorrect, &sup, &reason
	if rec, errs := ParseRecord(r, dev, now); len(errs) > 0 || rec.SupersedesID == nil {
		t.Fatalf("bản ghi bù hợp lệ: %v", errs)
	}
	r = ok
	r.EventTime = now.Add(4 * time.Minute).Format(time.RFC3339)
	if _, errs := ParseRecord(r, dev, now); len(errs) > 0 {
		t.Fatalf("lệch đồng hồ 4 phút phải được nhận: %v", errs)
	}
}

func TestNormalizeResult(t *testing.T) {
	id := ids.New()
	fixed := ids.New()
	gen := func() uuid.UUID { return fixed }
	if r := NormalizeResult(port.Result{Status: "weird"}, id, gen); r.Status != port.StatusRejected || r.Code != port.CodeSinkFailed {
		t.Fatalf("trạng thái lạ: %+v", r)
	}
	if r := NormalizeResult(port.Result{Status: port.StatusConflict}, id, gen); r.ConflictID == nil || *r.ConflictID != fixed {
		t.Fatalf("conflict thiếu mã: %+v", r)
	}
	if r := NormalizeResult(port.Result{Status: port.StatusRejected}, id, gen); r.Code != "validation_failed" || r.ID != id {
		t.Fatalf("rejected thiếu mã: %+v", r)
	}
}

func TestParseFileMeta(t *testing.T) {
	now := time.Now().UTC()
	pub, _, _ := envelope.GenerateKeyPair(nil)
	s, err := envelope.Seal([]byte("ảnh"), pub)
	if err != nil {
		t.Fatal(err)
	}
	m := envelope.NewMetadata(ids.New().String(), s, "image/jpeg", now.Format(time.RFC3339))
	m.GPS = &envelope.GPS{Lat: 10.03, Lon: 105.78, AccuracyM: 6.5}
	f, errs := ParseFileMeta(m, int64(len(s.Ciphertext)), now)
	if len(errs) > 0 || f.GPSLat == nil || *f.GPSLat != 10.03 {
		t.Fatalf("metadata hợp lệ: %v", errs)
	}
	bad := m
	bad.WrappedDEK = bad.WrappedDEK[:79]
	if _, errs := ParseFileMeta(bad, int64(len(s.Ciphertext)), now); len(errs) != 1 || errs[0].Field != "meta.wrapped_dek" {
		t.Fatalf("wrapped_dek 79 byte: %v", errs)
	}
	bad = m
	bad.V, bad.SubjectRef = 2, ""
	if _, errs := ParseFileMeta(bad, MaxFileBytes+1, now); len(errs) != 3 {
		t.Fatalf("v, subject_ref, kích thước: %v", errs)
	}
}
