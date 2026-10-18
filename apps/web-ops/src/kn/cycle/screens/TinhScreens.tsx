"use client";

// Cấp Tỉnh (BRD §4.3): Phát hành biểu mẫu FN-TI-02.02, Trạng thái nộp FN-TI-02.03, Rà soát & chốt kỳ FN-TI-02.04.
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { Dialog } from "@/components/Dialog";
import { ActionBar, BackLink, DetailLayout, LabelValueBlock, LogPanel } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { formLayout } from "@/kn/forms/api";
import type { FormDefinition } from "@/kn/forms/types";
import { FormGrid, communeRows } from "@/kn/grid";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { RawFile, type RcBatchInput, type RcFormItem, type RcPreview, type RcProvincialImport, type RcStatusItem, type RcTask } from "../api";
import {
  LoadState,
  REPORT_RULE_V2_FROM,
  TaskPill,
  dt,
  reportWeekStart,
  toLog,
  useAction,
  useCommuneNames,
  useCycleReadOnly,
  useLoad,
  weekRangeName,
  weekStartLocal,
  weekStartOfRule,
  ymdLocal,
} from "../common";
import { PiiHint } from "@/kn/pii";


// Hạn tính từ ngày cuối kỳ; "" = hạn mặc định của lịch kỳ máy chủ (v2: tuần → 12:00 Thứ Tư sau kỳ; W11.03).
const DEADLINES = [
  { value: "", label: "Mặc định theo lịch kỳ" },
  { value: "1|12:00", label: "1 ngày sau ngày cuối kỳ, 12:00" },
  { value: "1|17:00", label: "1 ngày sau ngày cuối kỳ, 17:00" },
  { value: "0|17:00", label: "Ngày cuối kỳ, 17:00" },
  { value: "2|11:00", label: "2 ngày sau ngày cuối kỳ, 11:00" },
];

type PublishReportType = "weekly" | "monthly" | "seasonal" | "adhoc";
type PeriodChoice = "" | NonNullable<RcBatchInput["period_type"]>;

/**
 * Cách chia kỳ theo loại báo cáo (W11.05b, như reportcycle/domain.ReportPeriodTypes): "" = mặc định của loại báo cáo.
 * Ngày / Tuần KSGM / Quý chỉ chia được với lịch kỳ v2 (đợt bắt đầu từ REPORT_RULE_V2_FROM); tuần KSGM theo ranh giới
 * tham số extension.ksgm_week_starts của từng tháng.
 */
const PERIOD_CHOICES: Record<PublishReportType, { value: PeriodChoice; label: string }[]> = {
  weekly: [
    { value: "", label: "Tuần báo cáo" },
    { value: "ksgm_week", label: "Tuần KSGM (4 tuần/tháng)" },
    { value: "day", label: "Ngày" },
  ],
  monthly: [
    { value: "", label: "Tháng" },
    { value: "quarter", label: "Quý" },
  ],
  seasonal: [{ value: "", label: "Cả vụ (một kỳ)" }],
  adhoc: [
    { value: "", label: "Một kỳ" },
    { value: "day", label: "Ngày" },
  ],
};

