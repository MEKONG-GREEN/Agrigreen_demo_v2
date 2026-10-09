-- Gói FC (ADR-0031 §3, R-DB-11): vai tổng hợp r_agg được EXECUTE các hàm đọc ngữ cảnh của k_ref.
--
-- Chính sách RLS trên bảng gốc của view công bố _pub gọi k_ref.ctx_uuid / k_ref.ctx_text: p_tenant, p_scope
-- (pf 000134, W3.4) và p_share (pf 000136, W4.2). View _pub chạy bằng quyền chủ ag_migrator (FORCE RLS) nhưng quyền
-- EXECUTE của hàm trong biểu thức chính sách được kiểm theo current_user — trong giao dịch job tổng hợp là r_agg
-- (db.WithAggTx). Thiếu quyền → mọi job tổng hợp lỗi 42501 "permission denied for function ctx_uuid".
--
-- Chỉ là quyền EXECUTE trên ba hàm STABLE đọc current_setting('app.*') — không mở bảng, view hay schema nào mới
-- (cổng TestSchemaIsolationAggRole_W64 vẫn kiểm r_agg chỉ đọc view _pub + k_ref.org.tenant_id). USAGE k_ref đã có
-- (pf 000129).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_agg' AND NOT rolcanlogin AND rolbypassrls) THEN
    RAISE EXCEPTION 'thiếu vai r_agg (NOLOGIN BYPASSRLS) — chạy cmd/migrate bootstrap trước migrate up (ADR-0031)';
  END IF;
END
$$;

-- ctx_reject: ctx_text (SQL, được inline) gọi nó ở nhánh tên biến sai — quyền kiểm lúc khởi tạo biểu thức, nên cấp đủ
-- ba hàm như mọi r_* ở pf 000091.
GRANT EXECUTE ON FUNCTION k_ref.ctx_uuid(text), k_ref.ctx_text(text), k_ref.ctx_reject(text) TO r_agg;
