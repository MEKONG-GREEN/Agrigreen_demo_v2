package app

import (
	"context"
	"fmt"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
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
	data := WordData(x.Content, x.Filter, x.Version)
	req := exportport.Request{Template: WordTemplateKey(x.Content.Data.Template), Version: WordTemplateVersion, Subject: WordSubject(id),
		FileName: fmt.Sprintf("bao-cao-khtc-%s-%s-v%d.docx", x.Content.Data.Template, id.String(), x.Version), Data: data,
		Date: &exportport.DateLine{Var: VarPlaceDate, Place: admin(x.Content, "place")}, IdemKey: idem}
	if x.Provisional {
		if data.Vars[VarNumber] == "" {
			data.Vars[VarNumber] = "…/" + WordSeries
		}
	} else {
		req.Number = &exportport.Numbering{Series: WordSeries, Var: VarNumber}
	}
	return s.Exporter.Export(ctx, req)
}

// WordHistory trả các lần xuất Word của báo cáo (mới nhất trước).
func (s *Service) WordHistory(ctx context.Context, id uuid.UUID) ([]exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.Exporter == nil {
		return nil, ErrDependency
	}
	if err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { _, _, e := loadPeriod(ctx, tx, id); return e }); err != nil {
		return nil, err
	}
	return s.Exporter.Runs(ctx, WordSubject(id))
}

// WordDownload trả byte Word đã lưu (pf/export kiểm băm), ghi điểm kiểm soát xuất như Excel.
func (s *Service) WordDownload(ctx context.Context, id, runID uuid.UUID) (exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return exportport.Run{}, err
	}
	if s.Exporter == nil {
		return exportport.Run{}, ErrDependency
	}
	if err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { _, _, e := loadPeriod(ctx, tx, id); return e }); err != nil {
		return exportport.Run{}, err
	}
	run, err := s.Exporter.Run(ctx, WordSubject(id), runID)
	if err != nil {
		return exportport.Run{}, err
	}
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		Internal: true, SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.principal.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.principal.ActorRef, Product: "kn_khtc_report",
		Records: 1, Bytes: run.Size, OccurredAt: s.now(), ActorRef: c.principal.ActorRef, TraceID: telemetry.TraceID(ctx),
	}, nil)
	return run, nil
}
