package app

// BF-CN-VACC (#168, quy tắc liên biểu số 7): lần ghi nhật ký NC-11 (extension/cntsdiary) của lứa có vaccination_batch_ref
// điền ngày tiêm thực tế của mũi tương ứng trong lịch tiêm — trong CÙNG giao dịch ghi của nhật ký (chỉ ghi DB, không I/O).
// Mỗi lần đồng bộ là một phiên bản lịch mới (sự việc dose_recorded, người thao tác = người ghi nhật ký, lý do ghi mã lần
// ghi) nên có lưu vết. Không ghi đè ngày thực tế đã có.

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// RecordFromDiary ghi ngày tiêm từ nhật ký sang lịch tiêm của lứa r.Batch trong giao dịch tx của nhật ký. Lứa không có,
// đã hủy, thuộc hộ khác, hoặc không mũi nào khớp → bỏ qua (nil). Gọi lặp cùng (lần ghi, phiên bản) → không ghi thêm.
func (s *Service) RecordFromDiary(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r domain.DiaryRecord) error {
	if r.Batch == uuid.Nil || len(r.Doses) == 0 {
		return nil
	}
	idem := idemBatch(fmt.Sprintf("diary:%s:%d", r.Entry, r.EntrySeq))
	if prev, err := store.BatchByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	if err := store.Lock(ctx, tx, "vbatch:"+r.Batch.String()); err != nil {
		return err
	}
	cur, err := store.LatestBatch(ctx, tx, r.Batch)
	if err != nil || cur == nil || cur.Event == domain.BatchCancelled || (cur.HouseholdRef != nil && *cur.HouseholdRef != r.Household) {
		return err
	}
	ups := domain.MatchDiaryDoses(cur.Doses, r.Doses, r.Names, cur.Received, r.Today)
	if len(ups) == 0 {
		return nil
	}
	doses, changed, es := domain.ApplyDoseUpdates(cur.Doses, ups, false, cur.Received, r.Today)
	if len(es) > 0 || !changed {
		return nil
	}
	next := *cur
	next.Meta = store.Meta{ID: ids.New(), EventTime: r.At, ActorRef: r.Actor, OnBehalfOf: r.OnBehalfOf, SupportReason: r.SupportReason, IdemKey: idem}
	next.Event, next.Doses, next.CapturedAt = domain.BatchDoseRecorded, doses, nil
	next.StatusReason = "Ghi từ nhật ký NC-11 (lần ghi " + r.Entry.String() + ")"
	return store.InsertBatch(ctx, tx, tid, &next)
}
