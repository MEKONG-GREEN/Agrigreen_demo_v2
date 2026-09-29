package harness

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
)

// User là một người dùng cuối có token do issuer giả ký.
type User struct {
	s     *Stack
	Actor uuid.UUID
	// Tenant, Org, Roles như trong token.
	Tenant uuid.UUID
	Org    uuid.UUID
	Roles  []string
	MFA    bool
	Token  string
	// DeviceID gắn header X-Device-Id cho lời gọi đồng bộ (rỗng → không gắn).
	DeviceID string
}

// UserSpec mô tả người dùng cần phát token.
type UserSpec struct {
	Tenant uuid.UUID
	Org    uuid.UUID // org_ref trong token (đơn vị công tác) — uuid.Nil: không có
	Roles  []string
	MFA    bool
	// Subject là sub của token (vd tài khoản nông dân vừa tạo ở pf/accounts); uuid.Nil → ngẫu nhiên.
	Subject uuid.UUID
}

// User phát token cho một người dùng (actor = Subject hoặc ngẫu nhiên).
func (s *Stack) User(spec UserSpec) *User {
	actor := spec.Subject
	if actor == uuid.Nil {
		actor = uuid.Must(uuid.NewV7())
	}
	tok := s.Issuer.Sign(s.T, authztest.Token{Subject: actor, TenantID: spec.Tenant, Roles: spec.Roles, MFA: spec.MFA, OrgRef: spec.Org, TTL: 2 * time.Hour})
	return &User{s: s, Actor: actor, Tenant: spec.Tenant, Org: spec.Org, Roles: spec.Roles, MFA: spec.MFA, Token: tok}
}

// Resp là phản hồi HTTP đã đọc hết thân.
type Resp struct {
	Status int
	Header http.Header
	Raw    []byte
	Body   map[string]any
}

// Code trả trường "code" của problem+json (rỗng nếu không có).
func (r Resp) Code() string { s, _ := r.Body["code"].(string); return s }

// Str trả trường chuỗi ở mức gốc.
func (r Resp) Str(k string) string { s, _ := r.Body[k].(string); return s }

// Items trả mảng "items" (hoặc mảng tên k khác nếu truyền).
func (r Resp) Items(k ...string) []map[string]any {
	key := "items"
	if len(k) > 0 {
		key = k[0]
	}
	arr, _ := r.Body[key].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// IsProblem cho biết phản hồi là application/problem+json có code và trace_id (R-15, R-63).
func (r Resp) IsProblem() bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/problem+json") && r.Code() != "" && r.Body["trace_id"] != nil
}

// Surface chọn server nhận lời gọi.
type Surface int

// Các bề mặt HTTP.
const (
	App Surface = iota
	Admin
	Vault
)

func (u *User) base(sf Surface) string {
	switch sf {
	case Admin:
		return u.s.Admin.URL
	case Vault:
		return u.s.Vault.URL
	default:
		return u.s.App.URL
	}
}

// NewIdem sinh Idempotency-Key mới.
func NewIdem() string { return "e2e-" + uuid.Must(uuid.NewV7()).String() }

// Do gọi một route. Với POST mà idem rỗng, tự sinh Idempotency-Key (mọi POST
// của kịch bản đều mang khóa — R-51); muốn gửi KHÔNG có khóa dùng DoRaw.
func (u *User) Do(sf Surface, method, path, idem string, body any) Resp {
	if method == http.MethodPost && idem == "" {
		idem = NewIdem()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			u.s.T.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	h := http.Header{}
	if body != nil {
		h.Set("Content-Type", "application/json")
	}
	if idem != "" {
		h.Set("Idempotency-Key", idem)
	}
	return u.DoRaw(sf, method, path, h, rd)
}

// DoRaw gửi request với header tùy ý (Authorization và X-Device-Id tự gắn).
func (u *User) DoRaw(sf Surface, method, path string, h http.Header, body io.Reader) Resp {
	t := u.s.T
	if tt, ok := t.(*testing.T); ok {
		tt.Helper()
	}
	req, err := http.NewRequest(method, u.base(sf)+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, vs := range h {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if u.Token != "" {
		req.Header.Set("Authorization", "Bearer "+u.Token)
	}
	if u.DeviceID != "" && req.Header.Get("X-Device-Id") == "" {
		req.Header.Set("X-Device-Id", u.DeviceID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return Resp{Status: resp.StatusCode, Header: resp.Header, Raw: raw, Body: m}
}

// Multipart gửi POST multipart/form-data: fields là trường văn bản, file là
// phần tệp tên fileField (bỏ qua nếu fileField rỗng).
func (u *User) Multipart(sf Surface, path, idem string, fields map[string]string, fileField string, file []byte) Resp {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			u.s.T.Fatal(err)
		}
	}
	if fileField != "" {
		fw, err := w.CreateFormFile(fileField, "blob.bin")
		if err != nil {
			u.s.T.Fatal(err)
		}
		if _, err := fw.Write(file); err != nil {
			u.s.T.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		u.s.T.Fatal(err)
	}
	h := http.Header{}
	h.Set("Content-Type", w.FormDataContentType())
	if idem == "" {
		idem = NewIdem()
	}
	h.Set("Idempotency-Key", idem)
	return u.DoRaw(sf, http.MethodPost, path, h, &buf)
}
