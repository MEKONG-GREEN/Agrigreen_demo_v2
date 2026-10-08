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

// M5: sửa bảng đã có — ADD CONSTRAINT CHECK/FOREIGN KEY phải NOT VALID, ALTER TABLE phải có lock_timeout.
func TestCheckLocking(t *testing.T) {
	ok := map[string]string{
		"bảng tạo trong cùng tệp": `CREATE TABLE pf_x.a (id int, tenant_id uuid, CHECK (id > 0));
ALTER TABLE pf_x.a ADD CONSTRAINT a_id CHECK (id < 10);
CREATE TABLE pf_x.a_2026 PARTITION OF pf_x.a FOR VALUES FROM (1) TO (2);
ALTER TABLE pf_x.a_2026 ADD CONSTRAINT a_fk FOREIGN KEY (id) REFERENCES pf_x.b (id);`,
		"NOT VALID + VALIDATE": `SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_x.ledger ADD CONSTRAINT ledger_kind CHECK (kind IN ('a', 'b')) NOT VALID;
ALTER TABLE pf_x.ledger VALIDATE CONSTRAINT ledger_kind;
ALTER TABLE ONLY pf_x.t ADD CONSTRAINT t_fk FOREIGN KEY (tenant_id, ref) REFERENCES pf_x.u (tenant_id, id) NOT VALID;`,
		"cột mới có CHECK": `SET LOCAL lock_timeout TO '3s';
ALTER TABLE pf_x.t ADD COLUMN note text NULL CHECK (char_length(note) < 10);`,
		"không ALTER TABLE": `CREATE INDEX IF NOT EXISTS t_ref ON pf_x.t (tenant_id, ref);
GRANT SELECT (tenant_id) ON pf_x.t TO ag_ops_jobs;
CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN ALTER TABLE pf_x.t ADD CHECK (true); END $$;`,
	}
	for name, src := range ok {
		if p := CheckLocking(src); len(p) != 0 {
			t.Errorf("%s: không được báo lỗi: %v", name, p)
		}
		if p := CheckSQL(src); len(p) != 0 {
			t.Errorf("%s: SET LOCAL lock_timeout phải qua quy tắc biến mức phiên: %v", name, p)
		}
	}
	bad := map[string][]string{
		`SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_x.ledger ADD CONSTRAINT ledger_kind CHECK (kind IN ('a'));`: {"NOT VALID"},
		`SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_x.t ADD FOREIGN KEY (tenant_id, ref) REFERENCES pf_x.u (tenant_id, id);`: {"NOT VALID"},
		`SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_x.t ADD CONSTRAINT a CHECK (x > 0) NOT VALID, ADD CONSTRAINT b CHECK (y > 0);`: {"NOT VALID"},
		`ALTER TABLE pf_x.t ADD COLUMN note text NULL;`:                          {"lock_timeout"},
		`ALTER TABLE IF EXISTS pf_x.t ADD CONSTRAINT c CHECK (x > 0) NOT VALID;`: {"lock_timeout"},
		// lock_timeout phải đặt TRƯỚC câu ALTER.
		`ALTER TABLE pf_x.t DROP CONSTRAINT c;
SET LOCAL lock_timeout = '5s';`: {"lock_timeout"},
		// SET mức phiên không tính (và bị quy tắc ADR-0028 chặn riêng).
		`SET lock_timeout = '5s';
ALTER TABLE pf_x.t ADD CHECK (x > 0);`: {"lock_timeout", "NOT VALID"},
	}
	for src, want := range bad {
		p := CheckLocking(src)
		if len(p) != len(want) {
			t.Errorf("%q: muốn %d lỗi %v, nhận %v", src, len(want), want, p)
			continue
		}
		for i, w := range want {
			if !strings.Contains(p[i], w) {
				t.Errorf("%q: lỗi %d phải chứa %q: %v", src, i, w, p)
			}
		}
	}
}

// Tệp được miễn theo số chỉ ở đúng cụm; tệp mới (số khác) bị kiểm.
func TestLockGrandfatheredByNumber(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "embed.go"), "package migrations\n\n//go:embed pf/*.sql mkg/*.sql\nvar FS embed.FS\n")
	alter := "ALTER TABLE pf_x.t ADD CONSTRAINT c CHECK (x > 0);"
	writeFile(t, filepath.Join(root, "pf", "000018_pf_geo_old.up.sql"), alter) // miễn (pf 18)
	writeFile(t, filepath.Join(root, "mkg", "000018_mkg_new.up.sql"), alter)   // mkg 18 không được miễn
	writeFile(t, filepath.Join(root, "pf", "000999_pf_geo_new.up.sql"), alter) // tệp mới
	p, err := checkTree(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(p, "\n")
	if strings.Contains(joined, "000018_pf_geo_old") || !strings.Contains(joined, "000018_mkg_new") || !strings.Contains(joined, "000999_pf_geo_new") {
		t.Fatalf("miễn sai: %v", p)
	}
}
