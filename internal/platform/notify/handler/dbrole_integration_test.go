package handler_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0028 (W2.3, module mẫu): pf/notify chạy trên vai đăng nhập app_platform (fixture: API + consumer qua
// db.ForModule như cmd/worker), giao dịch dưới r_notify — consumer ghi được thông báo, API đọc được; tự
// app_platform không đọc được pf_notify; r_notify không đọc được schema pf_content.
func TestModuleTxRunsAsGroupRole_ADR0028(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	env, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	f.process(port.ConsumerDirective, env)
	if got := a.inbox(); len(got) != 1 {
		t.Fatalf("hộp thư qua module trên app_platform: %v", got)
	}
	checkGroupRole(t, notify.DBModule, f.tenant, "pf_notify.notification", "pf_content.document")
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
