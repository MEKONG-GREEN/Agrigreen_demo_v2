-- pf/price (`cap/price`, W11.76 — ADR-0049 mục 4, BRD Báo giá nông sản v1.3 FN-TI-02.05 Duyệt bảng giá, FN-TI-02.06 Mở
-- duyệt, Mục II.3.3 luồng trạng thái bảng giá ngày, II.4 CT-04): BẢNG GIÁ NGÀY CÓ PHIÊN BẢN — khóa "Đang duyệt", duyệt
-- (phiên bản mới), mở duyệt có lý do (trong 30 ngày — Q-086/Q-052 tạm), hủy mở duyệt; ảnh chụp dòng bảng giá và phiếu
-- thuộc mỗi phiên bản đã duyệt (nguồn của view pf_price_pub.v1_board_daily, v1_ticket_week — pf 000152).
--
-- Cùng mẫu với pf 000138/000148: sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo
-- tenant, FORCE RLS); trạng thái hiện hành = phiên bản seq lớn nhất của chuỗi.
--
-- 1. board — trạng thái bảng giá của một ngày báo giá (chuỗi 'board:<profile>:<YYYY-MM-DD>'). status: 'open' (Chưa duyệt
--    — chỉ sau hủy khóa khi chưa có phiên bản), 'approving' (Đang duyệt — một cán bộ giữ khóa tới lock_until; base_status
--    = trạng thái trả về khi hủy / hết hạn khóa), 'approved' (Đã duyệt vN), 'reopened' (Đang mở duyệt — vN vẫn công bố).
--    board_version = số phiên bản ĐÃ DUYỆT hiện hành (0 = chưa duyệt lần nào); approved_at = lúc duyệt vN. Phiên bản
--    'approve' chụp khóa tuần báo cáo (lịch kỳ hệ thống W11.03: week_key, week_year, week_from, week_to — LƯU lúc duyệt,
--    không tính lại) và số ngày báo giá theo lịch W11.74 của tuần đó (days_scheduled — lịch có phiên bản + ngoại lệ, mặc
--    định của Profile khi tenant chưa đặt lịch — chỉ tính được ở ứng dụng). reason: lý do mở duyệt (bắt buộc — BR-01
--    FN-TI-02.06) / hủy mở duyệt.
-- 2. board_line — dòng bảng giá của MỘT phiên bản đã duyệt (chuỗi 'bline:<profile>:<ngày>:<vN>:<mặt hàng>', một dòng):
--    CT-04 min / max toàn tỉnh, trung bình cộng P các xã (numeric — không float, R-CODE-01), số xã có giá; tên, nhóm, ĐVT
--    chụp theo phiên bản mặt hàng có hiệu lực tại ngày báo giá. Không tính dòng Không có hàng, Đã loại, phiếu Đã xóa.
-- 3. board_ticket — phiếu thuộc MỘT phiên bản đã duyệt (chuỗi 'bticket:<profile>:<ngày>:<vN>:<ticket_id>', một dòng):
--    phiếu Đã gửi / Nộp muộn (phiên bản phiếu ticket_seq lúc duyệt), mã xã báo giá + mã ĐVHC xã chụp tại ngày.
--
-- Không dữ liệu cá nhân ngoài mã tham chiếu người ghi (R-04). Vai r_price SELECT, INSERT (sổ chỉ ghi thêm — R-05, R-20).

