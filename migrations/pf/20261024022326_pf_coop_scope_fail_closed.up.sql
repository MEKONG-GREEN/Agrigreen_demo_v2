-- P0-HTX-KREF (#130): forward-only hardening of the W3.4 restrictive policies.
-- Keep p_tenant and explicit node-less jobs/Admin behavior; scoped sessions need
-- a projected caller node even for commune/draft branches. An empty k_ref never
-- falls back to tenant-wide access. Preserve W4.2 read delegation for tables
-- with p_share: only USING accepts foreign-tenant rows, whose SELECT permission
-- still comes from p_share (purpose, grant, owner, node, validity). WITH CHECK
-- never receives that branch; p_tenant still rejects foreign writes. No source
-- or append-only ledger is rewritten.
SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
  spec constant text[][] := ARRAY[
    ['cooperative', 'org_ref', 'f', 'f'],
    ['household_membership', 'org_ref', 'f', 'f'],
    ['plot', 'cooperative_ref', 't', 't'],
    ['plot_boundary', 'cooperative_ref', 't', 't'],
    ['plot_event', 'cooperative_ref', 't', 't'],
    ['boundary_conflict', 'owner_ref', 't', 't'],
    ['cooperative_boundary', 'cooperative_ref', 'f', 't'],
    ['cooperative_area', 'cooperative_ref', 'f', 't'],
    ['season_event', 'cooperative_ref', 'f', 't'],
    ['season_current', 'cooperative_ref', 'f', 't'],
    ['farm_log_batch', 'cooperative_ref', 't', 'f'],
    ['farm_log', 'cooperative_ref', 'f', 't'],
    ['farm_log_review', 'cooperative_ref', 'f', 'f'],
    ['farm_log_current', 'cooperative_ref', 'f', 't'],
    ['harvest_declaration', 'cooperative_ref', 'f', 't'],
    ['yield_summary', 'cooperative_ref', 'f', 't'],
    ['coop_season_area', 'cooperative_ref', 'f', 'f']];
  ready constant text := $f$EXISTS (SELECT 1 FROM k_ref.node_closure actor_node
    WHERE actor_node.tenant_id = shared.current_tenant()
      AND actor_node.ancestor = k_ref.ctx_uuid('app.node_id')
      AND actor_node.descendant = k_ref.ctx_uuid('app.node_id') AND actor_node.depth = 0)$f$;
  unscoped constant text := $f$k_ref.ctx_uuid('app.node_id') IS NULL$f$;
  expr text;
  scoped text;
  read_expr text;
  t text;
  n text;
BEGIN
  FOR i IN 1 .. array_length(spec, 1) LOOP
    t := spec[i][1]; n := spec[i][2];
    scoped := format($f$EXISTS (SELECT 1 FROM k_ref.node_closure nc
      WHERE nc.tenant_id = %1$I.tenant_id AND nc.ancestor = k_ref.ctx_uuid('app.node_id')
        AND nc.descendant = %1$I.%2$I)$f$, t, n);
    IF spec[i][4] = 't' THEN
      scoped := scoped || format($f$ OR %1$I.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[])$f$, t);
    ELSE
      scoped := scoped || format($f$ OR EXISTS (SELECT 1 FROM k_ref.org o
        WHERE o.tenant_id = %1$I.tenant_id AND o.ref = %1$I.%2$I
          AND o.admin_unit_code = ANY (k_ref.ctx_text('app.xa_ids')::text[]))$f$, t, n);
    END IF;
    IF spec[i][3] = 't' THEN
      scoped := scoped || format($f$ OR (%1$I.%2$I IS NULL AND k_ref.ctx_text('app.xa_ids') IS NULL)$f$, t, n);
    END IF;
    expr := unscoped || ' OR (' || ready || ' AND (' || scoped || '))';
    read_expr := expr;
    IF EXISTS (SELECT 1 FROM pg_policy p
      WHERE p.polrelid = format('pf_coop.%I', t)::regclass AND p.polname = 'p_share'
        AND p.polpermissive AND p.polcmd = 'r' AND p.polwithcheck IS NULL) THEN
      read_expr := format('%I.tenant_id <> shared.current_tenant() OR (%s)', t, expr);
    END IF;
    EXECUTE format('ALTER POLICY p_scope ON pf_coop.%I USING (%s) WITH CHECK (%s)', t, read_expr, expr);
  END LOOP;
  scoped := $f$EXISTS (SELECT 1 FROM pf_coop.household_membership m
    JOIN k_ref.node_closure nc ON nc.tenant_id = m.tenant_id AND nc.descendant = m.org_ref
    WHERE m.tenant_id = household.tenant_id AND m.household_ref = household.household_ref
      AND (m.until_date IS NULL OR m.until_date > current_date)
      AND nc.ancestor = k_ref.ctx_uuid('app.node_id'))
    OR household.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[])$f$;
  expr := unscoped || ' OR (' || ready || ' AND (' || scoped || '))';
  -- Membership is inserted after the household in the same transaction. The
  -- INSERT readiness check must not depend on that not-yet-existing membership.
  -- household has a W4.2 p_share FOR SELECT as well. Its membership subquery
  -- must be able to read the owner's shared memberships through their p_share.
  read_expr := 'household.tenant_id <> shared.current_tenant() OR (' || expr || ')';
  EXECUTE format('ALTER POLICY p_scope ON pf_coop.household USING (%s) WITH CHECK (%s)', read_expr, unscoped || ' OR ' || ready);
  scoped := $f$(kn_profile_event.subject_kind = 'cooperative' AND EXISTS (
    SELECT 1 FROM k_ref.node_closure nc WHERE nc.tenant_id = kn_profile_event.tenant_id
      AND nc.ancestor = k_ref.ctx_uuid('app.node_id') AND nc.descendant = kn_profile_event.subject_ref))
    OR (kn_profile_event.subject_kind = 'household' AND EXISTS (
      SELECT 1 FROM pf_coop.household_membership m
      JOIN k_ref.node_closure nc ON nc.tenant_id = m.tenant_id AND nc.descendant = m.org_ref
      WHERE m.tenant_id = kn_profile_event.tenant_id AND m.household_ref = kn_profile_event.subject_ref
        AND (m.until_date IS NULL OR m.until_date > current_date)
        AND nc.ancestor = k_ref.ctx_uuid('app.node_id')))
    OR kn_profile_event.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[])$f$;
  expr := unscoped || ' OR (' || ready || ' AND (' || scoped || '))';
  EXECUTE format('ALTER POLICY p_scope ON pf_coop.kn_profile_event USING (%s) WITH CHECK (%s)', expr, expr);
  IF EXISTS (SELECT 1 FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
      WHERE c.relnamespace = 'pf_coop'::regnamespace AND p.polname = 'p_scope'
        AND (p.polpermissive OR pg_get_expr(p.polqual, p.polrelid) LIKE '%LIMIT 1%')) THEN
    RAISE EXCEPTION 'P0: cooperative scope policy must remain restrictive and fail closed';
  END IF;
  IF EXISTS (SELECT 1 FROM shared.v_missing_rls) THEN
    RAISE EXCEPTION 'P0: shared.v_missing_rls is not empty';
  END IF;
END $$;
