package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/clusterenv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
)

// anchorNow: neo mọi cụm một lần cho ngày AG_ANCHOR_DATE (YYYY-MM-DD, mặc định
// ngày vừa kết thúc như job worker) — neo bù ngày bị lỡ, hoặc neo trước khi sao
// lưu trong diễn tập phục hồi. Cùng hàm với job anchor (opsjobs.AnchorDaily):
// idempotent, manifest đã có thì bỏ qua. Vai ag_outbox_publisher, nối thẳng.
// Cần AG_DB_URL, AG_ANCHOR_SIGNING_KEY, AG_S3_* (hoặc AG_OBJSTORE_DIR).
// Xem docs/runbooks/anchor.md.
func anchorNow(ctx context.Context) error {
	base, err := need("AG_DB_URL")
	if err != nil {
		return err
	}
	day := opsjobs.AnchorDateFor(time.Now())
	if v := os.Getenv("AG_ANCHOR_DATE"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return fmt.Errorf("AG_ANCHOR_DATE phải dạng YYYY-MM-DD")
		}
		day = clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
	}
	signer, err := opsjobs.SignerFromEnv()
	if err != nil {
		return err
	}
	store, err := objstore.FromEnv(ctx)
	if err != nil {
		return err
	}
	clusters, err := clusterenv.Selected()
	if err != nil {
		return err
	}
	for _, c := range clusters {
		dsn, err := dbadmin.RoleURL(clusterenv.BaseURL(c, base), c, "", dbadmin.OutboxPublisher)
		if err != nil {
			return err
		}
		pool, err := db.Open(ctx, dsn, db.Options{AppName: "migrate-anchor", MaxConns: 2})
		if err != nil {
			return err
		}
		r, err := opsjobs.AnchorDaily(ctx, pool, store, signer, string(c), day)
		pool.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", c, err)
		}
		fmt.Printf("%s: neo ngày %s — %d dòng neo mới, %d đầu chuỗi trong %s (bỏ qua: %v)\n", c, day, r.Inserted, r.Entries, r.Key, r.Skipped)
	}
	return nil
}
