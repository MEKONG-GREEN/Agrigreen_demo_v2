package handler_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0012 (hạn 30 ngày của shared.ledger_idem), mẫu "nhiều dòng theo tiền tố idem_key":
// một lần lập chặng ghi các dòng idem_key = legs:<khóa>:<n> trong một giao dịch. Phát lại
// sau khi các dòng đăng ký hết hạn ghi bộ chặng thứ hai cùng khóa; LegsByIdemPrefix phải
// trả đúng bộ mới nhất (không gộp hai bộ).
func TestLegsByIdemPrefixReturnsNewestSetAfterExpiry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	tid, trip := ids.New(), ids.New()
	prefix := "legs:k-" + ids.New().String()
	pt := port.GeoPoint{Lat: decimal.RequireFromString("10.0452"), Lng: decimal.RequireFromString("105.7469")}
	reason := "km nhập tay khi kiểm thử"
	writeSet := func(rev int) []uuid.UUID {
		t.Helper()
		var out []uuid.UUID
		if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
			for n := 1; n <= 2; n++ {
				l := store.Leg{ID: ids.New(), TenantID: tid, TripID: trip, LegID: ids.New(), PlanRevision: rev, LegSeq: n,
					Mode: port.ModeRoad, FromKind: "point", From: pt, ToKind: "point", To: pt, DistanceKm: decimal.NewFromInt(1),
					RouteSource: port.RouteManual, ManualReason: &reason, RateVNDPerTKm: decimal.NewFromInt(1),
					PlannedCost: money.Amount{}, ParamVersion: "x", EventTime: f.clk.Now(), ActorRef: ids.New(),
					IdemKey: prefix + ":" + strconv.Itoa(n), RuleVersion: "x"}
				if err := store.InsertLeg(ctx, tx, &l); err != nil {
					return err
				}
				out = append(out, l.ID)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	byPrefix := func() []store.Leg {
		t.Helper()
		var ls []store.Leg
		if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) (err error) {
			ls, err = store.LegsByIdemPrefix(ctx, tx, prefix)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ls
	}
	same := func(ls []store.Leg, want []uuid.UUID) bool {
		if len(ls) != len(want) {
			return false
		}
		for i := range ls {
			if ls[i].ID != want[i] {
				return false
			}
		}
		return true
	}

	first := writeSet(1)
	if ls := byPrefix(); !same(ls, first) {
		t.Fatalf("một bộ: phải trả %v, được %d chặng", first, len(ls))
	}
	f.env.ExpireLedgerIdem(t, migrations.MKG, prefix+":1", prefix+":2")
	second := writeSet(2)
	if ls := byPrefix(); !same(ls, second) {
		t.Fatalf("phải trả đúng bộ mới nhất %v, được %d chặng", second, len(ls))
	}
}
