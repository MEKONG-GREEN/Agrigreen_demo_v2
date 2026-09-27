package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Giới hạn của chiều lên.
const (
	MaxBatchRecords = 500             // bản ghi mỗi lô
	MaxPayloadBytes = 256 << 10       // payload mỗi bản ghi
	MaxClockSkew    = 5 * time.Minute // event_time không được vượt quá now + 5 phút
	MaxIdemKeyLen   = 200
)

var (
	idemKeyRe = regexp.MustCompile(`^dev:([0-9a-fA-F-]{36}):([1-9][0-9]{0,18})$`)
	entityRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
)

// RawRecord là bản ghi máy gửi lên, chưa kiểm.
type RawRecord struct {
	ID               string
	IdemKey          string
	Entity           string
	Op               string
	EventTime        string
	SupersedesID     *string
	CorrectionReason *string
	Payload          json.RawMessage
}

// ParseRecord kiểm một bản ghi chiều lên theo docs/dev/sync-contract.md:
//   - id là UUIDv7;
//   - idem_key dạng "dev:<device_id>:<seq>" với device_id đúng thiết bị gọi;
//   - op create (không có supersedes_id/correction_reason) hoặc correct (bắt buộc cả hai);
//   - event_time RFC 3339 có múi giờ, không vượt quá now + 5 phút;
//   - payload là đối tượng JSON, tối đa 256 KiB.
//
// Entity chỉ được kiểm dạng ở đây; có Sink nhận hay không do app quyết định.
func ParseRecord(r RawRecord, device uuid.UUID, now time.Time) (port.Record, []problem.FieldError) {
	var errs []problem.FieldError
	bad := func(field, reason string) { errs = append(errs, problem.FieldError{Field: field, Reason: reason}) }
	rec := port.Record{IdemKey: r.IdemKey, Entity: r.Entity, Op: r.Op, DeviceID: device}

	if id, err := ids.Parse(r.ID); err != nil {
		bad("id", "phải là UUIDv7 sinh trên máy")
	} else {
		rec.ID = id
	}
	if m := idemKeyRe.FindStringSubmatch(r.IdemKey); m == nil || len(r.IdemKey) > MaxIdemKeyLen {
		bad("idem_key", "phải có dạng dev:<device_id>:<seq>")
	} else if d, err := uuid.Parse(m[1]); err != nil || d != device {
		bad("idem_key", "tiền tố phải là mã của chính thiết bị gửi")
	} else if _, err := strconv.ParseInt(m[2], 10, 64); err != nil {
		bad("idem_key", "seq phải là số nguyên dương")
	}
	if !entityRe.MatchString(r.Entity) {
		bad("entity", "sai định dạng")
	}
	switch r.Op {
	case port.OpCreate:
		if r.SupersedesID != nil || r.CorrectionReason != nil {
			bad("supersedes_id", "chỉ dùng với op = correct")
		}
	case port.OpCorrect:
		if r.SupersedesID == nil {
			bad("supersedes_id", "bắt buộc với op = correct")
		} else if s, err := ids.ParseRef(*r.SupersedesID); err != nil {
			bad("supersedes_id", "phải là UUID")
		} else {
			rec.SupersedesID = &s
		}
		if r.CorrectionReason == nil || !ValidReason(*r.CorrectionReason) {
			bad("correction_reason", "bắt buộc với op = correct, 10–500 ký tự")
		} else {
			reason := strings.TrimSpace(*r.CorrectionReason)
			rec.CorrectionReason = &reason
		}
	default:
		bad("op", "chỉ có create hoặc correct — không có update/delete")
	}
	if t, err := time.Parse(time.RFC3339Nano, r.EventTime); err != nil {
		bad("event_time", "phải theo RFC 3339 có múi giờ")
	} else if t.After(now.Add(MaxClockSkew)) {
		bad("event_time", "không được ở tương lai quá 5 phút so với máy chủ")
	} else {
		rec.EventTime = t.UTC()
	}
	p := bytes.TrimSpace(r.Payload)
	switch {
	case len(p) == 0 || p[0] != '{' || !json.Valid(p):
		bad("payload", "bắt buộc, phải là đối tượng JSON")
	case len(p) > MaxPayloadBytes:
		bad("payload", "vượt 256 KiB")
	default:
		rec.Payload = json.RawMessage(p)
	}
	return rec, errs
}

// PayloadSHA256 là băm của payload đã nén khoảng trắng — sổ inbound chỉ giữ băm,
// không giữ payload (có thể chứa dữ liệu cá nhân). nil nếu payload không hợp lệ.
func PayloadSHA256(payload json.RawMessage) *string {
	var buf bytes.Buffer
	if len(bytes.TrimSpace(payload)) == 0 || json.Compact(&buf, payload) != nil {
		return nil
	}
	h := sha256.Sum256(buf.Bytes())
	s := hex.EncodeToString(h[:])
	return &s
}

// Truncate cắt s tối đa n byte mà không cắt đôi ký tự UTF-8 — cho cột sổ khi máy gửi dữ liệu sai dạng.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// NormalizeResult làm sạch kết quả Sink trả về: trạng thái lạ → rejected
// sync.sink_failed; rejected thiếu mã → validation_failed; conflict thiếu mã
// xung đột → newID().
func NormalizeResult(res port.Result, id uuid.UUID, newID func() uuid.UUID) port.Result {
	res.ID = id
	switch res.Status {
	case port.StatusAccepted, port.StatusDuplicate:
		res.ConflictID, res.Code, res.Errors = nil, "", nil
	case port.StatusConflict:
		if res.ConflictID == nil || *res.ConflictID == uuid.Nil {
			c := newID()
			res.ConflictID = &c
		}
		res.Code, res.Errors = "", nil
	case port.StatusRejected:
		res.ConflictID = nil
		if res.Code == "" {
			res.Code = problem.Validation.Code
		}
	default:
		return port.Result{ID: id, Status: port.StatusRejected, Code: port.CodeSinkFailed}
	}
	return res
}
