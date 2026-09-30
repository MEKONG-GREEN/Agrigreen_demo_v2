package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// ADR-0006: standby_replication_lag_seconds — trên primary hàm báo "không phải standby"
// (không ghi điểm đo), không lỗi, với vai module thường (không cần quyền giám sát).
func TestStandbyLagOnPrimary_ADR0006(t *testing.T) {
	pool := pgtest.Get(t).Pool(t, migrations.PF, "sync_app")
	lag, standby, err := db.StandbyLag(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if standby || lag != 0 {
		t.Fatalf("primary: standby=%v lag=%v", standby, lag)
	}
	if _, err := db.RegisterStandbyLag(pool, "pf"); err != nil {
		t.Fatal(err)
	}
}
