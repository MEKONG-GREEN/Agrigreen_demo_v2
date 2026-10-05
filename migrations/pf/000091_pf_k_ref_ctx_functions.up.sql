-- Hàm đọc biến ngữ cảnh k_ref.ctx_uuid / k_ref.ctx_text (W2.4, DB-13, ADR-0029, kiến trúc v4.1 §17.2,
-- R-DB-08). Chỉ hàm — chưa đổi chính sách RLS nào (W3.x); các quan hệ k_ref khác do W2.5 dựng.
--
-- Biến app.* do internal/shared/db đặt bằng set_config(…, true) trong từng giao dịch (db.ForModule /
-- db.WithContextTx): app.tenant_id, app.node_id, app.dept, app.xa_ids, app.purpose, app.actor_ref,
-- app.on_behalf_of_ref (docs/dev/modules.md#biến-ngữ-cảnh-trong-db-w24).
--
-- Cùng mẫu shared.current_tenant() (ADR-0005): trên kết nối tái dùng current_setting(k, true) trả '' chứ
-- không phải NULL → nullif(…, '') → NULL. Khác bản §17.2:
--   * ctx_uuid trả NULL (không ném lỗi) khi giá trị không phải UUID dạng chuẩn — chính sách so sánh với
--     NULL thì không khớp hàng nào (đóng an toàn), không làm hỏng câu truy vấn.
--   * chỉ nhận tên app.<chữ thường, số, _> — tên khác (vd 'role', 'search_path') → lỗi 22023, để chính
--     sách không vô tình đọc biến hệ thống.
-- Thân hàm là một biểu thức SELECT, LANGUAGE sql STABLE, không SET/SECURITY DEFINER → planner inline
-- thành biểu thức current_setting(...) (không tốn lời gọi hàm theo hàng, DB-14). ctx_reject (plpgsql)
-- chỉ chạy ở nhánh tên sai; nó STABLE để không chặn inline.

CREATE SCHEMA IF NOT EXISTS k_ref;

CREATE FUNCTION k_ref.ctx_reject(k text) RETURNS text
LANGUAGE plpgsql STABLE PARALLEL SAFE AS $$
BEGIN
  RAISE EXCEPTION 'k_ref.ctx_*: chỉ đọc biến ngữ cảnh app.* (nhận %)', coalesce(quote_literal(k), 'NULL')
    USING ERRCODE = '22023';
END
$$;

CREATE FUNCTION k_ref.ctx_text(k text) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT CASE WHEN k ~ '^app\.[a-z][a-z0-9_]*$'
              THEN nullif(current_setting(k, true), '')
              ELSE k_ref.ctx_reject(k) END
$$;

CREATE FUNCTION k_ref.ctx_uuid(k text) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT CASE WHEN k_ref.ctx_text(k) ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
              THEN k_ref.ctx_text(k)::uuid END
$$;

COMMENT ON FUNCTION k_ref.ctx_text(text) IS 'Biến ngữ cảnh app.* của giao dịch; rỗng/thiếu → NULL (DB-13, W2.4)';
COMMENT ON FUNCTION k_ref.ctx_uuid(text) IS 'Biến ngữ cảnh app.* dạng uuid; rỗng/thiếu/sai dạng → NULL (DB-13, W2.4)';

-- Quyền: chỉ các vai nhóm r_<module> (ADR-0028); vai cũ <module>_app có qua membership (INHERIT).
-- Danh sách khớp dbadmin.ModuleGroups() (TestMigration91MatchesModuleGroups).
REVOKE ALL ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) FROM PUBLIC;

DO $$
DECLARE
  groups constant text[] := ARRAY[
    'r_accounts', 'r_apiclient', 'r_content', 'r_cropcycle', 'r_extension', 'r_geo', 'r_masterdata',
    'r_mechanization', 'r_metering', 'r_metrics', 'r_mrv', 'r_notify', 'r_party', 'r_reports',
    'r_simulation', 'r_sync', 'r_waterway'
  ];
  grp text;
BEGIN
  FOREACH grp IN ARRAY groups LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = grp AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', grp;
    END IF;
    EXECUTE format('GRANT USAGE ON SCHEMA k_ref TO %I', grp);
    EXECUTE format('GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO %I', grp);
  END LOOP;
END
$$;
