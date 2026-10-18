package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0012 (hạn 30 ngày của shared.ledger_idem), mẫu "helper tự thêm LIMIT 1, có JOIN":
// ParamVersionByIdem = oneParam(… v.idem_key = $1 + ledger.NewestFirst("v")) trên
// param_version v LEFT JOIN param_decision d. Sau khi dòng đăng ký hết hạn, phát lại cùng
// idem_key tạo dòng thứ hai; tra cứu phải trả dòng mới nhất.
func TestParamVersionByIdemReturnsNewestAfterExpiry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	tid := ids.New()
	idem := "k-" + ids.New().String()
	write := func(reason string) (store.ParamVersion, error) {
		v := store.ParamVersion{ID: ids.New(), TenantID: tid, Set: "risk", Rule: "hub.risk.v2", Params: json.RawMessage(`{"a":1}`),
			Reason: reason, EffectiveFrom: f.clk.Now(), EventTime: f.clk.Now(), ProposedBy: ids.New(), IdemKey: idem}
		err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { return store.InsertParamVersion(ctx, tx, &v, nil) })
		return v, ledger.Classify(err)
	}
	byIdem := func() *store.ParamVersion {
		t.Helper()
		var got *store.ParamVersion
		if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) (err error) {
			got, err = store.ParamVersionByIdem(ctx, tx, idem)
			return err
		}); err != nil || got == nil {
			t.Fatalf("phải tìm thấy phiên bản theo idem_key: %v", err)
		}
		return got
	}

	first, err := write("lần đặt đầu tiên")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write("ghi lặp trong hạn"); !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("trong hạn phải chặn trùng, được %v", err)
	}
	if got := byIdem(); got.ID != first.ID {
		t.Fatalf("một dòng: phải trả %s, được %s", first.ID, got.ID)
	}

	f.env.ExpireLedgerIdem(t, migrations.MKG, idem)
	second, err := write("phát lại sau 30 ngày")
	if err != nil {
		t.Fatalf("hết hạn: cùng idem_key phải được nhận như mới: %v", err)
	}
	if got := byIdem(); got.ID != second.ID || got.Seq != first.Seq+1 {
		t.Fatalf("phải trả dòng mới nhất %s (seq %d), được %s (seq %d)", second.ID, first.Seq+1, got.ID, got.Seq)
	}
}
