-- Vai tổng hợp r_agg thiếu EXECUTE trên k_ref.ctx_* (W6.4, ADR-0031 §3; bù thiếu sót của pf 000129).
-- Chạy bằng vai ag_migrator. Chỉ tiến (R-24); không đổi lược đồ, không UPDATE/DELETE dòng nào.
--
-- Triệu chứng: internal/platform/metrics/app.Agg.Run trả
--     ERROR: permission denied for function ctx_uuid (SQLSTATE 42501)
-- (TestAggRunAcrossTenants_W64, TestAggRunRefusedAndRoleScope_W64).
--
-- Nguyên nhân: pf 000091 thu hồi k_ref.ctx_reject/ctx_text/ctx_uuid khỏi PUBLIC rồi chỉ cấp cho 17 vai
-- nhóm r_<module> (danh sách khớp dbadmin.ModuleGroups()). r_agg KHÔNG phải vai nhóm module nên không nằm
-- trong danh sách đó, còn pf 000129 chỉ cấp cho nó USAGE k_ref + SELECT(tenant_id) trên k_ref.org — thiếu
-- EXECUTE hàm. Thiếu sót này không lộ ra cho tới W4.2: pf 000136 thêm nhánh p_share gọi k_ref.ctx_uuid và
-- k_ref.ctx_text trên các bảng mà view _pub đọc, mà view _pub chạy FORCE RLS (BYPASSRLS của bên đọc KHÔNG
-- xuyên qua view — ADR-0031 Hiện thực, chú thích db.AggTx). Từ đó mọi lượt đọc lát tenant của bộ chạy job
-- đều phải gọi được hai hàm ấy.
--
-- Vì sao an toàn: ctx_* chỉ ĐỌC biến phiên app.* của chính giao dịch; chúng không mở lối tới bất kỳ dữ liệu
-- nào. db.WithAggTx xóa sạch app.* ngay khi đổi vai, rồi db.AggTx.InTenant đặt app.tenant_id đúng bằng
-- tenant đang đọc — đó chính là cơ chế mà view _pub dựa vào. app.purpose để rỗng nên k_ref.ctx_text trả
-- NULL và nhánh p_share không khớp dòng nào: bộ chạy chỉ thấy lát của tenant hiện tại, đúng thiết kế.
--
-- Cấp cả BA hàm, như pf 000091 làm với vai nhóm: thân ctx_uuid gọi ctx_text, thân ctx_text gọi ctx_reject ở
-- nhánh tên biến sai. Cả ba đều LANGUAGE sql/plpgsql thường (không SECURITY DEFINER) nên quyền được kiểm
-- theo vai đang gọi; cấp lẻ một hàm chỉ dời lỗi sang hàm kế tiếp.
--
-- Không chạm quyền bảng, cột, sequence hay schema, nên TestSchemaIsolationAggRole_W64 (chỉ soi
-- has_table_privilege / has_column_privilege / has_sequence_privilege / has_schema_privilege) vẫn xanh.
--
-- Cổng agmodgraph: migration gắn pf/kref — đối tượng được đổi quyền là hàm của chính k_ref; r_agg chỉ là
-- bên nhận, không có DDL nào trên schema của module khác.

SET LOCAL lock_timeout = '5s';

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_agg' AND NOT rolcanlogin AND rolbypassrls) THEN
    RAISE EXCEPTION 'thiếu vai r_agg (NOLOGIN BYPASSRLS) — chạy cmd/migrate bootstrap trước migrate up (ADR-0031)';
  END IF;
END
$$;

GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_agg;

-- Kiểm cuối (lỗi → giao dịch lùi).
DO $$
DECLARE
  thieu text;
BEGIN
  SELECT string_agg(f, ', ' ORDER BY f) INTO thieu
    FROM unnest(ARRAY['k_ref.ctx_reject(text)', 'k_ref.ctx_text(text)', 'k_ref.ctx_uuid(text)']) f
   WHERE NOT has_function_privilege('r_agg', f, 'EXECUTE');
  IF thieu IS NOT NULL THEN
    RAISE EXCEPTION 'r_agg vẫn chưa EXECUTE được: %', thieu;
  END IF;
END
$$;
