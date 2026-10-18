package app

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// ProductScheduleExport là tên sản phẩm của lần xuất lịch công tác trong sổ egress_event (pf/metering).
const ProductScheduleExport = "kn_work_schedule"

// scheduleEgress đưa lần xuất lịch công tác qua điểm kiểm soát dùng chung (ADR-0024, quyết định A5): kênh
// report_export, bên nhận = chính cán bộ xuất, Internal → luôn observe (ghi, đếm, cảnh báo bất thường; không bao giờ
// từ chối kể cả AG_EGRESS_MODE=enforce). Chưa nối checkpoint → hành vi cũ.
func (s *Service) scheduleEgress(ctx context.Context, records, bytes int) {
	p, ok := authz.FromContext(ctx)
	if !ok || s.Egress == nil {
		return
	}
	// Guard với Internal không bao giờ trả lỗi (observe): lỗi hạ tầng chỉ được ghi log.
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: p.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: p.ActorRef, Product: ProductScheduleExport,
		Records: int64(records), Bytes: int64(bytes), OccurredAt: s.now(), ActorRef: p.ActorRef, TraceID: telemetry.TraceID(ctx),
		Internal: true,
	}, s.Log)
}

// ScheduleExport là tệp xuất lịch công tác.
type ScheduleExport struct {
	Body        []byte
	ContentType string
	FileName    string
}

var scheduleExportHeader = []string{"STT", "Cán bộ", "Cấp", "Xã phụ trách", "Trạng thái", "Ngày", "Buổi", "Giờ", "Nội dung",
	"Địa điểm", "Thành phần phối hợp", "Ghi chú"}

// ExportSchedules xuất lịch tuần trong phạm vi ra Excel hoặc PDF (FN-TI-02.07 luồng phụ "Xuất Excel/PDF").
// Mỗi dòng là một mục công tác của phiên bản hiện hành; lịch chưa có mục vẫn có một dòng.
func (s *Service) ExportSchedules(ctx context.Context, q ScheduleQuery, format string) (ScheduleExport, error) {
	if format == "" {
		format = "xlsx"
	}
	if format != "xlsx" && format != "pdf" {
		return ScheduleExport{}, problem.Validation.WithField("format", "xlsx hoặc pdf")
	}
	if format == "pdf" && s.TablePDF == nil {
		return ScheduleExport{}, ErrDependency.WithDetail("chưa nối bộ dựng PDF (pf/reports)")
	}
	var all []ScheduleDetail
	names := map[uuid.UUID]string{}
	q.Limit = 200
	var week time.Time
	for {
		pg, err := s.ListSchedules(ctx, q)
		if err != nil {
			return ScheduleExport{}, err
		}
		week = pg.Week
		all = append(all, pg.Items...)
		for k, v := range pg.Names {
			names[k] = v
		}
		if pg.Next == nil || len(all) >= 5000 {
			break
		}
		q.After = *pg.Next
	}
	rows := [][]string{}
	n := 0
	for _, d := range all {
		sc := d.Schedule
		owner := names[sc.OwnerRef]
		if owner == "" {
			owner = sc.OwnerRef.String()
		}
		level := "Cấp Xã"
		if sc.OwnerLevel == accountsport.ScheduleLevelProvince {
			level = "Cấp Tỉnh"
		}
		status := domain.StatusVI(sc.Status)
		if sc.Amendment && sc.Status == domain.SchedulePending {
			status += " (điều chỉnh)"
		}
		if sc.Late {
			status += " · Đăng ký muộn"
		}
		base := []string{"", owner, level, strings.Join(sc.Communes, ", "), status}
		if len(d.Items) == 0 {
			n++
			base[0] = strconv.Itoa(n)
			rows = append(rows, append(base, "", "", "", "", "", "", ""))
			continue
		}
		for _, it := range d.Items {
			n++
			r := append([]string(nil), base...)
			r[0] = strconv.Itoa(n)
			session := "Sáng"
			if it.Session == domain.SessionAfternoon {
				session = "Chiều"
			}
			hours := it.Start
			if it.End != "" {
				hours += "–" + it.End
			}
			loc := strings.TrimSpace(strings.Join(nonEmpty(it.CommuneCode, it.Location), " · "))
			rows = append(rows, append(r, it.Day.Format("02/01/2006"), session, hours, it.Content, loc, it.Participants, it.Note))
		}
	}
	end := week.AddDate(0, 0, 6)
	title := "LỊCH CÔNG TÁC TUẦN " + week.Format("02/01/2006") + " – " + end.Format("02/01/2006")
	fname := "lich-cong-tac-" + week.Format(time.DateOnly)
	if format == "pdf" {
		meta := [][2]string{{"Tuần", week.Format("02/01/2006") + " – " + end.Format("02/01/2006")}}
		if q.Status != "" {
			meta = append(meta, [2]string{"Trạng thái", domain.StatusVI(q.Status)})
		}
		if q.Commune != "" {
			meta = append(meta, [2]string{"Xã", q.Commune})
		}
		b, err := s.TablePDF.RenderTablePDF(title, meta, scheduleExportHeader, rows, s.now().In(clock.Zone()))
		if err != nil {
			return ScheduleExport{}, err
		}
		s.scheduleEgress(ctx, len(rows), len(b))
		return ScheduleExport{Body: b, ContentType: "application/pdf", FileName: fname + ".pdf"}, nil
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Lịch công tác", scheduleExportHeader, rows); err != nil {
		return ScheduleExport{}, err
	}
	s.scheduleEgress(ctx, len(rows), buf.Len())
	return ScheduleExport{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: fname + ".xlsx"}, nil
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
