-- W2.6 (ADR-0028 §4 bước 2, DB-04, R-DB-02): mọi module pf chạy dưới r_<module> qua vai đăng nhập đơn vị
-- (app_platform, app_worker, app_public, app_sync). Migration này:
--   1. chép lại (lần cuối) mọi quyền ag_migrator đã cấp cho vai cũ <module>_app sang r_<module> — bắt kịp quyền
--      cấp trực tiếp cho vai cũ sau pf 000090, nếu có;
--   2. THU HỒI mọi quyền trực tiếp (schema, bảng, chuỗi số, cột, hàm) của 17 vai <module>_app. Vai cũ giữ tên
--      (userlist PgBouncer) và membership INHERIT trong r_<module> một kỳ phát hành: nhị phân cũ vẫn chạy khi
--      lùi bản, mọi quyền chỉ còn MỘT nguồn là r_*; bản phát hành sau DROP ROLE vai cũ;
--   3. platform_api, public_api, worker_platform (không theo module, chỉ shared qua ag_shared_user): chép (rỗng)
--      sang r_ops cho đủ, vai giữ nguyên một kỳ.
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
    ['accounts_app', 'r_accounts', 'move'],
    ['apiclient_app', 'r_apiclient', 'move'],
    ['content_app', 'r_content', 'move'],
    ['cropcycle_app', 'r_cropcycle', 'move'],
    ['extension_app', 'r_extension', 'move'],
    ['geo_app', 'r_geo', 'move'],
    ['masterdata_app', 'r_masterdata', 'move'],
    ['mechanization_app', 'r_mechanization', 'move'],
    ['metering_app', 'r_metering', 'move'],
    ['metrics_app', 'r_metrics', 'move'],
    ['mrv_app', 'r_mrv', 'move'],
    ['notify_app', 'r_notify', 'move'],
    ['party_app', 'r_party', 'move'],
    ['reports_app', 'r_reports', 'move'],
    ['simulation_app', 'r_simulation', 'move'],
    ['sync_app', 'r_sync', 'move'],
    ['waterway_app', 'r_waterway', 'move'],
    ['platform_api', 'r_ops', 'copy'],
    ['public_api', 'r_ops', 'copy'],
    ['worker_platform', 'r_ops', 'copy']
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
