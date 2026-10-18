-- pf_extension: bảng số liệu phiếu "Báo cáo cho KHTC" (W11.63 rút gọn — BRD KHTC v2.2 FN-PN-03.02, FN-TI-02.04, BR-DL-05,
-- BR-DL-10, BR-DL-12; D4 KH3). Chỉ đụng schema pf_extension.
--
-- Phiếu của một phòng trong một kỳ KHTC là MỘT đơn vị của kỳ native cap/forms (profile kn_khtc — W11.62): nộp, giới hạn
-- 3 lần gửi, yêu cầu bổ sung (trả lại) / xác nhận (chấp nhận) bốn mắt, Không gửi / Đã khóa lúc đóng kỳ đều ở pf_forms.
-- Biểu cap/forms chỉ có kiểu trường phẳng (text ≤ 2000 ký tự), không chứa được lưới hạng mục × cột theo Kế hoạch năm, nên
-- lưới nằm ở sổ dưới đây và bài nộp cap/forms tham chiếu nó bằng trường "bang_so_lieu" (mã bảng đã niêm phong):
--
--   khtc_form_sheet — SỔ (append-only, shared.apply_ledger), luồng 'khtc_form_sheet:<kỳ KHTC>:<phòng>':
--     kind = 'draft'  — "Lưu nháp" ở máy chủ (không giới hạn; bản cuối là nháp hiện hành, không tính vào báo cáo);
--     kind = 'sealed' — bảng đã kiểm 3 quy tắc, niêm phong lúc "Gửi KHTC"; bài nộp cap/forms mang mã bảng. Bảng niêm
--       phong mà bài nộp không thành (lỗi giữa hai giao dịch) là bảng mồ côi: không bài nộp nào trỏ tới nên không vào
--       báo cáo; chỉ làm hạng mục được coi "đã có số liệu" (thận trọng — hạng mục chỉ ngừng, không xóa).
--   content: {"cells": {<entry_ref>: {"done", "status", "content", "people", "budget"}}, "adjustments": [{"entry_ref",
--     "period_id", "period_year", "amount", "reason"}], "texts": {<mục>: "…"}} — số là chuỗi thập phân (R-10).
--   Không ghi khi kỳ KHTC đã đóng hoặc đã hủy (trigger — Mục II.5.2: kỳ đóng không nhập, không gửi).

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.khtc_form_sheet (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11); mã bảng ghi vào bài nộp cap/forms
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'khtc_form_sheet:<period_id>:<department>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,                 -- kỳ KHTC (pf_extension.khtc_period)
  forms_period_ref  uuid        NOT NULL,                 -- kỳ native cap/forms của kỳ KHTC (không khóa ngoại liên schema)
  department        text        NOT NULL CHECK (department ~ '^[a-z][a-z0-9_]{0,39}$'),
  kind              text        NOT NULL CHECK (kind IN ('draft', 'sealed')),
  plan_year         integer     NOT NULL CHECK (plan_year BETWEEN 2000 AND 2100),
  template_code     text        NOT NULL CHECK (template_code ~ '^[A-Z0-9][A-Z0-9._-]{0,63}$'),
  template_version  integer     NOT NULL CHECK (template_version >= 1),
  plan_version_ref  uuid        NULL,                     -- phiên bản Kế hoạch năm dựng dòng phiếu (NULL: chưa có kế hoạch)
  base_attempt      integer     NOT NULL CHECK (base_attempt BETWEEN 0 AND 20), -- số lần đã gửi khi lưu/niêm phong
  content           jsonb       NOT NULL CHECK (jsonb_typeof(content) = 'object' AND pg_column_size(content) <= 1048576),
  content_hash      bytea       NOT NULL CHECK (length(content_hash) = 32), -- SHA-256 nội dung chuẩn hóa (ứng dụng tính)
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT khtc_form_sheet_stream CHECK (stream_key = 'khtc_form_sheet:' || period_id::text || ':' || department)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_form_sheet_stream ON pf_extension.khtc_form_sheet (tenant_id, period_id, department, seq DESC);
CREATE INDEX khtc_form_sheet_id     ON pf_extension.khtc_form_sheet (tenant_id, id);
CREATE INDEX khtc_form_sheet_year   ON pf_extension.khtc_form_sheet (tenant_id, plan_year, kind);

-- Bảng thuộc một kỳ KHTC có thật của tenant, phòng tham gia kỳ, cùng kỳ native và năm kế hoạch; kỳ đã đóng/hủy → không ghi.
CREATE FUNCTION pf_extension.fn_khtc_form_sheet_bind() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p
                  WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id AND p.forms_period_ref = NEW.forms_period_ref
                    AND p.plan_year = NEW.plan_year AND NEW.department = ANY (p.departments)) THEN
    RAISE EXCEPTION 'pf_extension: bảng số liệu không thuộc kỳ KHTC % / phòng %', NEW.period_id, NEW.department
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_form_sheet_period_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e
              WHERE e.tenant_id = NEW.tenant_id AND e.period_id = NEW.period_id AND e.action IN ('closed', 'cancelled')) THEN
    RAISE EXCEPTION 'pf_extension: kỳ KHTC % đã đóng hoặc đã hủy', NEW.period_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_form_sheet_period_terminal';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_form_sheet_bind BEFORE INSERT ON pf_extension.khtc_form_sheet
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_form_sheet_bind();

SELECT shared.apply_ledger('pf_extension.khtc_form_sheet', 'pf_extension.khtc_form_sheet');

GRANT SELECT, INSERT ON pf_extension.khtc_form_sheet TO r_extension;
