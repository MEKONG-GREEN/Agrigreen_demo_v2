package app

// BF-CN-VACC (#168, quy tắc liên biểu số 7): mũi tiêm ghi ở mục NC-11 của lần ghi đã gửi (hoặc điều chỉnh) điền ngày tiêm
// thực tế vào lịch tiêm BM-07 của lứa có vaccination_batch_ref — trong cùng giao dịch ghi nhật ký (extension/cntscal ghi một
// phiên bản lịch mới có lưu vết). Tên vaccine của danh mục đọc NGOÀI giao dịch.

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	caldomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// vaccineNames: mã → tên vaccine đang dùng hôm nay (khớp tên người ghi gõ ở NC-11). Lỗi danh mục → rỗng (chỉ khớp mã).
func (s *Service) vaccineNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.Vaccinations == nil || s.Catalogs == nil {
		return out
	}
	items, err := s.Catalogs.ListAt(ctx, knport.CatalogCNTSVaccine, s.today())
	if err != nil {
		return out
	}
	for _, it := range items {
		out[it.Code] = it.Name
	}
	return out
}

// syncVaccinations ghi ngày tiêm NC-11 của lần ghi e sang lịch tiêm của lứa cy (không có lịch / không có NC-11 → bỏ qua).
// env: tên vaccine, loại mô hình của điểm đã nạp ngoài giao dịch. Lịch chỉ được ghi khi thuộc hộ của lần ghi hoặc là lịch địa
// bàn cùng xã, loại mô hình và trong phạm vi xã của người ghi (Codex #168 [P1], cntscal kiểm DiaryRecord.Covers trong giao dịch).
func (s *Service) syncVaccinations(ctx context.Context, tx pgx.Tx, c caller, cy store.Cycle, e store.Entry, env evalEnv) error {
	if s.Vaccinations == nil || cy.VaccinationBatch == nil || len(e.Tables.Vaccinations) == 0 {
		return nil
	}
	var ds []caldomain.DiaryDose
	for _, v := range e.Tables.Vaccinations {
		d, err := time.Parse(time.DateOnly, strings.TrimSpace(v.Date))
		if err != nil {
			continue
		}
		ds = append(ds, caldomain.DiaryDose{Vaccine: v.Vaccine, Shot: v.Shot, Date: d})
	}
	return s.Vaccinations.RecordFromDiary(ctx, tx, c.p.TenantID, caldomain.DiaryRecord{Batch: *cy.VaccinationBatch, Household: cy.HouseholdRef,
		Entry: e.Ref, EntrySeq: e.Seq, Actor: e.ActorRef, OnBehalfOf: e.OnBehalfOf, SupportReason: e.SupportReason, At: e.EventTime,
		Today: s.today(), Doses: ds, Names: env.vnames, Commune: e.CommuneCode, ModelType: env.modelType, ScopeAll: c.admin() || c.k.Scope.All,
		Scope: c.k.Scope.Units})
}
