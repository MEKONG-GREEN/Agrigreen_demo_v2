"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { ApiClientProposal, ApiClientRecord, ApiClientStatus } from "@/api/types";
import {
  adminBlock,
  approveBlock,
  CLIENT_STATUS,
  isUuid,
  reactivateBlock,
  revokeClientBlock,
  shortRef,
  suspendBlock,
  validateClientProposal,
  type FieldError,
} from "@/apiclient/rules";
import { KeysPanel, PlansPanel, SubscriptionsPanel } from "@/apiclient/panels";
import { Chip, GuardedButton, ReasonDialog, who } from "@/apiclient/ui";
import { useAuth } from "@/auth/AuthProvider";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const FILTERS: { id: "" | ApiClientStatus; label: string }[] = [
  { id: "", label: "Tất cả" },
  { id: "proposed", label: "Chờ duyệt" },
  { id: "active", label: "Đang hoạt động" },
  { id: "suspended", label: "Tạm ngưng" },
  { id: "revoked", label: "Đã thu hồi" },
];

const AUTH_LABEL: Record<string, string> = { api_key: "Khóa API", oauth_client_credentials: "OAuth client credentials" };

/**
 * Khách hàng Public API (G5, ADR-0022): đăng ký bên thứ ba (tenant riêng), duyệt/tạm ngưng/bật lại/thu hồi,
 * khóa API (bí mật hiện một lần), đăng ký sản phẩm dữ liệu + scope, gói hạn mức. Admin API, MFA bắt buộc.
 * api_client_admin đề xuất; api_client_approver KHÁC người đề xuất duyệt (R-56 — máy chủ chặn ở app/ + CHECK DB).
 */
