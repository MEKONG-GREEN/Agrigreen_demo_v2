-- pf_forms: cờ "Nộp trễ" của bài nộp tính theo GIỜ MÁY CHỦ NHẬN BÀI (D8 — quyết định CEO 13/10/2026, gói lịch tuần
-- theo phòng ban; sửa giả định W11.42 "late tính theo captured_at của bản offline").
--
-- Trước: submission_meta.late = coalesce(captured_at, submission.event_time) > period.due_at — bản offline ghi trên
-- thiết bị trước hạn nhưng đồng bộ sau hạn được coi là đúng hạn, và giờ máy sai/chỉnh tay đổi được cờ.
-- Sau:   late = submission.event_time > period.due_at (event_time = giờ máy chủ ghi phiên bản). captured_at (giờ thiết
--        bị) vẫn ghi ở submission_meta để kiểm toán. App (pf/forms SubmitWith) đòi lý do trễ theo cùng giờ máy chủ.
-- Chỉ thay thân hàm trigger (CREATE OR REPLACE) — không đổi dòng đã ghi (sổ chỉ ghi thêm; dòng cũ giữ cờ đã tính).

SET LOCAL lock_timeout = '5s';

CREATE OR REPLACE FUNCTION pf_forms.fn_bind_meta() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  s record;
  v_due timestamptz;
BEGIN
  SELECT x.period_id, x.unit_code, x.profile, x.seq, x.event_time, x.recorded_at INTO s
    FROM pf_forms.submission x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.submission_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có bài nộp % trong tenant', NEW.submission_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'submission_meta_submission_fkey';
  END IF;
  IF s.recorded_at <> now() THEN
    RAISE EXCEPTION 'pf_forms: thông tin bài nộp % phải ghi cùng giao dịch nộp', NEW.submission_id USING ERRCODE = 'AGF01';
  END IF;
  IF NEW.base_version <> s.seq - 1 THEN
    RAISE EXCEPTION 'pf_forms: bài nộp dựa trên phiên bản % nhưng đơn vị % đã có phiên bản %', NEW.base_version, s.unit_code, s.seq - 1
      USING ERRCODE = 'AGF04';
  END IF;
  SELECT p.due_at INTO v_due FROM pf_forms.period p WHERE p.tenant_id = NEW.tenant_id AND p.id = s.period_id;
  NEW.period_id := s.period_id;
  NEW.unit_code := s.unit_code;
  NEW.profile   := s.profile;
  -- D8: trễ hạn theo giờ MÁY CHỦ nhận bài (event_time của phiên bản), không theo captured_at (giờ thiết bị —
  -- có thể sai giờ máy hoặc bị chỉnh); captured_at vẫn lưu để kiểm toán.
  NEW.late      := s.event_time > v_due;
  RETURN NEW;
END $$;
