SET LOCAL lock_timeout = '5s';

-- W11.46 / CNTS-ROSTER-SCOPE-001: tách luồng lịch trực theo tỉnh trong cùng tenant.
-- Giữ nguyên mọi dòng và chuỗi băm cũ; không thêm cột vào sổ. Khóa cũ chỉ đọc qua
-- chế độ legacy của Admin; mọi ghi mới dùng duty:<province>:<YYYY-MM>.
ALTER TABLE pf_extension.cnts_duty_roster
  DROP CONSTRAINT cnts_duty_roster_stream;
ALTER TABLE pf_extension.cnts_duty_roster
  ADD CONSTRAINT cnts_duty_roster_stream CHECK (
    stream_key = 'duty:' || to_char(month, 'YYYY-MM')
    OR stream_key ~ ('^duty:[A-Za-z0-9._-]{1,20}:' || to_char(month, 'YYYY-MM') || '$')
  ) NOT VALID;
ALTER TABLE pf_extension.cnts_duty_roster VALIDATE CONSTRAINT cnts_duty_roster_stream;
