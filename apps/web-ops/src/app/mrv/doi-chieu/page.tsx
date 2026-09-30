"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState, type FormEvent } from "react";
import { unitLabel } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { MrvComparisonLine, MrvComparisonView } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { useUnits } from "@/lib/useUnits";
import { latestComparison, RESULT_CHIP, RESULT_LABEL, tallyLines, validateComparison } from "@/mrv/rules";

const ACTIVITY: Record<string, string> = {
  sowing: "Gieo sạ",
  fertilizing: "Bón phân",
  awd_drying: "Tưới ướt khô xen kẽ (AWD)",
  straw_handling: "Xử lý rơm rạ (FR-07)",
  harvest: "Thu hoạch",
};
const LOCATION: Record<string, string> = { inside_plot: "Trong thửa", outside_plot: "Ngoài thửa", no_gps: "Không có GPS" };
const REVIEW: Record<string, string> = { approved: "HTX đã duyệt", rejected: "HTX từ chối", pending: "Chờ HTX duyệt" };
const activity = (a: string) => ACTIVITY[a] ?? a;

/** Đối chiếu song song nhật ký HTX ↔ minh chứng cán bộ KN của một thửa/vụ (KN US-MRV-02). */
export default function ComparisonPage() {
  return (
    <Suspense fallback={<Loading />}>
      <ComparisonInner />
    </Suspense>
  );
}

