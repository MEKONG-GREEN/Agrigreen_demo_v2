-- pf_content: danh mục cá nhân "đã lưu" của người đọc (KN US-LIB-05: lưu tài liệu
-- để đọc ngoại tuyến — "tải về").
--
-- Bảng trạng thái theo (người dùng, tài liệu): lưu → saved = true; bỏ lưu →
-- saved = false (không xóa cứng). Mỗi lần đổi lấy sync_seq mới để pf/sync giao
-- xuống máy của CHÍNH người đó (entity library_item); tài liệu đã lưu luôn nằm
-- trong entity document của người đó (tài liệu chỉ lưu được khi đang hiển thị với họ).
-- Không có dữ liệu cá nhân: actor_ref là mã tham chiếu tài khoản.

CREATE SEQUENCE pf_content.library_sync_seq;

CREATE TABLE pf_content.library_item (
  tenant_id    uuid        NOT NULL,
  actor_ref    uuid        NOT NULL,
  document_ref uuid        NOT NULL,
  saved        boolean     NOT NULL,
  note         text        NULL CHECK (note IS NULL OR length(note) <= 500),
  event_time   timestamptz NOT NULL,                   -- lúc lưu / bỏ lưu (R-13)
  recorded_at  timestamptz NOT NULL DEFAULT now(),     -- lúc ghi nhận lần đầu
  updated_at   timestamptz NOT NULL DEFAULT now(),
  sync_seq     bigint      NOT NULL DEFAULT nextval('pf_content.library_sync_seq'),
  PRIMARY KEY (tenant_id, actor_ref, document_ref)
);
CREATE UNIQUE INDEX library_item_sync ON pf_content.library_item (tenant_id, actor_ref, sync_seq);
SELECT shared.apply_tenant_rls('pf_content.library_item');

GRANT SELECT, INSERT, UPDATE ON pf_content.library_item TO content_app;
GRANT USAGE ON SEQUENCE pf_content.library_sync_seq TO content_app;
