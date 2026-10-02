-- pf_extension KN v7.7 gói P5: lịch công tác tuần (FN-XA-03.08 đăng ký của Cán bộ xã, FN-TI-02.07 duyệt
-- của Cấp Tỉnh và "Lịch của tôi" của cán bộ tỉnh). Kế hoạch docs/brd/kn-v7.7-plan.md §2 (số 000075).
--
-- Mỗi lịch = một người đăng ký × phòng ban × cấp (xã/tỉnh) × tuần (Thứ Hai, giờ Việt Nam). Sổ chỉ ghi thêm (RB-1):
--   * work_schedule_event: sự việc của lịch (draft_saved, submitted, amended, approved, change_requested{lý do},
--     locked) kèm trạng thái SAU sự việc để dựng lại bảng chiếu (R-22);
--   * work_schedule_item: mục công tác của MỖI phiên bản (ảnh chụp đủ mục); mục giữ item_id qua các phiên bản,
--     supersedes_id trỏ dòng của cùng mục ở phiên bản trước → Log "cũ → mới" dựng từ chuỗi phiên bản.
--   "Sửa lịch đã gửi/đã duyệt" = phiên bản mới; bản đã duyệt vẫn hiệu lực tới khi phần điều chỉnh được duyệt.
-- work_schedule_current là bảng chiếu (R-21), cập nhật trong CÙNG giao dịch với sổ.
-- work_schedule_reminder: lần nhắc đã gửi (chống nhắc lặp của job), chỉ ghi thêm.
--
-- Dữ liệu cá nhân: không có (R-04). Người đăng ký, người duyệt là actor_ref; tên tra lúc đọc (RB-3).
-- "Thành phần phối hợp" là văn bản tự do — tầng ứng dụng chặn SĐT/CCCD (domain.ContainsPII, gap §4.2).
-- Mọi bản ghi có department_code (kế hoạch §1.2).

