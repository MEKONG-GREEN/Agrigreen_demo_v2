"use client";

import { useState, type FormEvent } from "react";
import {
  attachMST,
  claimAnchor,
  createProvisional,
  CREATED_BY_LABEL,
  getAnchor,
  listAnchors,
  movedTotal,
  normalizeMST,
  STATUS_LABEL,
  type HTXAnchor,
  type HTXAnchorDetail,
  type HTXAnchorStatus,
  type HTXClaimReport,
} from "@/admin/htxAnchors";
import { NewAnchorForm } from "@/admin/NewAnchorForm";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { useAsync } from "@/lib/useAsync";

const CONF_CLS = { confirmed: "c-ok", unconfirmed: "c-warn" } as const;

/**
 * D2 (CEO 13/10/2026, ADR-0052): neo HTX theo mã số thuế. Khuyến nông hoặc MKG lập HTX TẠM ("Chưa xác nhận");
 * dữ liệu nhập dưới HTX tạm vẫn vào mọi báo cáo. Khi tài khoản HTX thật kích hoạt và MST được xác minh, quản trị
 * nhận quyền (chạy thử trước) — trạng thái lật sang "Đã xác nhận", không sao chép dữ liệu.
 */
export default function HTXAnchorsPage() {
  const { adminApi } = useAuth();
  const [status, setStatus] = useState<"" | HTXAnchorStatus>("");
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const list = useAsync(
    () => listAnchors(adminApi, { status: status || undefined, q }),
    [adminApi, status, q],
  );
  const items: HTXAnchor[] | null = list.data
    ? list.data.items
    : list.error
      ? []
      : null;
  const load = list.reload;

  return (
    <>
      <PageHead
        title="Neo HTX theo mã số thuế"
        subtitle="HTX tạm do Khuyến nông / MKG lập mang nhãn “Chưa xác nhận” tới khi HTX thật nhận quyền — dữ liệu đồng sở hữu MKG + HTX"
      />
      <MfaNotice />
      {list.error ? (
        <ProblemAlert problem={list.error} onRetry={list.reload} />
      ) : null}
      <NewAnchorForm onCreated={id => { setSelected(id); load(); }} />
      <CreateForm
        onCreated={(id) => {
          setSelected(id);
          load();
        }}
      />
      <section className="card sec stack" aria-label="Danh sách neo">
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
          <label className="field">
            <span>Trạng thái</span>
            <select
              value={status}
              onChange={(e) =>
                setStatus(e.target.value as "" | HTXAnchorStatus)
              }
              aria-label="Lọc trạng thái"
            >
              <option value="">Tất cả</option>
              <option value="provisional">HTX tạm (Chưa xác nhận)</option>
              <option value="claimed">Đã nhận quyền (Đã xác nhận)</option>
            </select>
          </label>
          <label className="field">
            <span>Mã số thuế / mã tạm</span>
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="0312345678 hoặc TMP-…"
              aria-label="Tìm mã số thuế"
            />
          </label>
        </div>
        {items === null ? (
          <Loading />
        ) : items.length === 0 ? (
          <Empty>Chưa có neo HTX nào khớp bộ lọc.</Empty>
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th scope="col">Khóa neo</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Xác nhận</th>
                <th scope="col">Bên lập</th>
                <th scope="col">Nút HTX</th>
                <th scope="col" />
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.anchor_id} aria-selected={selected === a.anchor_id}>
                  <td>
                    <code>{a.anchor_key}</code>
                    {a.temporary ? " (mã tạm)" : ""}
                  </td>
                  <td>{STATUS_LABEL[a.status]}</td>
                  <td className={CONF_CLS[a.confirmation_status]}>
                    {a.confirmation_label}
                  </td>
                  <td>{CREATED_BY_LABEL[a.created_by]}</td>
                  <td>
                    <code>{a.org_ref.slice(0, 8)}…</code>
                  </td>
                  <td>
                    <button
                      type="button"
                      className="btn btn-sm"
                      onClick={() => setSelected(a.anchor_id)}
                    >
                      Chi tiết
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
      {selected && (
        <AnchorDetail key={selected} id={selected} onChanged={load} />
      )}
    </>
  );
}

function CreateForm({ onCreated }: { onCreated: (id: string) => void }) {
  const { adminApi } = useAuth();
  const [org, setOrg] = useState("");
  const [by, setBy] = useState<"kn" | "mkg">("kn");
  const [mst, setMst] = useState("");
  const [agreement, setAgreement] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [idem, setIdem] = useState(newIdempotencyKey);
  const mstHint =
    mst.trim() === ""
      ? "Không có MST → mã tạm TMP-…; gắn MST trước khi nhận quyền"
      : (normalizeMST(mst) ?? "Sai dạng");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      const a = await createProvisional(
        adminApi,
        {
          org_ref: org.trim(),
          created_by: by,
          mst: mst.trim() || undefined,
          agreement_ref: agreement.trim() || undefined,
        },
        idem,
      );
      setIdem(newIdempotencyKey());
      onCreated(a.anchor_id);
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className="card sec stack"
      onSubmit={(e) => void submit(e)}
      aria-label="Lập HTX tạm"
    >
      <h2 className="h3">Lập HTX tạm</h2>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
        <label className="field">
          <span>Mã Nút HTX (org_ref)</span>
          <input
            value={org}
            onChange={(e) => setOrg(e.target.value)}
            required
            aria-label="Mã Nút HTX"
          />
        </label>
        <label className="field">
          <span>Bên lập</span>
          <select
            value={by}
            onChange={(e) => setBy(e.target.value as "kn" | "mkg")}
            aria-label="Bên lập"
          >
            <option value="kn">Khuyến nông</option>
            <option value="mkg">MKG</option>
          </select>
        </label>
        <label className="field">
          <span>Mã số thuế (tùy chọn)</span>
          <input
            value={mst}
            onChange={(e) => setMst(e.target.value)}
            aria-label="Mã số thuế"
            aria-describedby="mst-hint"
          />
          <small id="mst-hint">{mstHint}</small>
        </label>
        <label className="field">
          <span>Thỏa thuận HTX (căn cứ chia sẻ cho Khuyến nông)</span>
          <input
            value={agreement}
            onChange={(e) => setAgreement(e.target.value)}
            aria-label="Thỏa thuận HTX"
          />
        </label>
      </div>
      <div>
        <button
          type="submit"
          className="btn btn-primary"
          disabled={busy || org.trim() === ""}
        >
          Lập HTX tạm
        </button>
      </div>
    </form>
  );
}

