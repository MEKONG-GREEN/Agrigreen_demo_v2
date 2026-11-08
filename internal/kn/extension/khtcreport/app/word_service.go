package app

import (
	"context"
	"fmt"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WordSubject là đối tượng của lịch sử Word và số văn bản trong pf/export: một báo cáo (kỳ KHTC) nhận một số "{n}/BC-TTKN".
func WordSubject(period uuid.UUID) string { return "khtc_report:" + period.String() }

// ExportWord dựng Word (.docx) từ nội dung đóng băng của một bản xuất Excel (W11.66) qua pf/export: cùng số, cùng lời văn,
// cùng bộ lọc. Chỉ cán bộ KHTC phụ trách; được cả khi đã có Bản cuối (Word là hình thức trình ký của cùng nội dung bất
// biến). Bản Tạm tính không được cấp số văn bản.
func (s *Service) ExportWord(ctx context.Context, id, exportID uuid.UUID, idem string) (exportport.Run, error) {
	if idem == "" {
		return exportport.Run{}, problem.IdempotencyRequired
	}
	c, err := s.reader(ctx)
	if err != nil {
		return exportport.Run{}, err
	}
	if s.Exporter == nil {
		return exportport.Run{}, ErrDependency
	}
	var x *store.Export
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		p, term, e := loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = writer(c, p); e != nil {
			return e
		}
		if term == pd.PeriodCancelled {
			return ErrCancelled
		}
		x, e = store.ExportByID(ctx, tx, id, exportID)
		if e == nil && x == nil {
			return ErrNotFound
		}
		return e
	})
	if err != nil {
		return exportport.Run{}, err
	}
	if !archiveBodyMatches(x.Body, x.BodyHash) {
		return exportport.Run{}, ErrDependency.WithDetail("Excel đã lưu không khớp băm")
	}
	key, version, data, err := fixedTemplateWord(x.Content.TemplateSnapshot, x.Content, templateTerminal(x.Content), x.Filter, x.Version)
	if err != nil {
		return exportport.Run{}, err
	}
	req := exportport.Request{Template: key, Version: version, Subject: WordSubject(id),
		FileName: fmt.Sprintf("bao-cao-khtc-%s-%s-v%d.docx", x.Content.Data.Template, id.String(), x.Version), Data: data,
		Date: &exportport.DateLine{Var: VarPlaceDate, Place: admin(x.Content, "place")}, IdemKey: idem}
	if x.Provisional {
		if data.Vars[VarNumber] == "" {
			data.Vars[VarNumber] = "…/" + WordSeries
		}
	} else {
		req.Number = &exportport.Numbering{Series: WordSeries, Var: VarNumber}
	}
	run, err := s.Exporter.Export(ctx, req)
	if err != nil {
		return exportport.Run{}, err
	}
	if err = s.bindWord(ctx, c, id, x, req, run); err != nil {
		return exportport.Run{}, err
	}
	return run, nil
}
