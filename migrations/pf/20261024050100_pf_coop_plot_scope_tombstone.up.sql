-- P0-HTX-KREF (#130): read-only ID feed for native HTX managers whose plot
-- moved away. This projection never exposes the new owner, name or geometry.
SET LOCAL lock_timeout = '5s';
LOCK TABLE pf_coop.plot IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE pf_coop.plot_scope_tombstone (
  tenant_id uuid NOT NULL,
  plot_ref uuid NOT NULL,
  previous_cooperative_ref uuid NOT NULL,
  commune_code text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  sync_seq bigint NOT NULL CHECK (sync_seq > 0),
  PRIMARY KEY (tenant_id, previous_cooperative_ref, plot_ref, sync_seq)
);
CREATE INDEX plot_scope_tombstone_feed ON pf_coop.plot_scope_tombstone
  (tenant_id, previous_cooperative_ref, sync_seq);
SELECT shared.apply_tenant_rls('pf_coop.plot_scope_tombstone'::regclass);
CREATE POLICY p_scope ON pf_coop.plot_scope_tombstone AS RESTRICTIVE FOR SELECT
  USING (previous_cooperative_ref = k_ref.ctx_uuid('app.node_id')
    AND EXISTS (SELECT 1 FROM k_ref.node_closure n
      WHERE n.tenant_id = shared.current_tenant()
        AND n.ancestor = k_ref.ctx_uuid('app.node_id')
        AND n.descendant = k_ref.ctx_uuid('app.node_id') AND n.depth = 0));
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_coop.plot_scope_tombstone
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_coop.plot_scope_tombstone
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
REVOKE ALL ON pf_coop.plot_scope_tombstone FROM PUBLIC;
GRANT SELECT ON pf_coop.plot_scope_tombstone TO r_cooperative;
GRANT USAGE, CREATE ON SCHEMA pf_coop TO own_cooperative;
GRANT USAGE ON SCHEMA shared TO own_cooperative;
GRANT EXECUTE ON FUNCTION shared.current_tenant() TO own_cooperative;
GRANT INSERT ON pf_coop.plot_scope_tombstone TO own_cooperative;

CREATE FUNCTION pf_coop.capture_plot_scope_loss() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  previous_tenant text := current_setting('app.tenant_id', true);
BEGIN
  IF TG_OP <> 'UPDATE' OR TG_WHEN <> 'AFTER' OR TG_LEVEL <> 'ROW'
     OR TG_RELID <> 'pf_coop.plot'::regclass THEN
    RAISE EXCEPTION 'plot scope: invalid source trigger' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.tenant_id IS DISTINCT FROM NEW.tenant_id OR OLD.ref IS DISTINCT FROM NEW.ref
     OR NEW.tenant_id IS NULL OR NEW.ref IS NULL
     OR NEW.revision <= OLD.revision OR NEW.sync_seq <= OLD.sync_seq THEN
    RAISE EXCEPTION 'plot scope: transfer needs a new source revision and cursor'
      USING ERRCODE = 'check_violation';
  END IF;
  IF nullif(previous_tenant, '') IS NOT NULL AND previous_tenant::uuid <> NEW.tenant_id THEN
    RAISE EXCEPTION 'plot scope: source tenant does not match context'
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  PERFORM set_config('app.tenant_id', NEW.tenant_id::text, true);
  INSERT INTO pf_coop.plot_scope_tombstone
    (tenant_id, plot_ref, previous_cooperative_ref, commune_code, revision, sync_seq)
    VALUES (NEW.tenant_id, NEW.ref, OLD.cooperative_ref, OLD.commune_code, NEW.revision, NEW.sync_seq);
  PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
  RETURN NEW;
END $$;
ALTER FUNCTION pf_coop.capture_plot_scope_loss() OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.capture_plot_scope_loss() FROM PUBLIC;
CREATE TRIGGER trg_plot_scope_loss AFTER UPDATE OF cooperative_ref ON pf_coop.plot
  FOR EACH ROW WHEN (OLD.cooperative_ref IS NOT NULL
    AND OLD.cooperative_ref IS DISTINCT FROM NEW.cooperative_ref)
  EXECUTE FUNCTION pf_coop.capture_plot_scope_loss();

-- Atomic upgrade: enumerate tenant metadata (not RLS-protected source rows),
-- then read FORCE-RLS sources under each ordinary migration tenant context.
-- Fresh projection cursors heal clients already past the last source update.
-- DISTINCT must precede nextval; no current row, ledger or k_ref is rewritten.
DO $$
DECLARE
  t uuid;
  previous_tenant text := current_setting('app.tenant_id', true);
  previous_node text := current_setting('app.node_id', true);
  previous_xa text := current_setting('app.xa_ids', true);
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM shared.chain_head WHERE ledger = 'pf_geo.plot_event' LOOP
    PERFORM set_config('app.tenant_id', t::text, true);
    PERFORM set_config('app.node_id', '', true);
    PERFORM set_config('app.xa_ids', '', true);
    INSERT INTO pf_coop.plot_scope_tombstone
      (tenant_id, plot_ref, previous_cooperative_ref, commune_code, revision, sync_seq)
      SELECT lost.tenant_id, lost.plot_ref, lost.cooperative_ref, lost.commune_code,
        lost.revision, nextval('pf_coop.plot_sync_seq')
      FROM (SELECT DISTINCT p.tenant_id, p.ref AS plot_ref, e.cooperative_ref,
          e.commune_code, p.revision
        FROM pf_coop.plot p JOIN pf_coop.plot_event e
          ON e.tenant_id = p.tenant_id AND e.plot_ref = p.ref
        WHERE p.tenant_id = t AND p.cooperative_ref IS NOT NULL
          AND e.cooperative_ref IS NOT NULL AND e.cooperative_ref <> p.cooperative_ref) lost;
  END LOOP;
  PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
  PERFORM set_config('app.node_id', coalesce(previous_node, ''), true);
  PERFORM set_config('app.xa_ids', coalesce(previous_xa, ''), true);
END $$;