/** FN-TI-02.02 Phát hành biểu mẫu báo cáo (Hình 7). */
export function PublishScreen() {
  const { cycleApi, kn } = useAuth();
  const router = useRouter();
  const ws = weekStartLocal(new Date(), reportWeekStart(new Date(), kn.context?.department_code)); // D8: lịch tuần của phòng
  const [rt, setRt] = useState<PublishReportType>("weekly");
  const [pt, setPt] = useState<PeriodChoice>("");
  const [picked, setPicked] = useState<string[]>([]);
  const [mode, setMode] = useState<"province" | "regions" | "communes">("province");
  const [regions, setRegions] = useState<string[]>([]);
  const [communes, setCommunes] = useState<string[]>([]);
  const [from, setFrom] = useState(ymdLocal(ws));
  const [to, setTo] = useState(ymdLocal(new Date(ws.getFullYear(), ws.getMonth(), ws.getDate() + 27)));
  const [season, setSeason] = useState("Thu Đông 2026");
  const [deadline, setDeadline] = useState("");
  const [closeAfter, setCloseAfter] = useState(1);
  const [note, setNote] = useState("");
  const [pv, setPv] = useState<Record<string, Record<string, Record<string, string>>>>({});
  const [pvSource, setPvSource] = useState<"manual" | "excel">("manual");
  const [pvImport, setPvImport] = useState<RcProvincialImport | null>(null);
  const [preview, setPreview] = useState<RcPreview | null>(null);
  const [showBatches, setShowBatches] = useState(false);
  const act = useAction();
  const forms = useLoad(() => cycleApi.reportForms(rt, from), [rt, from]);
  const batches = useLoad(() => cycleApi.batches(), [preview]);
  const body = (): RcBatchInput => {
    const [offset, time] = deadline.split("|");
    return {
      report_type: rt,
      template_ids: picked,
      ...(pt ? { period_type: pt } : {}),
      scope: { mode, ...(mode === "regions" ? { regions } : {}), ...(mode === "communes" ? { communes } : {}) },
      from_date: from,
      to_date: to,
      season_label: season,
      ...(deadline ? { deadline: { offset_days: Number(offset), time: time! } } : {}),
      close_after_days: closeAfter,
      ...(note ? { note } : {}),
      provincial_values: pv,
      provincial_source: pvSource,
    };
  };
  const doPreview = async () => {
    const r = await act.run(() => cycleApi.preview(body()));
    if (r) {
      setPreview(r);
    }
  };
  const provCols = (forms.data?.items ?? []).filter((f) => picked.includes(f.template_id)).flatMap((f) => f.provincial_columns.map((c) => ({ f, c })));
  const previewCommunes = preview?.communes ?? [];
  const name = useCommuneNames(previewCommunes.map((c) => c.commune_code));
  /** UR-TI-02: tệp mẫu CSV UTF-8 (BOM) — cột Mã xã, Tên xã và mỗi cột Tỉnh giao "<Mã biểu> <Chữ cột>". */
  const downloadPvTemplate = () => {
    const esc = (v: string) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
    const head = ["Mã xã", "Tên xã", ...provCols.map(({ f, c }) => `${f.code} ${c.letter} ${c.title}${c.unit ? ` (${c.unit})` : ""}`)];
    const lines = [head, ...previewCommunes.map((cm) => [cm.commune_code, name(cm.commune_code), ...provCols.map(({ f, c }) => pv[f.template_id]?.[cm.commune_code]?.[c.letter] ?? "")])];
    const blob = new Blob([String.fromCharCode(0xfeff) + lines.map((l) => l.map(esc).join(",")).join("\r\n") + "\r\n"], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `tinh-giao-${rt}-${from}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const importPv = async (file: File) => {
    const xlsx = /\.xlsx$/i.test(file.name);
    const type = xlsx ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" : "text/csv; charset=utf-8";
    const r = await act.run(
      async () => cycleApi.provincialImport({ report_type: rt, from_date: from, template_ids: picked }, new RawFile(await file.arrayBuffer(), type)),
      (x) => `Đã đọc ${x.accepted} dòng từ “${file.name}”${x.rejected ? `, ${x.rejected} dòng lỗi` : ""}.`,
    );
    if (!r) return;
    setPvImport(r);
    setPvSource("excel");
    setPv((cur) => {
      const next = { ...cur };
      for (const [tpl, byCommune] of Object.entries(r.provincial_values)) next[tpl] = { ...(next[tpl] ?? {}), ...byCommune };
      return next;
    });
  };
  const inPreview = new Set(previewCommunes.map((c) => c.commune_code));
  const outsideImport = pvImport ? pvImport.rows.filter((x) => x.status === "accepted" && previewCommunes.length > 0 && !inPreview.has(x.commune_code)) : [];
  return (
    <>
      <PageHeader
        title="Phát hành biểu mẫu báo cáo"
        actions={
          <>
            <button type="button" className="btn btn-o" onClick={() => setShowBatches(true)}>
              Đợt đã phát hành ({batches.data?.items.length ?? 0})
            </button>
            <button type="button" className="btn btn-o" disabled={!picked.length || act.busy} onClick={() => void doPreview()}>
              <Icon name="eye" size={15} /> Xem trước
            </button>
            <button
              type="button"
              className="btn btn-p"
              disabled={!picked.length || act.busy}
              onClick={async () => {
                const r = await act.run(() => cycleApi.issue(body()), (x) => `Đã phát hành ${x.batch.task_count.toLocaleString("vi-VN")} công việc cho ${x.batch.communes.length} xã, ${x.periods.length} kỳ.`);
                if (r) {
                  setPreview(null);
                  batches.reload();
                }
              }}
            >
              <Icon name="send" size={15} /> Phát hành
            </button>
          </>
        }
      />
      {act.view}
      <div className="detail-grid">
        <div className="stack">
          <section className="card sec" aria-label="Bước 1 · Chọn biểu mẫu">
            <h2>Bước 1 · Chọn biểu mẫu (cùng loại kỳ)</h2>
            <LoadState s={forms}>
              {(d) => (
                <table className="dt">
                  <thead>
                    <tr>
                      <th scope="col">Chọn</th>
                      <th scope="col">Mã</th>
                      <th scope="col">Tên biểu</th>
                      <th scope="col">Mẫu</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.items.map((f: RcFormItem) => (
                      <tr key={f.template_id}>
                        <td>
                          <input
                            type="checkbox"
                            aria-label={`Chọn ${f.code}`}
                            checked={picked.includes(f.template_id)}
                            onChange={(e) => setPicked((p) => (e.target.checked ? [...p, f.template_id] : p.filter((x) => x !== f.template_id)))}
                          />
                        </td>
                        <td>
                          <b>{f.code}</b>
                        </td>
                        <td>{f.name}</td>
                        <td>
                          <StatusPill tone="info">v{f.version}</StatusPill>
                        </td>
                      </tr>
                    ))}
                    {!d.items.length ? (
                      <tr>
                        <td colSpan={4} className="empty">
                          Chưa có mẫu đã phát hành gán cho loại báo cáo này (FN-AD-04.06).
                        </td>
                      </tr>
                    ) : null}
                  </tbody>
                </table>
              )}
            </LoadState>
          </section>
          <section className="card sec" aria-label="Bước 2 · Chọn xã/phường nhận">
            <h2>Bước 2 · Chọn xã/phường nhận</h2>
            <div className="row" role="radiogroup" aria-label="Phạm vi xã">
              {(
                [
                  ["province", "Toàn tỉnh"],
                  ["regions", "Theo Khu vực"],
                  ["communes", "Chọn từng xã"],
                ] as const
              ).map(([v, l]) => (
                <button key={v} type="button" role="radio" aria-checked={mode === v} className={`btn btn-sm ${mode === v ? "btn-p" : "btn-o"}`} onClick={() => setMode(v)}>
                  {l}
                </button>
              ))}
            </div>
            {mode === "regions" ? (
              <div className="row" style={{ marginTop: 8 }}>
                {[
                  ["LX", "Khu vực Long Xuyên"],
                  ["CD", "Khu vực Châu Đốc"],
                ].map(([c, n]) => (
                  <label key={c} className="row" style={{ gap: 4 }}>
                    <input type="checkbox" checked={regions.includes(c!)} onChange={(e) => setRegions((r) => (e.target.checked ? [...r, c!] : r.filter((x) => x !== c)))} /> {n}
                  </label>
                ))}
              </div>
            ) : null}
            {mode === "communes" ? (
              <label className="field" style={{ marginTop: 8 }}>
                <span>Mã xã (cách nhau dấu phẩy)</span>
                <input className="input" value={communes.join(",")} onChange={(e) => setCommunes(e.target.value.split(",").map((x) => x.trim()).filter(Boolean))} />
              </label>
            ) : null}
            {preview ? (
              <div className="alert alert-warn" role="status" style={{ marginTop: 10 }}>
                <Icon name="alert" />
                <span>
                  {preview.commune_count} xã/phường · {preview.communes_without_officer.length} xã chưa có cán bộ phụ trách Phòng Trồng trọt
                  {preview.communes_without_officer.length ? ` (${preview.communes_without_officer.map(name).join(", ")})` : ""}
                </span>
              </div>
            ) : null}
          </section>
          {provCols.length ? (
            <section className="card sec" aria-label="Bước 4 · Giá trị cột Tỉnh giao">
              <h2>Bước 4 · Giá trị cột &quot;Tỉnh giao&quot; của biểu</h2>
              {!previewCommunes.length ? <p className="mute">Bấm “Xem trước” để có danh sách xã nhận.</p> : null}
              {previewCommunes.length ? (
                <table className="dt">
                  <thead>
                    <tr>
                      <th scope="col">Xã/phường</th>
                      {provCols.map(({ f, c }) => (
                        <th key={`${f.code}${c.letter}`} scope="col">
                          {f.code} · cột {c.letter} “{c.title}” {c.unit ? `(${c.unit})` : ""}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {previewCommunes.map((cm) => (
                      <tr key={cm.commune_code}>
                        <td>
                          {name(cm.commune_code)} · {cm.region_name}
                        </td>
                        {provCols.map(({ f, c }) => (
                          <td key={`${f.code}${c.letter}`}>
                            <input
                              className="input"
                              inputMode="decimal"
                              aria-label={`${f.code} ${c.letter} ${cm.commune_code}`}
                              value={pv[f.template_id]?.[cm.commune_code]?.[c.letter] ?? ""}
                              onChange={(e) =>
                                setPv((x) => ({ ...x, [f.template_id]: { ...(x[f.template_id] ?? {}), [cm.commune_code]: { ...(x[f.template_id]?.[cm.commune_code] ?? {}), [c.letter]: e.target.value } } }))
                              }
                            />
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
              <div className="row" style={{ gap: 8, marginTop: 10 }} aria-label="Nạp Tỉnh giao từ Excel">
                <button type="button" className="btn btn-o btn-sm" disabled={!previewCommunes.length} onClick={downloadPvTemplate}>
                  <Icon name="download" size={14} /> Tải tệp mẫu
                </button>
                <label className="btn btn-o btn-sm">
                  <Icon name="upload" size={14} /> Nạp từ Excel/CSV
                  <input
                    type="file"
                    accept=".xlsx,.csv,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                    aria-label="Tệp Tỉnh giao"
                    style={{ display: "none" }}
                    disabled={act.busy || !picked.length}
                    onChange={(e) => {
                      const file = e.target.files?.[0];
                      e.target.value = "";
                      if (file) void importPv(file);
                    }}
                  />
                </label>
                {pvSource === "excel" ? <StatusPill tone="info">Nguồn: Excel</StatusPill> : null}
              </div>
              {pvImport ? (
                <div className="small" role="status" aria-label="Kết quả nạp Tỉnh giao" style={{ marginTop: 8 }}>
                  <div>
                    Cột nhận: {pvImport.columns.map((c) => `${c.form_code} ${c.letter}`).join(", ")}
                    {pvImport.ignored_columns.length ? ` · bỏ qua: ${pvImport.ignored_columns.join(", ")}` : ""}
                  </div>
                  {pvImport.rows
                    .filter((x) => x.status === "rejected")
                    .map((x) => (
                      <div key={x.row} style={{ color: "var(--danger)" }}>
                        Dòng {x.row}
                        {x.commune_code ? ` (${x.commune_code})` : ""}: {x.reason}
                      </div>
                    ))}
                  {outsideImport.length ? (
                    <div style={{ color: "var(--warn, #b45309)" }}>Xã không nhận đợt này (sẽ bị từ chối khi phát hành): {outsideImport.map((x) => x.commune_code).join(", ")}</div>
                  ) : null}
                </div>
              ) : null}
              <p className="small mute" style={{ marginTop: 8 }}>
                Tệp một dòng một xã: cột “Mã xã” và mỗi cột Tỉnh giao có tiêu đề “&lt;Mã biểu&gt; &lt;Chữ cột&gt;” (vd “T01 E”). Ô công thức bị từ chối — dán giá trị. Cấp Xã thấy giá trị này ở dạng chỉ đọc.
              </p>
            </section>
          ) : null}
        </div>
        <div className="stack">
          <section className="card sec" aria-label="Bước 3 · Kỳ và hạn nộp">
            <h2>Bước 3 · Kỳ và hạn nộp</h2>
            <label className="field">
              <span>Loại kỳ*</span>
              <select className="input" value={rt} onChange={(e) => (setRt(e.target.value as typeof rt), setPt(""), setPicked([]), setPreview(null))}>
                <option value="weekly">Tuần</option>
                <option value="monthly">Tháng</option>
                <option value="seasonal">Vụ</option>
                <option value="adhoc">Đột xuất</option>
              </select>
            </label>
            <label className="field">
              <span>Chia kỳ theo</span>
              <select className="input" value={pt} onChange={(e) => (setPt(e.target.value as PeriodChoice), setPreview(null))}>
                {PERIOD_CHOICES[rt].map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </label>
            {pt && from < REPORT_RULE_V2_FROM ? (
              <p className="small" role="note" style={{ color: "var(--warn, #b45309)" }}>
                Kỳ ngày / tuần KSGM / quý theo lịch kỳ hệ thống mới — đợt phải bắt đầu từ {REPORT_RULE_V2_FROM.split("-").reverse().join("/")}.
              </p>
            ) : null}
            <label className="field">
              <span>Từ ngày*</span>
              <input className="input" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
            </label>
            <label className="field">
              <span>Đến ngày*</span>
              <input className="input" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
            </label>
            <label className="field">
              <span>Tên vụ / đợt*</span>
              <input className="input" value={season} onChange={(e) => setSeason(e.target.value)} />
            </label>
            <label className="field">
              <span>Hạn nộp mỗi kỳ</span>
              <select className="input" value={deadline} onChange={(e) => setDeadline(e.target.value)}>
                {DEADLINES.map((d) => (
                  <option key={d.value} value={d.value}>
                    {d.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="field">
              <span>Chốt kỳ sau hạn (ngày)</span>
              <input className="input" type="number" min={0} max={60} value={closeAfter} onChange={(e) => setCloseAfter(Number(e.target.value))} />
            </label>
            <label className="field">
              <span>Ghi chú giao việc</span>
              <input className="input" value={note} onChange={(e) => setNote(e.target.value)} />
              <PiiHint text={note} />
            </label>
          </section>
          <section className="card sec" aria-label="Xem trước">
            <h2>Xem trước</h2>
            {preview ? (
              <>
                <dl className="kv">
                  <dt>Số biểu</dt>
                  <dd>{preview.form_count}</dd>
                  <dt>Số xã</dt>
                  <dd>{preview.commune_count}</dd>
                  <dt>Số kỳ</dt>
                  <dd>{preview.period_count}</dd>
                  <dt>Lịch tuần</dt>
                  <dd data-testid="calendar">
                    {weekRangeName(weekStartOfRule(preview.period_rule))} ({preview.calendar_id} v{preview.calendar_version})
                  </dd>
                  <dt>Công việc sẽ tạo</dt>
                  <dd data-testid="task-count">{preview.task_count.toLocaleString("vi-VN")}</dd>
                </dl>
                <ol className="list-plain small">
                  {preview.periods.map((p) => (
                    <li key={p.slot}>
                      {p.label} ({p.range_label}){p.period_key ? ` · ${p.period_key}` : ""} — hạn {dt(p.due_at)}
                    </li>
                  ))}
                </ol>
                {preview.warnings.map((w) => (
                  <p key={w} className="small" style={{ color: "var(--warn, #b45309)" }}>
                    {w}
                  </p>
                ))}
              </>
            ) : (
              <p className="mute">Chọn biểu, phạm vi, kỳ rồi bấm “Xem trước”.</p>
            )}
          </section>
        </div>
      </div>
      {showBatches ? (
        <Dialog title="Đợt đã phát hành" onClose={() => setShowBatches(false)}>
          <ul className="list-plain">
            {(batches.data?.items ?? []).map((b) => (
              <li key={b.id} style={{ marginBottom: 8 }}>
                <b>{b.season_label}</b> · {b.forms.map((f) => f.code).join(", ")} · {b.communes.length} xã · {b.period_count} kỳ · {b.status === "withdrawn" ? "Đã thu hồi" : "Đang áp dụng"}{" "}
                <button type="button" className="btn btn-o btn-sm" onClick={() => router.push(`/kn/nhiem-vu/trang-thai-nop?dot=${b.id}`)}>
                  Trạng thái nộp
                </button>
                {b.status === "issued" ? (
                  <button
                    type="button"
                    className="btn btn-o btn-sm"
                    onClick={async () => {
                      const r = window.prompt("Lý do thu hồi đợt (≥ 10 ký tự):");
                      if (r) await act.run(() => cycleApi.withdraw(b.id, r), "Đã thu hồi đợt.").then(() => batches.reload());
                    }}
                  >
                    Thu hồi
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        </Dialog>
      ) : null}
    </>
  );
}

/** Chọn đợt + kỳ dùng chung (Trạng thái nộp, Tổng hợp, Chi cục, TW). */
export function useBatchPeriod(initialBatch?: string | null, reportType?: string) {
  const { cycleApi } = useAuth();
  const batches = useLoad(() => cycleApi.batches(reportType ? { report_type: reportType } : {}), [reportType]);
  const [batchId, setBatchId] = useState<string | null>(initialBatch ?? null);
  const active = batchId ?? batches.data?.items.find((b) => b.status === "issued")?.id ?? null;
  const detail = useLoad(active ? () => cycleApi.batch(active) : null, [active]);
  const [periodId, setPeriodId] = useState<string | null>(null);
  const [now] = useState(() => Date.now());
  const periods = detail.data?.periods ?? [];
  const defPeriod = [...periods].reverse().find((p) => Date.parse(p.from_date) <= now) ?? periods[0];
  const period = periods.find((p) => p.id === periodId) ?? defPeriod ?? null;
  return { batches, active, setBatchId: (id: string) => (setBatchId(id), setPeriodId(null)), detail, period, setPeriodId };
}

/** FN-TI-02.03 Trạng thái nộp và nhắc nhở (Hình 8). */
export function StatusScreen() {
  const { cycleApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const initial = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("dot") : null;
  const bp = useBatchPeriod(initial);
  const [form, setForm] = useState("");
  const [status, setStatus] = useState("");
  const [sel, setSel] = useState<string[]>([]);
  const [message, setMessage] = useState("Đề nghị các xã nộp báo cáo đúng hạn.");
  const [page, setPage] = useState(1);
  const act = useAction();
  const board = useLoad(bp.active && bp.period ? () => cycleApi.status(bp.active!, { period_id: bp.period!.id, template_id: form || undefined, status: status || undefined }) : null, [bp.active, bp.period?.id, form, status, act.note]);
  const reminders = useLoad(bp.active && bp.period ? () => cycleApi.reminders(bp.active!, bp.period!.id) : null, [bp.active, bp.period?.id, act.note]);
  const forms = bp.detail.data?.batch.forms ?? [];
  const name = useCommuneNames(board.data?.items.map((i) => i.commune_code) ?? []);
  const columns: Column<RcStatusItem>[] = [
    {
      key: "sel",
      header: "Chọn",
      cell: (r) => (
        <input type="checkbox" aria-label={`Chọn ${name(r.commune_code)} ${r.form_code}`} checked={sel.includes(r.commune_code)} disabled={r.no_officer} onChange={(e) => setSel((s) => (e.target.checked ? [...new Set([...s, r.commune_code])] : s.filter((x) => x !== r.commune_code)))} />
      ),
    },
    { key: "region", header: "Khu vực", cell: (r) => r.region_name ?? r.region_code },
    { key: "commune", header: "Xã", cell: (r) => name(r.commune_code) },
    { key: "form", header: "Biểu", cell: (r) => r.form_code },
    { key: "officer", header: "Cán bộ phụ trách", cell: (r) => (r.no_officer ? <StatusPill tone="bad">Chưa có cán bộ</StatusPill> : r.officers.map((o) => o.display).join(", ")) },
    { key: "status", header: "Trạng thái", cell: (r) => <TaskPill status={r.status} label={r.status_label} /> },
    {
      key: "sent",
      header: "Thời điểm gửi",
      cell: (r) =>
        r.submitted_at ? (
          <Link href={`/kn/nhiem-vu/ra-soat/${r.task_id}`} className="link-all">
            {dt(r.submitted_at)}
          </Link>
        ) : (
          "—"
        ),
    },
    { key: "remind", header: "Nhắc gần nhất", cell: (r) => dt(r.last_reminded_at) },
  ];
  return (
    <>
      <PageHeader title="Trạng thái nộp" />
      <FilterBar onReset={() => (setForm(""), setStatus(""))}>
        <FilterChip label="Đợt" value={bp.active ?? ""} onChange={bp.setBatchId} options={(bp.batches.data?.items ?? []).map((b) => ({ value: b.id, label: `${b.season_label} (${b.forms.map((f) => f.code).join(", ")})` }))} />
        <FilterChip label="Kỳ" value={bp.period?.id ?? ""} onChange={bp.setPeriodId} options={(bp.detail.data?.periods ?? []).map((p) => ({ value: p.id, label: `${p.label} (${p.range_label})` }))} />
        <FilterChip label="Biểu" value={form} onChange={setForm} options={[{ value: "", label: "Tất cả" }, ...forms.map((f) => ({ value: f.template_id, label: f.code }))]} />
        <FilterChip
          label="Trạng thái"
          value={status}
          onChange={setStatus}
          options={[
            { value: "", label: "Tất cả" },
            { value: "submitted", label: "Đã nộp" },
            { value: "draft", label: "Lưu nháp" },
            { value: "not_started", label: "Chưa nhập" },
            { value: "overdue", label: "Trễ hạn" },
            { value: "adjustment_requested", label: "Yêu cầu điều chỉnh" },
            { value: "no_officer", label: "Chưa có cán bộ" },
          ]}
        />
      </FilterBar>
      {act.view}
      <LoadState s={board}>
        {(b) => (
          <>
            <div className="hot-grid" aria-label="Số xã theo trạng thái">
              {(
                [
                  ["submitted", "Đã nộp"],
                  ["draft", "Lưu nháp"],
                  ["not_started", "Chưa nhập"],
                  ["overdue", "Trễ hạn"],
                ] as const
              ).map(([k, l]) => (
                <div className="card hot" key={k} style={{ minHeight: 0, paddingBottom: 14 }}>
                  <div className="lb">{l}</div>
                  <div className="vl">{b.counts[k] ?? 0}</div>
                </div>
              ))}
            </div>
            {readOnly ? null : (
              <div className="card sec row" style={{ gap: 8, flexWrap: "wrap", margin: "12px 0" }}>
                <label className="field" style={{ flex: "1 1 320px" }}>
                  <span>Nội dung nhắc</span>
                  <input className="input" value={message} onChange={(e) => setMessage(e.target.value)} />
                  <PiiHint text={message} />
                </label>
                <button type="button" className="btn btn-p btn-sm" disabled={!sel.length || act.busy} onClick={() => void act.run(() => cycleApi.remind(bp.active!, { period_id: bp.period!.id, template_id: form || null, communes: sel, message }), (r) => `Đã nhắc ${r.reminder.communes.length} xã (${r.reminder.recipient_count} cán bộ).`).then(() => setSel([]))}>
                  Gửi nhắc nhở xã đã chọn
                </button>
                <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void act.run(() => cycleApi.remind(bp.active!, { period_id: bp.period!.id, template_id: form || null, all_pending: true, message }), (r) => `Đã nhắc ${r.reminder.communes.length} xã chưa nộp${r.skipped_no_officer.length ? `; ${r.skipped_no_officer.length} xã chưa có cán bộ` : ""}.`)}>
                  Nhắc tất cả xã chưa nộp
                </button>
              </div>
            )}
            <DataTable caption="Trạng thái nộp theo xã" columns={columns} rows={b.items.slice((page - 1) * 20, page * 20)} rowKey={(r) => r.task_id} page={page} pageSize={20} total={b.items.length} onPage={setPage} />
          </>
        )}
      </LoadState>
      <section className="card sec" aria-label="Lịch sử nhắc" style={{ marginTop: 16 }}>
        <h2>Lịch sử nhắc</h2>
        <ul className="list-plain">
          {(reminders.data?.items ?? []).map((r) => (
            <li key={r.id}>
              {dt(r.at)} · {(r as { actor_display?: string }).actor_display} · {r.mode === "manual" ? "Thủ công" : "Tự động"} · {r.communes.length} xã: “{r.message}”
            </li>
          ))}
          {!reminders.data?.items.length ? <li className="mute">Chưa nhắc lần nào trong kỳ này.</li> : null}
        </ul>
      </section>
    </>
  );
}

/** FN-TI-02.04 Rà soát & chốt kỳ — danh sách báo cáo cần rà soát. */
export function ReviewListScreen({ base = "/kn/nhiem-vu/ra-soat" }: { base?: string }) {
  const { cycleApi } = useAuth();
  const [status, setStatus] = useState("submitted");
  const [form, setForm] = useState("");
  const [page, setPage] = useState(1);
  const s = useLoad(() => cycleApi.tasks({ status: status || undefined, form_code: form || undefined }), [status, form]);
  const name = useCommuneNames(s.data?.items.map((t) => t.commune_code) ?? []);
  const columns: Column<RcTask>[] = [
    { key: "commune", header: "Xã/phường", cell: (t) => name(t.commune_code) },
    { key: "form", header: "Biểu", cell: (t) => <b>{t.form_code}</b> },
    { key: "period", header: "Kỳ", cell: (t) => `${t.period_label} (${t.period_range})` },
    { key: "sent", header: "Gửi lúc", cell: (t) => dt(t.submitted_at) },
    { key: "status", header: "Trạng thái", cell: (t) => <TaskPill status={t.status} label={t.status_label} /> },
    {
      key: "act",
      header: "Thao tác",
      cell: (t) => (
        <Link className="btn btn-o btn-sm" href={`${base}/${t.id}`} aria-label={`Rà soát ${t.form_code} ${name(t.commune_code)} ${t.period_label}`}>
          Rà soát
        </Link>
      ),
    },
  ];
  return (
    <>
      <PageHeader title="Rà soát & chốt kỳ" />
      <FilterBar onReset={() => (setStatus("submitted"), setForm(""))}>
        <FilterChip
          label="Trạng thái"
          value={status}
          defaultValue="submitted"
          onChange={setStatus}
          options={[
            { value: "submitted", label: "Đã nộp (cần rà soát)" },
            { value: "adjustment_requested", label: "Yêu cầu điều chỉnh" },
            { value: "confirmed", label: "Đã xác nhận" },
            { value: "closed", label: "Đã chốt" },
            { value: "", label: "Tất cả" },
          ]}
        />
        <FilterChip label="Biểu" value={form} onChange={setForm} options={[{ value: "", label: "Tất cả" }, ...["T01", "T03", "T04", "T05", "T06"].map((x) => ({ value: x, label: x }))]} />
      </FilterBar>
      <LoadState s={s}>
        {(d) => <DataTable caption="Báo cáo cần rà soát" columns={columns} rows={d.items.slice((page - 1) * 20, page * 20)} rowKey={(t) => t.id} page={page} pageSize={20} total={d.items.length} onPage={setPage} empty="Không có báo cáo cần rà soát." />}
      </LoadState>
    </>
  );
}

/** FN-TI-02.04 Detail "Xã · Biểu · Kỳ" (Hình 9): số liệu đúng mẫu chỉ xem, yêu cầu điều chỉnh/xác nhận, chốt/mở lại kỳ. */
export function ReviewDetailScreen({ base = "/kn/nhiem-vu/ra-soat" }: { base?: string }) {
  const { id } = useParams<{ id: string }>();
  const { cycleApi, api } = useAuth();
  const readOnly = useCycleReadOnly();
  const router = useRouter();
  const act = useAction();
  const [reason, setReason] = useState("");
  const [reopen, setReopen] = useState(false);
  const [reopenReason, setReopenReason] = useState("");
  const [reopenUntil, setReopenUntil] = useState(() => ymdLocal(new Date(Date.now() + 2 * 86_400_000)));
  const s = useLoad(async () => {
    const d = await cycleApi.task(id);
    const f = d.batch.forms.find((x) => x.template_id === d.task.template_id)!;
    const lay = await formLayout(api, f.template_id, f.version, { period_label: d.layout.period_labels, season: d.layout.season });
    const [period, log, plog, list] = await Promise.all([cycleApi.period(d.task.period_id), cycleApi.taskLog(id), cycleApi.periodLog(d.task.period_id), cycleApi.tasks({ period_id: d.task.period_id, status: "submitted" })]);
    return { d, def: lay.definition as FormDefinition, period, log, plog, list };
  }, [id, act.note]);
  const name = useCommuneNames(s.data ? [s.data.d.task.commune_code] : []);
  useBreadcrumbTail(s.data ? [{ label: `${name(s.data.d.task.commune_code)} · ${s.data.d.task.form_code}` }] : []);
  const values = useMemo(() => {
    const out: Record<string, Record<string, string>> = {};
    for (const r of s.data?.d.latest_submitted?.rows ?? []) out[r.key] = r.cells;
    return out;
  }, [s.data]);
  return (
    <>
      <BackLink href={base}>Danh sách báo cáo cần rà soát</BackLink>
      <LoadState s={s}>
        {({ d, def, period, log, plog, list }) => {
          const idx = list.items.findIndex((t) => t.id === id);
          const prev = idx > 0 ? list.items[idx - 1] : undefined;
          const next = idx >= 0 ? list.items[idx + 1] : list.items[0];
          const sub = d.latest_submitted;
          return (
            <>
              <PageHeader title={`${name(d.task.commune_code)} · ${d.task.form_code} · ${d.period.label}`} />
              {readOnly ? null : (
                <ActionBar>
                  <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !sub} onClick={() => void act.run(() => cycleApi.requestAdjustment(id, reason), "Đã gửi yêu cầu điều chỉnh tới Cán bộ xã.")}>
                    <Icon name="edit" size={15} /> Yêu cầu điều chỉnh
                  </button>
                  <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !sub} onClick={() => void act.run(() => cycleApi.confirm(id), "Đã xác nhận báo cáo.")}>
                    <Icon name="check" size={15} /> Xác nhận
                  </button>
                  <button type="button" className="btn btn-o btn-sm" disabled={d.period.status !== "closed"} onClick={() => setReopen(true)}>
                    <Icon name="lock" size={15} /> Mở lại kỳ
                  </button>
                  <button type="button" className="btn btn-o btn-sm" disabled={!prev} onClick={() => prev && router.push(`${base}/${prev.id}`)}>
                    ‹ Xem báo cáo trước
                  </button>
                  <button type="button" className="btn btn-o btn-sm" disabled={!next || next.id === id} onClick={() => next && router.push(`${base}/${next.id}`)}>
                    Báo cáo tiếp theo ›
                  </button>
                </ActionBar>
              )}
              {act.view}
              {act.problem?.code === "self_approval_forbidden" ? (
                <p className="small" role="note">
                  Phân tách nhiệm vụ (R-56): bản này do chính bạn nhập thay ở chế độ hỗ trợ — người khác phải rà soát, xác nhận hoặc chốt kỳ.
                </p>
              ) : null}
              <DetailLayout
                main={
                  <>
                    <LabelValueBlock
                      title="Thông tin báo cáo"
                      items={[
                        { label: "Xã/phường", value: name(d.task.commune_code) },
                        { label: "Cán bộ gửi", value: sub ? (sub as { actor_display?: string }).actor_display : "—" },
                        { label: "Biểu", value: `${d.task.form_code} · ${d.form_name}` },
                        { label: "Gửi lúc", value: dt(d.task.submitted_at) },
                        { label: "Kỳ", value: `${d.period.label} (${d.period.range_label})` },
                        { label: "Trạng thái", value: <TaskPill status={d.task.status} label={d.task.status_label} /> },
                        ...(sub?.on_behalf_of ? [{ label: "Nhập thay", value: `Admin hỗ trợ — lý do: ${sub.support_reason ?? ""}` }] : []),
                      ]}
                    />
                    <h2 style={{ marginTop: 14 }}>Số liệu (đúng mẫu, chỉ xem)</h2>
                    {sub ? (
                      <FormGrid definition={def} rows={communeRows(def, { code: d.task.commune_code, name: name(d.task.commune_code), region: d.task.region_code })} values={values} computed={values} readOnly ariaLabel={`Số liệu ${d.task.form_code}`} maxHeight={260} />
                    ) : (
                      <p className="mute">Xã chưa gửi báo cáo.</p>
                    )}
                    <h2 style={{ marginTop: 14 }}>Kết quả kiểm tra</h2>
                    {sub && !sub.issues.length ? (
                      <div className="alert alert-info" role="status">
                        <Icon name="check" />
                        <span>Đạt yêu cầu của mẫu. Không có cảnh báo.</span>
                      </div>
                    ) : (
                      <ul>{(sub?.issues ?? []).map((i, k) => <li key={k}>{i.letter ? `Cột ${i.letter}: ` : ""}{i.message}</li>)}</ul>
                    )}
                    {readOnly ? null : (
                      <label className="field" style={{ marginTop: 10 }}>
                        <span>Lý do yêu cầu điều chỉnh*</span>
                        <textarea className="textarea" value={reason} onChange={(e) => setReason(e.target.value)} />
                        <PiiHint text={reason} />
                      </label>
                    )}
                  </>
                }
                log={
                  <div className="stack">
                    <aside className="card sec" aria-label="Chốt kỳ">
                      <h2>Chốt kỳ</h2>
                      <dl className="kv">
                        <dt>Kỳ</dt>
                        <dd>{period.period.label}</dd>
                        <dt>Trạng thái</dt>
                        <dd>{period.period.status === "closed" ? "Đã chốt" : "Đang mở"}</dd>
                        <dt>Chốt tự động</dt>
                        <dd>{dt(period.period.auto_close_at)}</dd>
                        <dt>Đã nộp</dt>
                        <dd>
                          {period.submitted}/{period.total}
                        </dd>
                      </dl>
                      {readOnly ? null : (
                        <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void act.run(() => cycleApi.close(period.period.id), period.period.status === "closed" ? "Đã chốt lại kỳ (Hệ thống tổng hợp lại)." : "Đã chốt kỳ — Hệ thống đã tổng hợp một lần.")}>
                          <Icon name="lock" size={15} /> {period.period.status === "closed" ? "Chốt lại" : "Chốt kỳ ngay"}
                        </button>
                      )}
                    </aside>
                    <LogPanel entries={toLog([...log.items, ...plog.items].sort((a, b) => (a.at < b.at ? 1 : -1)))} />
                  </div>
                }
              />
              {reopen ? (
                <Dialog
                  title="Mở lại kỳ đã chốt"
                  onClose={() => setReopen(false)}
                  actions={
                    <button
                      type="button"
                      className="btn btn-p"
                      onClick={async () => {
                        const r = await act.run(
                          () => cycleApi.reopen(d.period.id, { communes: [d.task.commune_code], template_ids: [d.task.template_id], reason: reopenReason, until: new Date(`${reopenUntil}T17:00:00`).toISOString() }),
                          (x) => `Đã mở lại kỳ cho ${x.task_ids.length} công việc đến ${reopenUntil}.${x.dispatch_changed ? " Cảnh báo: kỳ đã gửi Chi cục — số liệu đã thay đổi." : ""}`,
                        );
                        if (r) setReopen(false);
                      }}
                    >
                      Mở lại
                    </button>
                  }
                >
                  <label className="field">
                    <span>Lý do mở lại*</span>
                    <textarea className="textarea" value={reopenReason} onChange={(e) => setReopenReason(e.target.value)} />
                    <PiiHint text={reopenReason} />
                  </label>
                  <label className="field">
                    <span>Mở đến ngày* (17:00)</span>
                    <input className="input" type="date" value={reopenUntil} onChange={(e) => setReopenUntil(e.target.value)} />
                  </label>
                </Dialog>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}
