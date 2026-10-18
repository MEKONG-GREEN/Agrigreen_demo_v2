-- PLATFORM-DATA-A: current organization adjacency; no source writes/private grants.
SET LOCAL lock_timeout = '5s';
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='r_party_pub' AND NOT rolcanlogin AND NOT rolbypassrls) THEN
  RAISE EXCEPTION 'bootstrap r_party_pub before migrations';
 END IF;
END $$;
CREATE SCHEMA pf_party_pub;
REVOKE ALL ON SCHEMA pf_party_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_party_pub TO r_party_pub;
CREATE VIEW pf_party_pub.v1_org_tree WITH(security_barrier=true) AS
 SELECT tenant_id,ref AS org_ref,kind,code,name,admin_unit_code,parent_ref,active FROM pf_party.org;
REVOKE ALL ON pf_party_pub.v1_org_tree FROM PUBLIC;
GRANT SELECT ON pf_party_pub.v1_org_tree TO r_party_pub;
