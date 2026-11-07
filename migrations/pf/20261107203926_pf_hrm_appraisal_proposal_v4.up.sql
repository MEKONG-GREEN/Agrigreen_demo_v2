-- HC-2 / HCTH v4.0 FN16–18: chỉ thêm hàng rào ghi, không cập nhật/xóa dữ liệu lịch sử.
-- Sổ cũ giữ SELECT và chuỗi băm; chặn mọi writer tiêu chí, phiếu điểm và bước tự nộp còn sót.
CREATE FUNCTION pf_hrm.guard_appraisal_proposal_v4() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.entity_kind = 'form'
     OR NEW.action IN ('proxy_self', 'manager_draft', 'lock_self', 'unlock_self', 'lock_manager', 'unlock_manager')
     OR (NEW.entity_kind = 'export' AND NEW.payload->>'kind' = 'PHIEU_DIEM')
     OR (NEW.entity_kind = 'period' AND
         (COALESCE(NEW.payload->>'workflow_version', '') <> 'hrm.appraisal-proposal/v4'
          OR COALESCE(NEW.payload->'forms', '{}'::jsonb) <> '{}'::jsonb))
     OR (NEW.entity_kind = 'entry' AND
         (COALESCE(NEW.payload->>'status', '') NOT IN ('not_started', 'entered', 'sent', 'change_requested', 'closed')
          OR COALESCE(NEW.payload#>>'{self,total}', '') <> ''
          OR COALESCE(NEW.payload#>>'{manager,total}', '') <> ''
          OR COALESCE(NEW.payload#>>'{self,suggested}', '') <> ''
          OR COALESCE(NEW.payload#>>'{manager,suggested}', '') <> ''
          OR COALESCE(NEW.payload#>'{self,answers}', 'null'::jsonb) NOT IN ('null'::jsonb, '[]'::jsonb)
          OR COALESCE(NEW.payload#>'{manager,answers}', 'null'::jsonb) NOT IN ('null'::jsonb, '[]'::jsonb)))
  THEN
    RAISE EXCEPTION 'HCTH v4.0: scoring and legacy appraisal writes are read-only'
      USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION pf_hrm.guard_appraisal_proposal_v4() FROM PUBLIC;
CREATE TRIGGER appraisal_proposal_v4_write
BEFORE INSERT ON pf_hrm.appraisal_event
FOR EACH ROW EXECUTE FUNCTION pf_hrm.guard_appraisal_proposal_v4();

-- Kết quả FN18 nằm trong appraisal_event; annual_rating cũ chỉ còn đọc.
REVOKE INSERT ON pf_hrm.annual_rating FROM r_hrm;
