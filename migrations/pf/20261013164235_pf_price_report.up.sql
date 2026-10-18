-- pf/price (`cap/price`, W11.77 — BRD Báo giá nông sản v1.3 FN-TW-01.01…03, FN-TI-02.01, FN-TI-02.08; Q-085): BÁO CÁO
-- GIÁ. Báo cáo chỉ đọc bảng giá ĐÃ DUYỆT (pf 000151), phiếu (pf 000148), lịch (W11.74); migration này thêm hai thứ mà
-- báo cáo cần nhưng chưa có sổ:
--
-- 1. board_point — dòng giá THEO XÃ của MỘT phiên bản đã duyệt (chuỗi 'bpoint:<profile>:<ngày>:<vN>:<xã>:<mặt hàng>',
--    một dòng). board_line (pf 000151) chỉ chụp mức toàn tỉnh; Bảng giá tổng hợp theo nhóm vùng / theo xã (FN-TW-01.01
--    B1, luồng phụ), dashboard vùng (FN-TW-01.03) và báo cáo biến động theo xã (FN-TW-01.02) cần giá từng xã ĐÚNG phiên
--    bản đã duyệt — kể cả khi ngày đang mở duyệt và số liệu phiếu đã đổi ("Đang cập nhật", AC-03). Nhóm vùng chụp tại ngày
--    báo giá (BR-04 FN-AD-04.02). Giá sau hiệu chỉnh; không có dòng Không có hàng, Đã loại, phiếu Đã xóa (BR-03).
--    Ghi CÙNG giao dịch với board_line lúc duyệt. Phiên bản duyệt trước migration này không có dòng — ứng dụng dựng lại
--    từ phiếu khi ngày không mở duyệt (số liệu không đổi được khi bảng giá Đã duyệt).
-- 2. stat_month + stat_month_line — KHÓA THÁNG thống kê phiếu báo giá theo cán bộ Xã (Q-085 đề xuất của dev: "khóa tháng;
--    không ký số" — thống kê là căn cứ thanh toán ngoài hệ thống, BR-03 FN-TI-02.08). stat_month là trạng thái của một
--    tháng (chuỗi 'statmonth:<profile>:<YYYY-MM>'): 'closed' (Đã khóa vN) / 'open' (Mở lại — lý do bắt buộc). Chỉ cán
--    bộ Cấp Tỉnh có cờ Quản trị nghiệp vụ báo giá khóa / mở (D11). stat_month_line chụp số liệu của phiên bản khóa
--    (chuỗi 'statline:<profile>:<YYYY-MM>:<vN>:<cán bộ>:<xã>', một dòng): phải nộp, đúng hạn, nộp muộn, không nộp, bị
--    xóa, số dòng bị hiệu chỉnh — tháng đã khóa đọc ảnh chụp, không tính lại.
--
-- Cùng mẫu pf 000151: sổ chỉ ghi thêm (shared.apply_ledger), RLS theo tenant; số đếm integer, giá numeric (không float —
-- R-CODE-01). Không dữ liệu cá nhân ngoài mã tham chiếu (R-04): officer_ref là mã cán bộ, không tên / SĐT.

CREATE TABLE pf_price.board_point (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'bpoint:<profile>:<YYYY-MM-DD>:v<N>:<point_code>:<item_code>'
  seq               bigint        NOT NULL,
  profile           text          NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  board_date        date          NOT NULL,
  board_version     integer       NOT NULL CHECK (board_version >= 1),
  point_code        text          NOT NULL CHECK (point_code ~ '^XBG[0-9]{4}$'),
  region_code       text          NOT NULL,               -- nhóm vùng của xã tại ngày báo giá ('' = chưa có)
  item_code         text          NOT NULL CHECK (item_code ~ '^MH[0-9]{5}$'),
  price_min         numeric(14,2) NOT NULL CHECK (price_min > 0),
  price_max         numeric(14,2) NOT NULL CHECK (price_max > 0),
  price_mid         numeric(16,4) NOT NULL CHECK (price_mid > 0),   -- P (CT-01)
  adjusted          boolean       NOT NULL,               -- giá Cấp Tỉnh hiệu chỉnh
  late              boolean       NOT NULL,               -- phiếu Nộp muộn (nhập thay)
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
  CHECK (price_min <= price_mid AND price_mid <= price_max),
  CONSTRAINT board_point_stream CHECK (stream_key = 'bpoint:' || profile || ':' || to_char(board_date, 'YYYY-MM-DD') || ':v'
                                       || board_version::text || ':' || point_code || ':' || item_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX board_point_version ON pf_price.board_point (tenant_id, profile, board_date, board_version);
SELECT shared.apply_ledger('pf_price.board_point', 'pf_price.board_point');

CREATE TABLE pf_price.stat_month (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'statmonth:<profile>:<YYYY-MM>'
  seq               bigint      NOT NULL,
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  month_start       date        NOT NULL CHECK (extract(day FROM month_start) = 1),
  action            text        NOT NULL CHECK (action IN ('close', 'reopen')),
  status            text        NOT NULL CHECK (status IN ('closed', 'open')),
  stat_version      integer     NOT NULL CHECK (stat_version >= 1),   -- phiên bản khóa hiện hành / vừa mở lại
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  rows_count        integer     NULL CHECK (rows_count IS NULL OR rows_count >= 0),
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
  CHECK ((action = 'close') = (status = 'closed')),
  CHECK (action = 'close' OR reason IS NOT NULL),
  CONSTRAINT stat_month_stream CHECK (stream_key = 'statmonth:' || profile || ':' || to_char(month_start, 'YYYY-MM'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX stat_month_month ON pf_price.stat_month (tenant_id, profile, month_start);
SELECT shared.apply_ledger('pf_price.stat_month', 'pf_price.stat_month');

CREATE TABLE pf_price.stat_month_line (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'statline:<profile>:<YYYY-MM>:v<N>:<officer_ref>:<point_code>'
  seq               bigint      NOT NULL,
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  month_start       date        NOT NULL CHECK (extract(day FROM month_start) = 1),
  stat_version      integer     NOT NULL CHECK (stat_version >= 1),
  officer_ref       uuid        NOT NULL,                 -- cán bộ Xã phụ trách chính tại các ngày được tính
  point_code        text        NOT NULL CHECK (point_code ~ '^XBG[0-9]{4}$'),
  region_code       text        NOT NULL,
  days_required     integer     NOT NULL CHECK (days_required >= 0),
  on_time           integer     NOT NULL CHECK (on_time >= 0),
  late              integer     NOT NULL CHECK (late >= 0),
  missed            integer     NOT NULL CHECK (missed >= 0),
  deleted           integer     NOT NULL CHECK (deleted >= 0),
  adjusted_lines    integer     NOT NULL CHECK (adjusted_lines >= 0),
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
  CHECK (on_time + late + missed = days_required),
  CONSTRAINT stat_month_line_stream CHECK (stream_key = 'statline:' || profile || ':' || to_char(month_start, 'YYYY-MM') || ':v'
                                           || stat_version::text || ':' || officer_ref::text || ':' || point_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX stat_month_line_version ON pf_price.stat_month_line (tenant_id, profile, month_start, stat_version);
SELECT shared.apply_ledger('pf_price.stat_month_line', 'pf_price.stat_month_line');

REVOKE ALL ON pf_price.board_point, pf_price.stat_month, pf_price.stat_month_line FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT SELECT, INSERT ON pf_price.board_point, pf_price.stat_month, pf_price.stat_month_line TO r_price;
