-- pf/price (`cap/price`, W11.73 — ADR-0049 mục 4, BRD Báo giá nông sản v1.3 FN-AD-04.03, FN-XA-03.01): MẶT HÀNG THEO
-- THỜI KỲ — mặt hàng, gán mặt hàng cho xã, đề xuất mặt hàng từ xã, bộ mặt hàng theo vụ, "thay thế cho".
--
-- Cùng mẫu với danh mục nền pf 000138: mọi bảng là sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn
-- UPDATE/DELETE/TRUNCATE, RLS theo tenant); mỗi dòng là MỘT phiên bản của một chuỗi (version = seq), hiệu lực theo
-- valid_from (date, giờ VN). Phiên bản có hiệu lực tại ngày d của một chuỗi = valid_from lớn nhất ≤ d, cùng ngày lấy seq
-- lớn nhất (phép chiếu ở domain/store, như 000138). Ngưng = phiên bản status 'retired' (không xóa — BR-02, BR-04).
--
-- 1. item — mặt hàng (chuỗi 'item:<code>'). Mã do hệ thống sinh 'MH' + 5 chữ số, không tái sử dụng (BR-01). Định danh
--    nghiệp vụ = tên loại + giống/phẩm cấp + đơn vị tính (BR-07, AC-06: variety bắt buộc). group_code, unit_code tham
--    chiếu mã của pf_price.commodity_group, pf_price.unit (000138) — kiểm ở ứng dụng theo ngày hiệu lực (sổ có phiên bản,
--    không có khóa ngoại). kind: 'basic' (Cơ bản — áp dụng mọi xã đang hoạt động, không gán riêng — BR-03) | 'local'
--    (Địa phương — gán theo xã). replaces_code: "Thay thế cho" (đổi ĐVT/quy cách của mặt hàng đã có giá → mặt hàng mới
--    liên kết mặt hàng cũ để tra cứu lịch sử). proposal_id: mặt hàng tạo khi duyệt đề xuất. note: lý do ngưng/ghi chú.
-- 2. item_assignment — gán mặt hàng Địa phương cho xã báo giá (chuỗi 'assign:<point_code>:<item_code>'), active/retired
--    theo valid_from. point_code là mã xã báo giá — LIÊN KẾT LỎNG: xã báo giá (pf_price.collection_point, ↔ mã ĐVHC có
--    hiệu lực) do W11.72 dựng song song; mặc định mã xã báo giá = mã ĐVHC xã. Không khóa ngoại.
-- 3. item_proposal — đề xuất mặt hàng mới từ Cấp Xã (chuỗi 'proposal:<proposal_id>'): phiên bản 1 'pending', phiên bản 2
--    'approved' (item_code = mặt hàng tạo ra) hoặc 'rejected' (decision_note = lý do).
-- 4. item_set — bộ mặt hàng theo vụ/kỳ (chuỗi 'set:<code>', BR-08): tên, vụ, danh sách mã mặt hàng, cờ bật; bật/tắt cả
--    bộ theo ngày hiệu lực. Mặt hàng thuộc ít nhất một bộ đang hiệu lực chỉ xuất hiện khi có bộ chứa nó đang BẬT.
--
-- Không dữ liệu cá nhân (R-04): tên mặt hàng, mã, lý do nghiệp vụ, mã tham chiếu người ghi. Không cột số thực
-- (R-CODE-01). Vai r_price SELECT, INSERT (sổ chỉ ghi thêm — R-05, R-20).

