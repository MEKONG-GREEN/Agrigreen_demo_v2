package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/material/store"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0012 (hạn 30 ngày của shared.ledger_idem), mẫu "tra theo lô khóa" (idem_key = ANY):
// sau khi khóa A hết hạn và được phát lại, ByIdemKeys vẫn trả MỘT dòng mỗi khóa — dòng mới
// nhất của A — theo thứ tự khóa yêu cầu.
func TestMaterialByIdemKeysReturnsNewestPerKeyAfterExpiry(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := shareddb.Module("pf/extension")
	tid := ids.New()
	keyA, keyB := "k-"+ids.New().String(), "k-"+ids.New().String()
	in := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := shareddb.WithModuleTenantTx(ctx, pool, mod, tid, fn); err != nil {
			t.Fatal(err)
		}
	}
	write := func(key, qty string) uuid.UUID {
		t.Helper()
		r := store.Record{ID: ids.New(), CommuneCode: "31117", DepartmentCode: dept, RecordedOn: time.Now().UTC(),
			MaterialCode: "NPK", MaterialName: "Phân NPK", Quantity: decimal.RequireFromString(qty), Unit: "kg", Source: "manual",
			EventTime: time.Now().UTC(), ActorRef: ids.New(), IdemKey: key}
		in(func(tx pgx.Tx) error { return store.Insert(ctx, tx, tid, &r) })
		return r.ID
	}
	byKeys := func() []store.Record {
		t.Helper()
		var rs []store.Record
		in(func(tx pgx.Tx) (err error) { rs, err = store.ByIdemKeys(ctx, tx, []string{keyB, keyA}); return err })
		return rs
	}

	write(keyA, "1")
	b := write(keyB, "2")
	env.ExpireLedgerIdem(t, migrations.PF, keyA)
	a2 := write(keyA, "3")

	rs := byKeys()
	if len(rs) != 2 || rs[0].ID != b || rs[1].ID != a2 || !rs[1].Quantity.Equal(decimal.NewFromInt(3)) {
		got := make([]uuid.UUID, 0, len(rs))
		for _, r := range rs {
			got = append(got, r.ID)
		}
		t.Fatalf("phải trả [B=%s, A mới nhất=%s], được %v", b, a2, got)
	}
}
