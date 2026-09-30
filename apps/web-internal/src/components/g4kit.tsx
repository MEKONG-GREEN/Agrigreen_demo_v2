"use client";

// Thành phần dùng chung của màn hình G4 (vận tải, đơn hàng, giá thành, kế toán).
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { formatDate, formatDateTime } from "@/lib/format";
import { flagLabel } from "@/lib/g4labels";
import { formatVnd } from "@/lib/money";
import { isSelf, validReason, VERSION_STATE_LABEL } from "@/lib/rules";
import { toApiProblem } from "@/lib/useAsync";
import { ActionStatus, Chip, Provisional, Ref, SelectField, SelfHint, TextField, useAction } from "./kit";
import { ProblemAlert } from "./ProblemAlert";
import { Empty, Loading } from "./ui";

/** Tiền VND (chuỗi thập phân) — định dạng vi-VN không qua float. */
export function Money({ v, strong }: { v: string | null | undefined; strong?: boolean }) {
  const s = formatVnd(v);
  return strong ? <b className="mono">{s}</b> : <span className="mono">{s}</span>;
}

/** Cờ cảnh báo KHÔNG CHẶN (chip vàng). */
export function Flags({ flags, label = "Cảnh báo" }: { flags: readonly string[] | null | undefined; label?: string }) {
  if (!flags?.length) return <span className="mute">—</span>;
  return (
    <span className="row" role="list" aria-label={label} style={{ gap: 4, flexWrap: "wrap" }}>
      {flags.map((f) => (
        <span role="listitem" key={f} className="chip c-warn" title={f}>
          {flagLabel(f)}
        </span>
      ))}
    </span>
  );
}

