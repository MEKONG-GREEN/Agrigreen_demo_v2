"use client";

import Link from "next/link";
import { useMemo, useState, type FormEvent } from "react";
import { proposeBlockReason } from "@/admin/approval";
import {
  COLOR_ROWS,
  displayValuesByKey,
  FULL_KEY,
  MIN_ZOOM_KEY,
  pendingByKey,
  SECTION_TITLE,
  sourceLabel,
  thresholdPlan,
  versionValue,
  WARN_KEY,
  type ColorRowDef,
  type ColorSection,
} from "@/admin/displayConfig";
import { actorLabel, proposeParam, submitParam } from "@/admin/params";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import type { ParamVersion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { HEX_COLOR_ERROR, isHexColor, validateThresholds, type DisplayValue } from "@/gis/legend";

const SOURCE_DOC_ERROR = "Cần nhập văn bản căn cứ (số/ngày) trước khi đề xuất.";

function Swatch({ color, label }: { color: string | null | undefined; label?: string }) {
  if (!color || !isHexColor(color)) return <span className="mute">—</span>;
  return (
    <span className="row" style={{ gap: 6, display: "inline-flex" }}>
      <span aria-hidden="true" data-color={color.toUpperCase()} style={{ width: 22, height: 16, borderRadius: 4, background: color, border: "1px solid rgba(0,0,0,.18)", display: "inline-block" }} />
      <code>{color.toUpperCase()}</code>
      {label ? <span className="sr-only">{label}</span> : null}
    </span>
  );
}

/** Đề xuất = tạo nháp rồi gửi duyệt; lỗi gửi duyệt không mất bản nháp (xử lý tiếp ở Tham số & phê duyệt). */
async function propose(adminApi: ReturnType<typeof useAuth>["adminApi"], body: Parameters<typeof proposeParam>[1], key: string): Promise<ApiProblem | null> {
  const res = await proposeParam(adminApi, body, key);
  return res.submitError;
}

function ColorRow({
  row,
  current,
  pending,
  sourceDoc,
  block,
  onSourceDocMissing,
  onChanged,
}: {
  row: ColorRowDef;
  current: DisplayValue | undefined;
  pending: ParamVersion[];
  sourceDoc: string;
  block: string | null;
  onSourceDocMissing: () => void;
  onChanged: () => void;
}) {
  const { adminApi } = useAuth();
  const [hex, setHex] = useState("");
  const [localErr, setLocalErr] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [reason, setReason] = useState("");
  // Một khóa cho một thao tác: đổi dữ liệu thì khóa mới (R-51).
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const inputId = `hex-${row.key.replace(/[^\w]/g, "_")}`;
  const errId = `${inputId}-err`;
  const fieldErr = localErr ?? (problem ? (fieldErrorMap(problem).text ?? null) : null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const v = hex.trim();
    setDone(null);
    if (!isHexColor(v)) {
      setLocalErr(HEX_COLOR_ERROR);
      return;
    }
    setLocalErr(null);
    if (!sourceDoc.trim()) {
      onSourceDocMissing();
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const submitErr = await propose(adminApi, { key: row.key, text: v.toUpperCase(), effective_from: new Date().toISOString(), source_doc: sourceDoc.trim() }, idemKey);
      if (submitErr) setProblem(submitErr);
      else {
        setDone(`Đã gửi duyệt màu ${v.toUpperCase()} — chờ người phê duyệt khác.`);
        setHex("");
      }
      setIdemKey(newIdempotencyKey());
      onChanged();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };

  const restore = async () => {
    setBusy(true);
    setProblem(null);
    setDone(null);
    const key = newIdempotencyKey();
    try {
      const draft = await unwrap(
        adminApi.POST("/v1/masterdata/params/restore-default", { params: { header: { "Idempotency-Key": key } }, body: { key: row.key, reason: reason.trim() || undefined } }),
      );
      await submitParam(adminApi, draft.id, `${key}:submit`);
      setConfirm(false);
      setReason("");
      setDone(`Đã gửi duyệt khôi phục mặc định ${versionValue(draft)} — chờ người phê duyệt khác.`);
      onChanged();
    } catch (err) {
      setProblem(toApiProblem(err));
      setConfirm(false);
    } finally {
      setBusy(false);
    }
  };

  const isDefault = !current || current.source === "default";
  return (
    <tr data-key={row.key}>
      <th scope="row" style={{ textTransform: "none", fontSize: 13, color: "inherit", letterSpacing: 0 }}>
        {row.label}
        <div className="mute small">
          <code>{row.key}</code>
        </div>
      </th>
      <td>
        <Swatch color={current?.value} />
      </td>
      <td className="small">
        {sourceLabel(current)}
        {current && current.source === "approved" ? (
          <div className="mute">
            Mặc định <code>{current.default}</code>
          </div>
        ) : null}
      </td>
      <td>
        <form className="stack" style={{ gap: 4 }} onSubmit={(e) => void submit(e)} noValidate aria-label={`Đề xuất màu — ${row.label}`}>
          <label htmlFor={inputId} className="small">
            Mã màu mới — {row.label}
          </label>
          <div className="row" style={{ gap: 6, flexWrap: "nowrap" }}>
            <input
              id={inputId}
              className="input"
              style={{ width: 120 }}
              placeholder="#1E90FF"
              value={hex}
              onChange={(e) => {
                setHex(e.target.value);
                setLocalErr(null);
                setIdemKey(newIdempotencyKey());
              }}
              aria-invalid={!!fieldErr}
              aria-describedby={fieldErr ? errId : undefined}
              disabled={!!block}
            />
            <button type="submit" className="btn btn-p btn-sm" disabled={!!block || busy} title={block ?? undefined}>
              {busy ? "Đang gửi…" : "Đề xuất"}
            </button>
          </div>
          {fieldErr ? (
            <span className="err" id={errId} role="alert">
              {fieldErr}
            </span>
          ) : null}
        </form>
      </td>
      <td>
        <div className="stack" style={{ gap: 6 }}>
          {pending.map((p) => (
            <span key={p.id} className="chip c-warn" data-testid="pending-chip">
              Chờ duyệt: {versionValue(p)} (v.{p.version})
            </span>
          ))}
          <button
            type="button"
            className="btn btn-o btn-sm"
            disabled={!!block || busy || isDefault}
            title={block ?? (isDefault ? "Đang dùng giá trị mặc định" : undefined)}
            onClick={() => setConfirm(true)}
          >
            Khôi phục mặc định
          </button>
          {done ? (
            <span className="small" role="status" style={{ color: "var(--g800)" }}>
              {done}
            </span>
          ) : null}
          {problem && !fieldErrorMap(problem).text ? <ProblemAlert problem={problem} /> : null}
        </div>
        {confirm ? (
          <Dialog
            title={`Khôi phục mặc định — ${row.label}`}
            onClose={() => setConfirm(false)}
            actions={
              <button type="button" className="btn btn-p btn-sm" disabled={busy || (!!reason.trim() && reason.trim().length < 10)} onClick={() => void restore()}>
                {busy ? "Đang gửi…" : "Xác nhận khôi phục"}
              </button>
            }
          >
            <p>
              Tạo phiên bản mới mang giá trị mặc định <Swatch color={current?.default} /> và gửi duyệt. Màu hiện hành <Swatch color={current?.value} /> vẫn áp dụng cho tới khi
              một người phê duyệt khác duyệt.
            </p>
            <p className="small mute" style={{ marginTop: 8 }}>
              Xóa cấu hình = khôi phục giá trị mặc định bằng phiên bản mới; giá trị cũ vẫn lưu trong lịch sử (RB-1).
            </p>
            <div className="field" style={{ marginTop: 10 }}>
              <label htmlFor={`${inputId}-reason`}>Lý do (không bắt buộc, tối thiểu 10 ký tự)</label>
              <input id={`${inputId}-reason`} className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
            </div>
          </Dialog>
        ) : null}
      </td>
    </tr>
  );
}

function ThresholdForm({
  warn,
  full,
  pending,
  sourceDoc,
  block,
  onSourceDocMissing,
  onChanged,
}: {
  warn: DisplayValue | undefined;
  full: DisplayValue | undefined;
  pending: ParamVersion[];
  sourceDoc: string;
  block: string | null;
  onSourceDocMissing: () => void;
  onChanged: () => void;
}) {
  const { adminApi } = useAuth();
  const [w, setW] = useState<string | null>(null);
  const [f, setF] = useState<string | null>(null);
  const [localErr, setLocalErr] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const warnVal = w ?? warn?.value ?? "";
  const fullVal = f ?? full?.value ?? "";
  const errs = problem ? fieldErrorMap(problem) : {};

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setDone(null);
    setProblem(null);
    // Giữ nguyên CHUỖI người dùng nhập (dấu phẩy → dấu chấm), không parseFloat (R-10).
    const nw = warnVal.trim().replace(",", ".");
    const nf = fullVal.trim().replace(",", ".");
    const msg = validateThresholds(nw, nf);
    if (msg) {
      setLocalErr(msg);
      return;
    }
    setLocalErr(null);
    const plan = thresholdPlan({ warn: warn?.value ?? "", full: full?.value ?? "" }, { warn: nw, full: nf });
    if (plan.length === 0) {
      setLocalErr("Ngưỡng chưa thay đổi so với giá trị hiện hành.");
      return;
    }
    if (!sourceDoc.trim()) {
      onSourceDocMissing();
      return;
    }
    setBusy(true);
    const sent: string[] = [];
    try {
      for (const step of plan) {
        const submitErr = await propose(adminApi, { key: step.key, value: step.value, unit: "%", effective_from: new Date().toISOString(), source_doc: sourceDoc.trim() }, newIdempotencyKey());
        sent.push(`${step.key === WARN_KEY ? "Sắp đầy" : "Đã đầy"} ${step.value}%`);
        if (submitErr) {
          setProblem(submitErr);
          break;
        }
      }
      if (sent.length === plan.length) setDone(`Đã gửi duyệt ngưỡng ${sent.join(", ")} — chờ người phê duyệt khác.`);
    } catch (err) {
      setProblem(toApiProblem(err));
      if (sent.length > 0) setDone(`Đã gửi duyệt ${sent.join(", ")}; ngưỡng còn lại cần đề xuất lại sau khi ngưỡng đã gửi được phê duyệt.`);
    } finally {
      setBusy(false);
      onChanged();
    }
  };

  const showErr = localErr ?? (problem?.code === "masterdata.param_percent_thresholds" ? problem.message : (errs.value ?? null));
  return (
    <form className="stack" onSubmit={(e) => void submit(e)} noValidate aria-label="Đề xuất ngưỡng lấp đầy kho/Hub">
      <div className="grid2">
        <div className="field">
          <label htmlFor="thr-warn">Ngưỡng Sắp đầy (%)</label>
          <input id="thr-warn" className="input" inputMode="decimal" value={warnVal} onChange={(e) => setW(e.target.value)} aria-invalid={!!showErr} disabled={!!block} />
          <span className="small mute">
            Hiện hành {warn?.value ?? "—"}% · {sourceLabel(warn)} · mặc định {warn?.default ?? "—"}%
          </span>
        </div>
        <div className="field">
          <label htmlFor="thr-full">Ngưỡng Đã đầy (%)</label>
          <input id="thr-full" className="input" inputMode="decimal" value={fullVal} onChange={(e) => setF(e.target.value)} aria-invalid={!!showErr} disabled={!!block} />
          <span className="small mute">
            Hiện hành {full?.value ?? "—"}% · {sourceLabel(full)} · mặc định {full?.default ?? "—"}%
          </span>
        </div>
      </div>
      {showErr ? (
        <p className="err" role="alert">
          {showErr}
        </p>
      ) : null}
      {problem && problem.code !== "masterdata.param_percent_thresholds" && !errs.value ? <ProblemAlert problem={problem} /> : null}
      {pending.length > 0 ? (
        <div className="row" style={{ gap: 6 }}>
          {pending.map((p) => (
            <span key={p.id} className="chip c-warn" data-testid="pending-chip">
              Chờ duyệt: {p.key === WARN_KEY ? "Sắp đầy" : "Đã đầy"} {versionValue(p)}% (v.{p.version})
            </span>
          ))}
        </div>
      ) : null}
      <div className="row">
        <button type="submit" className="btn btn-p btn-sm" disabled={!!block || busy} title={block ?? undefined}>
          {busy ? "Đang gửi…" : "Đề xuất ngưỡng"}
        </button>
        <span className="small mute">Mỗi ngưỡng đổi là một phiên bản; hệ thống kiểm với ngưỡng còn lại đã duyệt.</span>
      </div>
      {done ? (
        <p className="small" role="status" style={{ color: "var(--g800)" }}>
          {done}
        </p>
      ) : null}
    </form>
  );
}

/**
 * Quy chuẩn hiển thị bản đồ (GIS US-CFG-01/02): bảng màu heatmap giai đoạn, màu mức lấp đầy kho/Hub, viền chín rộ
 * và ngưỡng % — mỗi mục là tham số có phê duyệt (Admin API, MFA). Phê duyệt ở màn Tham số & phê duyệt (R-56).
 */
export default function DisplayConfigPage() {
  const { api, adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const [sourceDoc, setSourceDoc] = useState("");
  const [docErr, setDocErr] = useState(false);
  const cfg = useAsync(() => unwrap(api.GET("/v1/masterdata/display-config")), [api, version]);
  const pendingList = useAsync(
    () => collectPages((cursor) => unwrap(adminApi.GET("/v1/masterdata/params/versions", { params: { query: { status: "submitted", cursor, limit: 200 } } })), 2000),
    [adminApi, version],
  );
  const values = useMemo(() => displayValuesByKey(cfg.data), [cfg.data]);
  const pending = useMemo(() => pendingByKey(pendingList.data ?? []), [pendingList.data]);
  const allPending = useMemo(() => Array.from(pending.values()).flat(), [pending]);
  const labelOf = useMemo(() => {
    const m = new Map<string, string>(COLOR_ROWS.map((r) => [r.key, `${SECTION_TITLE[r.section]} — ${r.label}`]));
    m.set(WARN_KEY, "Ngưỡng Sắp đầy kho/Hub");
    m.set(FULL_KEY, "Ngưỡng Đã đầy kho/Hub");
    m.set(MIN_ZOOM_KEY, "Mức zoom tối thiểu lớp thửa");
    return m;
  }, []);
  const block = proposeBlockReason(principal);
  const reload = () => setVersion((v) => v + 1);
  const missingDoc = () => {
    setDocErr(true);
    document.getElementById("cfg-source-doc")?.focus();
  };

  const sections: ColorSection[] = ["stage", "capacity", "ripening"];
  return (
    <>
      <PageHead title="Quy chuẩn hiển thị bản đồ" subtitle="Bảng màu bản đồ nhiệt giai đoạn mùa vụ, màu mức lấp đầy kho/Hub và ngưỡng % — dùng chung cho mọi bản đồ GIS" />
      <div className="alert alert-info" style={{ marginBottom: 16 }} role="note">
        <Icon name="info" />
        <span>
          <b style={{ display: "inline" }}>Áp dụng toàn hệ thống ngay khi được phê duyệt.</b> Mỗi thay đổi là một phiên bản tham số: người đặt đề xuất →
          một người phê duyệt <b style={{ display: "inline" }}>khác</b> duyệt ở màn <Link href="/quan-tri/tham-so">Tham số &amp; phê duyệt</Link> (R-56). Sau khi
          duyệt, chú giải và lớp bản đồ của mọi người dùng dùng giá trị mới. Xóa cấu hình = khôi phục giá trị mặc định bằng phiên bản mới; giá trị cũ vẫn lưu
          trong lịch sử (RB-1).
        </span>
      </div>
      <MfaNotice />
      {cfg.error ? <ProblemAlert problem={cfg.error} onRetry={cfg.reload} /> : null}
      {pendingList.error ? <ProblemAlert problem={pendingList.error} onRetry={pendingList.reload} /> : null}
      {cfg.loading && !cfg.data ? <Loading /> : null}
      {block ? (
        <p className="small" style={{ color: "var(--amber)", fontWeight: 600, marginBottom: 12 }}>
          <Icon name="info" size={14} /> {block} Bạn vẫn xem được giá trị hiện hành và các đề xuất chờ duyệt.
        </p>
      ) : null}

      <section className="card sec" style={{ marginBottom: 16 }} aria-label="Văn bản căn cứ">
        <div className="field" style={{ maxWidth: 520 }}>
          <label htmlFor="cfg-source-doc">Văn bản căn cứ (số/ngày) — dùng cho mọi đề xuất trên màn này</label>
          <input
            id="cfg-source-doc"
            className="input"
            value={sourceDoc}
            required
            aria-invalid={docErr && !sourceDoc.trim()}
            aria-describedby={docErr && !sourceDoc.trim() ? "cfg-source-doc-err" : undefined}
            onChange={(e) => {
              setSourceDoc(e.target.value);
              setDocErr(false);
            }}
            disabled={!!block}
            placeholder="VD: QĐ 15/QĐ-MKG ngày 30/09/2026"
          />
          {docErr && !sourceDoc.trim() ? (
            <span className="err" id="cfg-source-doc-err" role="alert">
              {SOURCE_DOC_ERROR}
            </span>
          ) : null}
        </div>
      </section>

      {cfg.data
        ? sections.map((sec) => (
            <section key={sec} className="card sec" style={{ marginBottom: 16 }} aria-labelledby={`sec-${sec}`}>
              <div className="sec-h">
                <h2 id={`sec-${sec}`}>{SECTION_TITLE[sec]}</h2>
              </div>
              <div className="tbl-wrap">
                <table className="tbl">
                  <caption className="sr-only">{SECTION_TITLE[sec]}</caption>
                  <thead>
                    <tr>
                      <th scope="col">Hạng mục</th>
                      <th scope="col">Màu hiện hành</th>
                      <th scope="col">Nguồn</th>
                      <th scope="col">Đề xuất màu mới</th>
                      <th scope="col">Trạng thái / thao tác</th>
                    </tr>
                  </thead>
                  <tbody>
                    {COLOR_ROWS.filter((r) => r.section === sec).map((r) => (
                      <ColorRow
                        key={r.key}
                        row={r}
                        current={values.get(r.key)}
                        pending={pending.get(r.key) ?? []}
                        sourceDoc={sourceDoc}
                        block={block}
                        onSourceDocMissing={missingDoc}
                        onChanged={reload}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ))
        : null}

      {cfg.data ? (
        <section className="card sec" style={{ marginBottom: 16 }} aria-labelledby="sec-thr">
          <div className="sec-h">
            <h2 id="sec-thr">Ngưỡng % lấp đầy kho/Hub</h2>
          </div>
          <p className="small mute" style={{ marginBottom: 10 }}>
            Sắp đầy từ ngưỡng thứ nhất, Đã đầy từ ngưỡng thứ hai — phải tăng dần và nằm trong 0–100%. Lớp thửa hiện từ mức zoom{" "}
            {values.get(MIN_ZOOM_KEY)?.value ?? "—"} ({sourceLabel(values.get(MIN_ZOOM_KEY))}).
          </p>
          <ThresholdForm
            key={`${values.get(WARN_KEY)?.value}-${values.get(FULL_KEY)?.value}`}
            warn={values.get(WARN_KEY)}
            full={values.get(FULL_KEY)}
            pending={[...(pending.get(WARN_KEY) ?? []), ...(pending.get(FULL_KEY) ?? [])]}
            sourceDoc={sourceDoc}
            block={block}
            onSourceDocMissing={missingDoc}
            onChanged={reload}
          />
        </section>
      ) : null}

      <section className="card sec" aria-labelledby="sec-pending">
        <div className="sec-h">
          <h2 id="sec-pending">Đề xuất chờ duyệt ({allPending.length})</h2>
          <Link href="/quan-tri/tham-so" className="btn btn-o btn-sm">
            Phê duyệt tại Tham số &amp; phê duyệt
          </Link>
        </div>
        {pendingList.loading && !pendingList.data ? <Loading /> : null}
        {pendingList.data && allPending.length === 0 ? <p className="small mute">Không có đề xuất quy chuẩn hiển thị nào đang chờ duyệt.</p> : null}
        {allPending.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Đề xuất quy chuẩn hiển thị chờ duyệt">
              <thead>
                <tr>
                  <th scope="col">Hạng mục</th>
                  <th scope="col">Giá trị đề xuất</th>
                  <th scope="col">Phiên bản</th>
                  <th scope="col">Người đặt</th>
                  <th scope="col">Gửi duyệt lúc</th>
                  <th scope="col">Căn cứ</th>
                </tr>
              </thead>
              <tbody>
                {allPending.map((p) => (
                  <tr key={p.id} data-key={p.key}>
                    <td>{labelOf.get(p.key) ?? p.key}</td>
                    <td>{p.text ? <Swatch color={p.text} /> : `${versionValue(p)}${p.unit === "%" ? "%" : ""}`}</td>
                    <td>v.{p.version}</td>
                    <td>{actorLabel(p.setter_name, p.setter_ref)}</td>
                    <td>{formatDateTime(p.submitted_at)}</td>
                    <td className="small">{p.source_doc}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
    </>
  );
}
