package migrations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// D2 (pf 000155, ADR-0052): RLS trước và sau khi HTX thật nhận quyền neo. Mô hình đích: dữ liệu ở tenant LƯU KÝ
// (đồng sở hữu MKG + HTX, tenant s của seedScope — HTX C1, C2); Khuyến nông là bên thứ ba ở tenant riêng v, đọc qua
// cấp phép (share_scope, purpose khuyen-nong — basis_ref = thỏa thuận HTX ở share_grant_terms); MKG vận hành tenant
// lưu ký không gắn Nút (cả tenant). Nhận quyền chỉ đổi neo (bảng chiếu + bí danh + khóa nhận quyền), không đổi dòng
// dữ liệu nào → mọi đường đọc giữ nguyên, chỉ trạng thái xác nhận lật sang "Đã xác nhận".
func TestHTXAnchorRLSAfterClaim_D2(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s := seedScope(ctx, t, tx, true) // tenant lưu ký (MKG + HTX)
	v := seedScope(ctx, t, tx, true) // tenant Khuyến nông (bên thứ ba)
	grantScope(ctx, t, tx, s.tid, v.tid, s.c1, ids.New(), "pf_coop.plot", sharePurpose, "-1 day", "+1 year")
	grantScope(ctx, t, tx, s.tid, v.tid, s.c1, ids.New(), "pf_coop.household", sharePurpose, "-1 day", "+1 year")

	// Neo HTX tạm (KN lập) cho C1.
	anchor, ev := ids.New(), ids.New()
	scopeExec(ctx, t, tx, `INSERT INTO pf_coop.htx_anchor (tenant_id, anchor_id, anchor_key, mst, status, created_by_kind, created_by,
	    provisional_org_ref, org_ref, agreement_ref, last_event_id, last_seq)
	  VALUES ($1, $2, '0355555555', '0355555555', 'provisional', 'kn', gen_random_uuid(), $3, $3, 'TT-HTX/2026/01', $4, 1)`, s.tid, anchor, s.c1, ev)
	scopeExec(ctx, t, tx, `INSERT INTO pf_coop.htx_anchor_alias (tenant_id, org_ref, anchor_id, role, event_id) VALUES ($1, $2, $3, 'provisional', $4)`,
		s.tid, s.c1, anchor, ev)

	plotsOf := `SELECT ref FROM pf_coop.plot WHERE tenant_id = '` + s.tid.String() + `'`
	hhOf := `SELECT household_ref FROM pf_coop.household WHERE tenant_id = '` + s.tid.String() + `'`
	confirmed := func() bool {
		t.Helper()
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_coop.htx_anchor_alias al JOIN pf_coop.htx_anchor a
		    ON a.tenant_id = al.tenant_id AND a.anchor_id = al.anchor_id
		    WHERE al.org_ref = $1 AND a.status = 'claimed')`, s.c1).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	check := func(phase string) {
		t.Helper()
		// KN (tenant v, mọi Nút của KN) đọc C1 qua cấp phép — không thấy C2, không thấy thửa nháp.
		for _, node := range []uuid.UUID{v.p, v.x1, uuid.Nil} {
			asViewer(ctx, t, tx, v.tid, node, sharePurpose)
			if got := visible(ctx, t, tx, plotsOf); !sameSet(got, s.plot1) {
				t.Errorf("%s: KN (Nút %v) thấy thửa %v, muốn chỉ thửa C1 qua cấp phép", phase, node, got)
			}
			if got := visible(ctx, t, tx, hhOf); !sameSet(got, s.hh1) {
				t.Errorf("%s: KN thấy hộ %v", phase, got)
			}
			asAdmin(ctx, t, tx)
		}
		// KN không có mục đích (không đi qua cấp phép) → không thấy gì.
		asViewer(ctx, t, tx, v.tid, v.p, "")
		if got := visible(ctx, t, tx, plotsOf); len(got) != 0 {
			t.Errorf("%s: KN không mục đích thấy %v", phase, got)
		}
		asAdmin(ctx, t, tx)
		// HTX khác (C2) cùng tenant lưu ký không thấy dữ liệu C1 (thửa nháp chưa gắn Nút: nhánh (e) của W3.4).
		asCoop(ctx, t, tx, s.tid, s.c2, "")
		if got := visible(ctx, t, tx, plotsOf); !sameSet(got, s.plot2, s.draft1, s.draft2) {
			t.Errorf("%s: HTX C2 thấy %v", phase, got)
		}
		// HTX C1 thấy dữ liệu của mình.
		asCoop(ctx, t, tx, s.tid, s.c1, "")
		if got := visible(ctx, t, tx, plotsOf); !sameSet(got, s.plot1, s.draft1, s.draft2) {
			t.Errorf("%s: HTX C1 thấy %v", phase, got)
		}
		// MKG (đồng sở hữu, vận hành tenant lưu ký, không gắn Nút) giữ toàn quyền đọc.
		asCoop(ctx, t, tx, s.tid, uuid.Nil, "")
		if got := visible(ctx, t, tx, plotsOf); !sameSet(got, s.plot1, s.plot2, s.draft1, s.draft2) {
			t.Errorf("%s: MKG thấy %v", phase, got)
		}
		// Neo chỉ thấy trong tenant lưu ký (p_tenant), KN không đọc được sổ neo của bên khác.
		if got := visible(ctx, t, tx, `SELECT anchor_id FROM pf_coop.htx_anchor`); !sameSet(got, anchor) {
			t.Errorf("%s: tenant lưu ký thấy neo %v", phase, got)
		}
		asViewer(ctx, t, tx, v.tid, uuid.Nil, sharePurpose)
		if got := visible(ctx, t, tx, `SELECT anchor_id FROM pf_coop.htx_anchor`); len(got) != 0 {
			t.Errorf("%s: KN thấy neo của tenant lưu ký %v", phase, got)
		}
		asAdmin(ctx, t, tx)
	}

	check("trước nhận quyền")
	if confirmed() {
		t.Fatal("HTX tạm phải Chưa xác nhận")
	}
	// Nhận quyền (như app: khóa nhận quyền + bảng chiếu — không chạm dòng dữ liệu).
	asCoop(ctx, t, tx, s.tid, uuid.Nil, "")
	claimEv := ids.New()
	scopeExec(ctx, t, tx, `INSERT INTO pf_coop.htx_anchor_claim (tenant_id, anchor_id, event_id, org_ref, claimed_at) VALUES ($1, $2, $3, $4, now())`,
		s.tid, anchor, claimEv, s.c1)
	scopeExec(ctx, t, tx, `UPDATE pf_coop.htx_anchor SET status = 'claimed', htx_account_ref = gen_random_uuid(), claimed_at = now(),
	    claimed_by = gen_random_uuid(), last_event_id = $2, last_seq = 2 WHERE anchor_id = $1`, anchor, claimEv)
	// Một lần nhận quyền mỗi neo (khóa chính) — lần hai bị chặn.
	if _, err := tryExec(ctx, t, tx, `INSERT INTO pf_coop.htx_anchor_claim (tenant_id, anchor_id, event_id, org_ref, claimed_at)
	    VALUES ($1, $2, gen_random_uuid(), $3, now())`, s.tid, anchor, s.c2); err == nil {
		t.Fatal("nhận quyền lần hai phải bị khóa chính chặn")
	}
	// Bí danh chỉ ghi thêm: r_cooperative không xóa được.
	if n, err := tryExec(ctx, t, tx, `DELETE FROM pf_coop.htx_anchor_alias WHERE anchor_id = $1`, anchor); err == nil && n > 0 {
		t.Fatal("bí danh phải chỉ ghi thêm")
	}
	asAdmin(ctx, t, tx)
	check("sau nhận quyền")
	if !confirmed() {
		t.Fatal("sau nhận quyền phải Đã xác nhận")
	}
	var rows int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.plot WHERE tenant_id = $1`, s.tid).Scan(&rows); err != nil || rows != 4 {
		t.Fatalf("không sao chép dữ liệu: %d %v", rows, err)
	}
}
