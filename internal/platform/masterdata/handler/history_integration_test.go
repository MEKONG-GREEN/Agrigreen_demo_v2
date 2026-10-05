package handler_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// port.AdminUnitHistory (W2.2, ADR-0032 mục 5) trên danh mục nhúng: AdminUnitAt,
// SuccessorsAt, RollForward; nhập danh mục ghi bảng kế thừa 'pending_c9'; dòng
// 'official' thay dòng 'pending_c9' cùng cặp.
func TestAdminUnitHistory(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	u := f.caller(tid)
	units := f.mod.AdminUnits()
	hist := f.mod.AdminUnitHistory()
	if _, ok := units.(port.AdminUnitHistory); !ok {
		t.Fatal("adapter AdminUnits phải thực hiện AdminUnitHistory (type assertion)")
	}
	ctx := u.ctx()

	// Seed đi qua đường nhập → mọi kế thừa của danh mục nhúng đã có dòng pending_c9.
	var total, pending int
	pool := f.env.Pool(t, migrations.PF, masterdata.DBRole)
	if err := tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE basis_status = 'pending_c9')
		  FROM pf_masterdata.admin_unit_succession`).Scan(&total, &pending)
	}); err != nil || total == 0 || total != pending {
		t.Fatalf("seed: %d dòng kế thừa, %d pending_c9 (%v)", total, pending, err)
	}

	old, err := hist.AdminUnitAt(ctx, "91", day("2024-05-01"))
	if err != nil || old.Name != "Kiên Giang" {
		t.Fatalf("AdminUnitAt 91@2024: %+v %v", old, err)
	}
	if _, err := hist.AdminUnitAt(ctx, "89", day("2026-01-01")); !errors.Is(err, port.ErrUnitNotFound) {
		t.Fatalf("AdminUnitAt 89@2026: %v", err)
	}

	// RollForward cho cùng tập mã như Current (đi theo successor_codes) trên dữ liệu nhúng.
	for _, c := range []struct{ code, at string }{
		{"89", "2024-05-01"}, {"95", "2024-05-01"}, {"883", "2024-05-01"}, {"31178", "2024-05-01"}, {"91", "2024-05-01"},
	} {
		want, err := units.Current(ctx, c.code, day("2026-03-01"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := hist.RollForward(ctx, c.code, day(c.at), day("2026-03-01"))
		if err != nil {
			t.Fatalf("RollForward %s: %v", c.code, err)
		}
		codes := make([]string, len(got))
		for i, r := range got {
			codes[i] = r.Code
			if !r.Pending {
				t.Errorf("RollForward %s → %s: căn cứ chưa chính thức phải gắn Pending", c.code, r.Code)
			}
		}
		if !slices.Equal(codes, want) {
			t.Errorf("RollForward %s = %v; Current = %v", c.code, codes, want)
		}
	}
	// 89 nhập trọn vào 91: tỉ lệ 1, không ước tính. 31178 nhập một phần vào hai phường: ước tính.
	if got, _ := hist.RollForward(ctx, "89", day("2024-05-01"), day("2026-03-01")); len(got) != 1 || got[0].Share == nil ||
		!got[0].Share.Equal(decimal.NewFromInt(1)) || got[0].Estimated {
		t.Fatalf("89 → 91: %+v", got)
	}
	if got, _ := hist.RollForward(ctx, "31178", day("2024-05-01"), day("2026-03-01")); len(got) != 2 || !got[0].Estimated || got[0].Share != nil {
		t.Fatalf("31178 tách: %+v", got)
	}
	// Đích trước nguồn → 422.
	var pe *problem.Problem
	if _, err := hist.RollForward(ctx, "91", day("2026-03-01"), day("2024-05-01")); !errors.As(err, &pe) || pe.Status != 422 {
		t.Fatalf("đi ngược: %v", err)
	}

	succ, err := hist.SuccessorsAt(ctx, "89", day("2024-05-01"), day("2026-03-01"))
	if err != nil || len(succ) != 1 || succ[0].SuccessorCode != "91" || succ[0].BasisStatus != port.BasisPendingC9 ||
		succ[0].LegalBasis == "" || succ[0].AreaShare != nil || !succ[0].EffectiveOn.Equal(day("2025-07-01")) {
		t.Fatalf("SuccessorsAt 89: %+v %v", succ, err)
	}
	if succ, err := hist.SuccessorsAt(ctx, "89", day("2024-05-01"), day("2025-06-30")); err != nil || len(succ) != 0 {
		t.Fatalf("SuccessorsAt trước sắp xếp: %+v %v", succ, err)
	}

	// Bản ghi bù 'official' (căn cứ giả lập — mã kiểm thử) thay dòng pending_c9 cùng cặp.
	if err := tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_masterdata.admin_unit_succession
		    (tenant_id, predecessor_code, predecessor_valid_from, successor_code, successor_valid_from,
		     effective_on, area_share, legal_basis, basis_status, actor_ref)
		  SELECT tenant_id, predecessor_code, predecessor_valid_from, successor_code, successor_valid_from,
		         effective_on, 1, 'Căn cứ kiểm thử (không phải văn bản thật)', 'official', actor_ref
		    FROM pf_masterdata.admin_unit_succession WHERE predecessor_code = '89'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	succ, err = hist.SuccessorsAt(ctx, "89", day("2024-05-01"), day("2026-03-01"))
	if err != nil || len(succ) != 1 || succ[0].BasisStatus != port.BasisOfficial || succ[0].AreaShare == nil {
		t.Fatalf("SuccessorsAt 89 sau bản ghi bù: %+v %v", succ, err)
	}
	if got, _ := hist.RollForward(ctx, "89", day("2024-05-01"), day("2026-03-01")); len(got) != 1 || got[0].Pending {
		t.Fatalf("89 → 91 sau bản ghi bù không còn Pending: %+v", got)
	}

	// Chia tách qua nhập danh mục: ghi successor_codes và bảng kế thừa cùng giao dịch.
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	split := map[string]any{
		"format": "agrigreen.admin_units/v1", "source": "Kiểm thử chia tách W2.2",
		"units": []map[string]any{
			{"code": "31186", "name": "Phường Cái Răng", "level": "commune", "parent_code": "92", "valid_from": "2025-07-01",
				"valid_to": "2027-01-01", "successors": []string{"KT-31186-A", "KT-31186-B"}},
			{"code": "KT-31186-A", "name": "Cái Răng A", "level": "commune", "parent_code": "92", "valid_from": "2027-01-01"},
			{"code": "KT-31186-B", "name": "Cái Răng B", "level": "commune", "parent_code": "92", "valid_from": "2027-01-01"},
		},
	}
	if code, body := admin.adm("POST", "/v1/masterdata/admin-units/import", split); code != 200 {
		t.Fatalf("nhập chia tách: %d %v", code, body)
	}
	succ, err = hist.SuccessorsAt(ctx, "31186", day("2026-01-01"), day("2027-06-01"))
	if err != nil || len(succ) != 2 || succ[0].LegalBasis != "Kiểm thử chia tách W2.2" || succ[1].SuccessorCode != "KT-31186-B" {
		t.Fatalf("SuccessorsAt 31186: %+v %v", succ, err)
	}
	got, err := hist.RollForward(ctx, "31186", day("2026-01-01"), day("2027-06-01"))
	if err != nil || len(got) != 2 || !got[0].Estimated || got[0].Share != nil {
		t.Fatalf("RollForward 31186: %+v %v", got, err)
	}

	// Tenant khác (chưa nạp danh mục) không thấy gì.
	other := f.caller(ids.New())
	if _, err := f.mod.AdminUnitHistory().SuccessorsAt(other.ctx(), "89", day("2024-05-01"), day("2026-03-01")); !errors.Is(err, port.ErrUnitNotFound) {
		t.Fatalf("tenant khác: %v", err)
	}
}
