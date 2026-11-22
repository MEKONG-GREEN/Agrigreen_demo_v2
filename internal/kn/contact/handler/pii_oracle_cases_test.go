package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const contactMetadata1970 = "019a0000-1970-7000-8000-000000001970"

func contactOracleRow(kind string) map[string]any {
	m := map[string]any{
		"id": contactMetadata1970, "tenant_id": contactMetadata1970, "actor_ref": contactMetadata1970, "supersedes_id": nil,
		"seq": 1, "prev_hash": `\x00`, "row_hash": `\x` + strings.Repeat("1970", 16),
		"event_time": "2026-10-13T03:00:00Z", "recorded_at": "2026-10-13T03:00:01Z", "change": "created",
		"changed_fields": []any{}, "organization": "Cơ quan", "device_id": nil, "rule_version": "contact.v1",
		"idem_key": "create:" + contactMetadata1970,
	}
	if kind == "unit" {
		m["unit_id"], m["org_ref"], m["stream_key"] = contactMetadata1970, nil, "unit:"+contactMetadata1970
		m["idem_key"] = "unit:" + contactMetadata1970
	} else {
		m["subject_ref"], m["stream_key"] = contactMetadata1970, "person:"+contactMetadata1970
	}
	return m
}

func contactOracleJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func contactOracleLog() map[string]any {
	return map[string]any{"time": "2026-10-13T03:00:00Z", "level": "INFO", "msg": "request", "method": "POST",
		"path": "/v1/contacts/persons", "status": 201, "errors": "", "actor_ref": contactMetadata1970, "tenant_id": contactMetadata1970}
}

func TestContactPIIOracle_Metadata(t *testing.T) {
	for _, kind := range []string{"person", "unit"} {
		t.Run(kind, func(t *testing.T) {
			if err := contactRowWithoutPII(kind, contactOracleJSON(t, contactOracleRow(kind)), contactPIISentinels()); err != nil {
				t.Fatal(err)
			}
		})
	}
	m := contactOracleRow("person")
	m["seq"], m["prev_hash"] = 1970, `\x`+strings.Repeat("1970", 16)
	m["event_time"] = "1970-01-01T00:00:00Z" // timestamp sổ có kiểu, khác birth_year ở text chưa biết.
	if err := contactRowWithoutPII("person", contactOracleJSON(t, m), contactPIISentinels()); err != nil {
		t.Fatal(err)
	}
	if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, contactOracleLog())+"\n"), contactPIISentinels()); err != nil {
		t.Fatal(err)
	}
}

func TestContactPIIOracle_TextAndNestedValues(t *testing.T) {
	for _, sentinel := range append(contactPIISentinels(), "Hoàng Văn Riêng Tư") {
		for _, embedded := range []bool{false, true} {
			t.Run(sentinel+map[bool]string{false: "/exact", true: "/embedded"}[embedded], func(t *testing.T) {
				value := sentinel
				if embedded {
					value = "prefix [" + value + "] suffix"
				}
				row := contactOracleRow("person")
				row["organization"] = value
				if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
					t.Fatal("DB text PII lọt oracle")
				}
				row = contactOracleRow("unit")
				row["extra"] = map[string]any{"items": []any{map[string]any{"note": value}}}
				if err := contactRowWithoutPII("unit", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
					t.Fatal("DB nested PII lọt oracle")
				}
				log := contactOracleLog()
				log["errors"] = value
				if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
					t.Fatal("log text PII lọt oracle")
				}
				log = contactOracleLog()
				log["extra"] = []any{map[string]any{"notes": []any{value}}}
				if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
					t.Fatal("log nested PII lọt oracle")
				}
			})
		}
	}
	for _, key := range []string{"birth_year", "device_id", "unknown"} {
		for _, value := range []any{1970, contactMetadata1970, "create:" + contactMetadata1970} {
			t.Run(key, func(t *testing.T) {
				row := contactOracleRow("person")
				row[key] = value
				if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
					t.Fatal("unknown/free-text UUID-looking PII lọt DB oracle")
				}
				log := contactOracleLog()
				log[key] = value
				if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
					t.Fatal("unknown/free-text UUID-looking PII lọt log oracle")
				}
			})
		}
	}
	for _, value := range []any{1970, contactMetadata1970} {
		row := contactOracleRow("person")
		row["extra"] = []any{map[string]any{"values": []any{value}}}
		if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
			t.Fatal("nested number/UUID-looking text lọt DB oracle")
		}
		log := contactOracleLog()
		log["extra"] = []any{map[string]any{"values": []any{value}}}
		if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
			t.Fatal("nested number/UUID-looking text lọt log oracle")
		}
	}
	row := contactOracleRow("person")
	row["extra"] = map[string]any{"actor_ref": contactMetadata1970}
	if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
		t.Fatal("nested actor_ref không có hợp đồng metadata")
	}
	log := contactOracleLog()
	log["extra"] = map[string]any{"actor_ref": contactMetadata1970}
	if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
		t.Fatal("nested actor_ref log không có hợp đồng metadata")
	}
	log = contactOracleLog()
	log["1970"] = "safe"
	if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
		t.Fatal("PII trong tên thuộc tính lọt oracle")
	}
	row = contactOracleRow("person")
	row["organization"] = "hoàng văn riêng"
	if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
		t.Fatal("name fragment khác hoa/thường lọt oracle")
	}
	log = contactOracleLog()
	log["errors"] = "1970"
	if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, contactOracleLog())+"\n"+contactOracleJSON(t, log)+"\n"), contactPIISentinels()); err == nil {
		t.Fatal("PII ở record log sau bị bỏ qua")
	}
}