CREATE TABLE pf_price.item (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'item:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  code              text        NOT NULL CHECK (code ~ '^MH[0-9]{5}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  variety           text        NOT NULL CHECK (char_length(variety) BETWEEN 1 AND 100), -- giống / phẩm cấp (BR-07)
  group_code        text        NOT NULL CHECK (group_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  unit_code         text        NOT NULL CHECK (unit_code ~ '^[a-z][a-z0-9_]{0,19}$'),
  spec              text        NULL CHECK (spec IS NULL OR char_length(spec) BETWEEN 1 AND 100),         -- quy cách
  sale_place        text        NULL CHECK (sale_place IS NULL OR char_length(sale_place) BETWEEN 1 AND 200), -- nơi bán
  kind              text        NOT NULL CHECK (kind IN ('basic', 'local')),
  sort_order        integer     NOT NULL CHECK (sort_order BETWEEN 0 AND 1000000),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  valid_from        date        NOT NULL,                 -- ngày báo giá đầu tiên phiên bản có hiệu lực (giờ VN)
  replaces_code     text        NULL CHECK (replaces_code IS NULL OR (replaces_code ~ '^MH[0-9]{5}$' AND replaces_code <> code)),
  proposal_id       uuid        NULL,
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
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
  CHECK (status = 'active' OR note IS NOT NULL),          -- ngưng phải có lý do (FN-AD-04.03 B4)
  CONSTRAINT item_stream CHECK (stream_key = 'item:' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX item_code ON pf_price.item (tenant_id, code, valid_from, seq);
SELECT shared.apply_ledger('pf_price.item', 'pf_price.item');

CREATE TABLE pf_price.item_assignment (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'assign:<point_code>:<item_code>'
  seq               bigint      NOT NULL,
  point_code        text        NOT NULL CHECK (point_code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$'),
  item_code         text        NOT NULL CHECK (item_code ~ '^MH[0-9]{5}$'),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  valid_from        date        NOT NULL,
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
  CONSTRAINT item_assignment_stream CHECK (stream_key = 'assign:' || point_code || ':' || item_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX item_assignment_point ON pf_price.item_assignment (tenant_id, point_code, item_code, valid_from, seq);
SELECT shared.apply_ledger('pf_price.item_assignment', 'pf_price.item_assignment');

CREATE TABLE pf_price.item_proposal (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'proposal:<proposal_id>'
  seq               bigint      NOT NULL,
  proposal_id       uuid        NOT NULL,
  point_code        text        NOT NULL CHECK (point_code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  variety           text        NOT NULL CHECK (char_length(variety) BETWEEN 1 AND 100),
  group_code        text        NOT NULL CHECK (group_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  unit_code         text        NOT NULL CHECK (unit_code ~ '^[a-z][a-z0-9_]{0,19}$'),
  spec              text        NULL CHECK (spec IS NULL OR char_length(spec) BETWEEN 1 AND 100),
  reason            text        NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
  status            text        NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  item_code         text        NULL CHECK (item_code IS NULL OR item_code ~ '^MH[0-9]{5}$'),
  decision_note     text        NULL CHECK (decision_note IS NULL OR char_length(decision_note) BETWEEN 1 AND 500),
  proposer_ref      uuid        NOT NULL,                 -- người đề xuất (mã tham chiếu, không PII)
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
  CHECK (status <> 'approved' OR item_code IS NOT NULL),
  CHECK (status <> 'rejected' OR decision_note IS NOT NULL),
  CONSTRAINT item_proposal_stream CHECK (stream_key = 'proposal:' || proposal_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX item_proposal_id ON pf_price.item_proposal (tenant_id, proposal_id, seq);
SELECT shared.apply_ledger('pf_price.item_proposal', 'pf_price.item_proposal');

CREATE TABLE pf_price.item_set (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'set:<code>'
  seq               bigint      NOT NULL,
  code              text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  season            text        NOT NULL CHECK (char_length(season) BETWEEN 1 AND 50), -- vụ / kỳ (Hè Thu, Thu Đông …)
  item_codes        text[]      NOT NULL CHECK (cardinality(item_codes) BETWEEN 1 AND 500),
  enabled           boolean     NOT NULL,
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  valid_from        date        NOT NULL,
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
  CONSTRAINT item_set_stream CHECK (stream_key = 'set:' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX item_set_code ON pf_price.item_set (tenant_id, code, valid_from, seq);
SELECT shared.apply_ledger('pf_price.item_set', 'pf_price.item_set');

REVOKE ALL ON pf_price.item, pf_price.item_assignment, pf_price.item_proposal, pf_price.item_set FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT SELECT, INSERT ON pf_price.item, pf_price.item_assignment, pf_price.item_proposal, pf_price.item_set TO r_price;

-- Ngưỡng theo mặt hàng của W11.74 (pf 000139, alert_threshold.scope_code) tham chiếu mã mặt hàng của sổ này ('MH#####',
-- chữ HOA): nới CHECK cho phép chữ hoa; nhóm hàng vẫn chữ thường (kiểm ở ứng dụng). Dòng đã ghi đều thỏa CHECK mới.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_price.alert_threshold DROP CONSTRAINT alert_threshold_scope_code_check;
ALTER TABLE pf_price.alert_threshold ADD CONSTRAINT alert_threshold_scope_code_check
  CHECK (scope_code = '' OR scope_code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$') NOT VALID;
ALTER TABLE pf_price.alert_threshold VALIDATE CONSTRAINT alert_threshold_scope_code_check;
