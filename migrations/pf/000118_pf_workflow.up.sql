-- pf_workflow: năng lực duyệt bốn mắt dùng chung `cap/workflow` (W6.1, kiến trúc v4.1 §26, R-56, ADR-0027).
--
-- Hai sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--   - request  — một yêu cầu duyệt cho MỘT đối tượng nghiệp vụ (kind, subject_ref): người lập, người được hưởng.
--   - decision — quyết định DUY NHẤT của một yêu cầu: approved | rejected | cancelled.
--
-- Bất biến ở DB (không chỉ ở app/):
--   - Người duyệt ≠ người lập, ≠ người được hưởng (CHECK decision_four_eyes) cho approved/rejected. Hai cột
--     requester_ref/beneficiary_ref của decision KHÔNG do bên ghi tự khai: trigger trg_bind_request (chạy trước
--     trg_chain theo thứ tự tên) chép từ dòng request cùng tenant, không có request → 23503.
--   - Một đối tượng một yêu cầu, một yêu cầu một quyết định: stream_key cố định theo đối tượng/yêu cầu và
--     CHECK seq = 1 (fn_chain đếm seq theo chuỗi — dòng thứ hai của cùng chuỗi có seq = 2, bị chặn).
--   - cancelled (bên lập rút, đối tượng bị thu hồi trước khi duyệt) không cần bốn mắt; rejected/cancelled phải có ghi chú.
--
-- Ghi: module nghiệp vụ (pf/masterdata — duyệt tham số; pf/party — cấp quyền chia sẻ) gọi port của pf/workflow
-- TRONG GIAO DỊCH CỦA CHÍNH NÓ (thay đổi nghiệp vụ và quyết định cùng commit, R-21). Giao dịch đó chạy dưới vai
-- của module gọi, nên lối ghi là hai hàm SECURITY DEFINER pf_workflow.submit / pf_workflow.decide: bên gọi chỉ
-- có EXECUTE (không có quyền bảng pf_workflow), thân hàm cố định, RLS vẫn áp (FORCE RLS — chủ bảng cũng bị lọc
-- theo app.tenant_id của giao dịch). Như shared.fn_chain: SECURITY DEFINER + search_path cố định.
-- Vai của module r_workflow (W2.3, vai nhóm không có vai cũ — dbadmin.groupOnlyModules): SELECT, INSERT hai sổ
-- cho giao dịch riêng của module (đọc trạng thái qua db.ForModule).
--
-- Không dữ liệu cá nhân (R-04): chỉ mã tham chiếu người (actor_ref), mã đối tượng và tóm tắt ngắn.

CREATE SCHEMA pf_workflow;