func TestContactPIIOracle_InvalidMetadataAndJSON(t *testing.T) {
	for _, key := range []string{"id", "tenant_id", "actor_ref", "subject_ref", "supersedes_id", "stream_key", "seq", "row_hash", "prev_hash", "event_time", "recorded_at"} {
		t.Run(key, func(t *testing.T) {
			row := contactOracleRow("person")
			row[key] = "not-valid-metadata"
			if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
				t.Fatal("metadata không hợp lệ được bỏ qua")
			}
		})
	}
	row := contactOracleRow("person")
	row["stream_key"] = "person:019a0000-0000-7000-8000-000000000001"
	if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
		t.Fatal("stream_key không dẫn xuất từ reference được bỏ qua")
	}
	row = contactOracleRow("person")
	row["idem_key"] = "create:prefix1970suffix"
	if err := contactRowWithoutPII("person", contactOracleJSON(t, row), contactPIISentinels()); err == nil {
		t.Fatal("idem_key text chứa PII được bỏ qua")
	}
	for _, raw := range []string{`{"actor_ref":`, `null`} {
		if err := contactRowWithoutPII("person", raw, contactPIISentinels()); err == nil {
			t.Fatal("JSON DB không đọc được lại PASS")
		}
		if err := contactLogsWithoutPII([]byte(raw), contactPIISentinels()); err == nil {
			t.Fatal("JSON log không đọc được lại PASS")
		}
	}
	for _, key := range []string{"actor_ref", "tenant_id", "time"} {
		log := contactOracleLog()
		log[key] = "not-valid-metadata"
		if err := contactLogsWithoutPII([]byte(contactOracleJSON(t, log)), contactPIISentinels()); err == nil {
			t.Fatal("metadata log không hợp lệ được bỏ qua")
		}
	}
}

type contactRowsStub struct {
	raw             string
	read            bool
	scanErr, rowErr error
}

func (r *contactRowsStub) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return r.raw != "" || r.scanErr != nil
}
func (r *contactRowsStub) Scan(dst ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dst[0].(*string), *dst[1].(*string) = "person", r.raw
	return nil
}
func (r *contactRowsStub) Err() error { return r.rowErr }

func TestContactPIIOracle_ReadFailures(t *testing.T) {
	broken := errors.New("read failed")
	for _, r := range []*contactRowsStub{{scanErr: broken}, {rowErr: broken}} {
		if err := contactRowsWithoutPII(r, contactPIISentinels()); !errors.Is(err, broken) {
			t.Fatalf("lỗi Scan/rows.Err bị nuốt: %v", err)
		}
	}
	r := &contactRowsStub{raw: contactOracleJSON(t, contactOracleRow("person"))}
	if err := contactRowsWithoutPII(r, contactPIISentinels()); err != nil {
		t.Fatal(err)
	}
}
