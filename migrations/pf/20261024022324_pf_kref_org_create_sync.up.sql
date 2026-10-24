-- P0-HTX-KREF (#130): bounded k/ref projection during the source INSERT transaction.
-- No application role gains projection DML or membership of r_kref. The NOLOGIN
-- module owner executes this trigger only for the authoritative source relation.
GRANT USAGE, CREATE ON SCHEMA k_ref TO own_kref;
GRANT USAGE ON SCHEMA shared TO own_kref;
GRANT EXECUTE ON FUNCTION shared.current_tenant() TO own_kref;
GRANT SELECT, INSERT, UPDATE ON k_ref.org TO own_kref;
GRANT SELECT, INSERT ON k_ref.node_closure TO own_kref;

CREATE FUNCTION k_ref.project_created_org() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  previous_tenant text := current_setting('app.tenant_id', true);
  previous_seq bigint;
BEGIN
  IF TG_OP <> 'INSERT' OR TG_LEVEL <> 'ROW'
     OR TG_TABLE_SCHEMA <> 'pf_party' OR TG_TABLE_NAME <> 'org' THEN
    RAISE EXCEPTION 'k_ref: invalid source trigger' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id IS NULL OR NEW.ref IS NULL OR NEW.last_seq < 1 THEN
    RAISE EXCEPTION 'k_ref: invalid source organization' USING ERRCODE = 'check_violation';
  END IF;
  IF nullif(previous_tenant, '') IS NOT NULL AND previous_tenant::uuid <> NEW.tenant_id THEN
    RAISE EXCEPTION 'k_ref: source tenant does not match context' USING ERRCODE = 'insufficient_privilege';
  END IF;
  -- Ordinary source inserts already passed pf_party RLS. Privileged bootstrap
  -- inserts may have no context: constrain even those writes to NEW.tenant_id,
  -- and restore the calling transaction's context before returning.
  PERFORM set_config('app.tenant_id', NEW.tenant_id::text, true);
  PERFORM pg_advisory_xact_lock(hashtextextended('k_ref:' || NEW.tenant_id::text, 0));
  SELECT source_seq INTO previous_seq FROM k_ref.org
   WHERE tenant_id = NEW.tenant_id AND ref = NEW.ref FOR UPDATE;
  IF FOUND AND previous_seq >= NEW.last_seq THEN
    PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
    RETURN NEW;
  END IF;
  IF NEW.parent_ref IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM k_ref.org p
        JOIN k_ref.node_closure c ON c.tenant_id = p.tenant_id AND c.ancestor = p.ref AND c.descendant = p.ref AND c.depth = 0
       WHERE p.tenant_id = NEW.tenant_id AND p.ref = NEW.parent_ref) THEN
    RAISE EXCEPTION 'k_ref: parent projection is not ready'
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'kref_parent_projection_ready';
  END IF;
  INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, parent_ref, active, source_seq)
    VALUES (NEW.tenant_id, NEW.ref, NEW.kind, NEW.code, NEW.admin_unit_code, NEW.parent_ref, NEW.active, NEW.last_seq)
    ON CONFLICT (tenant_id, ref) DO UPDATE
       SET kind = EXCLUDED.kind, code = EXCLUDED.code, admin_unit_code = EXCLUDED.admin_unit_code,
           parent_ref = EXCLUDED.parent_ref, active = EXCLUDED.active, source_seq = EXCLUDED.source_seq
       WHERE k_ref.org.source_seq < EXCLUDED.source_seq;
  INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
    SELECT NEW.tenant_id, NEW.ref, NEW.ref, 0
    UNION ALL
    SELECT NEW.tenant_id, c.ancestor, NEW.ref, c.depth + 1 FROM k_ref.node_closure c
     WHERE c.tenant_id = NEW.tenant_id AND c.descendant = NEW.parent_ref
    ON CONFLICT (tenant_id, ancestor, descendant) DO NOTHING;
  PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
  RETURN NEW;
END $$;
ALTER FUNCTION k_ref.project_created_org() OWNER TO own_kref;
REVOKE ALL ON FUNCTION k_ref.project_created_org() FROM PUBLIC;
