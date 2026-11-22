package app

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	c, err := s.reader(ctx)
	if err != nil {
		return ArchivePreview{}, err
	}
	x, err := s.Download(ctx, periodID, exportID)
	if err != nil {
		return ArchivePreview{}, err
	}
	document, err := renderFixedTemplate(x.Content.TemplateSnapshot, x.Content, templateTerminal(x.Content), x.Filter)
	if err != nil {
		return ArchivePreview{}, err
	}
	x.Content.Data.Rows = domain.SelectRows(x.Content.Data.Rows, x.Filter)
	// The local renderer has completed, but the response is withheld until the
	// access ledger commits. No exporter or network port runs in this transaction.
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		target := store.ArchiveTarget{PeriodID: periodID, ExportID: exportID, Format: "preview"}
		return s.auditArchive(ctx, tx, c, "preview_attempt", ArchiveSelection{PeriodID: periodID, ExportID: exportID, Format: "preview"}, 0, 1, []store.ArchiveTarget{target})
	})
	if err != nil {
		return ArchivePreview{}, err
	}
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
