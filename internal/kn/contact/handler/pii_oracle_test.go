package handler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

func contactPIISentinels() []string {
	return []string{"Hoàng Văn", "0987654321", "84987654321", "rieng@example.vn", "1970", "Trần Thị Mai", "0912000111", "mai@ttkn.vn"}
}

// Chỉ metadata ở root, có hợp đồng kiểu/cấu trúc đã kiểm, mới được tách khỏi nội dung cần quét.
// Không bỏ qua text giống UUID ở organization/device_id hay tại đường dẫn chưa biết.
func contactDropUUID(m map[string]any, key string, nullable bool) (string, error) {
	v, exists := m[key]
	if exists && v == nil && nullable {
		delete(m, key)
		return "", nil
	}
	s, ok := v.(string)
	id, err := uuid.Parse(s)
	if !exists || !ok || err != nil || id == uuid.Nil || id.String() != s {
		return "", fmt.Errorf("metadata UUID %s không hợp lệ", key)
	}
	delete(m, key)
	return s, nil
}

func contactDropTime(m map[string]any, key string) error {
	s, ok := m[key].(string)
	if _, err := time.Parse(time.RFC3339Nano, s); !ok || err != nil {
		return fmt.Errorf("metadata thời gian %s không hợp lệ", key)
	}
	delete(m, key)
	return nil
}

func contactDropLedgerMetadata(m map[string]any, kind string) error {
	if kind != "person" && kind != "unit" {
		return errors.New("loại dòng contact không hợp lệ")
	}
	for _, key := range []string{"id", "tenant_id", "actor_ref"} {
		if _, err := contactDropUUID(m, key, false); err != nil {
			return err
		}
	}
	if _, err := contactDropUUID(m, "supersedes_id", true); err != nil {
		return err
	}
	refKey := "subject_ref"
	if kind == "unit" {
		refKey = "unit_id"
		if _, err := contactDropUUID(m, "org_ref", true); err != nil {
			return err
		}
	}
	ref, err := contactDropUUID(m, refKey, false)
	if err != nil {
		return err
	}
	if m["stream_key"] != kind+":"+ref {
		return errors.New("stream_key contact không khớp reference")
	}
	delete(m, "stream_key")
	seq, ok := m["seq"].(json.Number)
	if !ok {
		return errors.New("seq contact không có kiểu số nguyên")
	}
	n, err := seq.Int64()
	if err != nil || n <= 0 {
		return errors.New("seq contact không hợp lệ")
	}
	delete(m, "seq")
	for _, key := range []string{"prev_hash", "row_hash"} {
		s, ok := m[key].(string)
		if !ok || !strings.HasPrefix(s, `\x`) {
			return fmt.Errorf("metadata hash %s không hợp lệ", key)
		}
		b, err := hex.DecodeString(s[2:])
		valid := err == nil && len(b) == 32
		if key == "prev_hash" && n == 1 {
			valid = err == nil && len(b) == 1 && b[0] == 0
		}
		if !valid {
			return fmt.Errorf("metadata hash %s sai độ dài/genesis", key)
		}
		delete(m, key)
	}
	for _, key := range []string{"event_time", "recorded_at"} {
		if err := contactDropTime(m, key); err != nil {
			return err
		}
	}
	// app/write.go tự sinh dạng prefix:UUIDv7 khi không có Idempotency-Key.
	// Chỉ dạng fallback có cấu trúc này là metadata; các key text khác vẫn được quét.
	prefix := "revise:"
	if m["change"] == "created" {
		prefix = "create:"
	}
	if kind == "unit" {
		prefix = "unit:"
	}
	if key, ok := m["idem_key"].(string); ok && strings.HasPrefix(key, prefix) {
		s := strings.TrimPrefix(key, prefix)
		id, err := uuid.Parse(s)
		if err == nil && id != uuid.Nil && id.Version() == 7 && id.String() == s {
			delete(m, "idem_key")
		}
	}
	return nil
}

func contactHasPII(v any, sentinels []string) bool {
	contains := func(s string) bool {
		for _, x := range sentinels {
			if strings.Contains(strings.ToLower(s), strings.ToLower(x)) {
				return true
			}
		}
		return false
	}
	switch x := v.(type) {
	case string:
		return contains(x)
	case json.Number:
		return contains(x.String())
	case map[string]any:
		for key, value := range x {
			if contains(key) || contactHasPII(value, sentinels) {
				return true
			}
		}
	case []any:
		for _, value := range x {
			if contactHasPII(value, sentinels) {
				return true
			}
		}
	}
	return false
}

func contactRowWithoutPII(kind, raw string, sentinels []string) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var row map[string]any
	if err := dec.Decode(&row); err != nil {
		return fmt.Errorf("đọc JSON contact: %w", err)
	}
	if err := contactDropLedgerMetadata(row, kind); err != nil {
		return err
	}
	if contactHasPII(row, sentinels) {
		return errors.New("PII trong nội dung dòng contact")
	}
	return nil
}

type contactJSONRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func contactRowsWithoutPII(rows contactJSONRows, sentinels []string) error {
	for rows.Next() {
		var kind, raw string
		if err := rows.Scan(&kind, &raw); err != nil {
			return fmt.Errorf("Scan contact: %w", err)
		}
		if err := contactRowWithoutPII(kind, raw, sentinels); err != nil {
			return err
		}
	}
	return rows.Err()
}

func contactLogsWithoutPII(raw []byte, sentinels []string) error {
	// Fixture slog.NewJSONHandler xuất các record NDJSON có key duy nhất.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	n := 0
	for {
		var record map[string]any
		err := dec.Decode(&record)
		if errors.Is(err, io.EOF) {
			if n == 0 {
				return errors.New("không có record log contact")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("đọc JSON log contact: %w", err)
		}
		if err := contactDropTime(record, "time"); err != nil {
			return err
		}
		for _, key := range []string{"actor_ref", "tenant_id"} {
			if _, exists := record[key]; exists {
				if _, err := contactDropUUID(record, key, false); err != nil {
					return err
				}
			}
		}
		if contactHasPII(record, sentinels) {
			return errors.New("PII trong nội dung log contact")
		}
		n++
	}
}
