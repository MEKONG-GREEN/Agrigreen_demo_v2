"use client";

import { useMemo, useState, type ChangeEvent, type FormEvent } from "react";
import { ACCOUNT_STATE, accountState, canResetTotp, fullName, importAccounts, lockReasonError, MFA_STATE, mfaState, rolesForOrgKind } from "@/admin/accounts";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages, usePagedList } from "@/api/pagination";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import type { AccountsEvent, AccountsGrants, AccountsImportResult, AccountsUser, PartyOrg } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { roleLabel } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatInt } from "@/lib/decimal";
import { readUpload, type UploadFile } from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const EVENT: Record<AccountsEvent["action"], string> = {
  created: "Tạo tài khoản",
  locked: "Khóa",
  unlocked: "Mở khóa",
  assignment_changed: "Đổi đơn vị/vai",
  actions_sent: "Gửi liên kết kích hoạt",
  dormant_warned: "Báo trước khóa do không hoạt động",
  totp_reset: "Đặt lại TOTP",
};

function RolePicker({ roles, value, onChange, idPrefix }: { roles: string[]; value: Set<string>; onChange: (v: Set<string>) => void; idPrefix: string }) {
  if (roles.length === 0) return <p className="mute small">Không có vai nào bạn được cấp cho loại tổ chức này.</p>;
  return (
    <div className="check-grid">
      {roles.map((r) => (
        <label key={r} className="row small" style={{ gap: 6 }} htmlFor={`${idPrefix}-${r}`}>
          <input
            id={`${idPrefix}-${r}`}
            type="checkbox"
            checked={value.has(r)}
            onChange={() => {
              const n = new Set(value);
              if (n.has(r)) n.delete(r);
              else n.add(r);
              onChange(n);
            }}
          />
          {roleLabel(r)} <code className="mute">{r}</code>
        </label>
      ))}
    </div>
  );
}

function OrgSelect({ id, orgs, value, onChange }: { id: string; orgs: PartyOrg[]; value: string; onChange: (v: string) => void }) {
  return (
    <select id={id} className="select" value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">— Chọn tổ chức —</option>
      {orgs
        .filter((o) => o.active)
        .map((o) => (
          <option key={o.ref} value={o.ref}>
            {o.name} ({o.code})
          </option>
        ))}
    </select>
  );
}

/** Tạo tài khoản — POST /v1/accounts/users; kích hoạt bằng liên kết một lần (ADR-0010), không mật khẩu tạm. */
function CreateForm({ grants, orgs, onDone }: { grants: AccountsGrants | undefined; orgs: PartyOrg[]; onDone: (u: AccountsUser, emailSent: boolean) => void }) {
  const { adminApi } = useAuth();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [orgRef, setOrgRef] = useState("");
  const [roles, setRoles] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const touch = <T,>(set: (v: T) => void) => (v: T) => {
    set(v);
    setIdemKey(newIdempotencyKey());
  };
  const org = orgs.find((o) => o.ref === orgRef);
  const allowed = rolesForOrgKind(grants, org?.kind);
  const errs = problem ? fieldErrorMap(problem) : {};

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/accounts/users", {
          params: { header: { "Idempotency-Key": idemKey } },
          body: { email: email.trim(), full_name: name.trim() || undefined, phone: phone.trim() || undefined, org_ref: orgRef, roles: Array.from(roles) },
        }),
      );
      setIdemKey(newIdempotencyKey());
      setEmail("");
      setName("");
      setPhone("");
      setRoles(new Set());
      onDone(res.user, res.activation_email_sent);
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="stack" style={{ gap: 12 }} onSubmit={(e) => void submit(e)} noValidate>
      <div className="grid2">
        <div className="field">
          <label htmlFor="ac-email">Email công vụ</label>
          <input id="ac-email" className="input" type="email" value={email} onChange={(e) => touch(setEmail)(e.target.value)} aria-invalid={!!errs.email} />
          {errs.email ? <span className="err">{errs.email}</span> : null}
        </div>
        <div className="field">
          <label htmlFor="ac-name">Họ tên</label>
          <input id="ac-name" className="input" value={name} onChange={(e) => touch(setName)(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="ac-phone">Số điện thoại (tuỳ chọn)</label>
          <input id="ac-phone" className="input" inputMode="tel" value={phone} onChange={(e) => touch(setPhone)(e.target.value)} aria-invalid={!!errs.phone} />
          {errs.phone ? <span className="err">{errs.phone}</span> : null}
        </div>
        <div className="field">
          <label htmlFor="ac-org">Đơn vị công tác</label>
          <OrgSelect id="ac-org" orgs={orgs} value={orgRef} onChange={(v) => touch(setOrgRef)(v)} />
          {errs.org_ref ? <span className="err">{errs.org_ref}</span> : null}
        </div>
      </div>
      <fieldset className="plain">
        <legend>Vai trò (theo ma trận cấp vai của bạn)</legend>
        {org ? <RolePicker roles={allowed} value={roles} onChange={(v) => touch(setRoles)(v)} idPrefix="ac-role" /> : <p className="mute small">Chọn đơn vị trước.</p>}
        {errs.roles ? <span className="err small">{errs.roles}</span> : null}
      </fieldset>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div className="row">
        <button type="submit" className="btn btn-p" disabled={busy || !email.trim() || !orgRef || roles.size === 0}>
          {busy ? "Đang tạo…" : "Tạo tài khoản và gửi liên kết kích hoạt"}
        </button>
        <span className="mute small">Người dùng tự đặt mật khẩu qua liên kết một lần (72 giờ); vai quản trị phải cấu hình OTP.</span>
      </div>
    </form>
  );
}