CREATE TABLE pf_extension.work_schedule_event (
  id               uuid        NOT NULL,
  tenant_id        uuid        NOT NULL,
  stream_key       text        NOT NULL,                     -- 'wsched:<schedule_id>'
  seq              bigint      NOT NULL,
  schedule_id      uuid        NOT NULL,
  version          integer     NOT NULL CHECK (version >= 1), -- phiên bản mục mà sự việc nói tới
  action           text        NOT NULL CHECK (action IN ('draft_saved', 'submitted', 'amended', 'approved', 'change_requested', 'locked')),
  status_after     text        NOT NULL CHECK (status_after IN ('draft', 'pending', 'approved', 'changes_requested')),
  owner_ref        uuid        NOT NULL,
  owner_level      text        NOT NULL CHECK (owner_level IN ('commune', 'province')),
  department_code  text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  province_code    text        NOT NULL,
  commune_codes    text[]      NOT NULL DEFAULT '{}',
  week_start       date        NOT NULL CHECK (extract(isodow FROM week_start) = 1),
  item_count       integer     NOT NULL CHECK (item_count BETWEEN 0 AND 100),
  late             boolean     NOT NULL DEFAULT false,         -- gửi sau hạn đăng ký ("Đăng ký muộn")
  reason           text        NULL CHECK (reason IS NULL OR char_length(reason) <= 1000),
  note             text        NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  event_time       timestamptz NOT NULL,                       -- R-13
  recorded_at      timestamptz NOT NULL DEFAULT now(),         -- R-13, khóa phân vùng
  supersedes_id    uuid        NULL,
  correction_reason text       NULL,
  actor_ref        uuid        NOT NULL,
  device_id        text        NULL,
  idem_key         text        NOT NULL,
  rule_version     text        NOT NULL,
  prev_hash        bytea       NOT NULL,
  row_hash         bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  -- FN-TI-02.07: "Yêu cầu chỉnh sửa" bắt buộc lý do.
  CHECK (action <> 'change_requested' OR char_length(btrim(reason)) >= 10),
  -- R-56: người duyệt / yêu cầu chỉnh sửa không phải người đăng ký (chặn cả ở tầng ứng dụng).
  CHECK (action NOT IN ('approved', 'change_requested') OR actor_ref <> owner_ref)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX work_schedule_event_idem     ON pf_extension.work_schedule_event (tenant_id, idem_key);
CREATE INDEX work_schedule_event_schedule ON pf_extension.work_schedule_event (tenant_id, schedule_id, seq);

SELECT shared.apply_ledger('pf_extension.work_schedule_event', 'pf_extension.work_schedule_event');

CREATE TABLE pf_extension.work_schedule_item (
  id               uuid        NOT NULL,                       -- dòng của một mục trong một phiên bản
  tenant_id        uuid        NOT NULL,
  stream_key       text        NOT NULL,                       -- 'wsched:<schedule_id>'
  seq              bigint      NOT NULL,
  schedule_id      uuid        NOT NULL,
  version          integer     NOT NULL CHECK (version >= 1),
  item_id          uuid        NOT NULL,                       -- mục logic, giữ qua các phiên bản
  position         integer     NOT NULL CHECK (position BETWEEN 0 AND 99),
  day              date        NOT NULL,
  session          text        NOT NULL CHECK (session IN ('morning', 'afternoon')),
  start_time       time        NULL,
  end_time         time        NULL,
  content          text        NOT NULL CHECK (char_length(btrim(content)) BETWEEN 3 AND 500),
  commune_code     text        NULL,
  cooperative_ref  uuid        NULL,                           -- HTX (pf_party, tham chiếu thuần R-06)
  location         text        NULL CHECK (location IS NULL OR char_length(location) <= 200),
  participants     text        NULL CHECK (participants IS NULL OR char_length(participants) <= 500),
  note             text        NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  supersedes_id    uuid        NULL,                           -- dòng của cùng item_id ở phiên bản trước
  correction_reason text       NULL,                           -- không dùng (phiên bản mới thay cho bản ghi bù)
  event_time       timestamptz NOT NULL,
  recorded_at      timestamptz NOT NULL DEFAULT now(),
  actor_ref        uuid        NOT NULL,
  idem_key         text        NOT NULL,
  rule_version     text        NOT NULL,
  prev_hash        bytea       NOT NULL,
  row_hash         bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (end_time IS NULL OR start_time IS NULL OR end_time > start_time),
  CHECK (correction_reason IS NULL)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX work_schedule_item_version ON pf_extension.work_schedule_item (tenant_id, schedule_id, version, position);
CREATE INDEX work_schedule_item_idem    ON pf_extension.work_schedule_item (tenant_id, idem_key);

SELECT shared.apply_ledger('pf_extension.work_schedule_item', 'pf_extension.work_schedule_item');

-- Bảng chiếu trạng thái hiện hành của lịch (R-21).
CREATE TABLE pf_extension.work_schedule_current (
  tenant_id          uuid        NOT NULL,
  schedule_id        uuid        NOT NULL,
  owner_ref          uuid        NOT NULL,
  owner_level        text        NOT NULL CHECK (owner_level IN ('commune', 'province')),
  department_code    text        NOT NULL,
  province_code      text        NOT NULL,
  commune_codes      text[]      NOT NULL DEFAULT '{}',
  week_start         date        NOT NULL CHECK (extract(isodow FROM week_start) = 1),
  status             text        NOT NULL CHECK (status IN ('draft', 'pending', 'approved', 'changes_requested')),
  version            integer     NOT NULL CHECK (version >= 1),
  approved_version   integer     NULL CHECK (approved_version IS NULL OR approved_version <= version),
  amendment          boolean     NOT NULL DEFAULT false,
  late               boolean     NOT NULL DEFAULT false,
  item_count         integer     NOT NULL,
  conflict_count     integer     NOT NULL DEFAULT 0,
  first_submitted_at timestamptz NULL,
  submitted_at       timestamptz NULL,
  reviewed_at        timestamptz NULL,
  reviewer_ref       uuid        NULL,
  last_reason        text        NULL,
  locked_at          timestamptz NULL,                         -- job ghi sự việc "locked" khi tuần kết thúc
  created_by         uuid        NOT NULL,
  created_at         timestamptz NOT NULL,
  updated_by         uuid        NOT NULL,
  updated_at         timestamptz NOT NULL,
  last_event_id      uuid        NOT NULL,
  last_seq           bigint      NOT NULL,
  PRIMARY KEY (tenant_id, schedule_id),
  UNIQUE (tenant_id, owner_ref, department_code, owner_level, week_start), -- một người có thể vừa Cấp Xã vừa Cấp Tỉnh
  CHECK (status <> 'approved' OR (approved_version = version AND NOT amendment)),
  CHECK (NOT amendment OR approved_version IS NOT NULL)
);
CREATE INDEX work_schedule_current_scope ON pf_extension.work_schedule_current (tenant_id, department_code, province_code, week_start, status);
CREATE INDEX work_schedule_current_owner ON pf_extension.work_schedule_current (tenant_id, owner_ref, week_start);
CREATE INDEX work_schedule_current_open  ON pf_extension.work_schedule_current (week_start) WHERE locked_at IS NULL;
SELECT shared.apply_tenant_rls('pf_extension.work_schedule_current');

-- Lần nhắc đã gửi (job extension_work_schedule): nhắc cán bộ chưa gửi trước hạn đăng ký, nhắc người duyệt
-- khi lịch chờ duyệt quá hạn duyệt. Một dòng mỗi (loại, tuần, phòng ban, tỉnh) → không nhắc lặp.
CREATE TABLE pf_extension.work_schedule_reminder (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  kind            text        NOT NULL CHECK (kind IN ('submit_due', 'review_overdue')),
  week_start      date        NOT NULL,
  department_code text        NOT NULL,
  province_code   text        NOT NULL,
  recipients      integer     NOT NULL CHECK (recipients >= 0),
  sent_at         timestamptz NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, kind, week_start, department_code, province_code)
);
SELECT shared.apply_tenant_rls('pf_extension.work_schedule_reminder');

CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_extension.work_schedule_reminder
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_extension.work_schedule_reminder
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.work_schedule_reminder
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_extension.work_schedule_event, pf_extension.work_schedule_item,
  pf_extension.work_schedule_reminder TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.work_schedule_event, pf_extension.work_schedule_item,
  pf_extension.work_schedule_reminder FROM extension_app;
GRANT SELECT, INSERT, UPDATE ON pf_extension.work_schedule_current TO extension_app;
REVOKE DELETE, TRUNCATE ON pf_extension.work_schedule_current FROM extension_app;

-- Job nhắc/khóa chạy ở worker (leader) cho MỌI tenant: vai ag_outbox_publisher (BYPASSRLS) chỉ đọc vài cột để
-- biết tenant nào có lịch hoặc cán bộ; việc thật chạy bằng extension_app trong ngữ cảnh từng tenant (R-05).
GRANT SELECT (tenant_id, week_start, locked_at) ON pf_extension.work_schedule_current TO ag_outbox_publisher;
GRANT SELECT (tenant_id, active) ON pf_extension.staff TO ag_outbox_publisher;
