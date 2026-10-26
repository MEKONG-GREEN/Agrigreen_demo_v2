package main

// Client của Application API /v1/hrm/* (module pf/hrm). Mọi lần ghi đi qua POST /v1/hrm/staff — lớp app của module
// kiểm quyền FN-20, danh mục, bảng hệ số, FN-08, đưa dữ liệu cá nhân vào kho định danh và ghi sổ chỉ ghi thêm. Công cụ
// không nối DB, không chạm kho định danh trực tiếp. Không đưa dữ liệu cá nhân vào URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// api là client HTTP đã xác thực (Bearer của người dùng HCTH/Admin + ngữ cảnh phiên X-AG-Context).
type api struct {
	base, token, kctx string
	hc                *http.Client
}

// problemBody là thân application/problem+json (R-14: không chứa dữ liệu cá nhân).
type problemBody struct {
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Errors []struct {
		Field  string `json:"field"`
		Reason string `json:"reason"`
	} `json:"errors"`
}

func (p problemBody) String() string {
	var b strings.Builder
	b.WriteString(p.Title)
	if p.Detail != "" {
		b.WriteString(": " + p.Detail)
	}
	for _, e := range p.Errors {
		b.WriteString("; " + e.Field + ": " + e.Reason)
	}
	return b.String()
}

// apiError là phản hồi lỗi của API.
type apiError struct {
	Status int
	P      problemBody
}

func (e *apiError) Error() string { return fmt.Sprintf("%d %s %s", e.Status, e.P.Code, e.P.String()) }

func (a *api) do(ctx context.Context, method, path, idem string, body, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.base, "/")+path, rd)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if a.kctx != "" {
		req.Header.Set("X-AG-Context", a.kctx)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	var res *http.Response
	for attempt := 0; ; attempt++ {
		res, err = a.hc.Do(req)
		// Chỉ thử lại lỗi mạng/503; POST an toàn nhờ Idempotency-Key.
		if (err == nil && res.StatusCode != http.StatusServiceUnavailable) || attempt >= 2 {
			break
		}
		if err == nil {
			_ = res.Body.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
		if req.GetBody != nil {
			req.Body, _ = req.GetBody()
		}
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		e := &apiError{Status: res.StatusCode}
		if json.Unmarshal(raw, &e.P) != nil || e.P.Code == "" {
			e.P.Title = http.StatusText(res.StatusCode)
		}
		return res.Header, e
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return res.Header, fmt.Errorf("%s %s: phản hồi không phải JSON hợp lệ", method, path)
		}
	}
	return res.Header, nil
}

// catalogItem là một mục danh mục nhân sự (GET /v1/hrm/catalogs/{kind}).
type catalogItem struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Attrs  struct {
		Leader bool `json:"leader"`
	} `json:"attrs"`
}

// unitItem là một đơn vị nội bộ (GET /v1/hrm/units).
type unitItem struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// scaleItem là bảng hệ số hiệu lực của một ngạch (GET /v1/hrm/grade-scales).
type scaleItem struct {
	Grade struct {
		Code string `json:"code"`
	} `json:"grade"`
	Scale *struct {
		Coefficients []string `json:"coefficients"`
	} `json:"scale"`
}

// Refs là dữ liệu tham chiếu đọc từ máy chủ (chỉ đọc): danh mục, đơn vị nội bộ, bảng hệ số.
type Refs struct {
	Catalogs map[string][]catalogItem // kind → mục (kể cả đã ngừng)
	Units    []unitItem               // rỗng = chưa nối danh mục đơn vị (máy chủ chỉ kiểm dạng mã)
	Scales   map[string][]string      // mã ngạch → hệ số theo bậc
}

// catalogKinds là danh mục mà tệp nhập tham chiếu.
var catalogKinds = []string{"position", "grade", "education_level", "major", "leave_reason", "political_theory", "state_mgmt", "it_level",
	"ethnicity", "religion"}

// loadRefs đọc dữ liệu tham chiếu; kiểm hồ sơ nhân sự đang bật cho người gọi.
func (a *api) loadRefs(ctx context.Context) (*Refs, error) {
	var acc struct {
		Enabled bool              `json:"enabled"`
		Perms   map[string]string `json:"perms"`
	}
	if _, err := a.do(ctx, http.MethodGet, "/hrm/access", "", nil, &acc); err != nil {
		return nil, fmt.Errorf("kiểm quyền: %w", err)
	}
	if !acc.Enabled {
		return nil, fmt.Errorf("hồ sơ nhân sự đang tắt (AG_HRM_RECORDS) trên máy chủ")
	}
	if s := acc.Perms["profile.edit"]; s != "all" {
		return nil, fmt.Errorf("tài khoản cần quyền profile.edit phạm vi toàn Trung tâm (đang %q) — dùng tài khoản Cấp Tỉnh - HCTH hoặc Admin", s)
	}
	r := &Refs{Catalogs: map[string][]catalogItem{}, Scales: map[string][]string{}}
	for _, k := range catalogKinds {
		var out struct {
			Items []catalogItem `json:"items"`
		}
		if _, err := a.do(ctx, http.MethodGet, "/hrm/catalogs/"+k+"?include_retired=true", "", nil, &out); err != nil {
			return nil, fmt.Errorf("danh mục %s: %w", k, err)
		}
		r.Catalogs[k] = out.Items
	}
	var us struct {
		Items []unitItem `json:"items"`
	}
	if _, err := a.do(ctx, http.MethodGet, "/hrm/units", "", nil, &us); err != nil {
		return nil, fmt.Errorf("đơn vị nội bộ: %w", err)
	}
	r.Units = us.Items
	var sc struct {
		Grades []scaleItem `json:"grades"`
	}
	if _, err := a.do(ctx, http.MethodGet, "/hrm/grade-scales", "", nil, &sc); err != nil {
		return nil, fmt.Errorf("bảng hệ số: %w", err)
	}
	for _, g := range sc.Grades {
		if g.Scale != nil {
			r.Scales[g.Grade.Code] = g.Scale.Coefficients
		}
	}
	return r, nil
}

// createResult là kết quả POST /v1/hrm/staff.
type createResult struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	Warnings []string `json:"warnings"`
}

// createStaff gửi một hồ sơ; replayed = phản hồi gọi lặp của cùng Idempotency-Key (đã tạo ở lần chạy trước).
func (a *api) createStaff(ctx context.Context, body StaffRequest, idem string) (createResult, bool, error) {
	var out createResult
	h, err := a.do(ctx, http.MethodPost, "/hrm/staff", idem, body, &out)
	return out, h != nil && h.Get("Idempotent-Replayed") != "", err
}
