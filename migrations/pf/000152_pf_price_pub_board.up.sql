-- View công bố của pf/price (W11.76 — hợp đồng W11.10: contracts/pubview/pf_price_pub.board_daily.v1.json,
-- pf_price_pub.ticket_week.v1.json; BRD KHTC §3.4, Báo giá BG10; Q-084 đề xuất của dev). Chỉ đụng schema pf_price_pub
-- (migration công bố <cụm>_pub_*); bảng nguồn ở pf 000151 (pf_price.board, board_line, board_ticket).
--
-- "Nguồn đã chốt" = phiên bản bảng giá ĐÃ DUYỆT hiện hành của mỗi ngày: phiên bản mới nhất của chuỗi 'board:…' cho
-- board_version (≥ 1) — dòng của phiên bản đó là ảnh chụp lúc duyệt (bất biến). Ngày đang mở duyệt (hoặc đang duyệt lại
-- sau mở duyệt) vẫn trả phiên bản đã duyệt trước đó, board_reopened = true. Ngày chưa duyệt lần nào không có dòng.
-- Khóa tuần (week_key/week_year/week_from/week_to) và số ngày báo giá theo lịch của tuần (days_scheduled) LƯU lúc duyệt
-- (lịch kỳ hệ thống W11.03, lịch báo giá W11.74) — không tính lại; days_scheduled của tuần lấy từ lần duyệt mới nhất
-- trong tuần (lịch chỉ đổi cho ngày tương lai).
--
-- Không dữ liệu cá nhân (R-14): không actor_ref, không tên người; chỉ mã xã báo giá / ĐVHC, mã mặt hàng, giá (numeric —
-- không cột số thực, R-CODE-01). Chủ view ag_migrator (NOBYPASSRLS), mọi bảng gốc FORCE RLS → p_tenant áp qua view.

CREATE VIEW pf_price_pub.v1_board_daily AS
  WITH cur AS (
    SELECT DISTINCT ON (b.tenant_id, b.stream_key) b.tenant_id, b.profile, b.board_date, b.status, b.base_status, b.board_version
      FROM pf_price.board b
     ORDER BY b.tenant_id, b.stream_key, b.seq DESC),
  pub AS (
    SELECT c.tenant_id, c.profile, c.board_date, c.board_version,
           (c.status = 'reopened' OR (c.status = 'approving' AND c.base_status = 'reopened')) AS board_reopened,
           a.week_key, a.week_year, a.approved_at
      FROM cur c
      JOIN pf_price.board a ON a.tenant_id = c.tenant_id AND a.profile = c.profile AND a.board_date = c.board_date
                           AND a.action = 'approve' AND a.board_version = c.board_version
     WHERE c.board_version >= 1)
  SELECT p.tenant_id, p.profile, p.board_date, p.week_key, p.week_year, p.board_version, p.approved_at, p.board_reopened,
         l.item_code, l.item_name, l.group_code, l.unit_code,
         l.price_min::numeric AS price_min, l.price_max::numeric AS price_max, l.price_avg::numeric AS price_avg,
         l.points_reported
    FROM pub p
    JOIN pf_price.board_line l ON l.tenant_id = p.tenant_id AND l.profile = p.profile AND l.board_date = p.board_date
                              AND l.board_version = p.board_version;

CREATE VIEW pf_price_pub.v1_ticket_week AS
  WITH cur AS (
    SELECT DISTINCT ON (b.tenant_id, b.stream_key) b.tenant_id, b.profile, b.board_date, b.status, b.base_status, b.board_version
      FROM pf_price.board b
     ORDER BY b.tenant_id, b.stream_key, b.seq DESC),
  day AS (
    SELECT c.tenant_id, c.profile, c.board_date, c.board_version,
           (c.status = 'reopened' OR (c.status = 'approving' AND c.base_status = 'reopened')) AS reopened,
           a.week_key, a.week_year, a.week_from, a.week_to, a.days_scheduled, a.approved_at
      FROM cur c
      JOIN pf_price.board a ON a.tenant_id = c.tenant_id AND a.profile = c.profile AND a.board_date = c.board_date
                           AND a.action = 'approve' AND a.board_version = c.board_version
     WHERE c.board_version >= 1),
  week AS (
    SELECT d.tenant_id, d.profile, d.week_key, min(d.week_year) AS week_year, min(d.week_from) AS week_from,
           max(d.week_to) AS week_to, count(*)::integer AS days_approved, bool_or(d.reopened) AS any_reopened,
           ((array_agg(d.days_scheduled ORDER BY d.approved_at DESC))[1])::integer AS days_scheduled
      FROM day d
     GROUP BY d.tenant_id, d.profile, d.week_key)
  SELECT w.tenant_id, w.profile, w.week_key, w.week_year, w.week_from, w.week_to, t.point_code,
         (array_agg(t.commune_code ORDER BY d.board_date DESC))[1] AS commune_code,
         count(*)::integer AS tickets_collected, w.days_scheduled, w.days_approved,
         (w.days_approved >= w.days_scheduled AND NOT w.any_reopened) AS week_closed
    FROM week w
    JOIN day d ON d.tenant_id = w.tenant_id AND d.profile = w.profile AND d.week_key = w.week_key
    JOIN pf_price.board_ticket t ON t.tenant_id = d.tenant_id AND t.profile = d.profile AND t.board_date = d.board_date
                                AND t.board_version = d.board_version
   GROUP BY w.tenant_id, w.profile, w.week_key, w.week_year, w.week_from, w.week_to, t.point_code, w.days_scheduled,
            w.days_approved, w.any_reopened;

GRANT SELECT ON pf_price_pub.v1_board_daily, pf_price_pub.v1_ticket_week TO r_price_pub;