/** Ô "người lập → người duyệt" có nhắc R-56 cho người đang xem. */
export function SoD({ maker, checker, makerLabel = "Người lập", checkerLabel = "Người duyệt" }: { maker: string | null | undefined; checker: string | null | undefined; makerLabel?: string; checkerLabel?: string }) {
  const { principal } = useAuth();
  return (
    <span className="small">
      {makerLabel} <Ref id={maker} />
      {isSelf(principal?.subject, maker) ? <b> (bạn)</b> : null} · {checkerLabel} <Ref id={checker} />
      {isSelf(principal?.subject, checker) ? <b> (bạn)</b> : null}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Theo dõi job 202 (mẫu mục 12): hỏi lại định kỳ tới khi xong / lỗi
// ---------------------------------------------------------------------------
export function useJobPoll<J extends { status?: string | null }>(fetchJob: (() => Promise<J>) | null, isDone: (j: J) => boolean, intervalMs = 900, max = 20) {
  const [job, setJob] = useState<J | null>(null);
  const [error, setError] = useState<ApiProblem | null>(null);
  const fnRef = useRef(fetchJob);
  const doneRef = useRef(isDone);
  useEffect(() => {
    fnRef.current = fetchJob;
    doneRef.current = isDone;
  });
  const active = !!fetchJob;
  useEffect(() => {
    if (!active) return;
    let n = 0;
    let stop = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      const fn = fnRef.current;
      if (!fn || stop) return;
      try {
        const j = await fn();
        if (stop) return;
        setJob(j);
        n += 1;
        if (!doneRef.current(j) && n < max) timer = setTimeout(() => void tick(), intervalMs);
      } catch (e) {
        if (!stop) setError(toApiProblem(e));
      }
    };
    timer = setTimeout(() => void tick(), intervalMs);
    return () => {
      stop = true;
      if (timer) clearTimeout(timer);
    };
  }, [active, intervalMs, max]);
  return { job, error };
}

// ---------------------------------------------------------------------------
// Tham số có phiên bản (logistics / orders): đề xuất → người KHÁC duyệt (R-56)
// ---------------------------------------------------------------------------
export interface ParamVersionView {
  id: string;
  seq: number;
  value: string;
  effective_from: string | null;
  status: string;
  provisional: boolean;
  proposed_by: string | null;
  reason: string | null;
  decided_by: string | null;
  decided_at: string | null;
  decision_reason: string | null;
}
export interface ParamView {
  key: string;
  value: string;
  version: string;
  provisional: boolean;
  default: string;
  note?: string;
  versions: ParamVersionView[];
}

export function ParamEditor({
  keys,
  labels,
  load,
  propose,
  decide,
  canPropose,
  canDecide,
  approverLabel,
  unitHint,
}: {
  keys: readonly string[];
  labels: Record<string, string>;
  load: (key: string) => Promise<ParamView>;
  propose: (key: string, body: { value: string; effective_from?: string; reason: string }) => Promise<unknown>;
  decide: (key: string, versionId: string, decision: "approved" | "rejected", reason?: string) => Promise<unknown>;
  canPropose: boolean;
  canDecide: boolean;
  approverLabel: string;
  unitHint?: (key: string) => string | undefined;
}) {
  const [key, setKey] = useState(keys[0]!);
  const [data, setData] = useState<{ key: string; v?: ParamView; err?: ApiProblem } | null>(null);
  const [tick, setTick] = useState(0);
  const [value, setValue] = useState("");
  const [from, setFrom] = useState("");
  const [reason, setReason] = useState("");
  const [decisionReason, setDecisionReason] = useState("");
  const act = useAction();
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  });
  useEffect(() => {
    let on = true;
    loadRef.current(key).then(
      (v) => on && setData({ key, v }),
      (e: unknown) => on && setData({ key, err: toApiProblem(e) }),
    );
    return () => {
      on = false;
    };
  }, [key, tick]);
  const v = data?.key === key ? data.v : undefined;
  const reload = () => setTick((t) => t + 1);
  const pending = v?.versions.find((x) => x.status === "pending");
  return (
    <div className="stack">
      <SelectField label="Khoá tham số" value={key} onChange={(k) => setKey(k)} options={keys.map((k) => ({ value: k, label: `${labels[k] ?? k} (${k})` }))} />
      {data?.key === key && data.err ? <ProblemAlert problem={data.err} /> : null}
      {!v && !(data?.key === key && data.err) ? <Loading /> : null}
      {v ? (
        <>
          <dl className="kv" aria-label="Giá trị đang áp dụng">
            <dt>Đang áp dụng</dt>
            <dd className="row">
              <b className="mono">{v.value}</b> {unitHint?.(key) ?? ""} <span className="mono small">{v.version}</span> {v.provisional ? <Provisional what="Q-7" /> : null}
            </dd>
            <dt>Mặc định #0</dt>
            <dd>
              <span className="mono">{v.default}</span> {v.note ? <span className="small mute">— {v.note}</span> : null}
            </dd>
          </dl>
          <ActionStatus a={act} />
          {v.versions.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Phiên bản tham số">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>Giá trị</th>
                    <th>Hiệu lực từ</th>
                    <th>Trạng thái</th>
                    <th>Người đề xuất / quyết định</th>
                    <th>Lý do</th>
                  </tr>
                </thead>
                <tbody>
                  {v.versions.map((x) => (
                    <tr key={x.id}>
                      <td className="mono">v{x.seq}</td>
                      <td className="mono">{x.value}</td>
                      <td>{formatDate(x.effective_from)}</td>
                      <td>
                        <Chip status={x.status} label={VERSION_STATE_LABEL[x.status]} />
                      </td>
                      <td>
                        <SoD maker={x.proposed_by} checker={x.decided_by} makerLabel="Đề xuất" checkerLabel="Quyết định" />
                      </td>
                      <td className="small">
                        {x.reason}
                        {x.decision_reason ? ` — ${x.decision_reason}` : ""}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <Empty>Chưa có phiên bản nào — đang dùng mặc định tạm, chờ khách.</Empty>
          )}
          {pending ? (
            <div className="panel stack">
              <b>
                Phiên bản v{pending.seq} chờ duyệt: <span className="mono">{pending.value}</span> — {approverLabel} KHÁC người đề xuất duyệt (R-56)
              </b>
              <SelfHint proposer={pending.proposed_by} />
              {canDecide ? (
                <>
                  <TextField label="Lý do từ chối" value={decisionReason} onChange={setDecisionReason} hint="Bắt buộc khi từ chối (≥ 10 ký tự)" />
                  <div className="row">
                    <button
                      type="button"
                      className="btn btn-p btn-sm"
                      disabled={act.busy}
                      onClick={() =>
                        void act.run(() => decide(key, pending.id, "approved"), `Đã duyệt phiên bản v${pending.seq} — áp theo thời điểm sự việc từ ${formatDate(pending.effective_from)}`).then((r) => {
                          if (r) reload();
                        })
                      }
                    >
                      Duyệt tham số
                    </button>
                    <button
                      type="button"
                      className="btn btn-d btn-sm"
                      disabled={act.busy || !validReason(decisionReason)}
                      onClick={() =>
                        void act.run(() => decide(key, pending.id, "rejected", decisionReason.trim()), `Đã từ chối phiên bản v${pending.seq}`).then((r) => {
                          if (r) reload();
                        })
                      }
                    >
                      Từ chối
                    </button>
                  </div>
                </>
              ) : (
                <p className="small mute">Bạn không có vai duyệt ({approverLabel}).</p>
              )}
            </div>
          ) : null}
          {canPropose && !pending ? (
            <div className="panel stack">
              <b>Đề xuất phiên bản mới</b>
              <div className="grid3">
                <TextField label="Giá trị mới" value={value} onChange={setValue} inputMode="decimal" hint={unitHint?.(key)} />
                <TextField label="Hiệu lực từ ngày" type="date" value={from} onChange={setFrom} hint="Bỏ trống = hôm nay; áp theo thời điểm sự việc" />
                <TextField label="Lý do đề xuất" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
              </div>
              <div className="row">
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  disabled={act.busy || !value.trim() || !validReason(reason)}
                  onClick={() =>
                    void act
                      .run(() => propose(key, { value: value.trim(), ...(from ? { effective_from: from } : {}), reason: reason.trim() }), `Đã đề xuất phiên bản mới — chờ ${approverLabel} khác duyệt`)
                      .then((r) => {
                        if (r) {
                          setValue("");
                          setReason("");
                          reload();
                        }
                      })
                  }
                >
                  Gửi đề xuất tham số
                </button>
              </div>
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

/** Dòng mô tả dạng dl. */
export function KV({ items, label }: { items: [string, ReactNode][]; label?: string }) {
  return (
    <dl className="kv" aria-label={label}>
      {items.map(([k, v]) => (
        <div key={k} style={{ display: "contents" }}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

export function When({ at }: { at: string | null | undefined }) {
  return <span>{formatDateTime(at)}</span>;
}