export default function ApiClientsPage() {
  const { adminApi, principal } = useAuth();
  const [status, setStatus] = useState<"" | ApiClientStatus>("");
  const [version, setVersion] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const list = usePagedList<ApiClientRecord>(
    (cursor) => unwrap(adminApi.GET("/v1/api-clients", { params: { query: { status: status || undefined, cursor, limit: 100 } } })),
    [adminApi, status, version],
  );
  const reload = () => setVersion((v) => v + 1);
  const canPropose = adminBlock(principal) === null;

  return (
    <>
      <PageHead title="Khách hàng Public API" subtitle="Bên thứ ba dùng dữ liệu tổng hợp qua Public API — khóa lưu băm, scope theo hợp đồng dữ liệu, hạn mức, mọi lời gọi ghi sổ đo lường" />
      <MfaNotice />
      <div className="alert alert-info" role="note" style={{ marginBottom: 16 }}>
        Public API <b>tắt ở ĐTK 1</b> và chỉ bật ở Phase K (ADR-0025). Bên thứ ba chỉ đọc được dữ liệu khi có đủ: khóa hiệu lực, đăng ký sản phẩm đã duyệt và cấp quyền
        dữ liệu <code>dp_*</code> của bên chủ dữ liệu (ADR-0020). Không có cổng tự phục vụ cho bên thứ ba.
      </div>
      {canPropose ? <ProposeClientForm onSaved={reload} /> : null}
      <section className="card sec" aria-labelledby="ac-list" style={{ marginTop: 16 }}>
        <div className="sec-h" style={{ flexWrap: "wrap", gap: 8 }}>
          <h2 id="ac-list">Danh sách khách hàng</h2>
          <div className="row">
            <label className="small" htmlFor="ac-filter">
              Trạng thái
            </label>
            <select id="ac-filter" className="input" value={status} onChange={(e) => setStatus(e.target.value as "" | ApiClientStatus)}>
              {FILTERS.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.loading && list.items.length === 0 ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có khách hàng nào.</Empty> : null}
        {list.items.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Khách hàng Public API">
              <thead>
                <tr>
                  <th scope="col">Tổ chức</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col">Tenant bên thứ ba</th>
                  <th scope="col">Xác thực</th>
                  <th scope="col">Đề xuất</th>
                  <th scope="col">
                    <span className="sr-only">Thao tác</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {list.items.map((c) => (
                  <tr key={c.id} data-client={c.id} aria-selected={selected === c.id}>
                    <td>
                      <b>{c.display_name}</b>
                      <div className="small mute">{c.purpose}</div>
                    </td>
                    <td>
                      <Chip s={CLIENT_STATUS[c.status]} />
                    </td>
                    <td className="small">
                      <code title={c.client_tenant_id}>{shortRef(c.client_tenant_id)}</code>
                    </td>
                    <td className="small">{c.auth_methods.map((m) => AUTH_LABEL[m] ?? m).join(", ")}</td>
                    <td className="small">
                      {who(c.proposed_by, principal)}
                      <div className="mute">{formatDateTime(c.proposed_at)}</div>
                    </td>
                    <td>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setSelected(c.id)} aria-label={`Xem chi tiết ${c.display_name}`}>
                        Chi tiết
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </section>
      {selected ? <ClientDetail key={selected} id={selected} onChanged={reload} onClose={() => setSelected(null)} /> : null}
    </>
  );
}

function ProposeClientForm({ onSaved }: { onSaved: () => void }) {
  const { adminApi } = useAuth();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<ApiClientProposal>({ client_tenant_id: "", display_name: "", purpose: "", contact_ref: "", auth_methods: ["api_key"] });
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const err = (f: string) => errors.find((e) => e.field === f)?.reason;
  const set = (patch: Partial<ApiClientProposal>) => setDraft((d) => ({ ...d, ...patch }));
  const methods = draft.auth_methods ?? [];
  const toggle = (m: "api_key" | "oauth_client_credentials") => set({ auth_methods: methods.includes(m) ? methods.filter((x) => x !== m) : [...methods, m] });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs = validateClientProposal(draft);
    setErrors(errs);
    if (errs.length) return;
    setBusy(true);
    setProblem(null);
    try {
      const body: ApiClientProposal = {
        client_tenant_id: draft.client_tenant_id.trim(),
        display_name: draft.display_name.trim(),
        purpose: draft.purpose.trim(),
        contact_ref: draft.contact_ref.trim(),
        auth_methods: methods,
        ...(methods.includes("oauth_client_credentials") ? { oauth_client_id: (draft.oauth_client_id ?? "").trim() } : {}),
      };
      const c = await unwrap(adminApi.POST("/v1/api-clients", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body }));
      setDone(`Đã đề xuất “${c.display_name}” — chờ người duyệt khác phê duyệt. Sau đó đề xuất đăng ký sản phẩm và gói hạn mức ở phần chi tiết.`);
      setDraft({ client_tenant_id: "", display_name: "", purpose: "", contact_ref: "", auth_methods: ["api_key"] });
      setOpen(false);
      onSaved();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(false);
    }
  };

  if (!open)
    return (
      <div className="stack" style={{ gap: 8 }}>
        {done ? (
          <div className="alert alert-info" role="status">
            {done}
          </div>
        ) : null}
        <div>
          <button type="button" className="btn btn-p btn-sm" onClick={() => setOpen(true)}>
            Đề xuất khách hàng mới
          </button>
        </div>
      </div>
    );

  return (
    <form className="card sec stack" onSubmit={submit} aria-labelledby="ac-new" noValidate>
      <h2 id="ac-new" style={{ fontSize: 15.5 }}>
        Đề xuất khách hàng Public API
      </h2>
      <p className="small mute">
        Bên thứ ba là một tenant riêng. Chỉ ghi tên <b>tổ chức</b> và mã tham chiếu đầu mối — tên, điện thoại người liên hệ nằm ở kho định danh (RB-3).
      </p>
      <div className="grid2">
        <div className="field">
          <label htmlFor="ac-name">Tên tổ chức</label>
          <input id="ac-name" className="input" value={draft.display_name} onChange={(e) => set({ display_name: e.target.value })} />
          {err("display_name") ? <span className="err">{err("display_name")}</span> : null}
        </div>
        <div className="field">
          <label htmlFor="ac-tenant">Mã tenant bên thứ ba (UUID)</label>
          <input id="ac-tenant" className="input" value={draft.client_tenant_id} onChange={(e) => set({ client_tenant_id: e.target.value })} placeholder="0192…" />
          {err("client_tenant_id") ? <span className="err">{err("client_tenant_id")}</span> : null}
        </div>
        <div className="field">
          <label htmlFor="ac-contact">Mã đầu mối (UUID, không nhập tên/SĐT)</label>
          <input id="ac-contact" className="input" value={draft.contact_ref} onChange={(e) => set({ contact_ref: e.target.value })} />
          {err("contact_ref") ? <span className="err">{err("contact_ref")}</span> : null}
        </div>
        <fieldset className="plain">
          <legend className="small">Cách xác thực</legend>
          <label className="row small">
            <input type="checkbox" checked={methods.includes("api_key")} onChange={() => toggle("api_key")} /> Khóa API (X-API-Key)
          </label>
          <label className="row small">
            <input type="checkbox" checked={methods.includes("oauth_client_credentials")} onChange={() => toggle("oauth_client_credentials")} /> OAuth client credentials (đơn vị thẩm định)
          </label>
          {err("auth_methods") ? <span className="err">{err("auth_methods")}</span> : null}
        </fieldset>
        {methods.includes("oauth_client_credentials") ? (
          <div className="field">
            <label htmlFor="ac-oauth">Mã client OAuth (Keycloak)</label>
            <input id="ac-oauth" className="input" value={draft.oauth_client_id ?? ""} onChange={(e) => set({ oauth_client_id: e.target.value })} />
            {err("oauth_client_id") ? <span className="err">{err("oauth_client_id")}</span> : null}
          </div>
        ) : null}
      </div>
      <div className="field">
        <label htmlFor="ac-purpose">Mục đích sử dụng dữ liệu</label>
        <textarea id="ac-purpose" className="input" rows={2} value={draft.purpose} onChange={(e) => set({ purpose: e.target.value })} />
        {err("purpose") ? <span className="err">{err("purpose")}</span> : null}
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div className="row">
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang gửi…" : "Đề xuất và gửi duyệt"}
        </button>
        <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(false)}>
          Hủy
        </button>
      </div>
    </form>
  );
}

