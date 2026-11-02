package app

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/google/uuid"
)

const ArchiveMaxFiles = 10
const ArchiveMaxBytes = 20 << 20

type ArchiveSelection struct {
	PeriodID uuid.UUID `json:"period_id"`
	ExportID uuid.UUID `json:"export_id"`
	Format   string    `json:"format,omitempty"`
	RunID    uuid.UUID `json:"run_id,omitempty"`
}

type ArchivePreview struct {
	ExportID uuid.UUID                `json:"export_id"`
	Version  int64                    `json:"version"`
	Content  domain.Draft             `json:"content"`
	Document *templatedomain.Rendered `json:"template_document,omitempty"`
}

// PreviewExport never loads a draft, current source values or a live template.
func (s *Service) PreviewExport(ctx context.Context, periodID, exportID uuid.UUID) (ArchivePreview, error) {
	x, err := s.Download(ctx, periodID, exportID)
	if err != nil {
		return ArchivePreview{}, err
	}
	document, err := renderFixedTemplate(x.Content.TemplateSnapshot, x.Content, templateTerminal(x.Content), x.Filter)
	if err != nil {
		return ArchivePreview{}, err
	}
	x.Content.Data.Rows = domain.SelectRows(x.Content.Data.Rows, x.Filter)
	return ArchivePreview{ExportID: x.ID, Version: x.Version, Content: x.Content, Document: document}, nil
}

func ValidateArchiveSelection(in []ArchiveSelection) error {
	if len(in) < 1 || len(in) > ArchiveMaxFiles {
		return ErrInvalid.WithDetail("Chọn từ 1 đến 10 tệp đã xuất")
	}
	seen := map[string]bool{}
	for _, x := range in {
		if x.PeriodID == uuid.Nil || x.ExportID == uuid.Nil {
			return ErrInvalid.WithDetail("Bản xuất không hợp lệ")
		}
		key := "excel:" + x.ExportID.String()
		switch x.Format {
		case "", "excel":
			if x.RunID != uuid.Nil {
				return ErrInvalid.WithDetail("Excel không nhận mã lần xuất Word")
			}
		case "word":
			if x.RunID == uuid.Nil {
				return ErrInvalid.WithDetail("Word cần mã lần xuất đã liên kết")
			}
			key = "word:" + x.RunID.String()
		default:
			return ErrInvalid.WithDetail("Định dạng tệp không hợp lệ")
		}
		if seen[key] {
			return ErrInvalid.WithDetail("Tệp được chọn bị trùng")
		}
		seen[key] = true
	}
	return nil
}

// ArchiveZIP downloads the stored bytes, with the same tenant/session/hash check and
// access checkpoint as each individual download. It never regenerates a report.
func (s *Service) ArchiveZIP(ctx context.Context, in []ArchiveSelection) ([]byte, error) {
	if err := ValidateArchiveSelection(in); err != nil {
		return nil, err
	}
	files := make([]store.Export, 0, len(in))
	total := 0
	for _, item := range in {
		if item.Format == "word" {
			return nil, ErrDependency.WithDetail("Bộ đọc Word đã liên kết chưa được nối")
		}
		x, err := s.Download(ctx, item.PeriodID, item.ExportID)
		if err != nil {
			return nil, err
		}
		if len(x.Body) > ArchiveMaxBytes-total {
			return nil, ErrInvalid.WithDetail("Tổng dung lượng vượt 20 MiB; chọn ít tệp hơn")
		}
		total += len(x.Body)
		files = append(files, x)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, x := range files {
		// IDs form a safe unique filename; no user-supplied path can escape the archive.
		w, err := z.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("bao-cao-%s-v%d.xlsx", x.ID, x.Version), Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(x.Body); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
