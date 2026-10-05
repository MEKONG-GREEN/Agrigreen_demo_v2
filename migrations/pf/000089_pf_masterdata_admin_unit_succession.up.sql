-- pf_masterdata: đơn vị hành chính — ràng buộc loại trừ khai báo và bảng kế thừa
-- (W2.2, DB-06, ADR-0032 mục 2–4, kiến trúc v4.1 §19 R-DB-09).
--
--   1. btree_gist + EXCLUDE: hai phiên bản cùng (tenant, mã) không được chồng hiệu
--      lực, kể cả khi trigger fn_admin_unit_guard bị vòng qua (vai chủ,
--      session_replication_role). Trigger GIỮ NGUYÊN: nó chặn sửa nội dung và mở lại
--      phiên bản đã đóng — việc EXCLUDE không làm. Cả hai trả 23P01 exclusion_violation
--      nên ánh xạ lỗi ở masterdata/app không đổi.
--   2. admin_unit_succession: quan hệ kế thừa có ngày hiệu lực, tỉ lệ diện tích, căn cứ
--      pháp lý và trạng thái căn cứ ('official' | 'pending_c9'). Chỉ ghi thêm.
--   3. Điền từ successor_codes bằng INSERT … SELECT (không UPDATE admin_unit), mọi dòng
--      'pending_c9': chưa có nguồn chính thức cho danh mục sau sắp xếp 2025 (C-9 / H-16).
--
-- Khác ADR-0032 mục 3 một điểm: khóa chính gồm basis_status thay cho effective_on.
-- effective_on luôn = valid_to của phiên bản tiền thân (trigger dưới), nên khóa theo
-- effective_on không cho có dòng 'official' bù cho dòng 'pending_c9' cùng cặp như mục 3
-- yêu cầu; khóa theo basis_status cho đúng một dòng mỗi trạng thái mỗi cặp.

-- btree_gist là trusted extension (PostgreSQL ≥ 13): chủ database (ag_migrator) tạo
-- được; dbadmin.Extensions cũng tạo sẵn ở Bootstrap như postgis.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- ---------------------------------------------------------------------------
-- 1. Ràng buộc loại trừ (valid_to NULL = vô hạn trong daterange)
-- ---------------------------------------------------------------------------
ALTER TABLE pf_masterdata.admin_unit
  ADD CONSTRAINT admin_unit_no_overlap
  EXCLUDE USING gist (tenant_id WITH =, code WITH =, daterange(valid_from, valid_to, '[)') WITH &&);

-- ---------------------------------------------------------------------------
-- 2. Bảng kế thừa
-- ---------------------------------------------------------------------------
-- Một dòng = phiên bản tiền thân (predecessor_code, predecessor_valid_from) đóng hiệu
-- lực ngày effective_on và (một phần) diện tích của nó thuộc về phiên bản kế thừa
-- (successor_code, successor_valid_from) có hiệu lực ngày đó. Trỏ phiên bản bằng khóa
-- (code, valid_from) hiện có của admin_unit.
CREATE TABLE pf_masterdata.admin_unit_succession (
  tenant_id              uuid         NOT NULL,
  predecessor_code       text         NOT NULL,
  predecessor_valid_from date         NOT NULL,
  successor_code         text         NOT NULL,
  successor_valid_from   date         NOT NULL,
  effective_on           date         NOT NULL,
  -- Tỉ lệ diện tích tiền thân chuyển sang kế thừa; NULL = chưa biết (dữ liệu từ
  -- successor_codes). Tổng theo tiền thân ≤ 1 kiểm ở masterdata/app khi ghi.
  area_share             numeric(9,6) NULL CHECK (area_share > 0 AND area_share <= 1),
  legal_basis            text         NOT NULL CHECK (length(legal_basis) BETWEEN 1 AND 300),
  basis_status           text         NOT NULL CHECK (basis_status IN ('official', 'pending_c9')),
  actor_ref              uuid         NOT NULL,
  recorded_at            timestamptz  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, predecessor_code, predecessor_valid_from, successor_code, basis_status),
  FOREIGN KEY (tenant_id, predecessor_code, predecessor_valid_from) REFERENCES pf_masterdata.admin_unit (tenant_id, code, valid_from),
  FOREIGN KEY (tenant_id, successor_code, successor_valid_from)     REFERENCES pf_masterdata.admin_unit (tenant_id, code, valid_from),
  CHECK (successor_valid_from <= effective_on)
);
-- Tra ngược (đơn vị mới ← đơn vị cũ) và tra theo ngày hiệu lực.
CREATE INDEX admin_unit_succession_successor ON pf_masterdata.admin_unit_succession (tenant_id, successor_code, successor_valid_from);
CREATE INDEX admin_unit_succession_effective ON pf_masterdata.admin_unit_succession (tenant_id, effective_on);

