"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { AcctbridgeMappingRule } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { SoD, When } from "@/components/g4kit";
import { ActionStatus, Chip, Provisional, Section, SelectField, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { COST_CATEGORY_LABEL, DIMENSION_ON_LABEL } from "@/lib/g4labels";
import { shortHash } from "@/lib/money";
import { validReason, VERSION_STATE_LABEL } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const ACCOUNT = /^[0-9]{3,4}[0-9A-Za-z.]{0,16}$/;

function RulesTable({ rules, label, edit }: { rules: AcctbridgeMappingRule[]; label: string; edit?: (i: number, r: Partial<AcctbridgeMappingRule>) => void }) {
  return (
    <div className="tbl-wrap">
      <table className="tbl" aria-label={label}>
        <thead>
          <tr>
            <th>Nhóm costing</th>
            <th>Chiều</th>
            <th>TK Nợ</th>
            <th>TK Có</th>
            <th>Ghi chiều</th>
          </tr>
        </thead>
        <tbody>
          {rules.map((r, i) => (
            <tr key={`${r.category}-${r.dimension_kind}`}>
              <td>{COST_CATEGORY_LABEL[r.category] ?? r.category}</td>
              <td className="mono">{r.dimension_kind === "*" ? "mọi chiều" : r.dimension_kind}</td>
              <td>
                {edit ? (
                  <input className="input mono" aria-label={`TK Nợ ${r.category}`} value={r.debit_account} onChange={(e) => edit(i, { debit_account: e.target.value.trim() })} style={{ width: 90 }} />
                ) : (
                  <span className="mono">{r.debit_account}</span>
                )}
              </td>
              <td>
                {edit ? (
                  <input className="input mono" aria-label={`TK Có ${r.category}`} value={r.credit_account} onChange={(e) => edit(i, { credit_account: e.target.value.trim() })} style={{ width: 90 }} />
                ) : (
                  <span className="mono">{r.credit_account}</span>
                )}
              </td>
              <td className="small">{DIMENSION_ON_LABEL[r.dimension_on]}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function rulesError(rules: AcctbridgeMappingRule[]): string | null {
  for (const r of rules) {
    if (!ACCOUNT.test(r.debit_account) || !ACCOUNT.test(r.credit_account)) return `Số hiệu tài khoản không hợp lệ ở nhóm ${r.category}`;
    if (r.debit_account === r.credit_account) return `Nợ trùng Có ở nhóm ${r.category}`;
  }
  return null;
}

export default function MappingsPage() {
  const { adminApi, principal } = useAuth();
  const data = useAsync(() => unwrap(adminApi.GET("/v1/acctbridge/mappings")), [adminApi]);
  const act = useAction();
  const [draft, setDraft] = useState<AcctbridgeMappingRule[] | null>(null);
  const [reason, setReason] = useState("");
  const [from, setFrom] = useState("");
  const [provisional, setProvisional] = useState("yes");
  const [decisionReason, setDecisionReason] = useState("");
  const operator = hasRole(principal, ROLES.accountingBridgeOperator);
  const chief = hasRole(principal, ROLES.chiefAccountant);
  if (data.loading && !data.data) return <Loading />;
  if (data.error) return <ProblemAlert problem={data.error} onRetry={data.reload} />;
  const d = data.data!;
  const versions = d.versions ?? [];
  const pending = versions.find((v) => v.status === "pending");
  const eff = d.effective ?? null;
  const err = draft ? rulesError(draft) : null;
  const done = (x: unknown) => {
    if (x) {
      setDraft(null);
      setReason("");
      setDecisionReason("");
      data.reload();
    }
  };
  return (
    <div className="stack">
      <PageHead
        title="Ánh xạ tài khoản"
        subtitle="Bảng (nhóm chi phí × chiều) → tài khoản Nợ / Có có phiên bản. Vận hành cầu nối đề xuất — kế toán trưởng KHÁC người đề xuất duyệt (R-56). Mặc định kiểu VAS TT200 là tạm, chờ khách chốt chế độ kế toán và hệ thống tài khoản (Q-1)."
      />
      <ActionStatus a={act} />
      <Section title={eff ? `Phiên bản hiệu lực v${eff.version}` : "Chưa có phiên bản được duyệt"}>
        {eff ? (
          <div className="stack">
            <p className="small">
              Hiệu lực từ {formatDate(eff.effective_from)} · băm quy tắc <span className="mono" title={eff.rules_sha256}>{shortHash(eff.rules_sha256)}</span> {eff.provisional ? <Provisional what="Q-1" /> : null} · <SoD maker={eff.proposed_by} checker={eff.decided_by} makerLabel="Đề xuất" checkerLabel="Duyệt" />
            </p>
            <RulesTable rules={eff.rules ?? []} label="Quy tắc ánh xạ hiệu lực" />
          </div>
        ) : (
          <Empty>Dựng bút toán sẽ lỗi acctbridge.mapping_missing tới khi có phiên bản được duyệt.</Empty>
        )}
      </Section>
      {pending ? (
        <Section title={`Phiên bản v${pending.version} chờ duyệt`}>
          <div className="stack">
            <p className="small">
              {pending.proposal_reason} · đề xuất <When at={pending.proposed_at} /> · hiệu lực từ {formatDate(pending.effective_from)} {pending.provisional ? <Provisional what="Q-1" /> : null}
            </p>
            <SelfHint proposer={pending.proposed_by} />
            <RulesTable rules={pending.rules ?? []} label="Quy tắc chờ duyệt" />
            {chief ? (
              <>
                <TextField label="Lý do từ chối ánh xạ" value={decisionReason} onChange={setDecisionReason} hint="Bắt buộc khi từ chối" />
                <div className="row">
                  <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => unwrap(adminApi.POST("/v1/acctbridge/mappings/{id}/decision", { params: { path: { id: pending.version_id! }, header: idem() }, body: { decision: "approved" } })), `Đã duyệt ánh xạ v${pending.version}`).then(done)}>
                    Duyệt ánh xạ
                  </button>
                  <button type="button" className="btn btn-d btn-sm" disabled={act.busy || !validReason(decisionReason)} onClick={() => void act.run(() => unwrap(adminApi.POST("/v1/acctbridge/mappings/{id}/decision", { params: { path: { id: pending.version_id! }, header: idem() }, body: { decision: "rejected", reason: decisionReason.trim() } })), `Đã từ chối ánh xạ v${pending.version}`).then(done)}>
                    Từ chối ánh xạ
                  </button>
                </div>
              </>
            ) : null}
          </div>
        </Section>
      ) : null}
      {operator && !pending ? (
        <Section title="Đề xuất phiên bản ánh xạ mới">
          {!draft ? (
            <button type="button" className="btn btn-o btn-sm" onClick={() => setDraft((eff?.rules ?? d.default_rules ?? []).map((r) => ({ ...r })))}>
              Soạn từ phiên bản hiện hành
            </button>
          ) : (
            <div className="stack">
              <RulesTable rules={draft} label="Quy tắc đề xuất" edit={(i, r) => setDraft(draft.map((x, j) => (j === i ? { ...x, ...r } : x)))} />
              <div className="grid3">
                <TextField label="Hiệu lực từ ngày" type="date" value={from} onChange={setFrom} hint="Bỏ trống = hôm nay" />
                <SelectField label="Tạm, chờ khách" value={provisional} onChange={setProvisional} options={[{ value: "yes", label: "Có — chờ khách chốt" }, { value: "no", label: "Không — khách đã chốt" }]} />
                <TextField label="Lý do đề xuất ánh xạ" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
              </div>
              {err ? <p className="pill-bad">{err}</p> : null}
              <div className="row">
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  disabled={act.busy || !!err || !validReason(reason)}
                  onClick={() => void act.run(() => unwrap(adminApi.POST("/v1/acctbridge/mappings", { params: { header: idem() }, body: { rules: draft, reason: reason.trim(), provisional: provisional === "yes", ...(from ? { effective_from: from } : {}) } })), (r) => `Đã đề xuất ánh xạ v${r.mapping?.version ?? ""} — chờ kế toán trưởng khác duyệt`).then(done)}
                >
                  Gửi đề xuất ánh xạ
                </button>
                <button type="button" className="btn btn-o btn-sm" onClick={() => setDraft(null)}>
                  Huỷ soạn
                </button>
              </div>
            </div>
          )}
        </Section>
      ) : null}
      <Section title="Lịch sử phiên bản">
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Phiên bản ánh xạ">
            <thead>
              <tr>
                <th>Phiên bản</th>
                <th>Trạng thái</th>
                <th>Hiệu lực từ</th>
                <th>Số quy tắc</th>
                <th>Đề xuất → quyết định</th>
                <th>Lý do</th>
              </tr>
            </thead>
            <tbody>
              {versions.map((v) => (
                <tr key={v.version_id}>
                  <td className="mono">v{v.version}</td>
                  <td>
                    <Chip status={v.status} label={VERSION_STATE_LABEL[v.status ?? ""]} /> {v.provisional ? <span className="chip c-warn">tạm</span> : null}
                  </td>
                  <td>{formatDate(v.effective_from)}</td>
                  <td>{v.rules?.length ?? 0}</td>
                  <td>
                    <SoD maker={v.proposed_by} checker={v.decided_by} makerLabel="Đề xuất" checkerLabel="Quyết định" />
                  </td>
                  <td className="small">
                    {v.proposal_reason}
                    {v.decision_reason ? ` — ${v.decision_reason}` : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Section>
    </div>
  );
}
