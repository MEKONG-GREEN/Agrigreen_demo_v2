package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowsFunctionBodiesAndGrants(t *testing.T) {
	src := `
-- UPDATE trong chú thích không tính
CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  UPDATE shared.chain_head SET last_seq = 1; -- trong thân hàm: được phép
  DELETE FROM x;
END $$;
GRANT SELECT, INSERT, UPDATE, DELETE ON shared.idempotency_key TO ag_shared_user;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.consent FROM party_app;
COMMENT ON TABLE t IS 'UPDATE; DELETE';
CREATE TABLE t (note text DEFAULT 'a;b');
`
	if p := CheckSQL(src); len(p) != 0 {
		t.Fatalf("không được báo lỗi: %v", p)
	}
}

func TestRejectsDataChangesAndProtectionBypass(t *testing.T) {
	cases := map[string]string{
		"UPDATE pf_party.consent SET status = 'x'":                   "UPDATE",
		"delete from pf_party.consent":                               "DELETE",
		"TRUNCATE mkg_fieldops.roll_count":                           "TRUNCATE",
		"WITH x AS (SELECT 1) UPDATE t SET a = 1":                    "UPDATE",
		"ALTER TABLE pf_party.consent DISABLE TRIGGER trg_immutable": "tắt trigger",
		"DROP TRIGGER trg_chain ON pf_party.consent":                 "xóa trigger",
		"ALTER TABLE pf_party.consent NO FORCE ROW LEVEL SECURITY":   "FORCE RLS",
		"ALTER TABLE pf_party.consent DISABLE ROW LEVEL SECURITY":    "tắt RLS",
		"DROP POLICY p_tenant ON pf_party.consent":                   "p_tenant",
		"SET session_replication_role = replica":                     "session_replication_role",
		// ADR-0028 §3: không đổi vai, không trạng thái mức phiên.
		"SET ROLE content_app":                           "SET ROLE",
		"SET LOCAL ROLE r_content":                       "SET ROLE",
		"RESET ROLE":                                     "mức phiên",
		"SET search_path = pf_content":                   "mức phiên",
		"SET SESSION AUTHORIZATION content_app":          "mức phiên",
		"SELECT set_config('app.tenant_id', 'x', false)": "set_config",
	}
	for sql, want := range cases {
		p := CheckSQL(sql + ";")
		if len(p) == 0 || !strings.Contains(p[0], want) {
			t.Errorf("%q: muốn lỗi chứa %q, nhận %v", sql, want, p)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Thư mục cụm mới (vd ts, G3) phải được nhúng, không thì migrate up bỏ qua im lặng.
func TestClusterDirMustBeEmbedded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "embed.go"), "package migrations\n\n//go:embed shared/*.sql pf/*.sql\nvar FS embed.FS\n")
	writeFile(t, filepath.Join(root, "pf", "000001_a.up.sql"), "CREATE TABLE a (id int);")
	writeFile(t, filepath.Join(root, "ts", "000001_b.up.sql"), "CREATE TABLE b (id int);")
	p, err := checkTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || !strings.Contains(p[0], "thư mục cụm ts không có trong //go:embed") {
		t.Fatalf("phải bắt thư mục ts chưa nhúng: %v", p)
	}
}

// ADR-0017 / R-07: cụm ts không tham chiếu schema nghiệp vụ, không dùng timescaledb.
func TestTSClusterRules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "embed.go"), "package migrations\n\n//go:embed ts/*.sql\nvar FS embed.FS\n")
	writeFile(t, filepath.Join(root, "ts", "000001_b.up.sql"), `-- mkg_measurement.weigh_ticket chỉ nhắc trong chú thích: không tính
CREATE TABLE ts_weigh.x (id int, note text DEFAULT 'pf_geo.plot');
CREATE VIEW ts_weigh.v AS SELECT * FROM ts_weigh.x JOIN mkg_hub.lot l ON true;
CREATE EXTENSION IF NOT EXISTS timescaledb;`)
	p, err := checkTree(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(p, "\n")
	if len(p) != 2 || !strings.Contains(joined, "R-07") || !strings.Contains(joined, "mkg_hub") || !strings.Contains(joined, "timescaledb") {
		t.Fatalf("phải bắt đúng join sang mkg_hub và extension timescaledb: %v", p)
	}
}

func TestRepositoryMigrationsPass(t *testing.T) {
	p, err := checkTree("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 0 {
		t.Fatalf("migration của repo vi phạm: %v", p)
	}
}
