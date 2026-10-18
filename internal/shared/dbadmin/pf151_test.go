package dbadmin

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Gộp dev → main: chỉ hạ 151 → 136 ở DB đúng bản main (sạch, đứng ở 151, chưa có đối tượng của dev 138–151).
func TestRepairMainPF151(t *testing.T) {
	admin := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if admin == "" {
		t.Skip("không có AG_TEST_PG_ADMIN_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	cases := []struct {
		name     string
		setup    string
		want     int64
		repaired bool
	}{
		{"main", "INSERT INTO schema_migrations_pf VALUES (151, false)", mainPF151Reset, true},
		{"dirty", "INSERT INTO schema_migrations_pf VALUES (151, true)", 151, false},
		{"dev-schema", "INSERT INTO schema_migrations_pf VALUES (151, false); CREATE SCHEMA pf_price", 151, false},
		{"dev-table", "INSERT INTO schema_migrations_pf VALUES (151, false); CREATE SCHEMA pf_forms; CREATE TABLE pf_forms.period_spec ()", 151, false},
		{"newer", "INSERT INTO schema_migrations_pf VALUES (152, false)", 152, false},
		{"older", "INSERT INTO schema_migrations_pf VALUES (136, false)", 136, false},
		{"no-ledger", "DROP TABLE schema_migrations_pf", -1, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := fmt.Sprintf("ag_pf151_%d_%d", time.Now().UnixNano()%1_000_000, i)
			if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident(db)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+ident(db)+" WITH (FORCE)") })
			u, err := url.Parse(admin)
			if err != nil {
				t.Fatal(err)
			}
			u.Path = "/" + db
			dsn := u.String()
			c, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close(context.Background()) }()
			if _, err := c.Exec(ctx, "CREATE TABLE schema_migrations_pf (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); "+tc.setup); err != nil {
				t.Fatal(err)
			}
			for run := range 2 {
				repaired, err := repairMainPF151(ctx, dsn)
				if err != nil {
					t.Fatal(err)
				}
				if want := run == 0 && tc.repaired; repaired != want {
					t.Fatalf("lần %d: repaired = %v, muốn %v", run+1, repaired, want)
				}
			}
			if tc.want < 0 {
				return
			}
			var v int64
			if err := c.QueryRow(ctx, "SELECT version FROM schema_migrations_pf").Scan(&v); err != nil {
				t.Fatal(err)
			}
			if v != tc.want {
				t.Fatalf("version = %d, muốn %d", v, tc.want)
			}
		})
	}
}