function AnchorDetail({
  id,
  onChanged,
}: {
  id: string;
  onChanged: () => void;
}) {
  const { adminApi } = useAuth();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [mst, setMst] = useState("");
  const [account, setAccount] = useState("");
  const [verified, setVerified] = useState("");
  const [vref, setVref] = useState("");
  const [target, setTarget] = useState("");
  const [report, setReport] = useState<HTXClaimReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [idem, setIdem] = useState(newIdempotencyKey);

  const detail = useAsync<HTXAnchorDetail>(
    () => getAnchor(adminApi, id),
    [adminApi, id],
  );
  const a = detail.data;
  const reload = detail.reload;

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setProblem(null);
    try {
      await fn();
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    } finally {
      setBusy(false);
    }
  };

  const claim = (dry: boolean) =>
    run(async () => {
      const body = {
        account_ref: account.trim(),
        verified_mst: verified,
        verification_ref: vref.trim(),
        target_org_ref: target.trim() || undefined,
        dry_run: dry,
      };
      setReport(
        await claimAnchor(adminApi, id, body, dry ? newIdempotencyKey() : idem),
      );
      if (!dry) {
        setIdem(newIdempotencyKey());
        reload();
        onChanged();
      }
    });

  if (!a)
    return detail.error ? (
      <ProblemAlert problem={detail.error} onRetry={reload} />
    ) : (
      <Loading />
    );
  return (
    <section className="card sec stack" aria-label="Chi tiết neo">
      <h2 className="h3">
        Neo <code>{a.anchor_key}</code> —{" "}
        <span className={CONF_CLS[a.confirmation_status]}>
          {a.confirmation_label}
        </span>
      </h2>
      {problem ? <ProblemAlert problem={problem} /> : null}
      <dl className="kv">
        <dt>Bên lập</dt>
        <dd>{CREATED_BY_LABEL[a.created_by]}</dd>
        <dt>Đồng sở hữu</dt>
        <dd>MKG + HTX</dd>
        <dt>Nút hiện hành</dt>
        <dd>
          <code>{a.org_ref}</code>
        </dd>
        <dt>Bí danh</dt>
        <dd>
          {a.aliases.map((x) => (
            <code key={x}>{x} </code>
          ))}
        </dd>
        <dt>Thỏa thuận HTX</dt>
        <dd>{a.agreement_ref ?? "—"}</dd>
      </dl>
      {a.temporary && (
        <form
          className="row"
          style={{ gap: 8 }}
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              await attachMST(adminApi, id, mst, newIdempotencyKey());
              reload();
              onChanged();
            });
          }}
        >
          <label className="field">
            <span>Gắn mã số thuế</span>
            <input
              value={mst}
              onChange={(e) => setMst(e.target.value)}
              aria-label="MST cần gắn"
            />
          </label>
          <button
            type="submit"
            className="btn"
            disabled={busy || !normalizeMST(mst)}
          >
            Gắn MST
          </button>
        </form>
      )}
      {a.status === "provisional" && (
        <div className="stack" aria-label="Nhận quyền">
          <h3 className="h4">HTX thật nhận quyền</h3>
          {a.temporary && (
            <p className="c-warn">Cần gắn mã số thuế trước khi nhận quyền.</p>
          )}
          <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
            <label className="field">
              <span>Tài khoản HTX đã kích hoạt</span>
              <input
                value={account}
                onChange={(e) => setAccount(e.target.value)}
                aria-label="Tài khoản HTX"
              />
            </label>
            <label className="field">
              <span>MST đã xác minh</span>
              <input
                value={verified}
                onChange={(e) => setVerified(e.target.value)}
                aria-label="MST đã xác minh"
              />
            </label>
            <label className="field">
              <span>Chứng cứ xác minh</span>
              <input
                value={vref}
                onChange={(e) => setVref(e.target.value)}
                aria-label="Chứng cứ xác minh"
              />
            </label>
            <label className="field">
              <span>Nút HTX thật (tùy chọn)</span>
              <input
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                aria-label="Nút HTX thật"
              />
            </label>
          </div>
          <div className="row" style={{ gap: 8 }}>
            <button
              type="button"
              className="btn"
              disabled={busy || a.temporary}
              onClick={() => void claim(true)}
            >
              Chạy thử
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={
                busy || a.temporary || !report?.dry_run || !report.would_succeed
              }
              onClick={() => void claim(false)}
            >
              Nhận quyền
            </button>
          </div>
        </div>
      )}
      {report && <ClaimReportView rep={report} />}
      <h3 className="h4">Nhật ký (vết kiểm toán)</h3>
      <ol>
        {a.events.map((e) => (
          <li key={e.id}>
            #{e.seq} {e.action} — <code>{e.anchor_key}</code> —{" "}
            {new Date(e.recorded_at).toLocaleString("vi-VN")}
            {e.verification_ref ? ` — xác minh: ${e.verification_ref}` : ""}
          </li>
        ))}
      </ol>
    </section>
  );
}

function ClaimReportView({ rep }: { rep: HTXClaimReport }) {
  return (
    <div
      className={`alert ${rep.would_succeed ? "alert-info" : "alert-warn"}`}
      role="status"
      aria-label="Báo cáo nhận quyền"
    >
      <div>
        <strong>
          {rep.dry_run
            ? "Chạy thử"
            : rep.replayed
              ? "Đã nhận quyền trước đó"
              : "Đã nhận quyền"}
        </strong>
        :{" "}
        {rep.would_succeed
          ? "có thể nhận quyền"
          : "có xung đột — xử lý trước khi nhận quyền"}
        . Trạng thái {rep.confirmation.before} → {rep.confirmation.after};{" "}
        {movedTotal(rep)} dòng chuyển chủ (không sao chép dữ liệu).
      </div>
      {rep.conflicts.length > 0 && (
        <ul>
          {rep.conflicts.map((c) => (
            <li key={c.org_ref + c.reason}>
              <code>{c.org_ref.slice(0, 8)}…</code>: {c.reason} (
              {c.counts.plots} thửa, {c.counts.memberships} xã viên)
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
