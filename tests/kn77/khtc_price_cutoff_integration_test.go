package kn77_test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/jackc/pgx/v5"
)

// M4: use the last two approved price days at the report cutoff, independently
// of the quantity period and of the age of the boards. Exercise the real _pub
// reader, HTTP refresh and immutable source snapshot on PostgreSQL.
func TestKH_M4_PriceCutoffPublishedReader(t *testing.T) {
	for _, tc := range []struct {
		name, cutoff string
		days, want   []string
	}{
		{"after_period", "2026-10-14", []string{"2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15"}, []string{"2026-10-13", "2026-10-14"}},
		{"older_than_62_days", "2026-10-13", []string{"2025-01-01", "2025-02-01", "2025-03-01"}, []string{"2025-02-01", "2025-03-01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newKHTCFormFx(t)
			ctx := context.Background()
			seedM4Boards(t, f, f.tid, tc.days, 1, 1000)
			var err error
			var p khtcPeriod
			c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{
				"report_type": "week", "year": 2026, "no": 41, "cutoff_date": tc.cutoff, "departments": []string{dTTTT}})
			f.ok(c, b, &p)
			var got struct {
				Values []struct {
					SourceKey string `json:"source_key"`
					PriceMin  string `json:"price_min"`
					PriceDays []struct {
						Day string `json:"day"`
					} `json:"price_days"`
				} `json:"values"`
			}
			c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/sources/refresh", f.khtcUser, "", nil)
			f.ok(c, b, &got)
			for _, v := range got.Values {
				if v.SourceKey != "price:thong_tin_truyen_thong:price-board:mh00001" {
					continue
				}
				if len(v.PriceDays) != 2 || v.PriceDays[0].Day != tc.want[0] || v.PriceDays[1].Day != tc.want[1] {
					t.Fatalf("M4: want %v, got %+v", tc.want, v.PriceDays)
				}

				// Read the persisted source rows, not just the computed response. A board
				// after Tuesday belongs to the next D8 week; old boards retain old keys.
				err = tenant.WithTenantTx(ctx, pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
					var days []string
					if err := tx.QueryRow(ctx, `SELECT array_agg(native_key ORDER BY native_key) FROM pf_extension.khtc_snapshot_row
      WHERE snapshot_id=(SELECT id FROM pf_extension.khtc_snapshot WHERE period_id=$1::uuid ORDER BY seq DESC LIMIT 1)
       AND form_code='PRICE-BOARD'`, p.ID).Scan(&days); err != nil {
						return err
					}
					if fmt.Sprint(days) != fmt.Sprint(tc.want) {
						t.Fatalf("captured price provenance: %v want %v", days, tc.want)
					}
					var target string
					if err := tx.QueryRow(ctx, `SELECT target_key FROM pf_extension.khtc_snapshot_row
      WHERE snapshot_id=(SELECT id FROM pf_extension.khtc_snapshot WHERE period_id=$1::uuid ORDER BY seq DESC LIMIT 1)
       AND native_key=$2 AND form_code='PRICE-BOARD'`, p.ID, tc.want[1]).Scan(&target); err != nil {
						return err
					}
					if tc.name == "after_period" && target != "2026-W42" {
						t.Fatalf("D8 target rewritten: %s", target)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				var status struct {
					SourceChanged bool `json:"source_changed"`
				}
				c, b = f.do(http.MethodGet, "/v1/extension/khtc/periods/"+p.ID+"/sources", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
				f.ok(c, b, &status)
				if status.SourceChanged {
					t.Fatal("unchanged as-of inputs reported changed")
				}
				return
			}
			t.Fatalf("M4: missing price value: %s", b)
		})
	}
}

func seedM4Boards(t *testing.T, f *kfFx, tid uuid.UUID, days []string, version, base int, itemCodes ...string) {
	t.Helper()
	ctx := context.Background()
	item := "MH00001"
	if len(itemCodes) > 0 {
		item = itemCodes[0]
	}
	err := tenant.WithTenantTx(ctx, pgtest.Get(t).AdminPool(t, migrations.PF), tid, func(tx pgx.Tx) error {
		for i, day := range days {
			// Each fixture board is an approved immutable ledger event, with a price
			// line. No UI internals or application price adapter is substituted.
			_, err := tx.Exec(ctx, `INSERT INTO pf_price.board
     (id,tenant_id,stream_key,seq,profile,board_date,action,status,board_version,approved_at,
      week_key,week_year,week_from,week_to,days_scheduled,lines_count,tickets_count,event_time,
      actor_ref,idem_key,rule_version,prev_hash,row_hash)
     VALUES ($1,$2,'board:kn:'||$3,$6::bigint,'kn',$3::date,'approve','approved',$6::integer,$3::date,
      '2026-W41',2026,$3::date,$3::date,1,1,0,$3::date,$4,$5,'fixture',decode('','hex'),decode('','hex'))`,
				ids.New(), tid, day, f.khtcUser, ids.New().String(), version)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO pf_price.board_line
     (id,tenant_id,stream_key,seq,profile,board_date,board_version,item_code,item_name,group_code,unit_code,
      price_min,price_max,price_avg,points_reported,event_time,actor_ref,idem_key,rule_version,prev_hash,row_hash)
     VALUES ($1,$2,'bline:kn:'||$3||':v'||$7::integer::text||':'||$8,1,'kn',$3::date,$7::integer,$8,'Lua','G1','dong_kg',
      $4::numeric,$4::numeric+100,$4::numeric+50,1,$3::date,$5,$6,'fixture',decode('','hex'),decode('','hex'))`,
				ids.New(), tid, day, fmt.Sprint(base+i*100), f.khtcUser, ids.New().String(), version, item)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestKH_M4_ExportKeepsPriceSnapshot(t *testing.T) {
	f := newKHTCFormFx(t)
	seedM4Boards(t, f, f.tid, []string{"2026-10-13", "2026-10-14"}, 1, 1000)
	// A foreign tenant with later and more expensive data must not enter either
	// the value or the captured provenance through the published price view.
	seedM4Boards(t, f, ids.New(), []string{"2026-10-14"}, 1, 9000)
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{
		"report_type": "week", "year": 2026, "no": 41, "cutoff_date": "2026-10-14", "departments": []string{dTTTT}})
	f.ok(c, b, &p)
	report := &reportFx{kfFx: f}
	d := report.refresh(p.ID, 0)
	x := report.export(p.ID, d.Version, true)
	original := report.download(p.ID, x.ID)
	checkM4Excel(t, original, "1100")
	seedM4Boards(t, f, f.tid, []string{"2026-10-14"}, 2, 3000)
	var source struct {
		SourceChanged bool `json:"source_changed"`
		Values        []struct {
			Key string  `json:"source_key"`
			Min *string `json:"price_min"`
		} `json:"values"`
	}
	c, b = f.khtc(http.MethodGet, "/periods/"+p.ID+"/sources", f.khtcUser, "", nil)
	f.ok(c, b, &source)
	if !source.SourceChanged {
		t.Fatal("changed after-period board was not detected")
	}
	for _, v := range source.Values {
		if v.Key == "price:thong_tin_truyen_thong:price-board:mh00001" && (v.Min == nil || *v.Min != "1100") {
			t.Fatalf("status rewrote captured value: %+v", v)
		}
	}
	if next := report.download(p.ID, x.ID); !bytes.Equal(original, next) {
		t.Fatal("old export changed after price approval")
	}
	d = report.refresh(p.ID, d.Version)
	updated := report.export(p.ID, d.Version, true)
	checkM4Excel(t, report.download(p.ID, updated.ID), "3000")
	if next := report.download(p.ID, x.ID); !bytes.Equal(original, next) {
		t.Fatal("old export changed after explicit refresh")
	}
}

func checkM4Excel(t *testing.T, body []byte, latest string) {
	t.Helper()
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	rows, err := file.GetRows("Báo cáo")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if len(row) >= 8 && row[0] == "Lua" {
			if row[2] != "2026-10-13" || row[5] != "2026-10-14" || row[6] != latest {
				t.Fatalf("price columns/range: %+v", row)
			}
			return
		}
	}
	t.Fatal("M4 price missing from exported workbook")
}

func TestKH_M4_SparsePriceItems(t *testing.T) {
	f := newKHTCFormFx(t)
	seedM4Boards(t, f, f.tid, []string{"2026-10-13", "2026-10-14"}, 1, 1000)
	seedM4Boards(t, f, f.tid, []string{"2025-02-01", "2025-03-01"}, 1, 2000, "MH00002")
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{
		"report_type": "week", "year": 2026, "no": 41, "cutoff_date": "2026-10-14", "departments": []string{dTTTT}})
	f.ok(c, b, &p)
	var got struct {
		Values []struct {
			Key  string `json:"source_key"`
			Days []struct {
				Day string `json:"day"`
			} `json:"price_days"`
		} `json:"values"`
	}
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/sources/refresh", f.khtcUser, "", nil)
	f.ok(c, b, &got)
	for _, v := range got.Values {
		if v.Key == "price:thong_tin_truyen_thong:price-board:mh00002" {
			if len(v.Days) != 2 || v.Days[0].Day != "2025-02-01" || v.Days[1].Day != "2025-03-01" {
				t.Fatalf("sparse item lost its own days: %+v", v)
			}
			return
		}
	}
	t.Fatalf("sparse item disappeared: %s", b)
}
