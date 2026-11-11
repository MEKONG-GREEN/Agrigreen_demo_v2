-- pf/mechanization — hợp đồng đọc có phiên bản cho lần chép MỘT LẦN số máy theo loại của HTX sang kho KN
-- (KN-STANDALONE-5-TT-DATA-MAY #306, migration 20261111002854_pf_extension_kn_machine_count_copy). Chỉ VIEW, không chép dữ
-- liệu, không đổi bảng gốc pf_mechanization. Chủ view là ag_migrator (NOBYPASSRLS), coop_machine_count FORCE RLS → p_tenant
-- áp qua view: người đọc phải đặt app.tenant_id. Chỉ cấp cho r_mechanization_pub (không cấp vai ứng dụng KN). Không dữ liệu
-- cá nhân (updated_by là mã tài khoản).
SET LOCAL lock_timeout = '5s';

CREATE VIEW pf_mechanization_pub.v1_machine_count_export WITH (security_barrier=true) AS
  SELECT tenant_id, cooperative_ref, machine_type, quantity, note, commune_code, department_code, active, updated_at, updated_by
  FROM pf_mechanization.coop_machine_count;

REVOKE ALL ON pf_mechanization_pub.v1_machine_count_export FROM PUBLIC;
GRANT SELECT ON pf_mechanization_pub.v1_machine_count_export TO r_mechanization_pub;
