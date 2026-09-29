"use client";

import { useMemo, useState, type ChangeEvent, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { FarmerAccount, FarmerActivation, FarmerImportResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { QrCode } from "@/components/QrCode";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { ResultList, StatusChip } from "@/htx/common";
import { GRANT_STATUS, normalizePhone } from "@/htx/rules";
import { CoopPicker, ReadOnlyNotice, useCoopFields, useHtxScope, type HtxScope } from "@/htx/scope";
import { readUpload, type UploadFile } from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

/**
 * Liên kết + QR kích hoạt một lần (HTX US-ACC-01). Chỉ có trong phản hồi đầu của máy chủ, không lưu ở đâu
 * trên trình duyệt; đóng thẻ là mất — cần thì "Phát lại QR" (lượt cũ bị thu hồi).
 */
function ActivationCard({ activation, who, onClose }: { activation: FarmerActivation; who: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const link = activation.link ?? null;
  const copy = async () => {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <section className="panel stack" style={{ gap: 10, marginBottom: 16, borderColor: "#f5d9a0" }} aria-label={`Kích hoạt tài khoản ${who}`}>
      <div className="alert alert-warn" role="alert">
        <Icon name="lock" />
        <span>
          <b>Chỉ hiển thị một lần — không chia sẻ cho người khác.</b> Đưa nông dân quét mã QR (hoặc mở liên kết) trên điện thoại của chính họ để đặt mật khẩu. Liên kết dùng một lần, hết hạn lúc{" "}
          {formatDateTime(activation.expires_at)}. Không chụp màn hình gửi qua mạng xã hội.
        </span>
      </div>
      {link ? (
        <div className="row" style={{ alignItems: "flex-start", gap: 20 }}>
          <div className="qr-box">
            <QrCode data={activation.qr?.data ?? link} label={`Mã QR kích hoạt tài khoản ${who}`} ecc={activation.qr?.error_correction} minModulePx={activation.qr?.min_module_px} />
          </div>
          <div className="stack" style={{ gap: 8, flex: "1 1 280px" }}>
            <b>{who}</b>
            <div className="field">
              <label htmlFor="act-link">Liên kết kích hoạt</label>
              <input id="act-link" className="input link-once" readOnly value={link} onFocus={(e) => e.currentTarget.select()} />
            </div>
            <div className="row">
              <button type="button" className="btn btn-o btn-sm" onClick={() => void copy()}>
                {copied ? "Đã sao chép" : "Sao chép liên kết"}
              </button>
              <button type="button" className="btn btn-p btn-sm" onClick={onClose}>
                Đã giao cho nông dân — đóng
              </button>
            </div>
          </div>
        </div>
      ) : (
        <p>Máy chủ không trả liên kết (lượt gọi lặp hoặc liên kết chưa phát được). Dùng “Phát lại QR” để có lượt mới.</p>
      )}
    </section>
  );
}

function CreateForm({ scope, onCreated }: { scope: HtxScope; onCreated: (a: FarmerActivation | null, who: string, err?: string | null) => void }) {
  const { api } = useAuth();
  const fields = useCoopFields(scope.coopRef);
  const [household, setHousehold] = useState("");
  const [phone, setPhone] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const normalized = normalizePhone(phone);
  const phoneErr = !phone.trim() ? "Nhập SĐT chủ hộ." : normalized ? null : "SĐT phải có dạng 0xxxxxxxxx (10 chữ số) hoặc +84…";
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (phoneErr) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        api.POST("/v1/farmer-accounts", { params: { header: { "Idempotency-Key": key } }, body: { phone: normalized!, household_ref: household || null } }),
      );
      setKey(newIdempotencyKey());
      setPhone("");
      setHousehold("");
      setTouched(false);
      onCreated(res.activation ?? null, fields.householdLabel(res.farmer?.household_ref), res.activation_error);
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 10, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Tạo tài khoản nông dân">
      <b>Tạo tài khoản nông dân cho nông hộ</b>
      <span className="small mute">Tên đăng nhập là SĐT chủ hộ đã đăng ký trong hồ sơ nông hộ. Không cần email, không gửi SMS — hệ thống phát QR/liên kết kích hoạt một lần.</span>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field" style={{ minWidth: 260 }}>
          <label htmlFor="fa-hh">Nông hộ</label>
          <select id="fa-hh" className="select" value={household} onChange={(e) => { setHousehold(e.target.value); setKey(newIdempotencyKey()); }}>
            <option value="">Tra theo SĐT</option>
            {fields.households.filter((h) => h.active).map((h) => (
              <option key={h.household_ref} value={h.household_ref}>
                {fields.householdLabel(h.household_ref)} · {h.masked_phone}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="fa-phone">SĐT chủ hộ</label>
          <input id="fa-phone" className="input" inputMode="tel" autoComplete="off" value={phone} aria-invalid={touched && !!phoneErr} onChange={(e) => { setPhone(e.target.value); setKey(newIdempotencyKey()); }} />
          {touched && phoneErr ? <span className="err">{phoneErr}</span> : null}
        </div>
        <div style={{ alignSelf: "flex-end" }}>
          <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
            {busy ? "Đang tạo…" : "Tạo tài khoản & phát QR"}
          </button>
        </div>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </form>
  );
}

function ImportForm({ onDone }: { onDone: (r: FarmerImportResult) => void }) {
  const { api } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const ok = !!file && (file.kind === "csv" || file.kind === "xlsx");
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setKey(newIdempotencyKey());
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || !ok) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        api.POST("/v1/farmer-accounts/import", {
          params: { header: { "Idempotency-Key": key } },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      onDone(res);
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 8, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Nhập tài khoản nông dân từ tệp">
      <b>Nhập tài khoản nông dân hàng loạt (CSV/.xlsx, tối đa 200 dòng)</b>
      <span className="small mute">Cột: SĐT (bắt buộc), Mã nông hộ (tùy chọn). Dòng lỗi bị bỏ qua kèm lý do; dòng hợp lệ nhận QR kích hoạt.</span>
      <div className="row">
        <input type="file" aria-label="Tệp tài khoản nông dân" accept=".xlsx,.csv" onChange={(e) => void onFile(e)} />
        <button type="submit" className="btn btn-p btn-sm" disabled={!ok || busy}>
          {busy ? "Đang nhập…" : "Nhập"}
        </button>
      </div>
      {file && !ok ? <span className="err small">Chỉ nhận .xlsx hoặc CSV UTF-8.</span> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
    </form>
  );
}

function GrantsDialog({ account, who, onClose }: { account: FarmerAccount; who: string; onClose: () => void }) {
  const { api } = useAuth();
  const g = useAsync(() => unwrap(api.GET("/v1/farmer-accounts/{id}/activation", { params: { path: { id: account.user_ref } } })), [api, account.user_ref]);
  const REVOKE: Record<string, string> = { reissued: "phát lượt mới", account_locked: "tài khoản bị khóa" };
  return (
    <Dialog title={`Lượt kích hoạt — ${who}`} onClose={onClose}>
      {g.error ? <ProblemAlert problem={g.error} /> : null}
      {g.loading && !g.data ? <Loading /> : null}
      <ul className="list-plain" aria-label="Lượt kích hoạt">
        {(g.data?.items ?? []).map((x) => (
          <li key={x.grant_id} className="small">
            <StatusChip map={GRANT_STATUS} value={x.status} /> Phát {formatDateTime(x.issued_at)} · hạn {formatDateTime(x.expires_at)}
            {x.used_at ? ` · kích hoạt ${formatDateTime(x.used_at)}` : ""}
            {x.revoke_reason ? ` · thu hồi do ${REVOKE[x.revoke_reason] ?? x.revoke_reason}` : ""}
          </li>
        ))}
      </ul>
      <p className="small mute">Chỉ siêu dữ liệu — hệ thống không lưu liên kết kích hoạt.</p>
    </Dialog>
  );
}

/** Tài khoản nông dân của HTX (HTX US-ACC-01/02): tạo, nhập hàng loạt, phát/phát lại QR kích hoạt. */
export default function FarmerAccountsPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const fields = useCoopFields(scope.coopRef);
  const [panel, setPanel] = useState<"" | "create" | "import">("");
  const [version, setVersion] = useState(0);
  const [shown, setShown] = useState<{ activation: FarmerActivation; who: string } | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [importResult, setImportResult] = useState<FarmerImportResult | null>(null);
  const [grants, setGrants] = useState<FarmerAccount | null>(null);
  const [rowProblem, setRowProblem] = useState<{ ref: string; p: ApiProblem } | null>(null);
  const list = usePagedList(
    (cursor) => (scope.coopRef ? unwrap(api.GET("/v1/farmer-accounts", { params: { query: { cooperative_ref: scope.coopRef, cursor, limit: 50 } } })) : Promise.resolve({ items: [], next_cursor: null })),
    [api, scope.coopRef, version],
  );
  const phoneOf = useMemo(() => new Map(fields.households.map((h) => [h.household_ref, h.masked_phone])), [fields.households]);
  const reload = () => setVersion((v) => v + 1);
  const writable = scope.canWrite && !!scope.coopRef;

  const reissue = async (a: FarmerAccount) => {
    setRowProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/farmer-accounts/{id}/activation", { params: { path: { id: a.user_ref }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
      if (res.activation) setShown({ activation: res.activation, who: fields.householdLabel(a.household_ref) });
      reload();
    } catch (e) {
      setRowProblem({ ref: a.user_ref, p: toApiProblem(e) });
    }
  };

  return (
    <>
      <PageHead
        title="Tài khoản nông dân"
        subtitle="Mỗi nông hộ một tài khoản App HTX; kích hoạt bằng QR/liên kết một lần do HTX trao tận tay"
        actions={
          writable ? (
            <>
              <button type="button" className="btn btn-p btn-sm" aria-expanded={panel === "create"} onClick={() => setPanel((p) => (p === "create" ? "" : "create"))}>
                <Icon name="user" size={15} />
                Tạo tài khoản
              </button>
              <button type="button" className="btn btn-o btn-sm" aria-expanded={panel === "import"} onClick={() => setPanel((p) => (p === "import" ? "" : "import"))}>
                <Icon name="upload" size={15} />
                Nhập từ tệp
              </button>
            </>
          ) : null
        }
      />
      <ReadOnlyNotice scope={scope} />
      {shown ? <ActivationCard activation={shown.activation} who={shown.who} onClose={() => setShown(null)} /> : null}
      {notice ? <p role="status" className="alert alert-warn">{notice}</p> : null}
      {panel === "create" && writable ? (
        <CreateForm
          scope={scope}
          onCreated={(a, who, err) => {
            if (a) setShown({ activation: a, who });
            setNotice(err ? `Đã tạo tài khoản ${who} nhưng chưa phát được liên kết (${err}) — dùng “Phát lại QR”.` : null);
            reload();
          }}
        />
      ) : null}
      {panel === "import" && writable ? (
        <ImportForm
          onDone={(r) => {
            setImportResult(r);
            reload();
          }}
        />
      ) : null}
      {importResult ? (
        <div className="card sec" style={{ marginBottom: 16 }}>
          <p role="status">
            <b>
              {importResult.created} tài khoản đã tạo · {importResult.rejected} dòng lỗi
            </b>
          </p>
          <ResultList
            caption="Kết quả nhập tài khoản nông dân"
            rows={importResult.rows.map((r) => ({
              key: String(r.row),
              label: `Dòng ${r.row}`,
              ok: r.status === "created",
              status: r.status === "created" ? "Đã tạo" : "Bị loại",
              reason:
                r.status === "created" && r.activation ? (
                  <button type="button" className="btn btn-o btn-sm" onClick={() => setShown({ activation: r.activation!, who: `${fields.householdLabel(r.household_ref)} (dòng ${r.row})` })}>
                    Xem QR dòng {r.row}
                  </button>
                ) : (
                  (r.reason ?? undefined)
                ),
            }))}
          />
        </div>
      ) : null}
      <div className="card sec">
        <div className="row" style={{ marginBottom: 14, alignItems: "flex-end" }}>
          <CoopPicker scope={scope} allowAll={false} />
        </div>
        {!scope.coopRef ? <Empty>Chọn một HTX để xem tài khoản nông dân.</Empty> : null}
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {scope.coopRef ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Tài khoản nông dân</caption>
              <thead>
                <tr>
                  <th scope="col">Nông hộ</th>
                  <th scope="col">SĐT (đã che)</th>
                  <th scope="col">Liên kết lúc</th>
                  <th scope="col">Kích hoạt</th>
                  <th scope="col">Thao tác</th>
                </tr>
              </thead>
              <tbody>
                {list.items.map((a) => (
                  <tr key={a.user_ref}>
                    <td>{fields.householdLabel(a.household_ref)}</td>
                    <td style={{ fontFamily: "ui-monospace, Consolas, monospace" }}>{phoneOf.get(a.household_ref) ?? "—"}</td>
                    <td>{formatDateTime(a.linked_at)}</td>
                    <td>
                      {a.latest_activation ? <StatusChip map={GRANT_STATUS} value={a.latest_activation.status} /> : <span className="chip c-mute">Chưa phát</span>}
                      {a.enabled === false ? <span className="chip c-bad">Đã khóa</span> : null}
                    </td>
                    <td>
                      <div className="row" style={{ gap: 6 }}>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setGrants(a)} aria-label={`Lượt kích hoạt ${fields.householdLabel(a.household_ref)}`}>
                          Lịch sử
                        </button>
                        {scope.canWrite ? (
                          <button type="button" className="btn btn-o btn-sm" onClick={() => void reissue(a)} aria-label={`Phát lại QR ${fields.householdLabel(a.household_ref)}`}>
                            Phát lại QR
                          </button>
                        ) : null}
                      </div>
                      {rowProblem?.ref === a.user_ref ? <ProblemAlert problem={rowProblem.p} /> : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {list.loading && scope.coopRef ? <Loading /> : null}
        {!list.loading && !list.error && scope.coopRef && list.items.length === 0 ? <Empty>HTX chưa có tài khoản nông dân.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
      {grants ? <GrantsDialog account={grants} who={fields.householdLabel(grants.household_ref)} onClose={() => setGrants(null)} /> : null}
    </>
  );
}
