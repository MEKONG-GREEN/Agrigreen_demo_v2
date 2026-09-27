-- Job vận hành dùng chung cho MỌI database (ag_pf, ag_mkg, ag_vault).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
-- Nội dung:
--   1. Neo chuỗi hằng ngày (mục 8.2): cho vai job gọi shared.anchor_chains và
--      đọc lại các neo của một ngày để xuất bản ký số.
--   2. Dọn khóa Idempotency-Key đã hết hạn (R-51, lưu 48 giờ).
--   3. Lan truyền ngữ cảnh trace W3C qua outbox (R-90, ADR-0013).
--
-- VAI CỦA JOB: ag_outbox_publisher.
--   - Là vai duy nhất có sẵn ở CẢ BA cụm (dbadmin.Roles) và có CONNECT tới cả ba
--     database; worker_platform chỉ có ở cụm pf.
--   - Có BYPASSRLS — bắt buộc cho việc dọn idempotency xuyên tenant: ag_migrator
--     là owner nhưng bị FORCE RLS, nên một hàm SECURITY DEFINER của owner cũng
--     không thấy dòng nào khi chưa đặt app.tenant_id.
--   - Quyền cấp thêm chỉ là EXECUTE trên hai hàm neo (SECURITY DEFINER, chỉ ghi
--     thêm vào shared.chain_anchor) và DELETE trên bảng vận hành idempotency_key.
--     Không có quyền nào trên bảng nghiệp vụ hay bảng sổ.
--   Khi tách vai riêng cho job vận hành (đề xuất: ag_ops_jobs, LOGIN, BYPASSRLS,
--   mọi cụm) thì một migration sau chỉ cần GRANT lại các quyền dưới đây.

-- ---------------------------------------------------------------------------
-- 1. Neo chuỗi hằng ngày
-- ---------------------------------------------------------------------------
GRANT EXECUTE ON FUNCTION shared.anchor_chains(date) TO ag_outbox_publisher;

-- Đọc các neo của một ngày nghiệp vụ để dựng manifest ký số. SECURITY DEFINER:
-- vai job không có SELECT trên shared.chain_anchor.
CREATE FUNCTION shared.chain_anchors_of(p_business_date date)
RETURNS TABLE (ledger text, tenant_id uuid, stream_key text, last_seq bigint, last_hash bytea, anchored_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
SET timezone = 'UTC'
AS $$
  SELECT a.ledger, a.tenant_id, a.stream_key, a.last_seq, a.last_hash, a.anchored_at
  FROM shared.chain_anchor a
  WHERE a.business_date = p_business_date
  ORDER BY a.ledger, a.tenant_id, a.stream_key
$$;
REVOKE ALL ON FUNCTION shared.chain_anchors_of(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.chain_anchors_of(date) TO ag_outbox_publisher;

-- ---------------------------------------------------------------------------
-- 2. Dọn khóa Idempotency-Key hết hạn
-- ---------------------------------------------------------------------------
-- SECURITY INVOKER: người gọi phải có BYPASSRLS để thấy mọi tenant. Xóa theo lô
-- p_limit dòng để không giữ khóa lâu; trả số dòng đã xóa (gọi lặp tới khi < p_limit).
CREATE FUNCTION shared.purge_expired_idempotency(p_limit integer DEFAULT 5000)
RETURNS bigint
LANGUAGE sql
SET search_path = pg_catalog, pg_temp
AS $$
  WITH victim AS (
    SELECT k.tenant_id, k.idem_key
    FROM shared.idempotency_key k
    WHERE k.expires_at < now()
    ORDER BY k.expires_at
    LIMIT greatest(p_limit, 1)
  ), del AS (
    DELETE FROM shared.idempotency_key k
    USING victim v
    WHERE k.tenant_id = v.tenant_id AND k.idem_key = v.idem_key AND k.expires_at < now()
    RETURNING 1
  )
  SELECT count(*) FROM del
$$;
REVOKE ALL ON FUNCTION shared.purge_expired_idempotency(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.purge_expired_idempotency(integer) TO ag_outbox_publisher;
GRANT SELECT (tenant_id, idem_key, expires_at), DELETE ON shared.idempotency_key TO ag_outbox_publisher;

-- ---------------------------------------------------------------------------
-- 3. Ngữ cảnh trace W3C cho outbox
-- ---------------------------------------------------------------------------
-- trace_id (đã có) giữ nguyên nghĩa; trace_parent là header traceparent đầy đủ
-- (có span cha) để publisher nối span phát vào đúng cây trace của request gốc.
ALTER TABLE shared.outbox ADD COLUMN trace_parent text NULL
  CHECK (trace_parent IS NULL OR trace_parent ~ '^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$');