/** Khóa / mở khóa với lý do ≥ 20 ký tự — POST /v1/accounts/users/{id}/lock | unlock. */
function LockForm({ user, onDone }: { user: AccountsUser; onDone: () => void }) {
  const { adminApi } = useAuth();
  const lock = user.enabled;
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const local = lockReasonError(reason);
  const errs = problem ? fieldErrorMap(problem) : {};

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (local) return;
    setBusy(true);
    setProblem(null);
    try {
      const path = lock ? "/v1/accounts/users/{id}/lock" : "/v1/accounts/users/{id}/unlock";
      await unwrap(adminApi.POST(path, { params: { path: { id: user.id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { reason: reason.trim() } }));
      setReason("");
      setTouched(false);
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 8 }} onSubmit={(e) => void submit(e)} noValidate data-testid="lock-form">
      <label htmlFor="lk-reason" style={{ fontWeight: 600 }}>
        {lock ? "Lý do khóa tài khoản" : "Lý do mở khóa"} (tối thiểu 20 ký tự, không ghi thông tin cá nhân)
      </label>
      <textarea
        id="lk-reason"
        className="input textarea"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        onBlur={() => setTouched(true)}
        aria-invalid={(touched && !!local) || !!errs.reason}
        aria-describedby="lk-hint"
      />
      <span id="lk-hint" className={touched && local ? "err small" : "mute small"} role={touched && local ? "alert" : undefined}>
        {touched && local ? local : `${reason.trim().length}/20 ký tự`}
      </span>
      {errs.reason ? <span className="err small">{errs.reason}</span> : null}
      {problem && problem.code !== "validation_failed" ? <ProblemAlert problem={problem} /> : null}
      {problem?.code === "validation_failed" ? (
        <span className="small">
          Máy chủ từ chối: {problem.message} · mã tra cứu <code>{problem.traceId ?? "—"}</code>
        </span>
      ) : null}
      <div>
        <button type="submit" className={`btn btn-sm ${lock ? "btn-d" : "btn-p"}`} disabled={busy}>
          <Icon name={lock ? "lock" : "unlock"} size={15} />
          {busy ? "Đang gửi…" : lock ? "Khóa tài khoản" : "Mở khóa tài khoản"}
        </button>
      </div>
    </form>
  );
}

