package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0012 (hạn 30 ngày của shared.ledger_idem): phát lại cùng idem_key sau khi dòng
// đăng ký hết hạn được ghi như bản ghi MỚI → sổ có hai dòng cùng khóa. Tra cứu theo
// idem_key (mẫu "một dòng": helper không LIMIT + ledger.NewestByIdem) phải trả dòng mới nhất.
func TestRollCountByIdemReturnsNewestAfterExpiry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	order := uuid.MustParse(f.approvedOrder())
	idem := key()
	write := func(rolls int) (store.RollCount, error) {
		rc := store.RollCount{ID: ids.New(), TenantID: f.tenant, OrderID: order, PlotRef: f.plots[0], HTXRef: f.htx,
			Stage: port.StageBale, Rolls: rolls, EventTime: f.clk.Now(), ActorRef: ids.New(), IdemKey: idem}
		err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { return store.InsertRollCount(ctx, tx, &rc) })
		return rc, ledger.Classify(err)
	}
	byIdem := func() *store.RollCount {
		t.Helper()
		var got *store.RollCount
		f.inTenant(func(tx pgx.Tx) (err error) { got, err = store.RollCountByIdem(ctx, tx, idem); return err })
		if got == nil {
			t.Fatal("phải tìm thấy bản ghi theo idem_key")
		}
		return got
	}

	first, err := write(3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write(4); !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("trong hạn phải chặn trùng, được %v", err)
	}
	if got := byIdem(); got.ID != first.ID {
		t.Fatalf("một dòng: phải trả %s, được %s", first.ID, got.ID)
	}

	f.env.ExpireLedgerIdem(t, migrations.MKG, idem)
	second, err := write(5)
	if err != nil {
		t.Fatalf("hết hạn: cùng idem_key phải được nhận như mới: %v", err)
	}
	var n int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM mkg_fieldops.roll_count WHERE idem_key = $1", idem).Scan(&n)
	})
	if n != 2 {
		t.Fatalf("sổ phải có 2 dòng cùng idem_key, có %d", n)
	}
	if got := byIdem(); got.ID != second.ID || got.Rolls != 5 {
		t.Fatalf("phải trả dòng mới nhất %s (5 cuộn), được %s (%d cuộn)", second.ID, got.ID, got.Rolls)
	}
}
