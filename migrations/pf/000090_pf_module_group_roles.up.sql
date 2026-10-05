-- Vai nhóm theo module r_<module> (W2.3, DB-04, ADR-0028 §1 + §4 bước 1, kiến trúc v4.1 §14 R-DB-02).
--
-- Vai r_<module> (NOLOGIN) và vai đăng nhập app_<đơn vị> (LOGIN NOINHERIT) do dbadmin.Bootstrap tạo
-- (ag_migrator không có CREATEROLE, không cấp membership được). Migration này chép NGUYÊN quyền hiện có
-- của vai đăng nhập cũ <module>_app sang r_<module> trong database cụm pf: USAGE trên schema, quyền
-- bảng/khung nhìn/chuỗi số, quyền theo cột, EXECUTE hàm. Chỉ chép quyền do chính ag_migrator cấp (chủ
-- các đối tượng do migration tạo) — quyền khác không cấp lại được bằng vai không phải superuser.
--
-- Vai cũ giữ nguyên quyền (đường lui, ADR-0028 §4): nó là thành viên r_<module> (INHERIT) nên GRANT mới
-- cho r_<module> từ nay cũng tới mã chưa chuyển. W2.6 (pf 000093) bỏ quyền bảng trực tiếp của <module>_app.
--
-- Danh sách cặp ghi tay, khớp dbadmin.ModuleGroups() (kiểm ở dbadmin: TestMigration90MatchesModuleGroups).
-- Thiếu vai r_* (chưa chạy `cmd/migrate bootstrap` bản mới) → lỗi, không lặng lẽ bỏ qua.
-- Không có quyền nào trên schema module khác ngoài những gì vai cũ đã có (giữ "không quyền chéo").
DO $$
DECLARE
  pairs constant text[][] := ARRAY[
    ['accounts_app', 'r_accounts'],
    ['apiclient_app', 'r_apiclient'],
    ['content_app', 'r_content'],
    ['cropcycle_app', 'r_cropcycle'],
    ['extension_app', 'r_extension'],
    ['geo_app', 'r_geo'],
    ['masterdata_app', 'r_masterdata'],
    ['mechanization_app', 'r_mechanization'],
    ['metering_app', 'r_metering'],
    ['metrics_app', 'r_metrics'],
    ['mrv_app', 'r_mrv'],
    ['notify_app', 'r_notify'],
    ['party_app', 'r_party'],
    ['reports_app', 'r_reports'],
    ['simulation_app', 'r_simulation'],
    ['sync_app', 'r_sync'],
    ['waterway_app', 'r_waterway']
  ];
  me oid := (SELECT oid FROM pg_roles WHERE rolname = current_user);
  legacy oid;
  grp text;
  p record;
BEGIN
  FOR i IN 1 .. array_length(pairs, 1) LOOP
    grp := pairs[i][2];
    SELECT oid INTO legacy FROM pg_roles WHERE rolname = pairs[i][1];
    IF legacy IS NULL THEN
      RAISE EXCEPTION 'thiếu vai %', pairs[i][1];
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = grp AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', grp;
    END IF;

    FOR p IN
      SELECT n.nspname, a.privilege_type
        FROM pg_namespace n, aclexplode(n.nspacl) a
       WHERE a.grantee = legacy AND a.grantor = me
    LOOP
      EXECUTE format('GRANT %s ON SCHEMA %I TO %I', p.privilege_type, p.nspname, grp);
    END LOOP;

    FOR p IN
      SELECT n.nspname, c.relname, c.relkind, a.privilege_type
        FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(c.relacl) a
       WHERE a.grantee = legacy AND a.grantor = me
    LOOP
      EXECUTE format('GRANT %s ON %s %I.%I TO %I', p.privilege_type,
                     CASE WHEN p.relkind = 'S' THEN 'SEQUENCE' ELSE 'TABLE' END, p.nspname, p.relname, grp);
    END LOOP;

    FOR p IN
      SELECT n.nspname, c.relname, att.attname, a.privilege_type
        FROM pg_attribute att
        JOIN pg_class c ON c.oid = att.attrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(att.attacl) a
       WHERE a.grantee = legacy AND a.grantor = me AND NOT att.attisdropped
    LOOP
      EXECUTE format('GRANT %s (%I) ON TABLE %I.%I TO %I', p.privilege_type, p.attname, p.nspname, p.relname, grp);
    END LOOP;

    FOR p IN
      SELECT f.oid::regprocedure AS fn, a.privilege_type
        FROM pg_proc f, aclexplode(f.proacl) a
       WHERE a.grantee = legacy AND a.grantor = me
    LOOP
      EXECUTE format('GRANT %s ON ROUTINE %s TO %I', p.privilege_type, p.fn, grp);
    END LOOP;
  END LOOP;
END
$$;
