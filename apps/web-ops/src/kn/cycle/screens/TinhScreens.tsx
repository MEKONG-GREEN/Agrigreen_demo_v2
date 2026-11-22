"use client";

// Cấp Tỉnh (BRD §4.3): Phát hành biểu mẫu FN-TI-02.02, Trạng thái nộp FN-TI-02.03, Rà soát & chốt kỳ FN-TI-02.04.
import Link from "next/link";
import { TT_FORM_CODES } from "../common"; // BF-TT-FORMS #107: đủ T01–T07, M01, V01–V06
import { useParams, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { Dialog } from "@/components/Dialog";
import { TextField, validateForm } from "@/components/form";
import { ActionBar, BackLink, DetailLayout, DetailPage, LabelValueBlock, LogPanel } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { formLayout } from "@/kn/forms/api";
import type { FormDefinition } from "@/kn/forms/types";
import { FormGrid, communeRows } from "@/kn/grid";
import { vnDate } from "@/kn/schedule/week";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { type RcLogEntry, type RcPeriod, type RcStatusItem, type RcTask } from "../api";
import { LoadState, TaskPill, dt, toLog, useAction, useCommuneNames, useCycleReadOnly, useLoad, ymdLocal } from "../common";
import { PiiHint } from "@/kn/pii";
import { DateInput } from "./PublishWizard";

/** FN-TI-02.02 Phát hành biểu mẫu — wizard 5 bước (BF-TT-PUBLISH #106) ở PublishWizard.tsx. */
export { PublishScreen } from "./PublishWizard";

/** FN-TI-02.02 Đợt đã phát hành — trang riêng (R-A2, thay pop-up cũ): xem trạng thái nộp, thu hồi đợt (lý do trong pop-up ngắn). */
export function BatchesScreen() {
  const { cycleApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const act = useAction();
  const s = useLoad(() => cycleApi.batches(), [act.note]);
  const [withdraw, setWithdraw] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [reasonErr, setReasonErr] = useState<string | undefined>();
  type Batch = NonNullable<typeof s.data>["items"][number];
  const columns: Column<Batch>[] = [
    { key: "season", header: "Vụ / đợt", cell: (b) => <b>{b.season_label}</b> },
    { key: "forms", header: "Biểu", cell: (b) => b.forms.map((f) => f.code).join(", ") },
    { key: "communes", header: "Số xã", cell: (b) => b.communes.length, className: "num" },
    { key: "periods", header: "Số kỳ", cell: (b) => b.period_count, className: "num" },
    { key: "st", header: "Trạng thái", cell: (b) => <StatusPill tone={b.status === "withdrawn" ? "neutral" : "ok"}>{b.status === "withdrawn" ? "Đã thu hồi" : "Đang áp dụng"}</StatusPill> },
    {
      key: "act",
      header: "Thao tác",
      cell: (b) => (
        <span className="row" style={{ gap: 6 }}>
          <Link className="btn btn-o btn-sm" href={`/kn/nhiem-vu/trang-thai-nop?dot=${b.id}`}>
            Trạng thái nộp
          </Link>
          {b.status === "issued" && !readOnly ? (
            <button type="button" className="btn btn-o btn-sm" onClick={() => (setReason(""), setReasonErr(undefined), setWithdraw(b.id))}>
              Thu hồi
            </button>
          ) : null}
        </span>
      ),
    },
  ];
  return (
    <DetailPage back={{ href: "/kn/nhiem-vu/phat-hanh", label: "Phát hành biểu mẫu" }} title="Đợt đã phát hành">
      {act.view}
      <LoadState s={s}>{(d) => <DataTable caption="Đợt đã phát hành" columns={columns} rows={d.items} rowKey={(b) => b.id} empty="Chưa phát hành đợt nào." />}</LoadState>
      {withdraw ? (
        <Dialog
          title="Thu hồi đợt"
          onClose={() => setWithdraw(null)}
          actions={
            <button
              type="button"
              className="btn btn-p"
              disabled={act.busy}
              onClick={async () => {
                const e = reason.trim() === "" ? "Bắt buộc nhập." : reason.trim().length < 10 ? "Tối thiểu 10 ký tự." : undefined;
                setReasonErr(e);
                if (e) return;
                if (await act.run(() => cycleApi.withdraw(withdraw, reason.trim()), "Đã thu hồi đợt.")) setWithdraw(null);
              }}
            >
              Thu hồi
            </button>
          }
        >
          <TextField label="Lý do thu hồi (≥ 10 ký tự)" required multiline value={reason} onChange={setReason} error={reasonErr} hint={<PiiHint text={reason} />} />
        </Dialog>
      ) : null}
    </DetailPage>
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
  // from_date là ngày lịch Việt Nam: so theo ngày VN (đầu tuần 00:00–07:00 giờ VN vẫn chọn kỳ hiện tại).
  const [today] = useState(() => vnDate(Date.now()));
  const periods = detail.data?.periods ?? [];
  const defPeriod = [...periods].reverse().find((p) => p.from_date <= today) ?? periods[0];
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
  const [tab, setTab] = useState<"review" | "closed">("review");
  return (
    <>
      <PageHeader title="Rà soát & chốt kỳ" />
      <div className="row" role="tablist" aria-label="Rà soát & chốt kỳ" style={{ gap: 8, marginBottom: 12 }}>
        {(
          [
            ["review", "Báo cáo cần rà soát"],
            ["closed", "Kỳ đã chốt"],
          ] as const
        ).map(([v, l]) => (
          <button key={v} type="button" role="tab" aria-selected={tab === v} className={`btn btn-sm ${tab === v ? "btn-p" : "btn-o"}`} onClick={() => setTab(v)}>
            {l}
          </button>
        ))}
      </div>
      {tab === "review" ? <ReviewTasks base={base} /> : <ClosedPeriods />}
    </>
  );
}

/** Người chốt kỳ: chốt tay → tên người thao tác ở log kỳ (sự kiện period.closed gần nhất); tự chốt → "Hệ thống". */
function ClosedBy({ p }: { p: RcPeriod }) {
  const { cycleApi } = useAuth();
  const log = useLoad(p.close_mode === "manual" ? () => cycleApi.periodLog(p.id) : null, [p.id, p.close_mode, p.closed_at]);
  if (p.close_mode !== "manual") return <>Hệ thống (tự chốt)</>;
  const ev = [...(log.data?.items ?? [])].filter((x) => x.event === "period.closed").sort((a, b) => (a.at < b.at ? 1 : -1))[0] as (RcLogEntry & { actor_display?: string }) | undefined;
  return <>{ev?.actor_display || (log.loading ? "…" : "—")}</>;
}

/** TT-04: danh sách kỳ đã chốt — xem lại ai chốt, lúc nào, cách chốt, trạng thái. */
function ClosedPeriods() {
  const { cycleApi } = useAuth();
  const [rt, setRt] = useState("");
  const [page, setPage] = useState(1);
  const periods = useLoad(() => cycleApi.periods({ report_type: rt || undefined }), [rt]);
  const batches = useLoad(() => cycleApi.batches(rt ? { report_type: rt } : {}), [rt]);
  const byBatch = new Map((batches.data?.items ?? []).map((b) => [b.id, b]));
  const rows = (periods.data?.items ?? []).filter((p) => p.status === "closed").sort((a, b) => ((a.closed_at ?? "") < (b.closed_at ?? "") ? 1 : -1));
  const columns: Column<RcPeriod>[] = [
    { key: "batch", header: "Đợt", cell: (p) => byBatch.get(p.batch_id)?.season_label ?? "—" },
    { key: "forms", header: "Biểu", cell: (p) => byBatch.get(p.batch_id)?.forms.map((f) => f.code).join(", ") ?? "—" },
    { key: "period", header: "Kỳ", cell: (p) => `${p.label} (${p.range_label})` },
    { key: "at", header: "Chốt lúc", cell: (p) => dt(p.closed_at) },
    { key: "by", header: "Người chốt", cell: (p) => <ClosedBy p={p} /> },
    { key: "mode", header: "Cách chốt", cell: (p) => (p.close_mode === "auto" ? "Tự động sau hạn" : "Thủ công") },
    { key: "st", header: "Trạng thái", cell: () => <StatusPill tone="neutral">Đã chốt</StatusPill> },
    {
      key: "act",
      header: "Thao tác",
      cell: (p) => (
        <Link className="btn btn-o btn-sm" href={`/kn/nhiem-vu/trang-thai-nop?dot=${p.batch_id}`} aria-label={`Trạng thái nộp ${p.label}`}>
          Trạng thái nộp
        </Link>
      ),
    },
  ];
  return (
    <>
      <FilterBar onReset={() => (setRt(""), setPage(1))}>
        <FilterChip
          label="Loại báo cáo"
          value={rt}
          onChange={(v) => (setRt(v), setPage(1))}
          options={[
            { value: "", label: "Tất cả" },
            { value: "weekly", label: "Tuần" },
            { value: "monthly", label: "Tháng" },
            { value: "seasonal", label: "Vụ" },
            { value: "adhoc", label: "Đột xuất" },
          ]}
        />
      </FilterBar>
      <LoadState s={periods}>
        {() => <DataTable caption="Kỳ đã chốt" columns={columns} rows={rows.slice((page - 1) * 20, page * 20)} rowKey={(p) => p.id} page={page} pageSize={20} total={rows.length} onPage={setPage} empty="Chưa có kỳ nào đã chốt." />}
      </LoadState>
    </>
  );
}

function ReviewTasks({ base }: { base: string }) {
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
        <FilterChip label="Biểu" value={form} onChange={setForm} options={[{ value: "", label: "Tất cả" }, ...TT_FORM_CODES.map((x) => ({ value: x, label: x }))]} />
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
  const [reasonErr, setReasonErr] = useState<string | undefined>();
  const [reopen, setReopen] = useState(false);
  const [reopenErr, setReopenErr] = useState<Partial<Record<"reason" | "until", string>>>({});
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
                  <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !sub} onClick={() => {
                      const err = reason.trim() ? undefined : "Bắt buộc nhập.";
                      setReasonErr(err);
                      if (!err) void act.run(() => cycleApi.requestAdjustment(id, reason.trim()), "Đã gửi yêu cầu điều chỉnh tới Cán bộ xã.");
                    }}
                  >
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
                      <div style={{ marginTop: 10 }}>
                        <TextField label="Lý do yêu cầu điều chỉnh" required multiline value={reason} onChange={setReason} error={reasonErr} hint={<PiiHint text={reason} />} />
                      </div>
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
                        const v = validateForm({ reason: reopenReason, until: reopenUntil }, { reason: { kind: "text", required: true }, until: { kind: "text", required: true } });
                        setReopenErr(v.errors);
                        if (!v.valid) return;
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
                  <TextField label="Lý do mở lại" required multiline value={reopenReason} onChange={setReopenReason} error={reopenErr.reason} hint={<PiiHint text={reopenReason} />} />
                  <DateInput label="Mở đến ngày (17:00)" value={reopenUntil} onChange={setReopenUntil} error={reopenErr.until} />
                </Dialog>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}