/** D24 — trạng thái TOTP và đặt lại TOTP có lý do ≥ 20 ký tự — POST /v1/accounts/users/{id}/reset-totp. */
function MfaPanel({ user, onDone }: { user: AccountsUser; onDone: () => void }) {
  const { adminApi } = useAuth();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState(false);
  const state = mfaState(user.mfa);
  const local = lockReasonError(reason);
  const errs = problem ? fieldErrorMap(problem) : {};

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (local) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/accounts/users/{id}/reset-totp", {
          params: { path: { id: user.id }, header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { reason: reason.trim() },
        }),
      );
      setReason("");
      setTouched(false);
      setOpen(false);
      setDone(true);
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="panel stack" style={{ gap: 8 }} data-testid="mfa-panel">
      <b>Xác thực hai yếu tố (TOTP)</b>
      {state ? (
        <span>
          <span className={`chip ${MFA_STATE[state].cls}`} data-testid="mfa-state">
            {MFA_STATE[state].text}
          </span>
        </span>
      ) : (
        <span className="small mute">Chưa đọc được trạng thái TOTP.</span>
      )}
      <span className="small mute">
        Cán bộ khuyến nông và vai quản trị bắt buộc nhập mã TOTP khi đăng nhập (D24); người chưa đăng ký được yêu cầu đăng ký ngay ở lần đăng nhập tới. Đặt
        lại khi cán bộ mất hoặc đổi điện thoại: mã cũ bị xóa, mọi phiên bị thu hồi.
      </span>
      {done ? (
        <span className="small" role="status">
          Đã đặt lại TOTP — cán bộ đăng ký lại ở lần đăng nhập tới.
        </span>
      ) : null}
      {canResetTotp(user) && !open ? (
        <div>
          <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(true)}>
            <Icon name="lock" size={15} />
            Đặt lại TOTP
          </button>
        </div>
      ) : null}
      {open ? (
        <form className="stack" style={{ gap: 8 }} onSubmit={(e) => void submit(e)} noValidate data-testid="totp-reset-form">
          <label htmlFor="totp-reason" style={{ fontWeight: 600 }}>
            Lý do đặt lại TOTP (tối thiểu 20 ký tự, không ghi thông tin cá nhân)
          </label>
          <textarea
            id="totp-reason"
            className="input textarea"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            onBlur={() => setTouched(true)}
            aria-invalid={(touched && !!local) || !!errs.reason}
            aria-describedby="totp-hint"
          />
          <span id="totp-hint" className={touched && local ? "err small" : "mute small"} role={touched && local ? "alert" : undefined}>
            {touched && local ? local : `${reason.trim().length}/20 ký tự`}
          </span>
          {errs.reason ? <span className="err small">{errs.reason}</span> : null}
          {problem && problem.code !== "validation_failed" ? <ProblemAlert problem={problem} /> : null}
          <div className="row" style={{ gap: 8 }}>
            <button type="submit" className="btn btn-d btn-sm" disabled={busy}>
              {busy ? "Đang gửi…" : "Xác nhận đặt lại TOTP"}
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(false)} disabled={busy}>
              Hủy
            </button>
          </div>
        </form>
      ) : null}
    </div>
  );
}

function AssignmentForm({ user, grants, orgs, onDone }: { user: AccountsUser; grants: AccountsGrants | undefined; orgs: PartyOrg[]; onDone: () => void }) {
  const { adminApi } = useAuth();
  const [orgRef, setOrgRef] = useState(user.org_ref ?? "");
  const [roles, setRoles] = useState<Set<string>>(new Set(user.roles ?? []));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const org = orgs.find((o) => o.ref === orgRef);
  const allowed = Array.from(new Set([...rolesForOrgKind(grants, org?.kind), ...(user.roles ?? [])]));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/accounts/users/{id}/assignment", {
          params: { path: { id: user.id }, header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { org_ref: orgRef || undefined, roles: Array.from(roles) },
        }),
      );
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 8 }} onSubmit={(e) => void submit(e)}>
      <b>Đổi đơn vị / vai</b>
      <div className="field">
        <label htmlFor="as-org">Đơn vị công tác</label>
        <OrgSelect id="as-org" orgs={orgs} value={orgRef} onChange={setOrgRef} />
      </div>
      <RolePicker roles={allowed} value={roles} onChange={setRoles} idPrefix="as-role" />
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div>
        <button type="submit" className="btn btn-o btn-sm" disabled={busy || roles.size === 0}>
          {busy ? "Đang lưu…" : "Lưu thay đổi"}
        </button>
      </div>
    </form>
  );
}

