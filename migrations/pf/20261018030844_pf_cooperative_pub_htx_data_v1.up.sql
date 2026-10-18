-- KNSPLIT-2-HTX-DATA-A: add only the pf/cooperative published schema; forward-only.
-- No data writes, private grants, RLS changes or ledger changes. Owner is the migration role,
-- NOBYPASSRLS; every private source is FORCE RLS. Readers must still have tenant/share context.
SET lock_timeout = '5s';

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='r_cooperative_pub' AND NOT rolcanlogin AND NOT rolbypassrls) THEN
    RAISE EXCEPTION 'bootstrap r_cooperative_pub NOLOGIN NOBYPASSRLS before migrations';
  END IF;
END
$$;

CREATE SCHEMA pf_cooperative_pub;
REVOKE ALL ON SCHEMA pf_cooperative_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_cooperative_pub TO r_cooperative_pub;

CREATE VIEW pf_cooperative_pub.v1_cooperatives WITH (security_barrier=true) AS
  WITH refs AS (
    SELECT tenant_id,org_ref FROM pf_coop.cooperative
    UNION
    SELECT tenant_id,org_ref FROM pf_coop.household_membership
  ), members AS (
    SELECT m.tenant_id,m.org_ref,count(*)::bigint AS member_households
    FROM pf_coop.household_membership m
    JOIN pf_coop.household h ON h.tenant_id=m.tenant_id AND h.household_ref=m.household_ref
    WHERE m.until_date IS NULL AND h.active
    GROUP BY m.tenant_id,m.org_ref
  )
  SELECT refs.tenant_id,refs.org_ref,
    c.org_ref IS NOT NULL AS profile_present,
    coalesce(m.member_households,0)::bigint AS member_households
  FROM refs
  LEFT JOIN pf_coop.cooperative c ON c.tenant_id=refs.tenant_id AND c.org_ref=refs.org_ref
  LEFT JOIN members m ON m.tenant_id=refs.tenant_id AND m.org_ref=refs.org_ref;

CREATE VIEW pf_cooperative_pub.v1_plots WITH (security_barrier=true) AS
  SELECT tenant_id,ref AS plot_ref,cooperative_ref,commune_code,status,area_ha
  FROM pf_coop.plot;

-- A season can temporarily have either projection absent (legacy/replay). FULL JOIN preserves
-- the independent native read sets. Each side keeps its own dimensions/status; no coalescing
-- denormalized production facts into season facts. No actor, name, raw GPS or free text.
CREATE VIEW pf_cooperative_pub.v1_seasons WITH (security_barrier=true) AS
  SELECT coalesce(s.tenant_id,y.tenant_id) AS tenant_id,
    coalesce(s.season_ref,y.season_ref) AS season_ref,
    s.season_ref IS NOT NULL AS season_present,y.season_ref IS NOT NULL AS production_present,
    s.plot_ref,s.cooperative_ref,s.commune_code,s.season_code,s.season_year,s.variety_code,
    s.household_ref,s.status,s.area_ha,s.opened_at,s.sowing_date,s.expected_harvest_date,s.harvest_open_date,s.completed_at,
    y.plot_ref AS production_plot_ref,y.cooperative_ref AS production_cooperative_ref,
    y.commune_code AS production_commune_code,y.season_code AS production_season_code,
    y.season_year AS production_season_year,y.variety_code AS production_variety_code,
    y.household_ref AS production_household_ref,y.status AS production_status,
    y.area_ha AS production_area_ha,y.expected_kg,y.actual_kg,y.sop_steps,y.sop_steps_done,y.approved_logs,y.pending_logs
  FROM pf_coop.season_current s
  FULL JOIN pf_coop.yield_summary y ON y.tenant_id=s.tenant_id AND y.season_ref=s.season_ref;

-- Only claimed aliases are positive facts. An absent alias is unconfirmed. Historical consumers
-- additionally require claimed_at <= reporting cutoff (M6); the view does not substitute now().
CREATE VIEW pf_cooperative_pub.v1_confirmations WITH (security_barrier=true) AS
  SELECT al.tenant_id,al.org_ref,a.claimed_at,'confirmed'::text AS confirmation_status
  FROM pf_coop.htx_anchor_alias al
  JOIN pf_coop.htx_anchor a ON a.tenant_id=al.tenant_id AND a.anchor_id=al.anchor_id
  WHERE a.status='claimed';

REVOKE ALL ON ALL TABLES IN SCHEMA pf_cooperative_pub FROM PUBLIC;
GRANT SELECT ON pf_cooperative_pub.v1_cooperatives,pf_cooperative_pub.v1_plots,
  pf_cooperative_pub.v1_seasons,pf_cooperative_pub.v1_confirmations TO r_cooperative_pub;
COMMENT ON SCHEMA pf_cooperative_pub IS
  'Versioned read contracts of pf/cooperative; private schema pf_coop unchanged. KNSPLIT-2-HTX-DATA-A.';
RESET lock_timeout;
