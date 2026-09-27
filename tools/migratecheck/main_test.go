package main

import (
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
	}
	for sql, want := range cases {
		p := CheckSQL(sql + ";")
		if len(p) == 0 || !strings.Contains(p[0], want) {
			t.Errorf("%q: muốn lỗi chứa %q, nhận %v", sql, want, p)
		}
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
