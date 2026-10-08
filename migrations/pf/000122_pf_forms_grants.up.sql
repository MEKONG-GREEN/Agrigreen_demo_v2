-- pf_forms (2/2): quyền của `cap/forms` (W6.2, ADR-0028).
--
--   - r_forms: vai nhóm của module (không có vai cũ — dbadmin sinh từ registry, W2.6; app_platform là thành viên).
--     Sổ chỉ SELECT, INSERT (R-05, R-20) — không có UPDATE/DELETE nào, trạng thái là phép chiếu của sổ.
--   - Rà soát bốn mắt qua cap/workflow (W6.1): r_forms gọi pf_workflow.submit/decide trong giao dịch của mình
--     (yêu cầu kind forms.submission, quyết định approved/rejected cùng commit với dòng review — R-21).
--   - Ghi song song của KN (extension/reportcycle, giao dịch vai r_extension): chỉ EXECUTE pf_forms.mirror,
--     không quyền bảng pf_forms.
-- Danh sách vai khớp dbadmin (modroles_test); thiếu vai → lỗi.

REVOKE ALL ON pf_forms.template, pf_forms.period, pf_forms.period_event, pf_forms.submission, pf_forms.review,
              pf_forms.close_snapshot FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_forms.mirror(uuid, uuid, text, uuid, text, text, text, text[], text, jsonb, uuid, timestamptz, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_forms.fn_bind_template(), pf_forms.fn_transition(), pf_forms.fn_bind_period(),
                       pf_forms.fn_bind_submission(), pf_forms.fn_bind_close() FROM PUBLIC;

DO $$
DECLARE
  groups constant text[] := ARRAY['r_extension', 'r_forms'];
  grp text;
BEGIN
  FOREACH grp IN ARRAY groups LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = grp AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)', grp;
    END IF;
    EXECUTE format('GRANT USAGE ON SCHEMA pf_forms TO %I', grp);
    EXECUTE format('GRANT EXECUTE ON FUNCTION pf_forms.mirror(uuid, uuid, text, uuid, text, text, text, text[], text, jsonb, uuid, '
                   'timestamptz, text, text) TO %I', grp);
  END LOOP;
END
$$;

GRANT SELECT, INSERT ON pf_forms.template, pf_forms.period, pf_forms.period_event, pf_forms.submission, pf_forms.review,
                        pf_forms.close_snapshot TO r_forms;

-- cap/workflow: lối ghi của port Approvals (pf 000118) cho rà soát bốn mắt.
GRANT USAGE ON SCHEMA pf_workflow TO r_forms;
GRANT EXECUTE ON FUNCTION pf_workflow.submit(uuid, text, uuid, uuid, uuid, uuid, text, timestamptz, text),
                          pf_workflow.decide(uuid, uuid, text, uuid, uuid, uuid, uuid, text, text, timestamptz, text) TO r_forms;

-- Module pf mới có r_* (docs/dev/modules.md, W2.3 bước 5): đọc k_ref + biến ngữ cảnh (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_forms;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_forms;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_forms;
