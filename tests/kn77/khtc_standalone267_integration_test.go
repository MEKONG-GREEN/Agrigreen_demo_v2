package kn77_test

import (
	"context"
	"net/http"
	"testing"

	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStandalone267RetiredExternalAPI(t *testing.T) {
	f := newReportFx(t)
	id := ids.New().String()
	for _, role := range []string{authz.KNTinh, authz.KNAdmin} {
		for _, route := range []struct{ method, path string }{
			{"GET", "/indicators"}, {"POST", "/indicators"}, {"GET", "/figures?year=2026"}, {"POST", "/figures"},
			{"POST", "/figures/withdraw"}, {"GET", "/history?year=2026"}, {"GET", "/template?year=2026"},
			{"POST", "/imports?year=2026&file_name=old.csv"}, {"GET", "/imports/" + id}, {"POST", "/imports/" + id + "/commit"},
			{"POST", "/evidence?file_name=old.pdf"}, {"GET", "/evidence/" + id},
		} {
			code, body := f.do(route.method, "/v1/extension/khtc/external"+route.path, role, khtcDept, f.khtcUser, "", map[string]any{})
			if code != http.StatusNotFound {
				t.Errorf("%s %s %s = %d, want404: %s", role, route.method, route.path, code, body)
			}
		}
	}
	// The legitimate report reader remains registered.
	p := f.open(39, dTT, dTTTT)
	f.read(p.ID)
}

func TestStandalone267OldExternalDataReadOnly(t *testing.T) {
	f := newReportFx(t)
	p := f.open(39, dTT, dTTTT)
	ctx := context.Background()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	indicator := ids.New()
	if err := tenant.WithTenantTx(ctx, admin, f.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_ext_indicator(tenant_id,id,code,name,name_key,unit,actor_ref,idem_key)
  VALUES($1,$2::uuid,'legacy267','Chỉ tiêu lịch sử','chi tieu lich su','ha',$3,$2::text)`, f.tid, indicator, f.khtcUser)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"khtc_ext_indicator", "khtc_ext_evidence", "khtc_ext_figure", "khtc_ext_import"} {
		var insert, update, del bool
		if err := admin.QueryRow(ctx, `SELECT has_table_privilege('r_extension',$1,'INSERT'),has_table_privilege('r_extension',$1,'UPDATE'),has_table_privilege('r_extension',$1,'DELETE')`, "pf_extension."+table).Scan(&insert, &update, &del); err != nil {
			t.Fatal(err)
		}
		if insert || update || del {
			t.Errorf("old %s remains writable: insert=%v update=%v delete=%v", table, insert, update, del)
		}
		// Execute INSERT with the actual application role: permission denial must precede missing required columns.
		err := tenant.WithTenantTx(ctx, admin, f.tid, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE r_extension"); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO pf_extension."+table+" DEFAULT VALUES")
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Errorf("app-role INSERT %s: want42501 got %v", table, err)
		}
	}
	if err := tenant.WithTenantTx(ctx, admin, f.tid, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE r_extension"); err != nil {
			return err
		}
		var name string
		if err := tx.QueryRow(ctx, "SELECT name FROM pf_extension.khtc_ext_indicator WHERE id=$1", indicator).Scan(&name); err != nil {
			return err
		}
		if name != "Chỉ tiêu lịch sử" {
			t.Errorf("legacy row changed: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.read(p.ID)
}

// Public HTTP counterpart: a real new draft and explicit refresh have no retired input field.
func TestStandalone267FreshHTTPReportHasNoExternalInput(t *testing.T) {
	f := newReportFx(t)
	p := f.open(39, dTT, dTTTT)
	for _, base := range []int64{-1, 0} {
		var code int
		var raw []byte
		if base < 0 {
			code, raw = f.call(http.MethodGet, p.ID, "", nil)
		} else {
			code, raw = f.call(http.MethodPost, p.ID, "/refresh", map[string]any{"base_version": base})
		}
		var result struct {
			Content struct {
				Data struct {
					External []map[string]any `json:"external"`
				} `json:"data"`
			} `json:"content"`
		}
		f.ok(code, raw, &result)
		if len(result.Content.Data.External) != 0 {
			t.Fatal("new HTTP report contains retired input")
		}
	}
}