function UserPanel({ id, grants, orgs, onChanged }: { id: string; grants: AccountsGrants | undefined; orgs: PartyOrg[]; onChanged: () => void }) {
  const { adminApi } = useAuth();
  const [version, setVersion] = useState(0);
  const user = useAsync(() => unwrap(adminApi.GET("/v1/accounts/users/{id}", { params: { path: { id } } })), [adminApi, id, version]);
  const events = useAsync(() => unwrap(adminApi.GET("/v1/accounts/users/{id}/events", { params: { path: { id } } })), [adminApi, id, version]);
  const [resend, setResend] = useState<{ busy: boolean; problem: ApiProblem | null; sent: boolean | null }>({ busy: false, problem: null, sent: null });
  const changed = () => {
    setVersion((v) => v + 1);
    onChanged();
  };
  const orgName = (ref: string | null) => orgs.find((o) => o.ref === ref)?.name ?? ref ?? "—";
  const u = user.data;

  const doResend = async () => {
    setResend({ busy: true, problem: null, sent: null });
    try {
      const r = await unwrap(adminApi.POST("/v1/accounts/users/{id}/resend-actions", { params: { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: {} }));
      setResend({ busy: false, problem: null, sent: r.activation_email_sent });
      changed();
    } catch (e) {
      setResend({ busy: false, problem: toApiProblem(e), sent: null });
    }
  };

  return (
    <div className="stack" data-testid="user-panel">
      {user.error ? <ProblemAlert problem={user.error} onRetry={user.reload} /> : null}
      {user.loading && !u ? <Loading /> : null}
      {u ? (
        <>
          <div>
            <h2 style={{ fontSize: 17 }}>{fullName(u) || u.username}</h2>
            <span className={`chip ${ACCOUNT_STATE[accountState(u)].cls}`}>{ACCOUNT_STATE[accountState(u)].text}</span>
          </div>
          <dl className="kv small">
            <dt>Tên đăng nhập</dt>
            <dd>{u.username}</dd>
            <dt>Email</dt>
            <dd>{u.email}</dd>
            <dt>SĐT</dt>
            <dd>{u.phone ?? "—"}</dd>
            <dt>Đơn vị</dt>
            <dd>{orgName(u.org_ref)}</dd>
            <dt>Vai</dt>
            <dd>{(u.roles ?? []).map(roleLabel).join(", ") || "—"}</dd>
            <dt>Việc chờ người dùng</dt>
            <dd>{u.required_actions.join(", ") || "—"}</dd>
          </dl>
          <MfaPanel key={`mfa-${u.id}`} user={u} onDone={changed} />
          <LockForm key={`${u.id}-${u.enabled}`} user={u} onDone={changed} />
          <div className="panel stack" style={{ gap: 6 }}>
            <b>Liên kết kích hoạt / đặt lại mật khẩu</b>
            <span className="small mute">Gửi lại liên kết một lần tới email công vụ (không gửi mật khẩu tạm).</span>
            <div>
              <button type="button" className="btn btn-o btn-sm" onClick={() => void doResend()} disabled={resend.busy || !u.enabled}>
                <Icon name="mail" size={15} />
                {resend.busy ? "Đang gửi…" : "Gửi lại liên kết"}
              </button>
            </div>
            {resend.sent === true ? <span className="small" role="status">Đã gửi liên kết.</span> : null}
            {resend.sent === false ? <span className="small err">Máy chủ chưa gửi được email — thử lại sau.</span> : null}
            {resend.problem ? <ProblemAlert problem={resend.problem} /> : null}
          </div>
          <AssignmentForm key={`as-${u.id}-${version}`} user={u} grants={grants} orgs={orgs} onDone={changed} />
          <div>
            <h3 style={{ fontSize: 14, marginBottom: 6 }}>Nhật ký thao tác</h3>
            {events.error ? <ProblemAlert problem={events.error} onRetry={events.reload} /> : null}
            <ol style={{ marginLeft: 18 }}>
              {(events.data?.items ?? []).map((ev) => (
                <li key={ev.seq} className="small" style={{ marginBottom: 4 }}>
                  <b style={{ display: "inline" }}>{EVENT[ev.action]}</b> · {formatDateTime(ev.event_time)}
                  {ev.reason ? <div className="mute">Lý do: {ev.reason}</div> : null}
                </li>
              ))}
            </ol>
          </div>
        </>
      ) : null}
    </div>
  );
}

function ImportPanel({ onDone }: { onDone: () => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<AccountsImportResult | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setIdemKey(newIdempotencyKey());
  };
  const bad = file && file.kind !== "csv" && file.kind !== "xlsx";
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || bad) return;
    setBusy(true);
    setProblem(null);
    try {
      setResult(await importAccounts(adminApi, file, idemKey));
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack">
      <p className="small">
        Cột: <code>email</code>, <code>full_name|ho_ten</code>, <code>org_ref|ma_to_chuc</code>, <code>roles|vai_tro</code> (phân tách , ; |), tuỳ chọn{" "}
        <code>username</code>, <code>phone|sdt</code>. CSV UTF-8 hoặc Excel .xlsx (trang tính đầu), tối đa 200 dòng.
      </p>
      <form className="row" onSubmit={(e) => void submit(e)} style={{ alignItems: "flex-end" }}>
        <div className="field">
          <label htmlFor="ac-file">Tệp CSV hoặc .xlsx</label>
          <input id="ac-file" type="file" accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(e) => void onFile(e)} />
        </div>
        <button type="submit" className="btn btn-p btn-sm" disabled={!file || !!bad || busy}>
          {busy ? "Đang nhập…" : "Nhập tài khoản"}
        </button>
      </form>
      {bad ? <p className="err small">{file?.kind === "xls" ? "Tệp .xls cũ không được nhận — lưu lại dạng .xlsx." : "Chỉ nhận CSV hoặc .xlsx."}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <div>
          <p className="small" style={{ marginBottom: 6 }}>
            <span className="chip c-ok">{formatInt(result.created)} đã tạo</span> <span className="chip c-bad">{formatInt(result.rejected)} bị loại</span>
          </p>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Kết quả nhập theo dòng</caption>
              <thead>
                <tr>
                  <th scope="col" className="num">Dòng</th>
                  <th scope="col">Kết quả</th>
                  <th scope="col">Lý do</th>
                  <th scope="col">Email kích hoạt</th>
                </tr>
              </thead>
              <tbody>
                {result.rows.map((r) => (
                  <tr key={r.row}>
                    <td className="num">{r.row}</td>
                    <td>{r.status === "created" ? <span className="chip c-ok">Đã tạo</span> : <span className="chip c-bad">Bị loại</span>}</td>
                    <td>{r.reason ?? "—"}</td>
                    <td>{r.status === "created" ? (r.activation_email_sent ? "Đã gửi" : "Chưa gửi được") : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}
    </div>
  );
}

type BulkAction = "lock" | "unlock" | "resend";
const BULK_LABEL: Record<BulkAction, string> = { lock: "Khóa", unlock: "Mở khóa", resend: "Gửi lại liên kết kích hoạt" };

/**
 * Thao tác loạt (KN US-ADM-03) — POST /v1/accounts/users/bulk: khóa, mở khóa (lý do ≥ 20 ký tự) hoặc gửi lại
 * liên kết cho tối đa 100 tài khoản đã chọn; kết quả theo từng tài khoản (tài khoản ngoài quyền báo lỗi riêng).
 */
function BulkBar({ ids, names, onDone }: { ids: string[]; names: ReadonlyMap<string, string>; onDone: () => void }) {
  const { adminApi } = useAuth();
  const [action, setAction] = useState<BulkAction>("lock");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<{ succeeded: number; failed: number; items: { user_id: string; ok: boolean; code: string | null }[] } | null>(null);
  const needReason = action !== "resend";
  const reasonErr = needReason ? lockReasonError(reason) : null;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (reasonErr) return;
    setBusy(true);
    setProblem(null);
    setResult(null);
    try {
      const r = await unwrap(
        adminApi.POST("/v1/accounts/users/bulk", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { action, user_ids: ids, reason: needReason ? reason.trim() : undefined },
        }),
      );
      setResult(r);
      onDone();
    } catch (x) {
      setProblem(toApiProblem(x));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 8, marginBottom: 12 }} onSubmit={(e) => void submit(e)} noValidate aria-label="Thao tác loạt" data-testid="bulk-bar">
      <div className="row" style={{ alignItems: "flex-end" }}>
        <b>Đã chọn {ids.length} tài khoản</b>
        <div className="field">
          <label htmlFor="ac-bulk-action">Thao tác</label>
          <select id="ac-bulk-action" className="select" value={action} onChange={(e) => setAction(e.target.value as BulkAction)}>
            {(Object.keys(BULK_LABEL) as BulkAction[]).map((a) => (
              <option key={a} value={a}>
                {BULK_LABEL[a]}
              </option>
            ))}
          </select>
        </div>
        {needReason ? (
          <div className="field" style={{ flex: "1 1 280px" }}>
            <label htmlFor="ac-bulk-reason">Lý do (tối thiểu 20 ký tự)</label>
            <input id="ac-bulk-reason" className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
          </div>
        ) : null}
        <button type="submit" className="btn btn-p btn-sm" disabled={busy || ids.length === 0 || !!reasonErr || ids.length > 100}>
          {busy ? "Đang thực hiện…" : `${BULK_LABEL[action]} ${ids.length} tài khoản`}
        </button>
      </div>
      {ids.length > 100 ? <p className="small" style={{ color: "var(--red)" }}>Mỗi lần tối đa 100 tài khoản.</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <div role="status" className="small" data-testid="bulk-result">
          Thành công {result.succeeded} · Lỗi {result.failed}
          {result.items.filter((i) => !i.ok).length > 0 ? (
            <ul className="list-plain">
              {result.items
                .filter((i) => !i.ok)
                .map((i) => (
                  <li key={i.user_id}>
                    {names.get(i.user_id) ?? i.user_id}: <code>{i.code}</code>
                  </li>
                ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </form>
  );
}

/** Tài khoản cán bộ (pf/accounts, Admin API, MFA) — KN US-ADM-01..03, HTX US-ACC-01/02, CGH FN-01. */
export default function AccountsPage() {
  const { adminApi } = useAuth();
  const [tab, setTab] = useState("list");
  const [orgFilter, setOrgFilter] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const [created, setCreated] = useState<{ user: AccountsUser; sent: boolean } | null>(null);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const grants = useAsync(() => unwrap(adminApi.GET("/v1/accounts/grants")), [adminApi]);
  const orgs = useAsync(
    () => collectPages((cursor) => unwrap(adminApi.GET("/v1/orgs", { params: { query: { include_inactive: true, cursor, limit: 200 } } })), 5000),
    [adminApi],
  );
  const orgList = useMemo(() => (orgs.data ?? []).filter((o) => o.kind !== "partner"), [orgs.data]);
  const orgName = (ref: string | null) => orgList.find((o) => o.ref === ref)?.name ?? "—";
  const list = usePagedList(
    (cursor) => unwrap(adminApi.GET("/v1/accounts/users", { params: { query: { org_ref: orgFilter || undefined, cursor, limit: 50 } } })),
    [adminApi, orgFilter, version],
  );

  return (
    <>
      <PageHead title="Tài khoản cán bộ" subtitle="Tạo, khóa/mở khóa, gửi lại liên kết kích hoạt, đổi đơn vị/vai — trong phạm vi và ma trận cấp vai của bạn" />
      <MfaNotice />
      {grants.error && grants.error.code !== "mfa_required" ? <ProblemAlert problem={grants.error} onRetry={grants.reload} /> : null}
      {grants.data ? (
        <p className="small mute" style={{ marginBottom: 12 }}>
          Bạn được cấp: {grants.data.grantable.map(roleLabel).join(", ") || "không có vai nào"}.
        </p>
      ) : null}
      <div className="card sec">
        <Tabs
          label="Quản trị tài khoản"
          value={tab}
          onChange={setTab}
          tabs={[
            { id: "list", label: "Danh sách" },
            { id: "create", label: "Tạo tài khoản" },
            { id: "import", label: "Nhập từ tệp" },
          ]}
        >
          {tab === "list" ? (
            <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.5fr) minmax(300px, 1fr)", alignItems: "start" }}>
              <div>
                <div className="field" style={{ marginBottom: 12, maxWidth: 420 }}>
                  <label htmlFor="ac-filter">Lọc theo đơn vị</label>
                  <OrgSelect
                    id="ac-filter"
                    orgs={orgList}
                    value={orgFilter}
                    onChange={(v) => {
                      setOrgFilter(v);
                      setChecked(new Set());
                    }}
                  />
                </div>
                {created ? (
                  <div className="alert alert-info" role="status" style={{ marginBottom: 12 }}>
                    <Icon name="check" />
                    <span>
                      Đã tạo tài khoản <b style={{ display: "inline" }}>{created.user.username}</b>.{" "}
                      {created.sent ? "Liên kết kích hoạt đã gửi tới email." : "Chưa gửi được email kích hoạt — dùng “Gửi lại liên kết”."}
                    </span>
                  </div>
                ) : null}
                {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
                {checked.size > 0 ? (
                  <BulkBar
                    ids={Array.from(checked)}
                    names={new Map(list.items.map((u) => [u.id, fullName(u) || u.username]))}
                    onDone={() => setVersion((v) => v + 1)}
                  />
                ) : null}
                <div className="tbl-wrap">
                  <table className="tbl">
                    <caption className="sr-only">Tài khoản trong phạm vi</caption>
                    <thead>
                      <tr>
                        <th scope="col">
                          <input
                            type="checkbox"
                            aria-label="Chọn tất cả tài khoản đang hiện"
                            checked={list.items.length > 0 && list.items.every((u) => checked.has(u.id))}
                            onChange={(e) => setChecked(e.target.checked ? new Set(list.items.map((u) => u.id)) : new Set())}
                          />
                        </th>
                        <th scope="col">Cán bộ</th>
                        <th scope="col">Đơn vị</th>
                        <th scope="col">Vai</th>
                        <th scope="col">Trạng thái</th>
                      </tr>
                    </thead>
                    <tbody>
                      {list.items.map((u) => (
                        <tr key={u.id} aria-selected={selected === u.id}>
                          <td>
                            <input
                              type="checkbox"
                              aria-label={`Chọn ${fullName(u) || u.username}`}
                              checked={checked.has(u.id)}
                              onChange={() => {
                                const n = new Set(checked);
                                if (n.has(u.id)) n.delete(u.id);
                                else n.add(u.id);
                                setChecked(n);
                              }}
                            />
                          </td>
                          <td>
                            <button type="button" onClick={() => setSelected(u.id)} style={{ fontWeight: 700, textAlign: "left", color: "var(--g800)" }} aria-label={`Chi tiết ${fullName(u) || u.username}`}>
                              {fullName(u) || u.username}
                            </button>
                            <div className="mute small">{u.email}</div>
                          </td>
                          <td className="small">{orgName(u.org_ref)}</td>
                          <td className="small">{(u.roles ?? []).map(roleLabel).join(", ") || "—"}</td>
                          <td>
                            <span className={`chip ${ACCOUNT_STATE[accountState(u)].cls}`}>{ACCOUNT_STATE[accountState(u)].text}</span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                {list.loading ? <Loading /> : null}
                {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có tài khoản trong phạm vi.</Empty> : null}
                <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
              </div>
              <aside className="card sec" aria-label="Chi tiết tài khoản">
                {selected ? (
                  <UserPanel key={selected} id={selected} grants={grants.data} orgs={orgList} onChanged={() => setVersion((v) => v + 1)} />
                ) : (
                  <p className="mute">Chọn một tài khoản để khóa/mở khóa, gửi lại liên kết, đổi đơn vị/vai hoặc xem nhật ký.</p>
                )}
              </aside>
            </div>
          ) : null}
          {tab === "create" ? (
            <CreateForm
              grants={grants.data}
              orgs={orgList}
              onDone={(u, sent) => {
                setCreated({ user: u, sent });
                setSelected(u.id);
                setVersion((v) => v + 1);
                setTab("list");
              }}
            />
          ) : null}
          {tab === "import" ? <ImportPanel onDone={() => setVersion((v) => v + 1)} /> : null}
        </Tabs>
      </div>
    </>
  );
}
