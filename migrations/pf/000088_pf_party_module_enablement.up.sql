-- pf_party: bật module theo đơn vị tham gia (W1.4, R-MOD-08 của kiến trúc v4.1 §7.3, ADR-0027 mục 6).
--
-- "Đơn vị tham gia" của v4.1 là tenant (RB-2: cách ly theo đơn vị tham gia, tenant_id ở mọi bảng
-- nghiệp vụ), khớp chính sách `enable: per_tenant` của module.yaml. Khóa là (tenant_id, module).
-- Module `enable: always` (lõi, mkg/*, pf/extdata) không có dòng ở đây — kiểm ở app/ qua modreg.
--
-- MẶC ĐỊNH BẬT: không có dòng = đang bật. Không cần nạp dữ liệu: mọi tenant hiện có (và tenant mới)
-- giữ nguyên hành vi trước W1.4; chỉ khi quản trị TẮT một module mới có dòng 'disabled'. Bật lại ghi
-- thêm dòng 'enabled' (sổ không xóa).
--
-- Mẫu như department_event (000062): sổ chỉ ghi thêm (seq, chuỗi băm, bất biến — shared.apply_ledger)
-- + bảng chiếu hiện hành cập nhật trong CÙNG giao dịch (R-21). Không phê duyệt hai người: cùng mức với
-- trạng thái phòng ban (Admin API, MFA, lý do bắt buộc khi tắt). Không dữ liệu cá nhân (R-04).

CREATE TABLE pf_party.module_enablement_event (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'module:<module>'
  seq               bigint      NOT NULL,
  module            text        NOT NULL CHECK (module ~ '^(pf|mkg|vault)/[a-z][a-z0-9_]{0,39}$'),
  status            text        NOT NULL CHECK (status IN ('enabled', 'disabled')),
  prev_status       text        NOT NULL CHECK (prev_status IN ('enabled', 'disabled')), -- 'enabled' khi chưa có dòng (mặc định)
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
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
  CHECK (status <> prev_status),
  -- Tắt module phải có căn cứ (người dùng mất menu và quyền của module).
  CHECK (status <> 'disabled' OR char_length(btrim(coalesce(reason, ''))) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX module_enablement_event_stream ON pf_party.module_enablement_event (tenant_id, stream_key, seq);
CREATE INDEX module_enablement_event_idem   ON pf_party.module_enablement_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_party.module_enablement_event', 'pf_party.module_enablement_event');

CREATE TABLE pf_party.module_enablement_current (
  tenant_id     uuid        NOT NULL,
  module        text        NOT NULL CHECK (module ~ '^(pf|mkg|vault)/[a-z][a-z0-9_]{0,39}$'),
  status        text        NOT NULL CHECK (status IN ('enabled', 'disabled')),
  reason        text        NULL,
  last_event_id uuid        NOT NULL,
  last_seq      bigint      NOT NULL,
  updated_by    uuid        NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, module)
);
SELECT shared.apply_tenant_rls('pf_party.module_enablement_current');

-- Quyền của party_app (R-05). Sổ: chỉ SELECT, INSERT. Bảng chiếu: không DELETE.
GRANT SELECT, INSERT ON pf_party.module_enablement_event TO party_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.module_enablement_event FROM party_app;
GRANT SELECT, INSERT, UPDATE ON pf_party.module_enablement_current TO party_app;
REVOKE DELETE, TRUNCATE ON pf_party.module_enablement_current FROM party_app;