-- Tiền thân phải là phiên bản đã đóng với valid_to = effective_on; kế thừa phải có
-- hiệu lực ngày effective_on. Đọc theo NEW.tenant_id (dưới RLS chỉ thấy tenant hiện hành,
-- cũng là tenant được WITH CHECK cho phép ghi).
CREATE FUNCTION pf_masterdata.fn_admin_unit_succession_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pf_masterdata.admin_unit p
     WHERE p.tenant_id = NEW.tenant_id AND p.code = NEW.predecessor_code
       AND p.valid_from = NEW.predecessor_valid_from AND p.valid_to = NEW.effective_on) THEN
    RAISE EXCEPTION 'admin_unit_succession %/%: tiền thân phải đã đóng hiệu lực đúng ngày %',
      NEW.predecessor_code, NEW.predecessor_valid_from, NEW.effective_on
      USING ERRCODE = 'check_violation';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pf_masterdata.admin_unit s
     WHERE s.tenant_id = NEW.tenant_id AND s.code = NEW.successor_code
       AND s.valid_from = NEW.successor_valid_from AND s.valid_from <= NEW.effective_on
       AND (s.valid_to IS NULL OR s.valid_to > NEW.effective_on)) THEN
    RAISE EXCEPTION 'admin_unit_succession %/%: kế thừa không có hiệu lực ngày %',
      NEW.successor_code, NEW.successor_valid_from, NEW.effective_on
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_admin_unit_succession_guard BEFORE INSERT ON pf_masterdata.admin_unit_succession
  FOR EACH ROW EXECUTE FUNCTION pf_masterdata.fn_admin_unit_succession_guard();

-- ---------------------------------------------------------------------------
-- 3. Điền từ successor_codes (ADR-0032 mục 4)
-- ---------------------------------------------------------------------------
-- ag_migrator là chủ bảng nhưng bị FORCE RLS và không BYPASSRLS: chưa đặt app.tenant_id
-- thì không thấy dòng nào. Một policy CHỈ ĐỌC tạm cho chính vai chạy migration mở
-- admin_unit trong lúc điền, rồi bỏ ngay trong cùng giao dịch của tệp này (policy
-- p_tenant không đổi; shared.v_missing_rls vẫn thấy đúng một policy). Bảng kế thừa chưa
-- bật RLS lúc INSERT, bật ở mục 4.
-- Mã kế thừa không có phiên bản hiệu lực tại valid_to (dữ liệu lỗi) bị bỏ qua — cùng
-- quy tắc ResolveCurrent đang dùng.
CREATE POLICY p_migrate_000089 ON pf_masterdata.admin_unit FOR SELECT TO CURRENT_USER USING (true);

-- backfill:begin
INSERT INTO pf_masterdata.admin_unit_succession
  (tenant_id, predecessor_code, predecessor_valid_from, successor_code, successor_valid_from,
   effective_on, area_share, legal_basis, basis_status, actor_ref)
SELECT p.tenant_id, p.code, p.valid_from, s.code, s.valid_from,
       p.valid_to, NULL, 'successor_codes (pf 000002)', 'pending_c9', coalesce(p.closed_by, p.actor_ref)
  FROM pf_masterdata.admin_unit p
  CROSS JOIN LATERAL unnest(p.successor_codes) AS sc(code)
  JOIN pf_masterdata.admin_unit s
    ON s.tenant_id = p.tenant_id AND s.code = sc.code
   AND s.valid_from <= p.valid_to AND (s.valid_to IS NULL OR s.valid_to > p.valid_to)
 WHERE p.valid_to IS NOT NULL AND cardinality(p.successor_codes) > 0
ON CONFLICT DO NOTHING;
-- backfill:end

DROP POLICY p_migrate_000089 ON pf_masterdata.admin_unit;

-- ---------------------------------------------------------------------------
-- 4. Bất biến, RLS, quyền
-- ---------------------------------------------------------------------------
-- shared.apply_append_only: trigger shared.fn_immutable chặn UPDATE/DELETE/TRUNCATE (áp cả
-- owner) + RLS p_tenant. Sửa sai bằng dòng 'official' cùng cặp; truy vấn đọc ưu tiên
-- 'official' hơn 'pending_c9'.
SELECT shared.apply_append_only('pf_masterdata.admin_unit_succession');

GRANT SELECT, INSERT ON pf_masterdata.admin_unit_succession TO masterdata_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_masterdata.admin_unit_succession FROM masterdata_app;
