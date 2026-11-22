-- HC-0 / HCTH v4.0 FN-16/17/20: không còn tài khoản tự đánh giá.
-- Event cũ giữ nguyên và vẫn được đọc bởi HCTH/lãnh đạo trong phạm vi.
-- Sổ đã cấm UPDATE/DELETE; trigger chỉ thu quyền INSERT của hai thao tác đã bỏ,
-- kể cả caller cũ đi thẳng vào DB, không phụ thuộc phiên bản API.
CREATE FUNCTION pf_hrm.reject_removed_self_appraisal() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'HCTH v4.0: self appraisal writes have been removed'
    USING ERRCODE = '42501';
END;
$$;

REVOKE ALL ON FUNCTION pf_hrm.reject_removed_self_appraisal() FROM PUBLIC;

CREATE TRIGGER appraisal_no_self_write
BEFORE INSERT ON pf_hrm.appraisal_event
FOR EACH ROW WHEN (NEW.action IN ('self_draft', 'self_submit'))
EXECUTE FUNCTION pf_hrm.reject_removed_self_appraisal();
