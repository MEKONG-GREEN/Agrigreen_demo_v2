-- W2.1 (DB-11, kiến trúc v4.1 §15.3): hạn lưu 30 ngày cho sổ đăng ký idem_key của bảng sổ.
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24). Áp cho schema shared của MỌI database.
--
-- Hợp đồng (ADR-0012, mục "Đối chiếu v4.1"):
--   - Chống trùng idem_key của sổ CHỈ BẢO ĐẢM TRONG 30 NGÀY kể từ lần ghi đầu (gấp 10 lần
--     cửa sổ ngoại tuyến 72 giờ). Trong hạn: ghi lại cùng khóa → unique_violation trên
--     ledger_idem_pkey → ledger.ErrDuplicate, như trước.
--   - Hết hạn (expires_at <= now()): ghi lại cùng khóa được nhận như bản ghi MỚI — kể cả khi
--     job dọn chưa kịp xóa dòng cũ (fn_chain thay dòng hết hạn), để hành vi không phụ thuộc lịch dọn.
--   - Khóa vẫn nằm ở cột idem_key của bảng sổ để tra cứu; chỉ dòng đăng ký bị xóa.
--
-- Dòng có sẵn: expires_at = thời điểm chạy migration + 30 ngày. Không suy từ recorded_at vì
-- migration không được UPDATE (R-25); cách này chỉ KÉO DÀI chống trùng của dòng cũ, không rút ngắn.

ALTER TABLE shared.ledger_idem
  ADD COLUMN expires_at timestamptz NOT NULL DEFAULT now() + interval '30 days';
COMMENT ON COLUMN shared.ledger_idem.expires_at IS
  'W2.1 (DB-11): hết hạn chống trùng — mặc định recorded_at + 30 ngày; dọn bởi shared.purge_expired_ledger_idem';
CREATE INDEX ledger_idem_expires ON shared.ledger_idem (expires_at);

-- fn_chain: giữ nguyên, chỉ đổi bước chống trùng — dòng đăng ký đã hết hạn được thay bằng
-- dòng mới (row_id, recorded_at, expires_at mặc định); còn hạn thì báo unique_violation với
-- CONSTRAINT ledger_idem_pkey như lỗi khóa chính cũ (ledger.Classify không đổi).
CREATE OR REPLACE FUNCTION shared.fn_chain() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
SET timezone = 'UTC'
AS $$
DECLARE
  v_ledger text := TG_ARGV[0];
  v_seq    bigint;
  v_hash   bytea;
BEGIN
  IF v_ledger IS NULL THEN
    RAISE EXCEPTION 'fn_chain: thiếu tên sổ trong tham số trigger';
  END IF;
  IF NEW.tenant_id IS NULL OR NEW.stream_key IS NULL OR NEW.idem_key IS NULL THEN
    RAISE EXCEPTION 'fn_chain: tenant_id, stream_key, idem_key là bắt buộc' USING ERRCODE = 'not_null_violation';
  END IF;
  -- recorded_at do hệ đặt (R-13): chỉ chấp nhận giá trị mặc định now().
  IF NEW.recorded_at IS DISTINCT FROM now() THEN
    RAISE EXCEPTION 'fn_chain: recorded_at do hệ thống ghi, không được truyền vào' USING ERRCODE = 'check_violation';
  END IF;

  -- Chống trùng trong hạn 30 ngày (W2.1): trùng khóa còn hạn → unique_violation trên
  -- ledger_idem_pkey, ứng dụng đọc lại bản ghi cũ. Khóa hết hạn → thay dòng, ghi như mới.
  INSERT INTO shared.ledger_idem AS li (ledger, tenant_id, idem_key, row_id)
  VALUES (v_ledger, NEW.tenant_id, NEW.idem_key, NEW.id)
  ON CONFLICT ON CONSTRAINT ledger_idem_pkey DO UPDATE
    SET row_id = EXCLUDED.row_id, recorded_at = EXCLUDED.recorded_at, expires_at = EXCLUDED.expires_at
    WHERE li.expires_at <= now();
  IF NOT FOUND THEN
    RAISE EXCEPTION 'duplicate key value violates unique constraint "ledger_idem_pkey"'
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'ledger_idem_pkey', SCHEMA = 'shared', TABLE = 'ledger_idem',
            DETAIL = format('Key (ledger, tenant_id, idem_key)=(%s, %s, %s) already exists.', v_ledger, NEW.tenant_id, NEW.idem_key);
  END IF;

  -- Khóa đầu chuỗi: mọi lần ghi cùng chuỗi xếp hàng tại đây.
  INSERT INTO shared.chain_head (ledger, tenant_id, stream_key, last_seq, last_hash)
  VALUES (v_ledger, NEW.tenant_id, NEW.stream_key, 0, '\x00'::bytea)
  ON CONFLICT DO NOTHING;

  SELECT last_seq, last_hash INTO v_seq, v_hash
  FROM shared.chain_head
  WHERE ledger = v_ledger AND tenant_id = NEW.tenant_id AND stream_key = NEW.stream_key
  FOR UPDATE;

  NEW.seq       := v_seq + 1;
  NEW.prev_hash := v_hash;
  NEW.row_hash  := sha256(NEW.prev_hash || shared.canonical(to_jsonb(NEW)));

  UPDATE shared.chain_head
     SET last_seq = NEW.seq, last_hash = NEW.row_hash, updated_at = now()
   WHERE ledger = v_ledger AND tenant_id = NEW.tenant_id AND stream_key = NEW.stream_key;

  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION shared.fn_chain() FROM PUBLIC;

-- Dọn dòng đăng ký hết hạn theo lô (gọi lặp tới khi < p_limit). SECURITY DEFINER của owner:
-- ledger_idem không bật RLS (ag:no-rls) và không cấp quyền bảng cho vai nào — vai job
-- (ag_outbox_publisher) chỉ có EXECUTE trên hàm này. ledger_idem là bảng vận hành, không
-- phải bảng sổ: không có trg_immutable/trg_chain, nên DELETE được phép.
CREATE FUNCTION shared.purge_expired_ledger_idem(p_limit integer DEFAULT 5000)
RETURNS bigint
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
  WITH victim AS (
    SELECT i.ledger, i.tenant_id, i.idem_key
    FROM shared.ledger_idem i
    WHERE i.expires_at < now()
    ORDER BY i.expires_at
    LIMIT greatest(p_limit, 1)
    FOR UPDATE SKIP LOCKED
  ), del AS (
    DELETE FROM shared.ledger_idem i
    USING victim v
    WHERE i.ledger = v.ledger AND i.tenant_id = v.tenant_id AND i.idem_key = v.idem_key
      AND i.expires_at < now()
    RETURNING 1
  )
  SELECT count(*) FROM del
$$;
REVOKE ALL ON FUNCTION shared.purge_expired_ledger_idem(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.purge_expired_ledger_idem(integer) TO ag_outbox_publisher;
