package problem

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteRFC9457(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/consents", nil)
	Write(rec, req, Validation.WithField("purpose", "bắt buộc").With("needs_confirm", true), "trace-1")

	if ct := rec.Header().Get("Content-Type"); ct != ContentType {
		t.Fatalf("Content-Type = %s", ct)
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "title", "status", "code", "instance", "trace_id", "errors", "needs_confirm"} {
		if _, ok := body[k]; !ok {
			t.Errorf("thiếu trường %s", k)
		}
	}
	if body["type"] != TypeBase+"validation_failed" {
		t.Errorf("type = %v", body["type"])
	}
}

func TestFromHidesRawErrors(t *testing.T) {
	raw := errors.New("pq: password authentication failed for user secret_user")
	p := From(raw)
	if p.Code != "internal_error" {
		t.Fatalf("code = %s", p.Code)
	}
	rec := httptest.NewRecorder()
	Write(rec, nil, p, "")
	if strings.Contains(rec.Body.String(), "secret_user") {
		t.Fatal("lỗi thô bị lộ ra client")
	}
	if !errors.Is(p, raw) {
		t.Fatal("phải giữ nguyên nhân gốc cho log")
	}
}

func TestBuildersDoNotMutateShared(t *testing.T) {
	_ = Validation.WithField("a", "b").With("x", 1)
	if len(Validation.Errors) != 0 || Validation.Extra != nil {
		t.Fatal("biến dùng chung bị sửa")
	}
}
