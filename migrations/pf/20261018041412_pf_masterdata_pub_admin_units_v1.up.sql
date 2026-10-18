-- PLATFORM-DATA-A: versioned administrative data; no source writes or private grants.
SET LOCAL lock_timeout = '5s';
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='r_masterdata_pub' AND NOT rolcanlogin AND NOT rolbypassrls) THEN
  RAISE EXCEPTION 'bootstrap r_masterdata_pub before migrations';
 END IF;
END $$;
CREATE SCHEMA pf_masterdata_pub;
REVOKE ALL ON SCHEMA pf_masterdata_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_masterdata_pub TO r_masterdata_pub;
CREATE VIEW pf_masterdata_pub.v1_admin_units WITH(security_barrier=true) AS
 SELECT tenant_id,code,valid_from,valid_to,name,level,parent_code,successor_codes FROM pf_masterdata.admin_unit;
REVOKE ALL ON pf_masterdata_pub.v1_admin_units FROM PUBLIC;
GRANT SELECT ON pf_masterdata_pub.v1_admin_units TO r_masterdata_pub;
