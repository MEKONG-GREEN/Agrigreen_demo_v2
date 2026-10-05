package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0028 (W2.3, module mẫu): pf/content chạy trên vai đăng nhập app_platform (fixture), giao dịch của
// module chạy dưới r_content — ghi được tài liệu qua API; tự app_platform không đọc được pf_content; r_content
// không đọc được schema pf_notify.
func TestModuleTxRunsAsGroupRole_ADR0028(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	createDoc(t, a.natEditor, procedureBody("Quy trình thử vai nhóm"))
	if r := a.natEditor.adminGet("/v1/documents"); r.status != http.StatusOK {
		t.Fatalf("liệt kê tài liệu qua module: %d %v", r.status, r.body)
	}
	checkGroupRole(t, content.DBModule, f.tenant, "pf_content.document", "pf_notify.notification")
}

// isDenied: 42501 insufficient_privilege.
func isDenied(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42501"
}

// checkGroupRole: trên pool app_platform, giao dịch KHÔNG qua db.ForModule bị từ chối; qua ForModule chạy dưới
// vai nhóm của module, đọc được bảng own, không đọc được bảng other.
func checkGroupRole(t *testing.T, m db.ModuleID, tid uuid.UUID, own, other string) {
	t.Helper()
	ctx := context.Background()
	app := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		var n int
		return tx.QueryRow(ctx, "SELECT count(*) FROM "+own).Scan(&n)
	})
	if !isDenied(err) {
		t.Fatalf("app_platform ngoài r_*: SELECT %s phải 42501, nhận %v", own, err)
	}
	err = db.WithModuleTenantTx(ctx, app, m, tid, func(tx pgx.Tx) error {
		var cur string
		var n int
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&cur); err != nil {
			return err
		}
		if cur != m.Role() {
			return fmt.Errorf("current_user=%s, muốn %s", cur, m.Role())
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+own).Scan(&n); err != nil || n == 0 {
			return fmt.Errorf("%s đọc %s: n=%d err=%w", m.Role(), own, n, err)
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = sp.Rollback(ctx) }()
		if err := sp.QueryRow(ctx, "SELECT count(*) FROM "+other).Scan(&n); !isDenied(err) {
			return fmt.Errorf("%s đọc %s phải 42501, nhận %w", m.Role(), other, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
