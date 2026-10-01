package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	svc      *app.Service
	clk      *clock.Fixed
	op       uuid.UUID // tenant đơn vị vận hành
	admin    context.Context
	admin2   context.Context
	approver context.Context
	logs     *bytes.Buffer
}

func pep(b byte) []byte { return bytes.Repeat([]byte{b}, port.MinPepperBytes) }

func principal(tid uuid.UUID, roles ...string) context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: tid, Roles: roles, MFA: true})
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := pgtest.Get(t).Pool(t, migrations.PF, port.DBRole)
	peppers, err := domain.NewPeppers(1, map[int][]byte{1: pep('k')})
	if err != nil {
		t.Fatal(err)
	}
	op := ids.New()
	clk := clock.NewFixed(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC))
	logs := &bytes.Buffer{}
	svc := &app.Service{DB: pool, Clock: clk, Peppers: peppers, OperatorTenant: op, Log: slog.New(slog.NewJSONHandler(logs, nil))}
	return &fixture{svc: svc, clk: clk, op: op, logs: logs,
		admin:    principal(op, port.RoleAPIClientAdmin),
		admin2:   principal(op, port.RoleAPIClientAdmin, port.RoleAPIClientApprover),
		approver: principal(op, port.RoleAPIClientApprover)}
}

func wantCode(t *testing.T, err error, code, what string) {
	t.Helper()
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("%s: muốn %s, nhận %v", what, code, err)
	}
}

