package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ADR-0020 (RB-2, Q-MKG-12): van MKG của tenant MKG riêng được ghi vào tenant BÊN CẤP (tenant
// chương trình) theo cấp quyền valve_collection — sổ nhận giữ source_tenant_id = tenant MKG, sự
// kiện received phát ở tenant bên cấp; tenant MKG không có dòng nào ở pf; tenant phong bì chưa có
// cấp quyền → lỗi party.valve_share_grant_missing (giao lại → DLQ), không ghi gì.
func TestCollectionValveRoutedToGrantor_ADR0020(t *testing.T) {
	f := newFixture(t)
	ref := ids.New()
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ref, f.coopA, f.p1, 40, 1)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	count := func(tid uuid.UUID, q string) int {
		var n int
		if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(f.tenant, "SELECT count(*) FROM pf_cropcycle.collection_valve_receipt WHERE source_tenant_id = '"+f.mkg.String()+"'"); n != 1 {
		t.Fatalf("sổ nhận ở tenant bên cấp, nguồn = tenant MKG: %d", n)
	}
	if n := count(f.mkg, "SELECT count(*) FROM pf_cropcycle.collection_valve_receipt"); n != 0 {
		t.Fatalf("tenant MKG không được có dòng pf: %d", n)
	}
	if n := f.countOutbox(port.EventCollectionStatementReceived); n != 1 {
		t.Fatalf("received phát ở tenant bên cấp: %d", n)
	}
	if r := f.managerA().get("/v1/cropcycle/htx-collection/statements"); len(list(r.body, "items")) != 1 {
		t.Fatalf("quản lý HTX (tenant chương trình) thấy bảng kê: %s", r.raw)
	}
	// Giao lặp cùng revision (event_id mới) → không ghi thêm.
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ref, f.coopA, f.p1, 40, 1)); err != nil {
		t.Fatal(err)
	}
	if n := count(f.tenant, "SELECT count(*) FROM pf_cropcycle.collection_valve_receipt"); n != 1 {
		t.Fatalf("giao lặp phải idempotent: %d", n)
	}
	// Tenant phong bì chưa có cấp quyền → lỗi rõ mã, không ghi gì.
	stranger := ids.New()
	_, err := f.deliver(stranger, port.EventMKGHTXCollectionStatementPublished, statementPayload(ids.New(), f.coopA, f.p1, 40, 1))
	if !errors.Is(err, partyport.ErrValveGrantMissing) || !strings.Contains(err.Error(), "party.valve_share_grant_missing") {
		t.Fatalf("không có cấp quyền → party.valve_share_grant_missing: %v", err)
	}
	if n := count(f.tenant, "SELECT count(*) FROM pf_cropcycle.collection_valve_receipt"); n != 1 {
		t.Fatalf("phong bì không có cấp quyền không được ghi: %d", n)
	}
}

// ADR-0020: bề mặt đối tác season_harvest_dates (proto FM-01/02) — đọc vụ ở tenant bên cấp, trường
// tối thiểu (không nông hộ, không sản lượng), ghi sổ đọc; loại không được cấp → 403; thu hồi → 403;
// sổ đọc không ghi được → 503 và không trả dữ liệu.
func TestPartnerSeasonHarvestDates_ADR0020(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	season := f.managerA().mustOpen(f.p1)
	sa := f.as(f.mkg, uuid.Nil, port.RoleMKGSync)
	path := "/v1/partner/season-harvest-dates?plot_ref=" + f.p1.String()
	before := f.share.ReadCount()
	r := sa.get(path)
	expect(t, r, http.StatusOK, "ngày thu hoạch")
	items := list(r.body, "items")
	if len(items) != 1 || str(items[0], "season_ref") != season || str(items[0], "expected_harvest_date") == "" || str(items[0], "harvest_open_date") == "" {
		t.Fatalf("vụ của thửa: %s", r.raw)
	}
	for _, k := range []string{"household_ref", "opened_by", "mass_kg", "yield_t_per_ha", "expected_yield_t_per_ha"} {
		if _, ok := items[0].(map[string]any)[k]; ok {
			t.Fatalf("bề mặt đối tác không được mang %s: %s", k, r.raw)
		}
	}
	if f.share.ReadCount() != before+1 || f.share.Reads[len(f.share.Reads)-1].Kind != partyport.KindSeasonHarvestDates {
		t.Fatalf("sổ đọc: %+v", f.share.Reads)
	}
	// Vai khác (quản lý HTX) không đọc được bề mặt đối tác.
	expect(t, f.managerA().get(path), http.StatusForbidden, "quản lý HTX")
	// Tài khoản dịch vụ của tenant thứ ba có cấp quyền KHÁC loại → 403.
	third := ids.New()
	f.share.Grant(f.tenant, third, partyport.KindCooperativeDirectory)
	expectCode(t, f.as(third, uuid.Nil, port.RoleMKGSync).get(path), http.StatusForbidden, "party.share_grant_missing", "loại không được cấp")
	// Sổ đọc hỏng → 503, không trả dữ liệu.
	f.share.FailAudit = true
	r = sa.get(path)
	if r.status != http.StatusServiceUnavailable || len(list(r.body, "items")) != 0 {
		t.Fatalf("sổ đọc không ghi được phải 503 không dữ liệu: %d %s", r.status, r.raw)
	}
	f.share.FailAudit = false
}
