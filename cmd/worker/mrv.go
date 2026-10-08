package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// mrvExportConsumer dựng gói bằng chứng MRV cho pf.mrv.export.requested (mục 12).
// Handler chạy trong giao dịch của inbox.Process trên pool r_mrv. Kho đối
// tượng (AG_S3_* hoặc AG_OBJSTORE_DIR) mở ở sự kiện đầu tiên; thiếu cấu hình →
// handler lỗi, sự kiện được giao lại rồi vào DLQ (R-62) — không mất yêu cầu.
func mrvExportConsumer(log *slog.Logger) consumer {
	var (
		once sync.Once
		h    inbox.Handler
		err  error
	)
	return consumer{
		name: mrvport.ExportConsumer, cluster: migrations.PF, module: mrv.DBModule,
		events: []string{mrvport.EventExportRequested},
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			once.Do(func() {
				var store objstore.Store
				if store, err = objstore.FromEnv(ctx); err == nil {
					h = mrv.New(nil, clock.System{}, mrv.Deps{Store: store}).ExportHandler()
				}
			})
			if err != nil {
				log.Error("chưa cấu hình kho đối tượng cho gói MRV", "event", env.EventType, "event_id", env.EventID, "outcome", "error")
				return err
			}
			return h(ctx, tx, env)
		},
	}
}
