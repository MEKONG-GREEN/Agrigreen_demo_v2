SET LOCAL lock_timeout = '5s';

-- P0-POOL-SHARED-2 (#143, bàn giao từ #139): "Hủy kỳ" KHTC không giữ giao dịch DB quanh lời gọi cap/forms CancelPeriod.
--
-- Trước: GuardCancel mở giao dịch giữ khóa tư vấn khtc_snapshot:<tenant>:<kỳ> rồi — TRONG giao dịch đó — đọc kỳ (giao dịch
-- riêng), đọc Bản cuối (giao dịch riêng) và hủy kỳ native ở cap/forms (giao dịch riêng): một yêu cầu giữ một kết nối trong
-- khi chờ kết nối thứ hai, N yêu cầu song song với N = cỡ pool treo cả API. Khóa phải bao trùm việc hủy ở cap/forms để
-- "Bản cuối" không chen vào giữa (cap/forms đã hủy nhưng sự kiện hủy KHTC bị trigger Bản cuối chặn).
--
-- Sau: thay khóa giữ suốt bằng Ý ĐỊNH HỦY bền (bảng trạng thái, một dòng/kỳ):
--   1. giao dịch ngắn (khóa khtc_snapshot): chưa có Bản cuối → ghi claim_id + claim_until (hạn), commit;
--   2. hủy kỳ native ở cap/forms NGOÀI giao dịch;
--   3. giao dịch ngắn (khóa): ghi sự kiện 'cancelled' + xóa ý định (claim_id = NULL). Lỗi ở 2 → xóa ý định.
-- Trong lúc ý định còn hạn: trigger chặn Bản cuối (khtc_report_final) và chặn đóng kỳ ('closed') — đúng như khóa cũ chặn
-- (cả hai đã lấy cùng khóa tư vấn, nên kiểm ý định ở đây là tuần tự với bước 1/3). Tiến trình chết giữa chừng → hết hạn ý
-- định thì Bản cuối/đóng kỳ chạy lại được; "Hủy kỳ" lần nữa hoàn tất (cap/forms đã hủy → bỏ qua lỗi "đã hủy").
-- Hạn tính bằng giờ DB (now()), không theo đồng hồ ứng dụng.

CREATE TABLE pf_extension.khtc_cancel_intent (
  tenant_id   uuid        NOT NULL,
  period_id   uuid        NOT NULL,
  claim_id    uuid        NULL,
  claim_until timestamptz NULL,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, period_id),
  CONSTRAINT khtc_cancel_intent_claim CHECK ((claim_id IS NULL) = (claim_until IS NULL))
);
SELECT shared.apply_tenant_rls('pf_extension.khtc_cancel_intent');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_extension.khtc_cancel_intent
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.khtc_cancel_intent
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
GRANT SELECT, INSERT, UPDATE ON pf_extension.khtc_cancel_intent TO r_extension;

-- Ý định hủy còn hạn của kỳ (gọi từ trigger, đã giữ khóa khtc_snapshot).
CREATE FUNCTION pf_extension.fn_khtc_cancel_pending(p_tenant uuid, p_period uuid) RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, pg_temp AS $$
  SELECT EXISTS (SELECT 1 FROM pf_extension.khtc_cancel_intent i
                  WHERE i.tenant_id = p_tenant AND i.period_id = p_period AND i.claim_until > now())
$$;
REVOKE ALL ON FUNCTION pf_extension.fn_khtc_cancel_pending(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_extension.fn_khtc_cancel_pending(uuid, uuid) TO r_extension;

-- Bản nháp/xuất/Bản cuối: như 20261015223036, thêm chặn Bản cuối khi đang hủy kỳ.
CREATE OR REPLACE FUNCTION pf_extension.fn_khtc_report_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_final boolean;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id) THEN
    RAISE EXCEPTION 'missing KHTC period' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_report_period_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id = NEW.tenant_id AND e.period_id = NEW.period_id AND e.action = 'cancelled') THEN
    RAISE EXCEPTION 'cancelled KHTC period' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_cancelled';
  END IF;
  SELECT EXISTS(SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id) INTO v_final;
  IF v_final THEN
    RAISE EXCEPTION 'final KHTC report is immutable' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_already_final';
  END IF;
  IF TG_TABLE_NAME LIKE 'khtc_report_export%' THEN
    IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_report_draft d WHERE d.tenant_id = NEW.tenant_id AND d.period_id = NEW.period_id
                    AND d.id = NEW.draft_id AND d.input_hash = NEW.input_hash AND d.payload = NEW.payload) THEN
      RAISE EXCEPTION 'export must reference an immutable report draft' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_export_draft';
    END IF;
  ELSIF TG_TABLE_NAME LIKE 'khtc_report_final%' THEN
    IF pf_extension.fn_khtc_cancel_pending(NEW.tenant_id, NEW.period_id) THEN
      RAISE EXCEPTION 'KHTC period is being cancelled' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_cancelling';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_report_export x WHERE x.tenant_id = NEW.tenant_id AND x.period_id = NEW.period_id
                    AND x.id = NEW.export_id AND NOT x.provisional) THEN
      RAISE EXCEPTION 'provisional or missing export cannot become final' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_final_export';
    END IF;
  END IF;
  RETURN NEW;
END $$;

-- Sự kiện kỳ: như 20261015223036, thêm chặn đóng kỳ khi đang hủy kỳ.
CREATE OR REPLACE FUNCTION pf_extension.fn_khtc_report_period_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NEW.action IN ('closed', 'cancelled') THEN
    PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  END IF;
  IF NEW.action = 'cancelled' AND EXISTS (SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id) THEN
    RAISE EXCEPTION 'final KHTC report prevents cancellation' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_final_cancel';
  END IF;
  IF NEW.action = 'closed' AND pf_extension.fn_khtc_cancel_pending(NEW.tenant_id, NEW.period_id) THEN
    RAISE EXCEPTION 'KHTC period is being cancelled' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_period_cancelling';
  END IF;
  RETURN NEW;
END $$;