// activeClient tạo khách hàng đã duyệt + đăng ký collection_volume (read, export) + usage:read đã duyệt.
func (f *fixture) activeClient(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()
	thirdParty := ids.New()
	c, err := f.svc.ProposeClient(f.admin, app.ProposeClientInput{ClientTenantID: thirdParty, DisplayName: "Nhà máy sinh khối VFT",
		Purpose: "Lập kế hoạch thu mua rơm theo xã và vụ", ContactRef: ids.New(), IdemKey: ids.New().String()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ChangeClient(f.approver, c.ClientID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	s, err := f.svc.ProposeSubscription(f.admin, c.ClientID, port.ProductCollectionVolume,
		[]string{"dp:collection_volume:read", "dp:collection_volume:export", port.ScopeUsageRead}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ChangeSubscription(f.approver, c.ClientID, s.SubscriptionID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	return c.ClientID, thirdParty
}

func (f *fixture) auth(key string) (port.Caller, error) {
	return app.Authenticator{Svc: f.svc}.Authenticate(context.Background(), key, f.clk.Now())
}

// ADR-0022 mục 4 (P-02, P-03): DB chỉ có prefix + HMAC; bí mật hiện MỘT lần; log chỉ có prefix.
func TestKeyHashNeverStoresPlaintext_ADR0022(t *testing.T) {
	f := newFixture(t)
	cid, _ := f.activeClient(t)
	idem := ids.New().String()
	k, err := f.svc.IssueKey(f.admin, cid, idem)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := port.ParseKey(k.Secret); !ok {
		t.Fatalf("bí mật phải đúng dạng agk_…")
	}
	secretPart := k.Secret[len("agk_")+port.KeyPrefixLength+1:]
	owner := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.Migrator)
	for _, tbl := range []string{"client_event", "client_current", "api_key_event", "api_key_current", "subscription_event", "subscription_current"} {
		var dump string
		err := tenant.WithTenantTx(context.Background(), owner, f.op, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), fmt.Sprintf(`SELECT coalesce(string_agg(t::text, '|'), '') FROM pf_apiclient.%s t`, tbl)).Scan(&dump)
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(dump, secretPart) || strings.Contains(dump, k.Secret) {
			t.Fatalf("bảng %s chứa bí mật khóa", tbl)
		}
	}
	if strings.Contains(f.logs.String(), secretPart) || !strings.Contains(f.logs.String(), k.Prefix) {
		t.Fatal("log chỉ được có key_prefix, không có bí mật")
	}
	// Gọi lặp cùng Idempotency-Key: khóa đã phát hành, KHÔNG kèm bí mật.
	again, err := f.svc.IssueKey(f.admin, cid, idem)
	if err != nil || again.KeyID != k.KeyID || again.Secret != "" {
		t.Fatalf("gọi lặp phải trả cùng khóa, không bí mật: %+v %v", again.KeyID, err)
	}
	// Khóa xác thực được; khóa sai bí mật cùng prefix → 401.
	c, err := f.auth(k.Secret)
	if err != nil || c.Client.ID != cid || c.KeyPrefix != k.Prefix || !c.Client.HasScope("dp:collection_volume:read") {
		t.Fatalf("xác thực khóa đúng thất bại: %v", err)
	}
	wrong := k.Secret[:len(k.Secret)-2] + "AA"
	if wrong == k.Secret {
		wrong = k.Secret[:len(k.Secret)-2] + "BB"
	}
	_, err = f.auth(wrong)
	wantCode(t, err, port.ErrUnauthenticated.Code, "sai bí mật")
	_, err = f.auth("agk_short")
	wantCode(t, err, port.ErrUnauthenticated.Code, "sai dạng")
}

// ADR-0022 mục 4 (P-04): xoay có chồng lấn 7 ngày, tối đa 2 khóa hiệu lực, thu hồi hiệu lực ngay.
func TestKeyRotationOverlapRevoke_ADR0022(t *testing.T) {
	f := newFixture(t)
	cid, _ := f.activeClient(t)
	k1, err := f.svc.IssueKey(f.admin, cid, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	k2, err := f.svc.RotateKey(f.admin, cid, k1.KeyID, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if k2.GraceUntil == nil || !k2.GraceUntil.Equal(f.clk.Now().Add(port.DefaultRotateGrace)) {
		t.Fatalf("hạn chồng lấn phải 7 ngày: %v", k2.GraceUntil)
	}
	for _, k := range []string{k1.Secret, k2.Secret} {
		if _, err := f.auth(k); err != nil {
			t.Fatalf("trong hạn chồng lấn cả hai khóa phải dùng được: %v", err)
		}
	}
	// Đã có 2 khóa hiệu lực → phát hành thêm / xoay tiếp bị chặn.
	_, err = f.svc.IssueKey(f.admin, cid, ids.New().String())
	wantCode(t, err, port.ErrTooManyKeys.Code, "khóa thứ ba")
	_, err = f.svc.RotateKey(f.admin, cid, k2.KeyID, ids.New().String())
	wantCode(t, err, port.ErrTooManyKeys.Code, "xoay khi còn khóa chồng lấn")

	f.clk.Advance(port.DefaultRotateGrace + time.Second)
	_, err = f.auth(k1.Secret)
	wantCode(t, err, port.ErrUnauthenticated.Code, "khóa cũ sau hạn chồng lấn")
	if _, err := f.auth(k2.Secret); err != nil {
		t.Fatalf("khóa mới phải dùng được: %v", err)
	}
	// Sau hạn chồng lấn phát hành được khóa thứ hai; khóa cũ được ghi expired (sổ).
	k3, err := f.svc.IssueKey(f.admin, cid, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := f.svc.ListKeys(f.admin, cid)
	if err != nil {
		t.Fatal(err)
	}
	status := map[uuid.UUID]string{}
	for _, k := range keys {
		status[k.KeyID] = k.Status
	}
	if status[k1.KeyID] != domain.KeyStatusExpired || status[k2.KeyID] != domain.KeyStatusActive || status[k3.KeyID] != domain.KeyStatusActive {
		t.Fatalf("trạng thái khóa sai: %v", status)
	}
	// Thu hồi: bắt buộc lý do, hiệu lực ngay ở lời gọi kế tiếp.
	_, err = f.svc.RevokeKey(f.admin, cid, k2.KeyID, "ngắn")
	wantCode(t, err, problem.Validation.Code, "thu hồi thiếu lý do")
	if _, err := f.svc.RevokeKey(f.admin, cid, k2.KeyID, "Đối tác báo lộ khóa qua email"); err != nil {
		t.Fatal(err)
	}
	_, err = f.auth(k2.Secret)
	wantCode(t, err, port.ErrUnauthenticated.Code, "khóa đã thu hồi")
	// Khách hàng bị tạm ngưng → mọi khóa 401.
	if _, err := f.svc.ChangeClient(f.admin, cid, "suspend", "Tạm ngưng chờ ký lại DPA"); err != nil {
		t.Fatal(err)
	}
	_, err = f.auth(k3.Secret)
	wantCode(t, err, port.ErrUnauthenticated.Code, "khách hàng suspended")

	// Sổ khóa: issued, rotated, issued, expired, issued, revoked — chuỗi băm nguyên vẹn.
	pool := pgtest.Get(t).Pool(t, migrations.PF, port.DBRole)
	err = tenant.WithTenantTx(context.Background(), pool, f.op, func(tx pgx.Tx) error {
		var actions []string
		if err := tx.QueryRow(context.Background(), `SELECT array_agg(action ORDER BY seq) FROM pf_apiclient.api_key_event WHERE client_id = $1::uuid`, cid).
			Scan(&actions); err != nil {
			return err
		}
		if strings.Join(actions, ",") != "issued,rotated,issued,expired,issued,revoked" {
			return fmt.Errorf("sổ khóa: %v", actions)
		}
		for _, tbl := range []string{"pf_apiclient.api_key_event", "pf_apiclient.client_event", "pf_apiclient.subscription_event"} {
			r, err := ledger.Verify(context.Background(), tx, tbl, f.op, domain.ClientStream(cid.String()))
			if err != nil {
				return err
			}
			if !r.OK || r.RowsChecked == 0 {
				return fmt.Errorf("verify_chain %s: %+v", tbl, r)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-56 (P-05): người duyệt ≠ người đề xuất — ở app/ và CHECK DB.
func TestSelfApprovalBlocked_R56(t *testing.T) {
	f := newFixture(t)
	c, err := f.svc.ProposeClient(f.admin2, app.ProposeClientInput{ClientTenantID: ids.New(), DisplayName: "Quỹ mua tín chỉ",
		Purpose: "Theo dõi gói MRV đã xuất", ContactRef: ids.New(), IdemKey: ids.New().String()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.ChangeClient(f.admin2, c.ClientID, "approve", "")
	wantCode(t, err, port.ErrSelfApproval.Code, "tự duyệt khách hàng")
	_, err = f.svc.ChangeClient(f.admin, c.ClientID, "approve", "")
	wantCode(t, err, problem.Forbidden.Code, "admin không có vai approver")
	if _, err := f.svc.ChangeClient(f.approver, c.ClientID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	s, err := f.svc.ProposeSubscription(f.admin2, c.ClientID, port.ProductMRVPackageStatus, []string{"dp:mrv_package_status:read"}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.ChangeSubscription(f.admin2, c.ClientID, s.SubscriptionID, "approve", "")
	wantCode(t, err, port.ErrSelfApproval.Code, "tự duyệt đăng ký")
	_, err = f.svc.ProposeSubscription(f.admin2, c.ClientID, port.ProductMRVPackageStatus, []string{"dp:collection_volume:read"}, ids.New().String())
	wantCode(t, err, problem.Validation.Code, "scope ngoài hợp đồng")
	plan, err := f.svc.ProposePlan(f.admin2, c.ClientID, port.Plan{RequestsPerMinute: 10, DailyUnits: 100, MonthlyUnits: 1000, MaxExportRows: 500}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.ApprovePlan(f.admin2, c.ClientID, plan.Version)
	wantCode(t, err, port.ErrSelfApproval.Code, "tự duyệt gói")

	// Gói: chưa duyệt → mặc định; duyệt → phiên bản 1.
	pl, err := f.svc.PlanFor(context.Background(), f.op, c.ClientID, f.clk.Now())
	if err != nil || pl.Version != 0 || pl.RequestsPerMinute != port.DefaultRequestsPerMinute {
		t.Fatalf("chưa duyệt → gói mặc định: %+v %v", pl, err)
	}
	if _, err := f.svc.ApprovePlan(f.approver, c.ClientID, plan.Version); err != nil {
		t.Fatal(err)
	}
	pl, err = f.svc.PlanFor(context.Background(), f.op, c.ClientID, f.clk.Now())
	if err != nil || pl.Version != 1 || pl.RequestsPerMinute != 10 || pl.MaxExportRows != 500 {
		t.Fatalf("gói v1: %+v %v", pl, err)
	}

	// CHECK DB: dòng sổ approved có actor_ref = proposed_by bị từ chối kể cả khi vòng qua app/.
	pool := pgtest.Get(t).Pool(t, migrations.PF, port.DBRole)
	actor := ids.New()
	err = tenant.WithTenantTx(context.Background(), pool, f.op, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_apiclient.subscription_event
		    (id, tenant_id, stream_key, subscription_id, client_id, action, product, scopes, proposed_by, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1::uuid, $2::uuid, 'client:x', $3::uuid, $4::uuid, 'approved', 'mrv_package_status', ARRAY['dp:mrv_package_status:read'],
		    $5::uuid, now(), $5::uuid, $6::text, 'apiclient.v1')`, ids.New(), f.op, ids.New(), c.ClientID, actor, ids.New().String())
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "23514") && !strings.Contains(err.Error(), "check") {
		t.Fatalf("CHECK R-56 phải chặn: %v", err)
	}
}

// RB-2 (X-02): khách hàng, khóa của đơn vị vận hành A không thấy / không dùng được từ tenant B.
func TestTenantIsolation_G5(t *testing.T) {
	a := newFixture(t)
	cid, _ := a.activeClient(t)
	k, err := a.svc.IssueKey(a.admin, cid, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	b := newFixture(t)
	_, err = b.svc.GetClient(b.admin, cid)
	wantCode(t, err, app.ErrClientNotFound.Code, "tenant khác đọc khách hàng")
	_, err = b.svc.IssueKey(b.admin, cid, ids.New().String())
	wantCode(t, err, app.ErrClientNotFound.Code, "tenant khác phát hành khóa")
	_, err = b.auth(k.Secret)
	wantCode(t, err, port.ErrUnauthenticated.Code, "khóa của tenant A ở đơn vị vận hành B")
	list, err := b.svc.ListClients(b.admin, "", uuid.Nil, 100)
	if err != nil || len(list) != 0 {
		t.Fatalf("tenant B không thấy khách hàng nào: %d %v", len(list), err)
	}
	// Người gọi không thuộc tenant đơn vị vận hành đã cấu hình → 403.
	_, err = a.svc.ListClients(principal(ids.New(), port.RoleAPIClientAdmin), "", uuid.Nil, 10)
	wantCode(t, err, app.ErrNotOperator.Code, "tenant khác đơn vị vận hành")
}

// RB-1 (X-01): sổ pf_apiclient chỉ ghi thêm — UPDATE/DELETE/TRUNCATE bằng vai ứng dụng và owner đều thất bại.
func TestAppendOnlySixWays_G5Apiclient(t *testing.T) {
	f := newFixture(t)
	cid, _ := f.activeClient(t)
	if _, err := f.svc.ProposePlan(f.admin, cid, port.Plan{RequestsPerMinute: 1, DailyUnits: 1, MonthlyUnits: 1, MaxExportRows: 1}, ids.New().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.IssueKey(f.admin, cid, ids.New().String()); err != nil {
		t.Fatal(err)
	}
	env := pgtest.Get(t)
	pools := map[string]tenant.Beginner{"apiclient_app": env.Pool(t, migrations.PF, port.DBRole), "owner": env.Pool(t, migrations.PF, dbadmin.Migrator)}
	for _, tbl := range []string{"client_event", "api_key_event", "subscription_event", "quota_plan", "export_file"} {
		for who, pool := range pools {
			for _, sql := range []string{"UPDATE pf_apiclient." + tbl + " SET tenant_id = tenant_id", "DELETE FROM pf_apiclient." + tbl, "TRUNCATE pf_apiclient." + tbl} {
				err := tenant.WithTenantTx(context.Background(), pool, f.op, func(tx pgx.Tx) error {
					// export_file rỗng: chèn trước để UPDATE/DELETE chạm trigger.
					if tbl == "export_file" && who == "owner" {
						if _, err := tx.Exec(context.Background(), `INSERT INTO pf_apiclient.export_file (tenant_id, job_id, object_key, sha256, size_bytes, rows_count, content_type)
						  VALUES ($1::uuid, $2::uuid, 'k', repeat('a', 64), 0, 0, 'text/csv')`, f.op, ids.New()); err != nil {
							return err
						}
					}
					_, err := tx.Exec(context.Background(), sql)
					return err
				})
				if tbl == "export_file" && who == "apiclient_app" && !strings.HasPrefix(sql, "TRUNCATE") && err == nil {
					// vai ứng dụng không có quyền UPDATE/DELETE: bảng rỗng ở tenant này vẫn phải lỗi quyền.
					t.Fatalf("%s %s bằng %s phải thất bại", sql, tbl, who)
				}
				if err == nil {
					t.Fatalf("%s bằng %s phải thất bại", sql, who)
				}
			}
		}
	}
	var missing int
	err := tenant.WithTenantTx(context.Background(), env.Pool(t, migrations.PF, dbadmin.Migrator), f.op, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.v_missing_immutability WHERE schema_name = 'pf_apiclient'`).Scan(&missing)
	})
	if err != nil || missing != 0 {
		t.Fatalf("v_missing_immutability pf_apiclient: %d %v", missing, err)
	}
}

// ADR-0022 mục 4 (P-04, RB-1): job port.JobKeyExpiry — khóa quá hạn (365 ngày) và khóa cũ quá hạn
// chồng lấn khi xoay được ghi sự kiện expired ở sổ api_key_event (không UPDATE cột trạng thái ngoài
// bảng chiếu); chạy lại không ghi thêm (idempotent); khóa còn hạn không đổi; tenant vận hành khác
// không bị chạm; khóa expired không xác thực được.
func TestExpireAllDueJob_ADR0022(t *testing.T) {
	f := newFixture(t)
	cidA, _ := f.activeClient(t)
	cidB, _ := f.activeClient(t)
	a1, err := f.svc.IssueKey(f.admin, cidA, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	a2, err := f.svc.RotateKey(f.admin, cidA, a1.KeyID, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(30 * 24 * time.Hour)
	b1, err := f.svc.IssueKey(f.admin, cidB, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Chưa tới hạn chồng lấn? Đã qua 30 ngày > 7 ngày → a1 (rotated) tới hạn; a2, b1 còn hạn.
	n, err := f.svc.ExpireAllDue(ctx, f.op, f.clk.Now())
	if err != nil || n != 1 {
		t.Fatalf("muốn 1 khóa expired (a1 quá hạn chồng lấn), nhận %d %v", n, err)
	}
	if n, err := f.svc.ExpireAllDue(ctx, f.op, f.clk.Now()); err != nil || n != 0 {
		t.Fatalf("chạy lại không ghi thêm: %d %v", n, err)
	}
	// Qua 365 ngày kể từ a2 → a2 expired; b1 (phát hành sau 30 ngày) còn hạn.
	f.clk.Advance(port.DefaultKeyLifetime - 30*24*time.Hour + time.Second)
	if n, err := f.svc.ExpireAllDue(ctx, f.op, f.clk.Now()); err != nil || n != 1 {
		t.Fatalf("muốn a2 expired: %d %v", n, err)
	}
	status := func(cid uuid.UUID) map[uuid.UUID]string {
		keys, err := f.svc.ListKeys(f.admin, cid)
		if err != nil {
			t.Fatal(err)
		}
		m := map[uuid.UUID]string{}
		for _, k := range keys {
			m[k.KeyID] = k.Status
		}
		return m
	}
	if s := status(cidA); s[a1.KeyID] != "expired" || s[a2.KeyID] != "expired" {
		t.Fatalf("khóa của A phải expired: %v", s)
	}
	if s := status(cidB); s[b1.KeyID] != "active" {
		t.Fatalf("khóa còn hạn không đổi: %v", s)
	}
	var events int
	if err := tenant.WithTenantTx(ctx, pgtest.Get(t).Pool(t, migrations.PF, port.DBRole), f.op, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_apiclient.api_key_event WHERE action = 'expired'`).Scan(&events)
	}); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("sổ phải có đúng 2 sự kiện expired, có %d", events)
	}
	if _, err := f.auth(a2.Secret); err == nil {
		t.Fatal("khóa expired không được xác thực")
	}
	// Tenant vận hành khác: không có gì để làm; tenant rỗng → lỗi.
	if n, err := f.svc.ExpireAllDue(ctx, ids.New(), f.clk.Now()); err != nil || n != 0 {
		t.Fatalf("tenant khác: %d %v", n, err)
	}
	if _, err := f.svc.ExpireAllDue(ctx, uuid.Nil, f.clk.Now()); err == nil {
		t.Fatal("thiếu tenant vận hành phải lỗi")
	}
}
