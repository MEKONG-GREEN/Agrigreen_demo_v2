package app

import (
	"errors"
	"io"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// MaxImportBytes giới hạn dung lượng tệp nhập (CSV hoặc .xlsx). Nhập nông hộ hàng loạt ở pf/cooperative (W3.3).
const MaxImportBytes = 2 << 20

// rowReason là lý do loại một dòng nhập lấy từ problem (không lặp lại giá trị người dùng nhập).
func rowReason(pr *problem.Problem) string {
	if len(pr.Errors) > 0 {
		return pr.Errors[0].Field + ": " + pr.Errors[0].Reason
	}
	if pr.Detail != "" {
		return pr.Detail
	}
	return pr.Title
}

// vaultError phân loại lỗi của kho định danh: kho từ chối dữ liệu hoặc quyền (4xx,
// vd SĐT sai dạng, thiếu vai vault_registrar) là lỗi của người gọi — trả nguyên
// problem của kho để client sửa, không thử lại; lỗi hệ thống hoặc không phản hồi →
// 503 party.dependency_unavailable.
func vaultError(err error) error {
	var p *problem.Problem
	if errors.As(err, &p) && p.Status >= 400 && p.Status < 500 {
		return p
	}
	return ErrDependency.WithDetail("kho định danh từ chối hoặc không phản hồi").WithCause(err)
}

// readImportTable đọc tệp CSV/.xlsx và quy lỗi đọc về party.import_invalid kèm lý do.
func readImportTable(src io.Reader, lim tabular.Limits) (*tabular.Table, error) {
	tb, err := tabular.Read(src, lim)
	if err == nil {
		return tb, nil
	}
	var fe *tabular.FormulaError
	switch {
	case errors.As(err, &fe), errors.Is(err, tabular.ErrTooManyRows), errors.Is(err, tabular.ErrTooLarge),
		errors.Is(err, tabular.ErrTooManyColumns), errors.Is(err, tabular.ErrEncoding), errors.Is(err, tabular.ErrLegacyExcel),
		errors.Is(err, tabular.ErrEmpty), errors.Is(err, tabular.ErrFormat):
		return nil, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	return nil, err
}
