package handler_test

import (
	"context"
	"errors"
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
		if err := tenant.WithTenantTx(ctx, f.db, tid, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&n) }); err != nil {
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
