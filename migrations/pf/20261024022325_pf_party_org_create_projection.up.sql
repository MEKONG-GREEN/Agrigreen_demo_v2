-- Validate through the source caller's existing SELECT permissions. A recent
-- move may still be waiting in outbox: never clone its previous ancestor path.
-- Only fields represented by k_ref matter; source-only renames need no rebuild.
CREATE FUNCTION pf_party.require_created_parent_projection() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  previous_tenant text := current_setting('app.tenant_id', true);
BEGIN
  IF TG_OP <> 'INSERT' OR TG_LEVEL <> 'ROW'
     OR TG_TABLE_SCHEMA <> 'pf_party' OR TG_TABLE_NAME <> 'org' THEN
    RAISE EXCEPTION 'party: invalid source trigger' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.parent_ref IS NULL THEN RETURN NEW; END IF;
  IF NEW.tenant_id IS NULL OR
     (nullif(previous_tenant, '') IS NOT NULL AND previous_tenant::uuid <> NEW.tenant_id) THEN
    RAISE EXCEPTION 'party: source tenant does not match context' USING ERRCODE = 'insufficient_privilege';
  END IF;
  PERFORM set_config('app.tenant_id', NEW.tenant_id::text, true);
  PERFORM pg_advisory_xact_lock(hashtextextended('k_ref:' || NEW.tenant_id::text, 0));
  IF NOT EXISTS (
      SELECT 1 FROM k_ref.org p JOIN k_ref.node_closure c
        ON c.tenant_id = p.tenant_id AND c.ancestor = p.ref AND c.descendant = p.ref AND c.depth = 0
       WHERE p.tenant_id = NEW.tenant_id AND p.ref = NEW.parent_ref)
     OR EXISTS (
      SELECT 1 FROM k_ref.node_closure c
      LEFT JOIN k_ref.org projected ON projected.tenant_id = c.tenant_id AND projected.ref = c.ancestor
      LEFT JOIN pf_party.org src ON src.tenant_id = c.tenant_id AND src.ref = c.ancestor
       WHERE c.tenant_id = NEW.tenant_id AND c.descendant = NEW.parent_ref AND (
         src.ref IS NULL OR projected.ref IS NULL
         OR src.parent_ref IS DISTINCT FROM projected.parent_ref
         OR src.admin_unit_code IS DISTINCT FROM projected.admin_unit_code
         OR src.active IS DISTINCT FROM projected.active
         OR (src.parent_ref IS NOT NULL AND NOT EXISTS (
           SELECT 1 FROM k_ref.node_closure parent_edge
            WHERE parent_edge.tenant_id = c.tenant_id AND parent_edge.descendant = c.descendant
              AND parent_edge.ancestor = src.parent_ref AND parent_edge.depth = c.depth + 1))
         OR (src.parent_ref IS NULL AND EXISTS (
           SELECT 1 FROM k_ref.node_closure extra_edge
            WHERE extra_edge.tenant_id = c.tenant_id AND extra_edge.descendant = c.descendant
              AND extra_edge.depth > c.depth))))
     OR (SELECT count(*) <> coalesce(max(depth), -1) + 1
           FROM k_ref.node_closure WHERE tenant_id = NEW.tenant_id AND descendant = NEW.parent_ref) THEN
    RAISE EXCEPTION 'k_ref: parent projection is not ready'
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'kref_parent_projection_ready';
  END IF;
  PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
  RETURN NEW;
END $$;
ALTER FUNCTION pf_party.require_created_parent_projection() OWNER TO own_party;
REVOKE ALL ON FUNCTION pf_party.require_created_parent_projection() FROM PUBLIC;
CREATE TRIGGER trg_kref_parent_current BEFORE INSERT ON pf_party.org
  FOR EACH ROW EXECUTE FUNCTION pf_party.require_created_parent_projection();

-- P0-HTX-KREF (#130): the k/ref-owned trigger runs inside INSERT pf_party.org.
-- On any projection error the source node, ledger and outbox transaction aborts.
-- Existing outbox emission remains for remote clusters and share-scope consumers.
CREATE TRIGGER trg_kref_created AFTER INSERT ON pf_party.org
  FOR EACH ROW EXECUTE FUNCTION k_ref.project_created_org();
