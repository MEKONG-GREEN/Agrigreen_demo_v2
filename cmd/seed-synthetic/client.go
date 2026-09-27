package main

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

// api gọi Application API và Admin API của một stack đang chạy bằng bearer token
// của tài khoản dịch vụ. Không có đường tắt vào DB: mọi dữ liệu đi qua API như
// người dùng thật (kiểm quyền, phạm vi, vault, idempotency).
type api struct {
	app, admin string
	token      string
	hc         *http.Client
}

// surface chọn bề mặt API.
type surface int

const (
	appAPI surface = iota
	adminAPI
)

// resp là phản hồi đã đọc.
type resp struct {
	status   int
	replayed bool // Idempotent-Replayed: phản hồi cũ của cùng Idempotency-Key
	raw      []byte
	body     map[string]any
}

// isNew: bản ghi vừa được tạo ở lần gọi này (201, không phải phản hồi gọi lặp).
func (r resp) isNew() bool { return r.status == http.StatusCreated && !r.replayed }

func (r resp) code() string { s, _ := r.body["code"].(string); return s }

func (r resp) String() string {
	b := r.raw
	if len(b) > 300 {
		b = b[:300]
	}
	return fmt.Sprintf("%d %s", r.status, b)
}

// errStatus là lỗi phản hồi ngoài mong đợi. Thân problem+json của nền tảng
// không chứa dữ liệu cá nhân (R-14), nên được phép đưa vào thông báo lỗi.
type errStatus struct {
	what string
	r    resp
}

func (e *errStatus) Error() string { return e.what + ": " + e.r.String() }

func (a *api) base(s surface) string {
	if s == adminAPI {
		return a.admin
	}
	return a.app
}

// do gửi một request JSON; POST luôn mang Idempotency-Key (khóa tất định theo
// seed để chạy lại trong 48 giờ trả đúng kết quả cũ — R-51).
func (a *api) do(ctx context.Context, s surface, method, path, idem string, body any) (resp, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return resp{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.base(s), "/")+path, rd)
	if err != nil {
		return resp{}, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
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
		// Chỉ thử lại lỗi mạng/503 (vd vault khởi động chậm); POST an toàn nhờ Idempotency-Key.
		if (err == nil && res.StatusCode != http.StatusServiceUnavailable) || attempt >= 3 {
			break
		}
		if err == nil {
			_ = res.Body.Close()
		}
		select {
		case <-ctx.Done():
			return resp{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
		}
		if req.GetBody != nil {
			if req.Body, err = req.GetBody(); err != nil {
				return resp{}, err
			}
		}
	}
	if err != nil {
		return resp{}, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return resp{}, err
	}
	out := resp{status: res.StatusCode, raw: raw, replayed: res.Header.Get("Idempotent-Replayed") == "true"}
	_ = json.Unmarshal(raw, &out.body)
	return out, nil
}

// expect gọi và đòi mã trạng thái thuộc ok.
func (a *api) expect(ctx context.Context, s surface, method, path, idem string, body any, ok ...int) (resp, error) {
	r, err := a.do(ctx, s, method, path, idem, body)
	if err != nil {
		return r, err
	}
	for _, c := range ok {
		if r.status == c {
			return r, nil
		}
	}
	return r, &errStatus{what: method + " " + path, r: r}
}

// list đọc hết các trang {items, next_cursor} của một route GET.
func (a *api) list(ctx context.Context, s surface, path string) ([]map[string]any, error) {
	var all []map[string]any
	cursor := ""
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for page := 0; page < 1000; page++ {
		p := path + sep + "limit=200"
		if cursor != "" {
			p += "&cursor=" + cursor
		}
		r, err := a.expect(ctx, s, http.MethodGet, p, "", nil, http.StatusOK)
		if err != nil {
			return nil, err
		}
		items, _ := r.body["items"].([]any)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				all = append(all, m)
			}
		}
		next, _ := r.body["next_cursor"].(string)
		if next == "" {
			return all, nil
		}
		cursor = next
	}
	return all, fmt.Errorf("GET %s: quá nhiều trang", path)
}

func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func str(m map[string]any, k string) string         { v, _ := m[k].(string); return v }