type Transition = "approve" | "suspend" | "reactivate" | "revoke";

function ClientDetail({ id, onChanged, onClose }: { id: string; onChanged: () => void; onClose: () => void }) {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const [tab, setTab] = useState("keys");
  const [busy, setBusy] = useState<Transition | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [ask, setAsk] = useState<"suspend" | "revoke" | null>(null);
  const client = useAsync(() => unwrap(adminApi.GET("/v1/api-clients/{id}", { params: { path: { id } } })), [adminApi, id, version]);
  const refresh = () => {
    setVersion((v) => v + 1);
    onChanged();
  };

  const run = async (op: Transition, reason?: string) => {
    setBusy(op);
    setProblem(null);
    const params = { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } };
    try {
      if (op === "approve") await unwrap(adminApi.POST("/v1/api-clients/{id}/approve", { params }));
      else if (op === "reactivate") await unwrap(adminApi.POST("/v1/api-clients/{id}/reactivate", { params }));
      else if (op === "suspend") await unwrap(adminApi.POST("/v1/api-clients/{id}/suspend", { params, body: { reason: reason ?? "" } }));
      else await unwrap(adminApi.POST("/v1/api-clients/{id}/revoke", { params, body: { reason: reason ?? "" } }));
      setAsk(null);
      refresh();
    } catch (x) {
      const p = toApiProblem(x);
      if (reason !== undefined) throw p; // hiển thị trong hộp thoại lý do
      setProblem(p);
    } finally {
      setBusy(null);
    }
  };

  const c = client.data;
  return (
    <section className="card sec" aria-labelledby="ac-detail" style={{ marginTop: 16 }} data-testid="client-detail">
      <div className="sec-h" style={{ flexWrap: "wrap", gap: 8 }}>
        <h2 id="ac-detail">{c ? c.display_name : "Chi tiết khách hàng"}</h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng chi tiết
        </button>
      </div>
      {client.error ? <ProblemAlert problem={client.error} onRetry={client.reload} /> : null}
      {client.loading && !c ? <Loading /> : null}
      {c ? (
        <>
          <dl className="kv small">
            <dt>Trạng thái</dt>
            <dd>
              <Chip s={CLIENT_STATUS[c.status]} />
              {c.status_reason ? <span className="mute"> · Lý do: {c.status_reason}</span> : null}
            </dd>
            <dt>Mục đích</dt>
            <dd>{c.purpose}</dd>
            <dt>Tenant bên thứ ba</dt>
            <dd>
              <code>{c.client_tenant_id}</code>
            </dd>
            <dt>Mã đầu mối</dt>
            <dd>
              <code>{isUuid(c.contact_ref) ? shortRef(c.contact_ref) : c.contact_ref}</code>
            </dd>
            {c.oauth_client_id ? (
              <>
                <dt>Client OAuth</dt>
                <dd>
                  <code>{c.oauth_client_id}</code>
                </dd>
              </>
            ) : null}
            <dt>Đề xuất</dt>
            <dd>
              {who(c.proposed_by, principal)} · {formatDateTime(c.proposed_at)}
            </dd>
            <dt>Duyệt</dt>
            <dd>{c.approved_by ? `${who(c.approved_by, principal)} · ${formatDateTime(c.approved_at)}` : "—"}</dd>
          </dl>
          <div className="row" style={{ gap: 12, flexWrap: "wrap", alignItems: "flex-start", marginTop: 10 }} aria-label="Thao tác khách hàng" role="group">
            {c.status === "proposed" ? (
              <GuardedButton id={`ac-approve-${c.id}`} label="Phê duyệt khách hàng" busyLabel="Đang duyệt…" block={approveBlock(c, principal)} busy={busy === "approve"} onClick={() => void run("approve")} />
            ) : null}
            {c.status === "active" ? (
              <GuardedButton id={`ac-suspend-${c.id}`} label="Tạm ngưng" variant="btn-o" block={suspendBlock(c, principal)} busy={busy === "suspend"} onClick={() => setAsk("suspend")} />
            ) : null}
            {c.status === "suspended" ? (
              <GuardedButton id={`ac-react-${c.id}`} label="Bật lại" block={reactivateBlock(c, principal)} busy={busy === "reactivate"} onClick={() => void run("reactivate")} />
            ) : null}
            {c.status !== "revoked" ? (
              <GuardedButton id={`ac-revoke-${c.id}`} label="Thu hồi khách hàng" variant="btn-d" block={revokeClientBlock(c, principal)} busy={busy === "revoke"} onClick={() => setAsk("revoke")} />
            ) : null}
          </div>
          {problem ? <ProblemAlert problem={problem} /> : null}
          <div style={{ marginTop: 16 }}>
            <Tabs
              label="Chi tiết khách hàng"
              value={tab}
              onChange={setTab}
              tabs={[
                { id: "keys", label: "Khóa API" },
                { id: "subs", label: "Đăng ký sản phẩm" },
                { id: "plans", label: "Gói hạn mức" },
              ]}
            >
              {tab === "keys" ? <KeysPanel client={c} /> : tab === "subs" ? <SubscriptionsPanel client={c} /> : <PlansPanel client={c} />}
            </Tabs>
          </div>
        </>
      ) : null}
      {ask && c ? (
        <ReasonDialog
          title={ask === "suspend" ? `Tạm ngưng ${c.display_name}` : `Thu hồi ${c.display_name}`}
          intro={
            ask === "suspend"
              ? "Mọi khóa của khách hàng bị từ chối (401) ngay ở lời gọi kế tiếp. Người duyệt có thể bật lại."
              : "Thu hồi là bước cuối: mọi khóa và đăng ký bị thu hồi, không bật lại được. Lịch sử vẫn giữ trong sổ."
          }
          confirmLabel={ask === "suspend" ? "Tạm ngưng" : "Thu hồi"}
          onClose={() => setAsk(null)}
          onConfirm={(reason) => run(ask, reason)}
        />
      ) : null}
    </section>
  );
}
