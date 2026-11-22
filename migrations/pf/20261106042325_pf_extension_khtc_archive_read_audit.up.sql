-- W11.69-ARCHIVE-POLICY-LEGACY (#227): append-only read-access attempts.
-- Metadata/preview entries do not claim successful network delivery or a
-- measured serialized response size. Existing Word/ZIP semantics remain intact.
SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_extension.khtc_archive_access
 DROP CONSTRAINT khtc_archive_access_action_check,
 ADD CONSTRAINT khtc_archive_access_action_check CHECK(action IN(
  'word_list','word_attempt','zip_entry_attempt','zip_attempt',
  'read_list','read_periods','read_fields','read_history','preview_attempt','excel_attempt')) NOT VALID,
 DROP CONSTRAINT khtc_archive_access_format_check,
 ADD CONSTRAINT khtc_archive_access_format_check CHECK(format IN('excel','word','zip','metadata','preview')) NOT VALID,
 ADD CONSTRAINT khtc_archive_access_metadata_check CHECK(
  action NOT IN('read_list','read_periods','read_fields','read_history') OR
  (format='metadata' AND expected_bytes=0 AND file_count=0 AND export_id IS NULL AND run_id IS NULL AND jsonb_array_length(targets)=0)) NOT VALID,
 ADD CONSTRAINT khtc_archive_access_history_scope_check CHECK(action<>'read_history' OR period_id IS NOT NULL) NOT VALID,
 ADD CONSTRAINT khtc_archive_access_preview_scope_check CHECK(action<>'preview_attempt' OR
  (format='preview' AND period_id IS NOT NULL AND export_id IS NOT NULL AND run_id IS NULL AND expected_bytes=0 AND file_count=1 AND jsonb_array_length(targets)=1)) NOT VALID,
 ADD CONSTRAINT khtc_archive_access_excel_scope_check CHECK(action<>'excel_attempt' OR
  (format='excel' AND period_id IS NOT NULL AND export_id IS NOT NULL AND run_id IS NULL AND expected_bytes>0 AND file_count=1 AND jsonb_array_length(targets)=1)) NOT VALID,
 ADD CONSTRAINT khtc_archive_access_read_rule_check CHECK(
  action NOT IN('read_list','read_periods','read_fields','read_history','preview_attempt','excel_attempt') OR rule_version='archive-read-v1') NOT VALID;

-- R-25: validate separately to avoid a long exclusive lock.
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_action_check;
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_format_check;
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_metadata_check;
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_history_scope_check;
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_preview_scope_check;
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_excel_scope_check;
ALTER TABLE pf_extension.khtc_archive_access VALIDATE CONSTRAINT khtc_archive_access_read_rule_check;
