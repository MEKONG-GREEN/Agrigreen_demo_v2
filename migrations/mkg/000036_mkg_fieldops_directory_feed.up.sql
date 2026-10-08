-- mkg_fieldops W5.5 (ADR-0033 §2, §5; kiến trúc v4.1 §41.4, R-INT-02): bộ điều hợp FEED của danh bạ HTX/thửa — MKG
-- kéo GET /v1/feeds/cooperative_directory, /plot_directory trên cổng tích hợp (khóa API, như mọi đối tác) thay
-- /v1/partner/cooperatives, /plots. Chạy SONG SONG một kỳ phát hành với bản sao cũ (mkg 000033): cờ
-- AG_FIELDOPS_DIRECTORY_SOURCE chọn nguồn mà lập/sửa lệnh gom đối chiếu; job fieldops_directory_feed ghi log khác biệt
-- giữa hai nguồn (đối soát trước W5.10).
--
--   directory_feed_state   một dòng mỗi feed × tenant MKG: cursor (mờ, do cổng cấp) đã áp tới đâu, contract_version,
--                          as_of + max_age của trang cuối khi đã đọc tới đầu feed (caught_up), revoked_at (cổng trả 403
--                          party.share_grant_missing — bản ghi đã xóa, mục coi là "không có"), lỗi gần nhất.
--                          Quá max_age → lệnh gom gắn cờ fieldops.directory_stale (không chặn), job ghi cảnh báo.
--   directory_feed_record  trạng thái hiện hành dựng từ feed (một dòng mỗi entity_key = ref): bản ghi theo hợp đồng
--                          contracts/integration/feeds/<feed>.v<N> (jsonb, đã kiểm ở cổng), seq + event_id của mục đã áp
--                          (áp lại cùng mục là no-op — giao ít nhất một lần, idempotent theo seq/event_id). retract → xóa
--                          dòng. Bảng ĐỆM dựng lại được từ feed (đặt lại cursor), không phải nguồn sự thật.
--
-- Số 000036 (bản kế hoạch đầu ghi mkg 000024 — đã lỗi thời: 000023…000032 dành theo thứ tự gộp, 000033–000035 đã dùng).
-- Không dữ liệu cá nhân (R-04): hợp đồng chỉ có mã, tên HTX, xã, trạng thái, diện tích.

CREATE TABLE mkg_fieldops.directory_feed_state (
  tenant_id         uuid        NOT NULL,
  feed              text        NOT NULL CHECK (feed IN ('cooperative_directory', 'plot_directory')),
  feed_cursor       text        NULL CHECK (feed_cursor IS NULL OR char_length(feed_cursor) BETWEEN 1 AND 200),
  contract_version  integer     NULL CHECK (contract_version IS NULL OR contract_version >= 1),
  caught_up         boolean     NOT NULL DEFAULT false,
  as_of             timestamptz NULL,                    -- mốc dữ liệu của trang cuối khi caught_up (cổng trả)
  max_age_seconds   integer     NULL CHECK (max_age_seconds IS NULL OR max_age_seconds > 0),
  revoked_at        timestamptz NULL,
  last_pulled_at    timestamptz NULL,
  last_error        text        NULL CHECK (last_error IS NULL OR char_length(last_error) <= 300),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, feed),
  CHECK (NOT caught_up OR as_of IS NOT NULL),
  CHECK (revoked_at IS NULL OR NOT caught_up)
);
SELECT shared.apply_tenant_rls('mkg_fieldops.directory_feed_state');

CREATE TABLE mkg_fieldops.directory_feed_record (
  tenant_id    uuid        NOT NULL,
  feed         text        NOT NULL CHECK (feed IN ('cooperative_directory', 'plot_directory')),
  ref          uuid        NOT NULL,                     -- entity_key của hợp đồng (cooperative_ref | plot_ref)
  seq          bigint      NOT NULL CHECK (seq >= 1),
  event_id     uuid        NOT NULL,
  record       jsonb       NOT NULL CHECK (jsonb_typeof(record) = 'object'),
  recorded_at  timestamptz NOT NULL,                     -- recorded_at của mục feed
  applied_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, feed, ref)
);
SELECT shared.apply_tenant_rls('mkg_fieldops.directory_feed_record');

-- Bảng đệm: UPDATE (áp mục mới / trạng thái), DELETE (retract, thu hồi, đồng bộ lại). Vai nhóm của module (W2.6).
REVOKE ALL ON mkg_fieldops.directory_feed_state, mkg_fieldops.directory_feed_record FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_fieldops.directory_feed_state, mkg_fieldops.directory_feed_record TO r_fieldops;
