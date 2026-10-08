-- W2.6 (ADR-0028 §1 + §4, DB-04, R-DB-02): vai nhóm r_<module> cho 7 module mkg (vai đăng nhập đơn vị
-- app_internal, app_worker_internal, app_ingest). Như pf 000090 + pf 000119 trong một bước:
--   1. chép mọi quyền ag_migrator đã cấp cho <module>_app sang r_<module> (gồm k_ref của mkg 000022);
--   2. thu hồi quyền trực tiếp của 7 vai <module>_app (giữ tên + membership INHERIT một kỳ cho lùi bản);
--   3. ingest_mkg (cmd/ingest) gộp vào r_measurement (ingest thuộc mkg/measurement); mkg_api, worker_internal
--      (chỉ shared qua ag_shared_user) gộp vào r_ops — chỉ chép, vai cũ giữ nguyên quyền một kỳ.
-- Thứ tự triển khai (docs/runbooks/db-roles-w26.md): `migrate bootstrap` bản W2.6 (vai nhóm, vai đăng nhập đơn vị,
-- membership vai cũ → vai nhóm) → `migrate up` → bí mật/pool app_* → triển khai ứng dụng. Thiếu vai nhóm hoặc
-- membership → migration lỗi, không lặng lẽ bỏ qua. Danh sách cặp ghi tay, khớp dbadmin.ModuleGroups()/FoldedLogins
-- (kiểm ở dbadmin: TestMigrationsW26MatchModuleGroups).
DO $$
DECLARE
  -- [vai cũ, vai nhóm, cách]: 'move' = chép rồi thu hồi quyền trực tiếp của vai cũ, vai cũ phải là thành viên
  -- INHERIT của vai nhóm (lùi bản); 'copy' = chỉ chép (vai đăng nhập không theo module giữ quyền một kỳ, không
  -- là thành viên vai nhóm).
  pairs constant text[][] := ARRAY[
    ['acctbridge_app', 'r_acctbridge', 'move'],
    ['costing_app', 'r_costing', 'move'],
    ['fieldops_app', 'r_fieldops', 'move'],
    ['hub_app', 'r_hub', 'move'],
    ['logistics_app', 'r_logistics', 'move'],
    ['measurement_app', 'r_measurement', 'move'],
    ['orders_app', 'r_orders', 'move'],
    ['ingest_mkg', 'r_measurement', 'copy'],
    ['mkg_api', 'r_ops', 'copy'],
    ['worker_internal', 'r_ops', 'copy']
  ];
  me oid := (SELECT oid FROM pg_roles WHERE rolname = current_user);
  legacy oid;
  grp oid;
  lname text;
  gname text;
  p record;
BEGIN
  FOR i IN 1 .. array_length(pairs, 1) LOOP
    lname := pairs[i][1];
    gname := pairs[i][2];
    SELECT oid INTO legacy FROM pg_roles WHERE rolname = lname;
    SELECT oid INTO grp FROM pg_roles WHERE rolname = gname AND NOT rolcanlogin;
    IF legacy IS NULL THEN
      RAISE EXCEPTION 'thiếu vai %', lname;
    END IF;
    IF grp IS NULL THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', gname;
    END IF;
    IF pairs[i][3] = 'move' AND NOT EXISTS (
         SELECT 1 FROM pg_auth_members WHERE roleid = grp AND member = legacy AND inherit_option) THEN
      RAISE EXCEPTION 'vai cũ % chưa là thành viên INHERIT của % — chạy cmd/migrate bootstrap bản W2.6 trước', lname, gname;
    END IF;

    -- 1. Chép (GRANT lặp lại vô hại): mọi quyền ag_migrator đã cấp cho vai cũ.
    FOR p IN SELECT n.nspname, a.privilege_type FROM pg_namespace n, aclexplode(n.nspacl) a
              WHERE a.grantee = legacy AND a.grantor = me LOOP
      EXECUTE format('GRANT %s ON SCHEMA %I TO %I', p.privilege_type, p.nspname, gname);
    END LOOP;
    FOR p IN SELECT n.nspname, c.relname, c.relkind, a.privilege_type
               FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(c.relacl) a
              WHERE a.grantee = legacy AND a.grantor = me LOOP
      EXECUTE format('GRANT %s ON %s %I.%I TO %I', p.privilege_type,
                     CASE WHEN p.relkind = 'S' THEN 'SEQUENCE' ELSE 'TABLE' END, p.nspname, p.relname, gname);
    END LOOP;
    FOR p IN SELECT n.nspname, c.relname, att.attname, a.privilege_type
               FROM pg_attribute att JOIN pg_class c ON c.oid = att.attrelid
               JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(att.attacl) a
              WHERE a.grantee = legacy AND a.grantor = me AND NOT att.attisdropped LOOP
      EXECUTE format('GRANT %s (%I) ON TABLE %I.%I TO %I', p.privilege_type, p.attname, p.nspname, p.relname, gname);
    END LOOP;
    FOR p IN SELECT f.oid::regprocedure AS fn, a.privilege_type FROM pg_proc f, aclexplode(f.proacl) a
              WHERE a.grantee = legacy AND a.grantor = me LOOP
      EXECUTE format('GRANT %s ON ROUTINE %s TO %I', p.privilege_type, p.fn, gname);
    END LOOP;

    CONTINUE WHEN pairs[i][3] <> 'move';

    -- 2. Thu hồi quyền trực tiếp của vai cũ (cột trước bảng; quyền qua membership r_* giữ nguyên).
    FOR p IN SELECT DISTINCT n.nspname, c.relname, att.attname, a.privilege_type
               FROM pg_attribute att JOIN pg_class c ON c.oid = att.attrelid
               JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(att.attacl) a
              WHERE a.grantee = legacy AND a.grantor = me AND NOT att.attisdropped LOOP
      EXECUTE format('REVOKE %s (%I) ON TABLE %I.%I FROM %I', p.privilege_type, p.attname, p.nspname, p.relname, lname);
    END LOOP;
    FOR p IN SELECT DISTINCT n.nspname, c.relname, c.relkind, a.privilege_type
               FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(c.relacl) a
              WHERE a.grantee = legacy AND a.grantor = me LOOP
      EXECUTE format('REVOKE %s ON %s %I.%I FROM %I', p.privilege_type,
                     CASE WHEN p.relkind = 'S' THEN 'SEQUENCE' ELSE 'TABLE' END, p.nspname, p.relname, lname);
    END LOOP;
    FOR p IN SELECT DISTINCT f.oid::regprocedure AS fn, a.privilege_type FROM pg_proc f, aclexplode(f.proacl) a
              WHERE a.grantee = legacy AND a.grantor = me LOOP
      EXECUTE format('REVOKE %s ON ROUTINE %s FROM %I', p.privilege_type, p.fn, lname);
    END LOOP;
    FOR p IN SELECT DISTINCT n.nspname, a.privilege_type FROM pg_namespace n, aclexplode(n.nspacl) a
              WHERE a.grantee = legacy AND a.grantor = me LOOP
      EXECUTE format('REVOKE %s ON SCHEMA %I FROM %I', p.privilege_type, p.nspname, lname);
    END LOOP;
  END LOOP;
END
$$;
