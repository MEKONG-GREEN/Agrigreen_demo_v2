-- pf/price (`cap/price`, W11.75 — ADR-0049 mục 4, BRD Báo giá nông sản v1.3 FN-XA-03.01…03, 03.05, FN-TI-02.02…04,
-- 02.09; Mục II.3 luồng trạng thái, II.4 CT-01…CT-03): PHIẾU BÁO GIÁ + XÁC MINH — phiếu của một xã báo giá trong một
-- ngày báo giá, dòng giá theo mặt hàng, cảnh báo biến động + trao đổi xác minh hai chiều, nhập thay, hiệu chỉnh, sửa ngày,
-- xóa mềm.
--
-- Cùng mẫu với pf 000138/000141/000145: mọi bảng là sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn
-- UPDATE/DELETE/TRUNCATE, RLS theo tenant); mỗi dòng là MỘT phiên bản của một chuỗi (version = seq). Trạng thái hiện hành
-- = phiên bản có seq lớn nhất của chuỗi. Không có trạng thái "Không nộp", "Đã khóa", "Chờ rà soát", "Quá hạn phản hồi"
-- ghi ở sổ: chúng là phép chiếu theo giờ khóa / hạn phản hồi của lịch W11.74 (port DayPlan.Locked) tại thời điểm đọc —
-- không cần job ghi lúc 12:00, ghi bị chặn đúng giờ khóa.
--
-- 1. ticket — đầu phiếu (chuỗi 'ticket:<ticket_id>'). Một xã một phiếu còn hiệu lực (status ≠ 'deleted') cho một ngày báo
--    giá (BR-03 FN-TI-02.04) — kiểm ở ứng dụng dưới khóa cố vấn theo tenant + xã (phiếu có thể đổi ngày — sửa ngày phiếu
--    là phiên bản mới cùng ticket_id, quote_day mới; không có ràng buộc UNIQUE trên sổ chỉ ghi thêm). status: 'draft'
--    (Bản nháp), 'submitted' (Đã gửi trước giờ khóa), 'late' (Nộp muộn — Cấp Tỉnh nhập thay, FN-TI-02.09), 'deleted' (Đã
--    xóa — xóa mềm, trạng thái cuối). source: 'xa' (cán bộ Xã nhập) | 'proxy' (Cấp Tỉnh/Admin nhập thay). op: thao tác
--    tạo phiên bản. officer_ref: cán bộ Xã phụ trách xã tại ngày báo giá (chụp — in phiếu, thống kê W11.77). reason: lý do
--    nộp muộn / hiệu chỉnh / sửa ngày / xóa phiếu (bắt buộc — BR-02 FN-TI-02.04, BR-03 FN-TI-02.09).
-- 2. ticket_line — dòng giá (chuỗi 'line:<ticket_id>:<item_code>'): giá thương lái thấp nhất / cao nhất (numeric — không
--    float, R-CODE-01), "Không có hàng", ghi chú biến động; adj_* = giá sau hiệu chỉnh của Cấp Tỉnh (BR-05 FN-TI-02.04:
--    giữ cả giá cán bộ Xã gửi để in phiếu và giá hiệu chỉnh cho bảng giá / giá tham chiếu). Dòng trống (chưa nhập) được
--    phép trong bản nháp.
-- 3. price_alert — cảnh báo biến động của một dòng (chuỗi 'alert:<ticket_id>:<item_code>', Mục II.3.2): status
--    'provisional' (Tạm tính; sau giờ khóa đọc là Chờ rà soát), 'cancelled' (tự hủy — Xã sửa về dưới ngưỡng và gửi lại),
--    'requested' (Đã yêu cầu xác minh; quá respond_by đọc là Quá hạn phản hồi), 'responded' (Xã đã phản hồi), 'adjusted'
--    (Đã hiệu chỉnh), 'kept' (Giữ nguyên), 'excluded' (Đã loại — dòng không tính vào bảng giá, vẫn in), 'reopened' (mở lại
--    khi bảng giá đang mở duyệt — W11.76; đọc là Chờ rà soát). Mỗi phiên bản chụp giá tham chiếu (CT-02), giá kỳ này
--    P = (thấp nhất + cao nhất) / 2 (CT-01), Δ% (CT-03), ngưỡng đã áp; message = nội dung yêu cầu xác minh / ghi chú giữ
--    nguyên / lý do loại / lý do hiệu chỉnh; response + proposed_* + explanation = phản hồi của Xã (giữ giá / đề xuất giá,
--    giải trình bắt buộc — FN-XA-03.05). Trao đổi xác minh = lịch sử phiên bản của chuỗi.
--
-- Không dữ liệu cá nhân (R-04): giá, mã, ghi chú nghiệp vụ, mã tham chiếu người ghi. Không cột số thực (R-CODE-01).
-- Vai r_price SELECT, INSERT (sổ chỉ ghi thêm — R-05, R-20).

