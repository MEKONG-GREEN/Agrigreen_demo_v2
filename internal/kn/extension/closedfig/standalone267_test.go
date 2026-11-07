package closedfig

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"strings"
	"testing"
)

// The only database surface supplied is the closed-figure family. There is no
// cooperative publication or confirmation connection to consult.
type standalone267DB struct {
	figure port.ClosedFigure
	t      *testing.T
}

func (d standalone267DB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return &standalone267Tx{db: d}, nil
}

type standalone267Tx struct {
	pgx.Tx
	db standalone267DB
}

func (*standalone267Tx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (*standalone267Tx) Commit(context.Context) error   { return nil }
func (*standalone267Tx) Rollback(context.Context) error { return nil }
func (x *standalone267Tx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "pf_cooperative_pub") {
		x.db.t.Fatal("retired HTX publication lookup")
	}
	if sql != query {
		x.db.t.Fatalf("unexpected source query: %s", sql)
	}
	return &standalone267Rows{figure: x.db.figure}, nil
}

type standalone267Rows struct {
	pgx.Rows
	figure port.ClosedFigure
	seen   bool
}

func (*standalone267Rows) Close()     {}
func (*standalone267Rows) Err() error { return nil }
func (r *standalone267Rows) Next() bool {
	if r.seen {
		return false
	}
	r.seen = true
	return true
}
func (r *standalone267Rows) Scan(dest ...any) error {
	f := r.figure
	source := []any{&f.DepartmentCode, &f.ProvinceCode, &f.CalendarID, &f.CalendarVersion, &f.PeriodType, &f.PeriodKey, &f.WeekYear, &f.PeriodRule,
		&f.PeriodFrom, &f.PeriodTo, &f.PeriodLabel, &f.Source, &f.SourceModule, &f.SourcePeriodID, &f.FormCode, &f.FormVersionID,
		&f.FormClosed, &f.PeriodClosed, &f.KeyClosed, &f.ClosedAt, &f.SnapshotID, &f.SnapshotNo, &f.DataAt, &f.Provisional,
		&f.UnitsTotal, &f.UnitsWithData, &f.MissingUnits, &f.Rows, &f.Totals, &f.ConfirmationStatus, &f.FiguresHash, &f.StatusHash, &f.KeyClosedAll}
	for i := range source {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(source[i]).Elem())
	}
	return nil
}

func TestStandalone267ClosedFiguresPreservePublishedRowsWithoutHTX(t *testing.T) {
	f := weekFig(cntsRule(t), d(2026, 9, 14), "BM-04", map[string]string{"heo": "2.5"})
	f.Rows = json.RawMessage(`[{"key":"legacy","unit":"01920000-0000-7000-8000-000000000001","unit_kind":"cooperative","confirmation":"confirmed","cells":{"heo":"2.5"}}]`)
	f.ConfirmationStatus = port.ConfirmationConfirmed
	f.StatusHash = hashOf("published-status")
	reader := Reader{DB: standalone267DB{figure: f, t: t}}
	out, err := reader.ClosedFigures(context.Background(), uuid.New(), port.ClosedFiguresQuery{})
	if err != nil || len(out) != 1 {
		t.Fatalf("read: %v %v", out, err)
	}
	if !reflect.DeepEqual(out[0], f) {
		t.Fatalf("reader relabelled/rehash published legacy figure: got%+v want%+v", out[0], f)
	}
	// The KHTC period reader shares the same publication-only path.
	periods, err := reader.Periods(context.Background(), uuid.New(), port.KHTCQuery{From: d(2026, 9, 9), To: d(2026, 9, 15), Departments: []string{"chan_nuoi_thuy_san"}})
	if err != nil || len(periods) != 1 {
		t.Fatalf("periods: %+v %v", periods, err)
	}
	for _, department := range periods[0].Departments {
		for _, figure := range department.Forms {
			if len(figure.SumUnconfirmed) > 0 || figure.SumConfirmed["heo"] != "2.5" {
				t.Fatalf("KHTC relabelled historical row: %+v", figure)
			}
		}
	}
}
