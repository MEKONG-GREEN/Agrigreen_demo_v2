-- Review Go 03/10/2026 (M7): thu hồi liên kết tải Chi cục là MỘT CHIỀU. 000085 cấp reports_app quyền UPDATE ba cột
-- thu hồi (revoked_at, revoked_by, revoke_reason) và CHECK chỉ buộc ba cột cùng NULL/cùng có — vẫn đặt lại được về
-- NULL (mở lại liên kết đã thu hồi) hoặc đổi người/lý do thu hồi. Trigger dưới đây chỉ cho NULL → có giá trị, một lần.

CREATE FUNCTION pf_reports.fn_dispatch_link_revoke_oneway() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
      OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by OR NEW.revoke_reason IS DISTINCT FROM OLD.revoke_reason) THEN
    RAISE EXCEPTION 'pf_reports.dispatch_link: liên kết đã thu hồi không được sửa/mở lại (id %)', OLD.id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END
$$;

CREATE TRIGGER trg_revoke_oneway BEFORE UPDATE ON pf_reports.dispatch_link
  FOR EACH ROW EXECUTE FUNCTION pf_reports.fn_dispatch_link_revoke_oneway();