CREATE TABLE pf_price.board (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'board:<profile>:<YYYY-MM-DD>'
  seq               bigint      NOT NULL,                 -- khóa lạc quan của màn Kiểm duyệt
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  board_date        date        NOT NULL,                 -- ngày báo giá (giờ VN)
  action            text        NOT NULL CHECK (action IN ('lock', 'unlock', 'approve', 'reopen', 'cancel_reopen')),
  status            text        NOT NULL CHECK (status IN ('open', 'approving', 'approved', 'reopened')),
  base_status       text        NULL CHECK (base_status IS NULL OR base_status IN ('open', 'reopened')),
  lock_until        timestamptz NULL,                     -- hết hạn khóa "Đang duyệt"
  board_version     integer     NOT NULL CHECK (board_version >= 0),
  approved_at       timestamptz NULL,                     -- lúc duyệt phiên bản board_version
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  week_key          text        NULL CHECK (week_key IS NULL OR week_key ~ '^[0-9]{4}-W[0-9]{2}$'),
  week_year         integer     NULL,
  week_from         date        NULL,
  week_to           date        NULL,
  days_scheduled    smallint    NULL CHECK (days_scheduled IS NULL OR days_scheduled BETWEEN 0 AND 7),
  lines_count       integer     NULL CHECK (lines_count IS NULL OR lines_count >= 0),
  tickets_count     integer     NULL CHECK (tickets_count IS NULL OR tickets_count >= 0),
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
  CHECK ((status = 'approving') = (action = 'lock')),
  CHECK ((status = 'approving') = (lock_until IS NOT NULL AND base_status IS NOT NULL)),
  CHECK (action <> 'approve' OR (status = 'approved' AND board_version >= 1)),
  CHECK (action <> 'reopen' OR (status = 'reopened' AND reason IS NOT NULL)),
  CHECK (status NOT IN ('approved', 'reopened') OR (board_version >= 1 AND approved_at IS NOT NULL)),
  CHECK (status <> 'open' OR board_version = 0),
  CHECK ((action = 'approve') = (week_key IS NOT NULL)),
  CHECK ((week_key IS NULL) = (week_year IS NULL) AND (week_key IS NULL) = (week_from IS NULL)
         AND (week_key IS NULL) = (week_to IS NULL) AND (week_key IS NULL) = (days_scheduled IS NULL)
         AND (week_key IS NULL) = (lines_count IS NULL) AND (week_key IS NULL) = (tickets_count IS NULL)),
  CHECK (week_from IS NULL OR (week_from <= board_date AND board_date <= week_to)),
  CONSTRAINT board_stream CHECK (stream_key = 'board:' || profile || ':' || to_char(board_date, 'YYYY-MM-DD'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX board_day ON pf_price.board (tenant_id, profile, board_date, seq);
SELECT shared.apply_ledger('pf_price.board', 'pf_price.board');

CREATE TABLE pf_price.board_line (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'bline:<profile>:<YYYY-MM-DD>:<vN>:<item_code>'
  seq               bigint        NOT NULL,
  profile           text          NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  board_date        date          NOT NULL,
  board_version     integer       NOT NULL CHECK (board_version >= 1),
  item_code         text          NOT NULL CHECK (item_code ~ '^MH[0-9]{5}$'),
  item_name         text          NOT NULL CHECK (char_length(item_name) BETWEEN 1 AND 300),
  group_code        text          NOT NULL,
  unit_code         text          NOT NULL,
  price_min         numeric(14,2) NOT NULL CHECK (price_min > 0),
  price_max         numeric(14,2) NOT NULL CHECK (price_max > 0),
  price_avg         numeric(16,4) NOT NULL CHECK (price_avg > 0),
  points_reported   integer       NOT NULL CHECK (points_reported >= 1),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (price_min <= price_avg AND price_avg <= price_max),
  CONSTRAINT board_line_stream CHECK (stream_key = 'bline:' || profile || ':' || to_char(board_date, 'YYYY-MM-DD') || ':v'
                                      || board_version::text || ':' || item_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX board_line_version ON pf_price.board_line (tenant_id, profile, board_date, board_version);
SELECT shared.apply_ledger('pf_price.board_line', 'pf_price.board_line');

CREATE TABLE pf_price.board_ticket (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'bticket:<profile>:<YYYY-MM-DD>:<vN>:<ticket_id>'
  seq               bigint      NOT NULL,
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  board_date        date        NOT NULL,
  board_version     integer     NOT NULL CHECK (board_version >= 1),
  ticket_id         uuid        NOT NULL,
  ticket_seq        bigint      NOT NULL CHECK (ticket_seq >= 1),
  point_code        text        NOT NULL CHECK (point_code ~ '^XBG[0-9]{4}$'),
  commune_code      text        NOT NULL,                 -- mã ĐVHC xã của xã báo giá tại ngày báo giá
  late              boolean     NOT NULL,                 -- Nộp muộn (nhập thay)
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT board_ticket_stream CHECK (stream_key = 'bticket:' || profile || ':' || to_char(board_date, 'YYYY-MM-DD') || ':v'
                                        || board_version::text || ':' || ticket_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX board_ticket_version ON pf_price.board_ticket (tenant_id, profile, board_date, board_version);
SELECT shared.apply_ledger('pf_price.board_ticket', 'pf_price.board_ticket');

REVOKE ALL ON pf_price.board, pf_price.board_line, pf_price.board_ticket FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT SELECT, INSERT ON pf_price.board, pf_price.board_line, pf_price.board_ticket TO r_price;
