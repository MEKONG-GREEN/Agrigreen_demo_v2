package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"io"
)

const MaxDesignImportBytes = 2 << 20

type CNTSDesignView struct {
	Code       string             `json:"code"`
	BM         string             `json:"bm"`
	Title      string             `json:"title"`
	PeriodType string             `json:"period_type"`
	Due        string             `json:"due"`
	TemplateID *string            `json:"template_id"`
	Version    *int64             `json:"version"`
	Fields     []domain.CNTSField `json:"fields"`
}

func designReader(ctx context.Context) error {
	_, k, err := port.RequireSession(ctx, port.SessionRoleAdmin, port.SessionRoleProvince)
	if err != nil {
		return err
	}
	if k.Role != port.SessionRoleAdmin && !k.AllowsDepartment(partyport.DeptChanNuoiThuySan) {
		return problem.Forbidden.WithDetail("Thiết kế biểu mẫu thuộc phòng Chăn nuôi – Thủy sản")
	}
	return nil
}

// CNTSDesign đọc đúng cấu trúc phiên bản đã cài, không dùng cấu trúc mã nguồn thay thế bản đó.
// Phần đầu #174: năm biểu kỳ native; nhật ký/biểu điều tra/ghi phiên bản ở #191/#192.
func (s *Service) CNTSDesign(ctx context.Context) ([]CNTSDesignView, error) {
	if err := designReader(ctx); err != nil {
		return nil, err
	}
	if s.FormsNative == nil {
		return nil, ErrDependency.WithDetail("chưa nối cap/forms")
	}
	out := make([]CNTSDesignView, 0, len(domain.CNTSForms))
	for _, f := range domain.CNTSForms {
		v := CNTSDesignView{Code: f.Code, BM: f.BM, Title: f.Title, PeriodType: f.PeriodType, Due: f.Due, Fields: f.Fields}
		ref, err := s.FormsNative.LatestTemplate(ctx, domain.ProfileCNTS, f.Code)
		if err != nil && !errors.Is(err, formsport.ErrTemplateNotFound) {
			return nil, err
		}
		if err == nil {
			var fields []domain.CNTSField
			if json.Unmarshal(ref.Fields, &fields) != nil || len(fields) == 0 || len(fields) > domain.MaxDesignFields {
				return nil, ErrDependency.WithDetail("cấu trúc phiên bản biểu không hợp lệ")
			}
			id, version := ref.ID.String(), ref.Version
			v.TemplateID, v.Version, v.Title, v.Fields = &id, &version, ref.Title, fields
		}
		out = append(out, v)
	}
	return out, nil
}

// PreviewCNTSDesignImport chỉ đọc/kiểm tệp; không gọi DefineTemplate, không ghi DB/kỳ/nhật ký.
func (s *Service) PreviewCNTSDesignImport(ctx context.Context, src io.Reader) (domain.DesignImport, error) {
	if err := designReader(ctx); err != nil {
		return domain.DesignImport{}, err
	}
	lim := tabular.Limits{MaxBytes: MaxDesignImportBytes, MaxRows: domain.MaxDesignFields, MaxCols: len(domain.DesignHeaders)}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		return domain.DesignImport{}, problem.Validation.WithField("file", tabular.Message(err, lim))
	}
	return domain.ParseDesign(tb), nil
}