CREATE TABLE pf_workflow.request (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'subject:<kind>:<subject_ref>'
  seq               bigint      NOT NULL,
  kind              text        NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{0,39}\.[a-z][a-z0-9_]{0,59}$'), -- '<module>.<đối tượng>'
  subject_ref       uuid        NOT NULL,                 -- mã đối tượng của module chủ (phiên bản tham số, cấp quyền…)
  requester_ref     uuid        NOT NULL,                 -- người lập
  beneficiary_ref   uuid        NULL,                     -- người được hưởng (nếu là một người)
  summary           text        NULL CHECK (summary IS NULL OR char_length(summary) BETWEEN 1 AND 200),
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người ghi (≠ requester khi yêu cầu được lập bù lúc quyết định)
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'submit:<kind>:<subject_ref>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT request_one_per_subject CHECK (stream_key = 'subject:' || kind || ':' || subject_ref::text AND seq = 1)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX request_subject ON pf_workflow.request (tenant_id, kind, subject_ref);
CREATE INDEX request_id      ON pf_workflow.request (tenant_id, id);
SELECT shared.apply_ledger('pf_workflow.request', 'pf_workflow.request');

CREATE TABLE pf_workflow.decision (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'request:<request_id>'
  seq               bigint      NOT NULL,
  request_id        uuid        NOT NULL,
  kind              text        NOT NULL,                 -- chép từ request (trg_bind_request)
  subject_ref       uuid        NOT NULL,                 -- chép từ request
  requester_ref     uuid        NOT NULL,                 -- chép từ request
  beneficiary_ref   uuid        NULL,                     -- chép từ request
  decider_ref       uuid        NOT NULL,
  outcome           text        NOT NULL CHECK (outcome IN ('approved', 'rejected', 'cancelled')),
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 1000),
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'decide:<request_id>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (actor_ref = decider_ref),
  CONSTRAINT decision_one_per_request CHECK (stream_key = 'request:' || request_id::text AND seq = 1),
  -- R-56 / §26: người duyệt khác người lập, khác người được hưởng.
  CONSTRAINT decision_four_eyes CHECK (outcome = 'cancelled' OR (decider_ref <> requester_ref AND decider_ref IS DISTINCT FROM beneficiary_ref)),
  CONSTRAINT decision_note_required CHECK (outcome = 'approved' OR char_length(btrim(coalesce(note, ''))) >= 1)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX decision_request ON pf_workflow.decision (tenant_id, request_id);
CREATE INDEX decision_subject ON pf_workflow.decision (tenant_id, kind, subject_ref);

-- Gắn quyết định vào yêu cầu (kiểm chéo dòng, R-56 ở DB): chép kind, subject_ref, requester_ref, beneficiary_ref từ
-- request cùng tenant (RLS của người ghi vẫn áp). Tên trigger xếp TRƯỚC trg_chain để chuỗi băm phủ giá trị đã chép.
CREATE FUNCTION pf_workflow.fn_bind_request() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r record;
BEGIN
  SELECT q.kind, q.subject_ref, q.requester_ref, q.beneficiary_ref INTO r
    FROM pf_workflow.request q
   WHERE q.tenant_id = NEW.tenant_id AND q.id = NEW.request_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_workflow: không có yêu cầu % trong tenant', NEW.request_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'decision_request_fkey';
  END IF;
  NEW.kind            := r.kind;
  NEW.subject_ref     := r.subject_ref;
  NEW.requester_ref   := r.requester_ref;
  NEW.beneficiary_ref := r.beneficiary_ref;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_request BEFORE INSERT ON pf_workflow.decision
  FOR EACH ROW EXECUTE FUNCTION pf_workflow.fn_bind_request();
SELECT shared.apply_ledger('pf_workflow.decision', 'pf_workflow.decision');

-- Lối ghi của port (W6.1). Tenant = app.tenant_id của giao dịch bên gọi; thiếu → 42501.
-- submit: idempotent theo (kind, subject_ref) — đã có yêu cầu cùng người lập/người hưởng thì trả lại (o_replayed);
-- khác người lập/người hưởng → SQLSTATE AGW01.
CREATE FUNCTION pf_workflow.submit(p_id uuid, p_kind text, p_subject uuid, p_requester uuid, p_beneficiary uuid,
                                   p_actor uuid, p_summary text, p_event_time timestamptz, p_rule_version text)
RETURNS TABLE (o_request_id uuid, o_requester_ref uuid, o_beneficiary_ref uuid, o_seq bigint, o_replayed boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_tenant uuid := shared.current_tenant();
  r pf_workflow.request%ROWTYPE;
BEGIN
  IF v_tenant IS NULL THEN
    RAISE EXCEPTION 'pf_workflow.submit: chưa đặt app.tenant_id' USING ERRCODE = 'insufficient_privilege';
  END IF;
  SELECT * INTO r FROM pf_workflow.request q WHERE q.tenant_id = v_tenant AND q.kind = p_kind AND q.subject_ref = p_subject;
  IF FOUND THEN
    IF r.requester_ref <> p_requester OR r.beneficiary_ref IS DISTINCT FROM p_beneficiary THEN
      RAISE EXCEPTION 'pf_workflow.submit: yêu cầu % đã có với người lập/người được hưởng khác', r.id USING ERRCODE = 'AGW01';
    END IF;
    RETURN QUERY SELECT r.id, r.requester_ref, r.beneficiary_ref, r.seq, true;
    RETURN;
  END IF;
  INSERT INTO pf_workflow.request (id, tenant_id, stream_key, kind, subject_ref, requester_ref, beneficiary_ref, summary,
                                   event_time, actor_ref, idem_key, rule_version)
  VALUES (p_id, v_tenant, 'subject:' || p_kind || ':' || p_subject::text, p_kind, p_subject, p_requester, p_beneficiary, p_summary,
          p_event_time, p_actor, 'submit:' || p_kind || ':' || p_subject::text, p_rule_version)
  RETURNING * INTO r;
  RETURN QUERY SELECT r.id, r.requester_ref, r.beneficiary_ref, r.seq, false;
END $$;

-- decide: lập bù yêu cầu nếu chưa có (đối tượng gửi duyệt trước W6.1 — chạy song song, nguyên tắc 3), rồi ghi
-- quyết định. Gọi lặp cùng người quyết định + cùng kết quả → trả lại (o_replayed); đã có quyết định khác → AGW02.
-- Bốn mắt: CHECK decision_four_eyes (23514).
CREATE FUNCTION pf_workflow.decide(p_request_id uuid, p_decision_id uuid, p_kind text, p_subject uuid, p_requester uuid,
                                   p_beneficiary uuid, p_decider uuid, p_outcome text, p_note text,
                                   p_event_time timestamptz, p_rule_version text)
RETURNS TABLE (o_decision_id uuid, o_request_id uuid, o_outcome text, o_decider_ref uuid, o_event_time timestamptz, o_replayed boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_tenant uuid := shared.current_tenant();
  v_request uuid;
  d pf_workflow.decision%ROWTYPE;
BEGIN
  IF v_tenant IS NULL THEN
    RAISE EXCEPTION 'pf_workflow.decide: chưa đặt app.tenant_id' USING ERRCODE = 'insufficient_privilege';
  END IF;
  SELECT s.o_request_id INTO v_request
    FROM pf_workflow.submit(p_request_id, p_kind, p_subject, p_requester, p_beneficiary, p_decider, NULL, p_event_time, p_rule_version) s;
  SELECT * INTO d FROM pf_workflow.decision x WHERE x.tenant_id = v_tenant AND x.request_id = v_request;
  IF FOUND THEN
    IF d.outcome = p_outcome AND d.decider_ref = p_decider THEN
      RETURN QUERY SELECT d.id, d.request_id, d.outcome, d.decider_ref, d.event_time, true;
      RETURN;
    END IF;
    RAISE EXCEPTION 'pf_workflow.decide: yêu cầu % đã có quyết định %', v_request, d.outcome USING ERRCODE = 'AGW02';
  END IF;
  INSERT INTO pf_workflow.decision (id, tenant_id, stream_key, request_id, kind, subject_ref, requester_ref, beneficiary_ref,
                                    decider_ref, outcome, note, event_time, actor_ref, idem_key, rule_version)
  VALUES (p_decision_id, v_tenant, 'request:' || v_request::text, v_request, p_kind, p_subject, p_requester, p_beneficiary,
          p_decider, p_outcome, p_note, p_event_time, p_decider, 'decide:' || v_request::text, p_rule_version)
  RETURNING * INTO d;
  RETURN QUERY SELECT d.id, d.request_id, d.outcome, d.decider_ref, d.event_time, false;
END $$;

-- Quyền. Hàm mặc định cho PUBLIC EXECUTE — thu lại.
REVOKE ALL ON FUNCTION pf_workflow.submit(uuid, text, uuid, uuid, uuid, uuid, text, timestamptz, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_workflow.decide(uuid, uuid, text, uuid, uuid, uuid, uuid, text, text, timestamptz, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_workflow.fn_bind_request() FROM PUBLIC;
REVOKE ALL ON pf_workflow.request, pf_workflow.decision FROM PUBLIC;

DO $$
DECLARE
  -- r_workflow: vai của module. r_masterdata, r_party: module gọi port trong giao dịch của mình (chỉ EXECUTE).
  -- Danh sách khớp dbadmin (TestMigration105Grants); thiếu vai → lỗi.
  groups constant text[] := ARRAY['r_masterdata', 'r_party', 'r_workflow'];
  grp text;
BEGIN
  FOREACH grp IN ARRAY groups LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = grp AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', grp;
    END IF;
    EXECUTE format('GRANT USAGE ON SCHEMA pf_workflow TO %I', grp);
    EXECUTE format('GRANT EXECUTE ON FUNCTION pf_workflow.submit(uuid, text, uuid, uuid, uuid, uuid, text, timestamptz, text), '
                   'pf_workflow.decide(uuid, uuid, text, uuid, uuid, uuid, uuid, text, text, timestamptz, text) TO %I', grp);
  END LOOP;
END
$$;

-- Sổ của module: chỉ SELECT, INSERT (R-05, R-20).
GRANT SELECT, INSERT ON pf_workflow.request, pf_workflow.decision TO r_workflow;

-- Module pf mới có r_* (docs/dev/modules.md, W2.3 bước 5): đọc k_ref + biến ngữ cảnh (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_workflow;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_workflow;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_workflow;
