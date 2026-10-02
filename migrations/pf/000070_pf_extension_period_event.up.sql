-- pf_extension KN v7.7 P4 (3/4): chốt kỳ và mở lại kỳ là SỰ KIỆN BÙ, không sửa số liệu (FN-TI-02.04;
-- docs/brd/kn-v7.7-plan.md §2 pf 000070, gap §4.1). Không đụng pf_extension.period_close (000006, kỳ
-- YYYY-MM của G1 — giữ nguyên cho báo cáo hiện trường cũ).
--
--   closed       Cấp Tỉnh "Chốt kỳ ngay"
--   auto_closed  hệ thống chốt theo lịch (hạn + số ngày chốt sau hạn) — người = Hệ thống
--   reopened     mở lại cho một số xã/biểu {lý do ≥ 10 ký tự, hạn mở}; số liệu sau mở lại là phiên bản mới
--   reclosed     tự chốt lại khi hết hạn mở (hoặc Cấp Tỉnh chốt lại)
--
-- Trạng thái "đang chốt" là phép chiếu: report_period.status (cả kỳ) + report_task.reopened_until (xã × biểu).

CREATE TABLE pf_extension.period_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'period:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  batch_id          uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('closed', 'auto_closed', 'reopened', 'reclosed')),
  communes          text[]      NOT NULL DEFAULT '{}',    -- reopened/reclosed: phạm vi xã ('{}' = cả kỳ)
  template_ids      uuid[]      NOT NULL DEFAULT '{}',    -- reopened/reclosed: phạm vi biểu ('{}' = mọi biểu)
  task_ids          uuid[]      NOT NULL DEFAULT '{}',
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  reopen_until      timestamptz NULL,
  department_code   text        NOT NULL,
  on_behalf_of      uuid        NULL,
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
  -- Mở lại kỳ: bắt buộc lý do và thời hạn mở.
  CHECK (action <> 'reopened' OR (reason IS NOT NULL AND reopen_until IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_event_period ON pf_extension.period_event (tenant_id, period_id, id);
SELECT shared.apply_ledger('pf_extension.period_event', 'pf_extension.period_event');

-- Job tự chốt lại tìm công việc có hạn mở đã qua.
CREATE INDEX report_task_reopened ON pf_extension.report_task (tenant_id, reopened_until) WHERE reopened_until IS NOT NULL;

-- Job vòng đời báo cáo (worker, leader) cho MỌI tenant: ag_outbox_publisher chỉ đọc hai cột để tìm tenant có kỳ
-- còn mở (tự chốt, tự nhắc) — ghi bằng vai extension_app (mẫu pf 000006).
GRANT SELECT (tenant_id, status) ON pf_extension.report_period TO ag_outbox_publisher;

GRANT SELECT, INSERT ON pf_extension.period_event TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.period_event FROM extension_app;