CREATE TABLE pf_price.ticket (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'ticket:<ticket_id>'
  seq               bigint      NOT NULL,                 -- = số phiên bản (khóa lạc quan của màn nhập/sửa)
  ticket_id         uuid        NOT NULL,                 -- mã phiếu (ổn định qua các phiên bản)
  point_code        text        NOT NULL CHECK (point_code ~ '^XBG[0-9]{4}$'),
  quote_day         date        NOT NULL,                 -- ngày báo giá (giờ VN)
  status            text        NOT NULL CHECK (status IN ('draft', 'submitted', 'late', 'deleted')),
  source            text        NOT NULL CHECK (source IN ('xa', 'proxy')),
  op                text        NOT NULL CHECK (op IN ('save', 'submit', 'proxy_save', 'proxy_submit', 'correct', 'redate', 'delete')),
  officer_ref       uuid        NULL,                     -- cán bộ Xã phụ trách tại ngày báo giá (NULL = chưa có)
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  submitted_at      timestamptz NULL,                     -- lần gửi chính thức gần nhất (Đã gửi / Nộp muộn)
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
  CHECK (op NOT IN ('proxy_save', 'proxy_submit', 'correct', 'redate', 'delete') OR reason IS NOT NULL),
  CHECK ((status = 'deleted') = (op = 'delete')),
  CHECK (status NOT IN ('submitted', 'late') OR submitted_at IS NOT NULL),
  CHECK ((status = 'late') <= (source = 'proxy')),
  CONSTRAINT ticket_stream CHECK (stream_key = 'ticket:' || ticket_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ticket_point_day ON pf_price.ticket (tenant_id, point_code, quote_day);
CREATE INDEX ticket_day ON pf_price.ticket (tenant_id, quote_day);
CREATE INDEX ticket_id_seq ON pf_price.ticket (tenant_id, ticket_id, seq);
SELECT shared.apply_ledger('pf_price.ticket', 'pf_price.ticket');

CREATE TABLE pf_price.ticket_line (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'line:<ticket_id>:<item_code>'
  seq               bigint        NOT NULL,
  ticket_id         uuid          NOT NULL,
  item_code         text          NOT NULL CHECK (item_code ~ '^MH[0-9]{5}$'),
  min_price         numeric(14,2) NULL CHECK (min_price IS NULL OR min_price > 0),
  max_price         numeric(14,2) NULL CHECK (max_price IS NULL OR max_price > 0),
  no_goods          boolean       NOT NULL,               -- "Không có hàng" trong ngày (BR-03 FN-XA-03.01)
  note              text          NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500), -- ghi chú biến động
  adj_min           numeric(14,2) NULL CHECK (adj_min IS NULL OR adj_min > 0),
  adj_max           numeric(14,2) NULL CHECK (adj_max IS NULL OR adj_max > 0),
  adj_reason        text          NULL CHECK (adj_reason IS NULL OR char_length(adj_reason) BETWEEN 1 AND 500),
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
  CHECK ((min_price IS NULL) = (max_price IS NULL)),       -- BR-02: một mức giá → thấp nhất = cao nhất
  CHECK (min_price IS NULL OR min_price <= max_price),
  CHECK (NOT no_goods OR min_price IS NULL),
  CHECK ((adj_min IS NULL) = (adj_max IS NULL)),
  CHECK (adj_min IS NULL OR adj_min <= adj_max),
  CHECK ((adj_min IS NULL) = (adj_reason IS NULL)),
  CHECK (adj_min IS NULL OR NOT no_goods),
  CONSTRAINT ticket_line_stream CHECK (stream_key = 'line:' || ticket_id::text || ':' || item_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ticket_line_ticket ON pf_price.ticket_line (tenant_id, ticket_id, item_code, seq);
CREATE INDEX ticket_line_item ON pf_price.ticket_line (tenant_id, item_code);
SELECT shared.apply_ledger('pf_price.ticket_line', 'pf_price.ticket_line');

CREATE TABLE pf_price.price_alert (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'alert:<ticket_id>:<item_code>'
  seq               bigint        NOT NULL,
  ticket_id         uuid          NOT NULL,
  item_code         text          NOT NULL CHECK (item_code ~ '^MH[0-9]{5}$'),
  status            text          NOT NULL CHECK (status IN ('provisional', 'cancelled', 'requested', 'responded', 'adjusted',
                                                           'kept', 'excluded', 'reopened')),
  direction         text          NOT NULL CHECK (direction IN ('high', 'low')),     -- ▲ Cao / ▼ Thấp bất thường
  ref_price         numeric(16,4) NOT NULL CHECK (ref_price > 0),                     -- P_ref (CT-02)
  price             numeric(16,4) NOT NULL CHECK (price > 0),                         -- P (CT-01)
  change_pct        numeric(20,4) NOT NULL,                                           -- Δ% (CT-03)
  threshold_pct     numeric(5,2)  NOT NULL CHECK (threshold_pct > 0 AND threshold_pct <= 100),
  message           text          NULL CHECK (message IS NULL OR char_length(message) BETWEEN 1 AND 1000),
  respond_by        timestamptz   NULL,                   -- hạn phản hồi của yêu cầu xác minh
  response          text          NULL CHECK (response IS NULL OR response IN ('keep', 'propose')),
  proposed_min      numeric(14,2) NULL CHECK (proposed_min IS NULL OR proposed_min > 0),
  proposed_max      numeric(14,2) NULL CHECK (proposed_max IS NULL OR proposed_max > 0),
  explanation       text          NULL CHECK (explanation IS NULL OR char_length(explanation) BETWEEN 1 AND 1000),
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
  CHECK (status NOT IN ('requested', 'responded') OR (message IS NOT NULL AND respond_by IS NOT NULL)), -- BR-05
  CHECK (status NOT IN ('kept', 'excluded', 'adjusted', 'reopened') OR message IS NOT NULL),            -- BR-05
  CHECK ((status = 'responded') = (response IS NOT NULL)),
  CHECK (response IS NULL OR explanation IS NOT NULL),                                                  -- BR-03 FN-XA-03.05
  CHECK ((proposed_min IS NULL) = (proposed_max IS NULL)),
  CHECK ((response = 'propose') = (proposed_min IS NOT NULL)),
  CHECK (proposed_min IS NULL OR proposed_min <= proposed_max),
  CONSTRAINT price_alert_stream CHECK (stream_key = 'alert:' || ticket_id::text || ':' || item_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX price_alert_ticket ON pf_price.price_alert (tenant_id, ticket_id, item_code, seq);
SELECT shared.apply_ledger('pf_price.price_alert', 'pf_price.price_alert');

REVOKE ALL ON pf_price.ticket, pf_price.ticket_line, pf_price.price_alert FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT SELECT, INSERT ON pf_price.ticket, pf_price.ticket_line, pf_price.price_alert TO r_price;
