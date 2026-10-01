"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { ApiClientRecord, ApiDataProduct, ApiKeyIssued, ApiKeyRecord, ApiQuotaPlanProposal, ApiSubscription } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { formatCount } from "@/metering/money";
import {
  adminBlock,
  approveBlock,
  DEFAULT_PLAN,
  effectivePlan,
  issueKeyBlock,
  KEY_STATUS,
  PLAN_STATUS,
  PRODUCTS,
  PRODUCT_LABEL,
  scopeLabel,
  SUB_STATUS,
  validatePlan,
  validateSubscription,
  type FieldError,
} from "./rules";
import { SecretOnceDialog, type SecretOnce } from "./SecretOnceDialog";
import { Chip, GuardedButton, ReasonDialog, who } from "./ui";

const idem = () => ({ "Idempotency-Key": newIdempotencyKey() });

// ---------------------------------------------------------------------------
// Khóa API
// ---------------------------------------------------------------------------

/**
 * Khóa API: chỉ tiền tố, trạng thái, hạn (máy chủ không bao giờ trả bí mật hay HMAC ở danh sách).
 * Phát hành/xoay → bí mật trong phản hồi 201 đi THẲNG vào hộp thoại một lần; đóng là xóa khỏi state.
 */
export function KeysPanel({ client }: { client: ApiClientRecord }) {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const keys = useAsync(() => unwrap(adminApi.GET("/v1/api-clients/{id}/keys", { params: { path: { id: client.id } } })), [adminApi, client.id, version]);
  const [secret, setSecret] = useState<SecretOnce | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [revoking, setRevoking] = useState<ApiKeyRecord | null>(null);
  const items = keys.data?.items ?? [];
  const reload = () => setVersion((v) => v + 1);

  const showIssued = (r: ApiKeyIssued) => {
    if (r.api_key) {
      setSecret({ apiKey: r.api_key, keyPrefix: r.key_prefix, expiresAt: r.expires_at, previousGraceUntil: r.previous_key_grace_until ?? null, clientName: client.display_name });
      setNotice(null);
    } else {
      // Phát lại cùng Idempotency-Key: máy chủ không trả lại bí mật (ADR-0022 §4).
      setNotice(`Máy chủ không trả lại bí mật của khóa ${r.key_prefix} (lời gọi lặp). Nếu chưa lưu khóa, thu hồi và phát hành khóa mới.`);
    }
  };

  const issue = async () => {
    setBusy("issue");
    setProblem(null);
    try {
      showIssued(await unwrap(adminApi.POST("/v1/api-clients/{id}/keys", { params: { path: { id: client.id }, header: idem() } })));
      reload();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };

  const rotate = async (k: ApiKeyRecord) => {
    setBusy(`rotate-${k.id}`);
    setProblem(null);
    try {
      showIssued(await unwrap(adminApi.POST("/v1/api-clients/{id}/keys/{key_id}/rotate", { params: { path: { id: client.id, key_id: k.id }, header: idem() } })));
      reload();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };

  const revoke = async (k: ApiKeyRecord, reason: string) => {
    await unwrap(adminApi.POST("/v1/api-clients/{id}/keys/{key_id}/revoke", { params: { path: { id: client.id, key_id: k.id }, header: idem() }, body: { reason } }));
    setRevoking(null);
    reload();
  };

  const admin = adminBlock(principal);
  return (
    <div className="stack" style={{ gap: 10, paddingTop: 12 }}>
      <p className="small mute">
        Khóa dạng <code>agk_&lt;tiền tố&gt;_&lt;bí mật&gt;</code>, hạn 365 ngày, tối đa 2 khóa hiệu lực. Xoay khóa: khóa cũ còn dùng được 7 ngày. Hệ thống chỉ lưu tiền tố và
        mã băm — không ai xem lại được bí mật.
      </p>
      <div>
        <GuardedButton id={`issue-${client.id}`} label="Phát hành khóa mới" busyLabel="Đang phát hành…" block={issueKeyBlock(client, items, principal)} busy={busy === "issue"} onClick={() => void issue()} />
      </div>
      {notice ? (
        <div className="alert alert-warn" role="status">
          {notice}
        </div>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {keys.error ? <ProblemAlert problem={keys.error} onRetry={keys.reload} /> : null}
      {keys.loading && !keys.data ? <Loading /> : null}
      {keys.data && items.length === 0 ? <Empty>Chưa có khóa nào.</Empty> : null}
      {items.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Khóa API">
            <thead>
              <tr>
                <th scope="col">Khóa</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Phát hành</th>
                <th scope="col">Hết hạn</th>
                <th scope="col">Ghi chú</th>
                <th scope="col">
                  <span className="sr-only">Thao tác</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((k) => (
                <tr key={k.id} data-key-prefix={k.key_prefix} data-status={k.status}>
                  <td>
                    <code>agk_{k.key_prefix}_••••</code>
                  </td>
                  <td>
                    <Chip s={KEY_STATUS[k.status]} />
                  </td>
                  <td className="small">{formatDateTime(k.issued_at)}</td>
                  <td className="small">{formatDateTime(k.expires_at)}</td>
                  <td className="small">
                    {k.grace_until && k.status === "rotated" ? `Chồng lấn tới ${formatDateTime(k.grace_until)}` : null}
                    {k.revoked_at ? `Thu hồi ${formatDateTime(k.revoked_at)}${k.revoke_reason ? ` — ${k.revoke_reason}` : ""}` : null}
                  </td>
                  <td>
                    <div className="row" style={{ gap: 6 }}>
                      {k.status === "active" && client.status === "active" ? (
                        <button type="button" className="btn btn-o btn-sm" disabled={!!admin || !!busy} onClick={() => void rotate(k)} aria-label={`Xoay khóa ${k.key_prefix}`}>
                          {busy === `rotate-${k.id}` ? "Đang xoay…" : "Xoay"}
                        </button>
                      ) : null}
                      {k.status === "active" || k.status === "rotated" ? (
                        <button type="button" className="btn btn-d btn-sm" disabled={!!admin || !!busy} onClick={() => setRevoking(k)} aria-label={`Thu hồi khóa ${k.key_prefix}`}>
                          Thu hồi
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {admin && items.length > 0 ? <p className="small mute">{admin}</p> : null}
      {secret ? <SecretOnceDialog secret={secret} onConfirm={() => setSecret(null)} /> : null}
      {revoking ? (
        <ReasonDialog
          title={`Thu hồi khóa ${revoking.key_prefix}`}
          intro="Khóa bị từ chối (401) ngay ở lời gọi kế tiếp. Thu hồi là bản ghi mới trong sổ khóa, không xóa lịch sử."
          confirmLabel="Thu hồi khóa"
          onClose={() => setRevoking(null)}
          onConfirm={(reason) => revoke(revoking, reason)}
        />
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Đăng ký sản phẩm
// ---------------------------------------------------------------------------
export function SubscriptionsPanel({ client }: { client: ApiClientRecord }) {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const subs = useAsync(() => unwrap(adminApi.GET("/v1/api-clients/{id}/subscriptions", { params: { path: { id: client.id } } })), [adminApi, client.id, version]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<ApiSubscription | null>(null);
  const reload = () => setVersion((v) => v + 1);

  const approve = async (s: ApiSubscription) => {
    setBusy(s.id);
    setProblem(null);
    try {
      await unwrap(adminApi.POST("/v1/api-clients/{id}/subscriptions/{sub_id}/approve", { params: { path: { id: client.id, sub_id: s.id }, header: idem() } }));
      reload();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };
  const revoke = async (s: ApiSubscription, reason: string) => {
    await unwrap(adminApi.POST("/v1/api-clients/{id}/subscriptions/{sub_id}/revoke", { params: { path: { id: client.id, sub_id: s.id }, header: idem() }, body: { reason } }));
    setRevoking(null);
    reload();
  };
  const admin = adminBlock(principal);
  const items = subs.data?.items ?? [];

  return (
    <div className="stack" style={{ gap: 10, paddingTop: 12 }}>
      <p className="small mute">
        Scope lấy từ hợp đồng <code>contracts/dataproduct</code>. Thêm điều kiện: bên chủ dữ liệu phải cấp quyền <code>dp_&lt;sản phẩm&gt;</code> cho tenant bên thứ ba (ADR-0020).
      </p>
      {!admin && client.status !== "revoked" ? <ProposeSubscription key={items.filter((x) => x.status !== "revoked").map((x) => x.product).join(",")} client={client} onSaved={reload} existing={items} /> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {subs.error ? <ProblemAlert problem={subs.error} onRetry={subs.reload} /> : null}
      {subs.loading && !subs.data ? <Loading /> : null}
      {subs.data && items.length === 0 ? <Empty>Chưa đăng ký sản phẩm nào.</Empty> : null}
      <ul className="list-plain stack" aria-label="Đăng ký sản phẩm">
        {items.map((s) => (
          <li key={s.id} className="card" style={{ padding: 12 }} data-product={s.product} data-status={s.status}>
            <div className="row" style={{ justifyContent: "space-between", flexWrap: "wrap", alignItems: "flex-start" }}>
              <div>
                <b>{PRODUCT_LABEL(s.product)}</b> <code className="small">{s.product}</code> <Chip s={SUB_STATUS[s.status]} />
                <div className="small">Phạm vi: {s.scopes.map(scopeLabel).join(" · ")}</div>
                <div className="small mute">
                  Đề xuất: {who(s.proposed_by, principal)} · {formatDateTime(s.proposed_at)}
                  {s.approved_by ? ` · Duyệt: ${who(s.approved_by, principal)} · ${formatDateTime(s.approved_at)}` : ""}
                  {s.revoked_at ? ` · Thu hồi ${formatDateTime(s.revoked_at)} — ${s.revoke_reason ?? ""}` : ""}
                </div>
              </div>
              <div className="row" style={{ gap: 8, alignItems: "flex-start" }}>
                {s.status === "proposed" ? (
                  <GuardedButton id={`sub-ap-${s.id}`} label="Phê duyệt" busyLabel="Đang duyệt…" block={approveBlock(s, principal)} busy={busy === s.id} onClick={() => void approve(s)} />
                ) : null}
                {s.status !== "revoked" && !admin ? (
                  <button type="button" className="btn btn-d btn-sm" onClick={() => setRevoking(s)}>
                    Thu hồi đăng ký
                  </button>
                ) : null}
              </div>
            </div>
          </li>
        ))}
      </ul>
      {revoking ? (
        <ReasonDialog
          title={`Thu hồi đăng ký ${revoking.product}`}
          intro="Scope mất hiệu lực ngay ở lời gọi kế tiếp (403 apiclient.forbidden)."
          confirmLabel="Thu hồi đăng ký"
          onClose={() => setRevoking(null)}
          onConfirm={(reason) => revoke(revoking, reason)}
        />
      ) : null}
    </div>
  );
}

function ProposeSubscription({ client, existing, onSaved }: { client: ApiClientRecord; existing: readonly ApiSubscription[]; onSaved: () => void }) {
  const { adminApi } = useAuth();
  const taken = new Set(existing.filter((s) => s.status !== "revoked").map((s) => s.product));
  const options = (Object.keys(PRODUCTS) as ApiDataProduct[]).filter((p) => !taken.has(p));
  const [product, setProduct] = useState<ApiDataProduct | "">(options[0] ?? "");
  const [scopes, setScopes] = useState<string[]>(options[0] ? [`dp:${options[0]}:read`] : []);
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  if (options.length === 0) return <p className="small mute">Khách hàng đã đăng ký (hoặc đang chờ duyệt) mọi sản phẩm.</p>;
  const current = product || options[0]!;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs = validateSubscription(current, scopes);
    setErrors(errs);
    if (errs.length) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(adminApi.POST("/v1/api-clients/{id}/subscriptions", { params: { path: { id: client.id }, header: idem() }, body: { product: current, scopes } }));
      onSaved();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 8 }} onSubmit={submit} aria-label="Đề xuất đăng ký sản phẩm" noValidate>
      <div className="row" style={{ flexWrap: "wrap", gap: 12, alignItems: "flex-end" }}>
        <div className="field">
          <label htmlFor={`sub-p-${client.id}`}>Sản phẩm dữ liệu</label>
          <select
            id={`sub-p-${client.id}`}
            className="input"
            value={current}
            onChange={(e) => {
              const p = e.target.value as ApiDataProduct;
              setProduct(p);
              setScopes([`dp:${p}:read`]);
            }}
          >
            {options.map((p) => (
              <option key={p} value={p}>
                {PRODUCTS[p].label}
              </option>
            ))}
          </select>
        </div>
        <fieldset className="plain">
          <legend className="small">Phạm vi (scope)</legend>
          {PRODUCTS[current].scopes.map((s) => (
            <label key={s} className="row small">
              <input type="checkbox" checked={scopes.includes(s)} onChange={() => setScopes((l) => (l.includes(s) ? l.filter((x) => x !== s) : [...l, s]))} />
              <code>{s}</code> — {scopeLabel(s)}
            </label>
          ))}
        </fieldset>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang gửi…" : "Đề xuất đăng ký"}
        </button>
      </div>
      {PRODUCTS[current].note ? <p className="small mute">{PRODUCTS[current].note}</p> : null}
      {errors.map((e, i) => (
        <span key={i} className="err">
          {e.reason}
        </span>
      ))}
      {problem ? <ProblemAlert problem={problem} /> : null}
    </form>
  );
}

// ---------------------------------------------------------------------------
// Gói hạn mức
// ---------------------------------------------------------------------------
export function PlansPanel({ client }: { client: ApiClientRecord }) {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const plans = useAsync(() => unwrap(adminApi.GET("/v1/api-clients/{id}/plans", { params: { path: { id: client.id } } })), [adminApi, client.id, version]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  const reload = () => setVersion((v) => v + 1);
  const items = plans.data?.items ?? [];
  const effective = effectivePlan(items);
  const admin = adminBlock(principal);

  const approve = async (v: number) => {
    setBusy(v);
    setProblem(null);
    try {
      await unwrap(adminApi.POST("/v1/api-clients/{id}/plans/{version}/approve", { params: { path: { id: client.id, version: v }, header: idem() } }));
      reload();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="stack" style={{ gap: 10, paddingTop: 12 }}>
      <div className="alert alert-warn" role="note">
        Hạn mức mặc định là <b>tạm, chờ khách</b> (Q5-5): {formatCount(plans.data?.defaults.requests_per_minute ?? DEFAULT_PLAN.requests_per_minute)} lời gọi/phút ·{" "}
        {formatCount(plans.data?.defaults.daily_units ?? DEFAULT_PLAN.daily_units)} đơn vị/ngày · {formatCount(plans.data?.defaults.monthly_units ?? DEFAULT_PLAN.monthly_units)}{" "}
        đơn vị/tháng · {formatCount(plans.data?.defaults.max_export_rows ?? DEFAULT_PLAN.max_export_rows)} dòng/tệp xuất.
      </div>
      {!admin && client.status !== "revoked" ? <ProposePlan client={client} key={items.length} base={effective ?? { ...DEFAULT_PLAN, ...plans.data?.defaults }} onSaved={reload} /> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {plans.error ? <ProblemAlert problem={plans.error} onRetry={plans.reload} /> : null}
      {plans.loading && !plans.data ? <Loading /> : null}
      {plans.data && items.length === 0 ? <Empty>Chưa có phiên bản gói riêng — áp hạn mức mặc định.</Empty> : null}
      {items.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Phiên bản gói hạn mức">
            <thead>
              <tr>
                <th scope="col">Phiên bản</th>
                <th scope="col">Lời gọi/phút</th>
                <th scope="col">Đơn vị/ngày</th>
                <th scope="col">Đơn vị/tháng</th>
                <th scope="col">Dòng/tệp xuất</th>
                <th scope="col">Hiệu lực từ</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Đề xuất / duyệt</th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => (
                <tr key={p.version} data-plan-version={p.version} data-status={p.status}>
                  <td>
                    v{p.version}
                    {effective?.version === p.version ? (
                      <span className="chip c-ok" style={{ marginLeft: 6 }}>
                        đang áp dụng
                      </span>
                    ) : null}
                  </td>
                  <td>{formatCount(p.requests_per_minute)}</td>
                  <td>{formatCount(p.daily_units)}</td>
                  <td>{formatCount(p.monthly_units)}</td>
                  <td>{formatCount(p.max_export_rows)}</td>
                  <td className="small">{formatDateTime(p.effective_from)}</td>
                  <td>
                    <Chip s={PLAN_STATUS[p.status]} />
                  </td>
                  <td className="small">
                    {who(p.proposed_by, principal)} / {p.approved_by ? who(p.approved_by, principal) : "—"}
                    {p.status === "proposed" ? (
                      <div style={{ marginTop: 4 }}>
                        <GuardedButton id={`plan-ap-${p.version}`} label={`Phê duyệt v${p.version}`} busyLabel="Đang duyệt…" block={approveBlock(p, principal)} busy={busy === p.version} onClick={() => void approve(p.version)} />
                      </div>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}

function ProposePlan({ client, base, onSaved }: { client: ApiClientRecord; base: Omit<ApiQuotaPlanProposal, "effective_from">; onSaved: () => void }) {
  const { adminApi } = useAuth();
  const [d, setD] = useState({
    requests_per_minute: String(base.requests_per_minute),
    daily_units: String(base.daily_units),
    monthly_units: String(base.monthly_units),
    max_export_rows: String(base.max_export_rows),
    effective_from: "",
  });
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const err = (f: string) => errors.find((e) => e.field === f)?.reason;
  const num = (s: string) => (/^\d+$/.test(s.trim()) ? Number.parseInt(s.trim(), 10) : Number.NaN);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const body: ApiQuotaPlanProposal = {
      requests_per_minute: num(d.requests_per_minute),
      daily_units: num(d.daily_units),
      monthly_units: num(d.monthly_units),
      max_export_rows: num(d.max_export_rows),
      ...(d.effective_from ? { effective_from: new Date(d.effective_from).toISOString() } : {}),
    };
    const errs = validatePlan(body);
    setErrors(errs);
    if (errs.length) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(adminApi.POST("/v1/api-clients/{id}/plans", { params: { path: { id: client.id }, header: idem() }, body }));
      onSaved();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(false);
    }
  };

  const field = (k: keyof typeof d, label: string) => (
    <div className="field">
      <label htmlFor={`plan-${k}-${client.id}`}>{label}</label>
      <input
        id={`plan-${k}-${client.id}`}
        className="input"
        inputMode={k === "effective_from" ? undefined : "numeric"}
        type={k === "effective_from" ? "datetime-local" : "text"}
        value={d[k]}
        onChange={(e) => setD((x) => ({ ...x, [k]: e.target.value }))}
      />
      {err(k) ? <span className="err">{err(k)}</span> : null}
    </div>
  );

  return (
    <form className="panel stack" style={{ gap: 8 }} onSubmit={submit} aria-label="Đề xuất phiên bản gói hạn mức" noValidate>
      <div className="grid4">
        {field("requests_per_minute", "Lời gọi/phút")}
        {field("daily_units", "Đơn vị/ngày")}
        {field("monthly_units", "Đơn vị/tháng")}
        {field("max_export_rows", "Dòng tối đa/tệp xuất")}
      </div>
      <div className="row" style={{ alignItems: "flex-end", gap: 12, flexWrap: "wrap" }}>
        {field("effective_from", "Hiệu lực từ (bỏ trống = từ lúc đề xuất)")}
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang gửi…" : "Đề xuất phiên bản gói"}
        </button>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </form>
  );
}
