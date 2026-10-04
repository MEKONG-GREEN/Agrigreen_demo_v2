// Package problem trả lỗi theo RFC 9457 application/problem+json (R-15).
//
// Không bao giờ trả error.Error() thô ra ngoài: lỗi không phải *Problem được
// quy về 500 với mã internal_error, chi tiết chỉ ghi vào log.
// Mã lỗi (Code) là hợp đồng ổn định với client — xem docs/errors.md.
package problem

import (
	"encoding/json"
	"errors"
	"net/http"
)

// ContentType của phản hồi lỗi.
const ContentType = "application/problem+json"

// TypeBase là tiền tố URI của trường "type".
const TypeBase = "https://errors.agrigreen.vn/"

// Problem là một lỗi nghiệp vụ có mã ổn định.
type Problem struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	// Errors liệt kê lỗi theo trường khi kiểm dữ liệu vào.
	Errors []FieldError `json:"errors,omitempty"`
	// Extra là các trường mở rộng nghiệp vụ, vd needs_confirm.
	Extra map[string]any `json:"-"`
	cause error
}

// FieldError mô tả một trường không hợp lệ.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Code + ": " + p.Detail
	}
	return p.Code
}

// Unwrap cho phép errors.Is/As tới nguyên nhân gốc (chỉ dùng nội bộ).
func (p *Problem) Unwrap() error { return p.cause }

// New tạo Problem.
func New(status int, code, title string) *Problem {
	return &Problem{Status: status, Code: code, Title: title}
}

// WithDetail gắn mô tả cho người dùng. Không đưa dữ liệu cá nhân vào (R-14).
func (p *Problem) WithDetail(d string) *Problem {
	cp := *p
	cp.Detail = d
	return &cp
}

// WithCause gắn nguyên nhân gốc — không lộ ra ngoài.
func (p *Problem) WithCause(err error) *Problem {
	cp := *p
	cp.cause = err
	return &cp
}

// WithField thêm lỗi theo trường.
func (p *Problem) WithField(field, reason string) *Problem {
	cp := *p
	cp.Errors = append(append([]FieldError(nil), p.Errors...), FieldError{Field: field, Reason: reason})
	return &cp
}

// With thêm trường mở rộng.
func (p *Problem) With(key string, v any) *Problem {
	cp := *p
	cp.Extra = map[string]any{}
	for k, val := range p.Extra {
		cp.Extra[k] = val
	}
	cp.Extra[key] = v
	return &cp
}

// Mã lỗi dùng chung. Module định nghĩa mã riêng theo dạng <module>.<lỗi>.
var (
	BadRequest          = New(http.StatusBadRequest, "bad_request", "Yêu cầu không hợp lệ")
	Validation          = New(http.StatusUnprocessableEntity, "validation_failed", "Dữ liệu không hợp lệ")
	IdempotencyRequired = New(http.StatusBadRequest, "idempotency_key_required", "Thiếu header Idempotency-Key")
	IdempotencyConflict = New(http.StatusUnprocessableEntity, "idempotency_key_reused", "Idempotency-Key đã dùng cho yêu cầu khác")
	IdempotencyInFlight = New(http.StatusConflict, "idempotency_in_progress", "Yêu cầu cùng Idempotency-Key đang được xử lý")
	Unauthenticated     = New(http.StatusUnauthorized, "unauthenticated", "Chưa xác thực")
	Forbidden           = New(http.StatusForbidden, "forbidden", "Không có quyền")
	MFARequired         = New(http.StatusForbidden, "mfa_required", "Vai trò này bắt buộc xác thực đa yếu tố")
	SelfApproval        = New(http.StatusForbidden, "self_approval_forbidden", "Không được phê duyệt thay đổi do chính mình đặt")
	ModuleNotEnabled    = New(http.StatusForbidden, "module_not_enabled", "Module chưa được bật cho đơn vị") // R-MOD-08 (W1.4)
	NotFound            = New(http.StatusNotFound, "not_found", "Không tìm thấy")
	Conflict            = New(http.StatusConflict, "conflict", "Xung đột dữ liệu")
	PayloadTooLarge     = New(http.StatusRequestEntityTooLarge, "payload_too_large", "Dữ liệu gửi lên quá lớn")
	Internal            = New(http.StatusInternalServerError, "internal_error", "Lỗi hệ thống")
)

// From quy mọi error về *Problem. Lỗi lạ → Internal, giữ nguyên nhân để ghi log.
func From(err error) *Problem {
	var p *Problem
	if errors.As(err, &p) {
		return p
	}
	return Internal.WithCause(err)
}

// Write ghi Problem ra client, kèm trace_id (R-63) và instance là đường dẫn.
func Write(w http.ResponseWriter, r *http.Request, p *Problem, traceID string) {
	body := map[string]any{
		"type":   TypeBase + p.Code,
		"title":  p.Title,
		"status": p.Status,
		"code":   p.Code,
	}
	if p.Detail != "" {
		body["detail"] = p.Detail
	}
	if len(p.Errors) > 0 {
		body["errors"] = p.Errors
	}
	if r != nil {
		body["instance"] = r.URL.Path
	}
	if traceID != "" {
		body["trace_id"] = traceID
	}
	for k, v := range p.Extra {
		if _, taken := body[k]; !taken {
			body[k] = v
		}
	}
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(body)
}