function ComparisonInner() {
  const { api, principal } = useAuth();
  const seasonRef = useSearchParams().get("season") ?? "";
  const canConfirm = hasRole(principal, ROLES.mrvCollector);
  const [editing, setEditing] = useState<MrvComparisonLine | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const view = useAsync(
    () => (seasonRef ? unwrap(api.GET("/v1/mrv/seasons/{id}/comparison", { params: { path: { id: seasonRef } } })) : Promise.resolve(null)),
    [api, seasonRef],
  );
  const units = useUnits(api, [view.data?.season.commune_code]);
  const v = view.data;
  const tally = v ? tallyLines(v.lines) : null;

  return (
    <>
      <PageHead
        title="Đối chiếu nhật ký HTX ↔ minh chứng KN"
        subtitle={
          v ? (
            <>
              Thửa #{v.season.plot_ref.slice(-6)} · vụ {v.season.season_code} {v.season.season_year} · {unitLabel(units, v.season.commune_code)} · cửa sổ ghép ±{v.window_days} ngày
            </>
          ) : (
            "KN US-MRV-02"
          )
        }
        actions={
          <Link className="btn btn-o btn-sm" href="/mrv">
            Về danh sách MRV
          </Link>
        }
      />
      {!seasonRef ? <p className="mute">Chọn một thửa/vụ từ danh sách MRV để đối chiếu.</p> : null}
      {view.error ? <ProblemAlert problem={view.error} onRetry={view.reload} /> : null}
      {view.loading && !v ? <Loading /> : null}
      {saved ? (
        <div className="alert alert-info" role="status" style={{ marginBottom: 12 }}>
          {saved}
        </div>
      ) : null}
      {v && tally ? (
        <>
          <section className="row" aria-label="Tóm tắt đối chiếu" style={{ gap: 8, marginBottom: 12, flexWrap: "wrap" }} data-testid="comparison-tally">
            <span className="chip c-ok">Khớp: {formatInt(tally.match)}</span>
            <span className="chip c-bad">Khác biệt: {formatInt(tally.mismatch)}</span>
            <span className="chip c-warn">Chưa kiểm chứng: {formatInt(tally.unverified)}</span>
            <span className="chip">
              Cán bộ đã xác nhận {formatInt(tally.confirmed)}/{formatInt(tally.total)}
            </span>
          </section>
          <div className="card sec">
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Bảng đối chiếu HTX và KN">
                <thead>
                  <tr>
                    <th scope="col">Nhật ký HTX</th>
                    <th scope="col">Minh chứng cán bộ KN gợi ý</th>
                    <th scope="col">Kết quả</th>
                    <th scope="col">Lịch sử xác nhận</th>
                    {canConfirm ? <th scope="col">Thao tác</th> : null}
                  </tr>
                </thead>
                <tbody>
                  {v.lines.map((l) => {
                    const head = latestComparison(l);
                    return (
                      <tr key={l.log_ref} data-log={l.activity_code}>
                        <td>
                          <b>{activity(l.activity_code)}</b>
                          <div className="small mute">
                            {formatDateTime(l.log_event_time)} · {LOCATION[l.location_status] ?? l.location_status} · {l.htx_review_decision ? (REVIEW[l.htx_review_decision] ?? l.htx_review_decision) : "Chờ HTX duyệt"}
                            {l.catalog_version ? ` · danh mục v${l.catalog_version}` : ""}
                          </div>
                        </td>
                        <td>
                          {l.suggested_officer_record ? (
                            <>
                              {activity(l.suggested_officer_record.activity_code)} · {formatDateTime(l.suggested_officer_record.event_time)}
                              <div className="small mute">
                                <code>{l.suggested_officer_record.evidence_type}</code> · {l.suggested_officer_record.review_status === "accepted" ? "Đạt chuẩn" : l.suggested_officer_record.review_status === "rejected" ? "Không đạt" : "Chờ duyệt"}
                              </div>
                            </>
                          ) : (
                            <span className="mute small">Không có minh chứng trong cửa sổ ±{v.window_days} ngày</span>
                          )}
                        </td>
                        <td>
                          <span className={`chip ${RESULT_CHIP[l.result]}`} data-testid="line-result">
                            {RESULT_LABEL[l.result]}
                          </span>
                          <div className="small mute">{l.confirmed ? "Cán bộ đã xác nhận" : `Gợi ý tự động: ${RESULT_LABEL[l.suggested]}`}</div>
                        </td>
                        <td className="small">
                          {l.comparisons.length === 0 ? (
                            <span className="mute">—</span>
                          ) : (
                            <ol className="list-plain">
                              {l.comparisons.map((c) => (
                                <li key={c.id} style={c.id !== head?.id ? { textDecoration: "line-through", opacity: 0.7 } : undefined}>
                                  {RESULT_LABEL[c.result]} · {formatDateTime(c.recorded_at)}
                                  {c.note ? ` · “${c.note}”` : ""}
                                  {c.correction_reason ? ` · sửa: ${c.correction_reason}` : ""}
                                </li>
                              ))}
                            </ol>
                          )}
                        </td>
                        {canConfirm ? (
                          <td>
                            <button type="button" className="btn btn-o btn-sm" onClick={() => setEditing(l)} disabled={v.officer_records.length === 0}>
                              {head ? "Sửa kết quả" : "Xác nhận"}
                            </button>
                          </td>
                        ) : null}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {!canConfirm ? <p className="small mute" style={{ marginTop: 8 }}>Chỉ cán bộ thu minh chứng (mrv_collector) trong địa bàn mới xác nhận Khớp/Khác biệt.</p> : null}
            <p className="small mute" style={{ marginTop: 8 }}>
              Ngày sạ {formatDate(v.season.sowing_date)}. Kết quả đã xác nhận không bị sửa tại chỗ — đổi kết quả tạo bản ghi bù kèm lý do.
            </p>
          </div>
          {editing ? (
            <ConfirmDialog
              view={v}
              line={editing}
              onClose={() => setEditing(null)}
              onSaved={(msg) => {
                setEditing(null);
                setSaved(msg);
                view.reload();
              }}
            />
          ) : null}
        </>
      ) : null}
    </>
  );
}

function ConfirmDialog({ view, line, onClose, onSaved }: { view: MrvComparisonView; line: MrvComparisonLine; onClose: () => void; onSaved: (msg: string) => void }) {
  const { api } = useAuth();
  const head = latestComparison(line);
  const [evidenceId, setEvidenceId] = useState(head?.evidence_id ?? line.suggested_officer_record?.evidence_id ?? view.officer_records[0]?.evidence_id ?? "");
  const [result, setResult] = useState<"match" | "mismatch" | "">(line.suggested === "unverified" ? "" : line.suggested);
  const [note, setNote] = useState("");
  const [reason, setReason] = useState("");
  const [errors, setErrors] = useState<{ field: string; reason: string }[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  // Đổi minh chứng khác cặp đã xác nhận → bản ghi mới (không phải bản ghi bù).
  const supersedes = head && head.evidence_id === evidenceId ? head.id : null;
  const err = (f: string) => errors.find((e) => e.field === f)?.reason;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs = validateComparison({ result, note, evidenceId, supersedesId: supersedes, correctionReason: reason });
    setErrors(errs);
    if (errs.length) return;
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        api.POST("/v1/mrv/comparisons", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: {
            season_ref: view.season.season_ref,
            log_ref: line.log_ref,
            evidence_id: evidenceId,
            result: result as "match" | "mismatch",
            note: note.trim() || null,
            file_refs: [],
            supersedes_id: supersedes,
            correction_reason: supersedes ? reason.trim() : null,
          },
        }),
      );
      onSaved(`Đã ghi kết quả “${RESULT_LABEL[result as "match" | "mismatch"]}” cho ${activity(line.activity_code)}.`);
    } catch (x) {
      setProblem(x instanceof ApiProblem ? x : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={`${supersedes ? "Sửa kết quả" : "Xác nhận"} đối chiếu: ${activity(line.activity_code)}`}
      onClose={onClose}
      actions={
        <button type="submit" form="cmp-form" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang ghi…" : "Ghi kết quả"}
        </button>
      }
    >
      <form id="cmp-form" className="stack" onSubmit={submit} noValidate>
        <div className="field">
          <label htmlFor="cmp-evidence">Minh chứng cán bộ KN</label>
          <select id="cmp-evidence" className="select" value={evidenceId} onChange={(e) => setEvidenceId(e.target.value)}>
            {view.officer_records.map((o) => (
              <option key={o.evidence_id} value={o.evidence_id}>
                {activity(o.activity_code)} — {formatDateTime(o.event_time)}
              </option>
            ))}
          </select>
          {err("evidence_id") ? <span className="err">{err("evidence_id")}</span> : null}
        </div>
        <fieldset className="plain">
          <legend>Kết quả</legend>
          <label className="row small">
            <input type="radio" name="cmp-result" checked={result === "match"} onChange={() => setResult("match")} /> Khớp
          </label>
          <label className="row small">
            <input type="radio" name="cmp-result" checked={result === "mismatch"} onChange={() => setResult("mismatch")} /> Khác biệt
          </label>
          {err("result") ? <span className="err">{err("result")}</span> : null}
        </fieldset>
        <div className="field">
          <label htmlFor="cmp-note">Ghi chú {result === "mismatch" ? "(bắt buộc khi Khác biệt)" : "(không bắt buộc)"}</label>
          <textarea id="cmp-note" className="input" rows={3} value={note} onChange={(e) => setNote(e.target.value)} aria-invalid={!!err("note")} />
          {err("note") ? <span className="err">{err("note")}</span> : null}
        </div>
        {supersedes ? (
          <div className="field">
            <label htmlFor="cmp-reason">Lý do sửa (ít nhất 10 ký tự)</label>
            <input id="cmp-reason" className="input" value={reason} onChange={(e) => setReason(e.target.value)} aria-invalid={!!err("correction_reason")} />
            {err("correction_reason") ? <span className="err">{err("correction_reason")}</span> : null}
          </div>
        ) : null}
        {problem ? <ProblemAlert problem={problem} /> : null}
      </form>
    </Dialog>
  );
}
