"use client";

// Cấp Xã (BRD §4.4): Công việc cần làm FN-XA-03.01 (trang mặc định), Nhập báo cáo FN-XA-03.02 (FormEntry, mỗi xã một
// tab, lưu nháp/gửi, lịch sử phiên bản), Báo cáo của xã FN-XA-03.07 (danh sách + thống kê), Báo cáo tuần/tháng/vụ.
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ActionBar, BackLink, LogPanel } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { appEvaluator, formLayout } from "@/kn/forms/api";
import type { FormDefinition } from "@/kn/forms/types";
import { FormEntry, communeRows, type GridCommune, type GridValues } from "@/kn/grid";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import type { RcTask, RcTaskDetail } from "../api";
import { LoadState, TASK_STATUS_LABEL, TaskPill, ddmm, dt, num, taskAction, toLog, useAction, useLoad } from "../common";

const PAGE = 10;
const TODO = new Set(["not_started", "draft", "adjustment_requested", "overdue"]);

function communeNameOf(ctxCommunes: { code: string; name: string }[], code: string) {
  return ctxCommunes.find((c) => c.code === code)?.name ?? code;
}

/** FN-XA-03.01 Công việc cần làm (Hình 13). */
export function TasksScreen({ periodType, title = "Công việc cần làm" }: { periodType?: "week" | "month" | "season" | "adhoc"; title?: string }) {
  const { cycleApi, kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const defaults = { commune: "", kind: periodType ?? "", status: periodType ? "" : "todo", form: "" };
  const [f, setF] = useState(defaults);
  const [page, setPage] = useState(1);
  const s = useLoad(() => cycleApi.tasks({ commune: f.commune || undefined, period_type: f.kind || undefined, form_code: f.form || undefined }), [f.commune, f.kind, f.form, kn.context?.context_id]);
  const set = (p: Partial<typeof defaults>) => {
    setF({ ...f, ...p });
    setPage(1);
  };
  return (
    <>
      <PageHeader title={title} />
      <FilterBar onReset={() => set(defaults)}>
        <FilterChip label="Xã/phường" value={f.commune} onChange={(commune) => set({ commune })} options={[{ value: "", label: `Tất cả xã phụ trách (${communes.length})` }, ...communes.map((c) => ({ value: c.code, label: c.name }))]} />
        {periodType ? null : (
          <FilterChip
            label="Loại kỳ"
            value={f.kind}
            onChange={(kind) => set({ kind })}
            options={[
              { value: "", label: "Tất cả" },
              { value: "week", label: "Tuần" },
              { value: "month", label: "Tháng" },
              { value: "season", label: "Vụ" },
            ]}
          />
        )}
        <FilterChip
          label="Trạng thái"
          value={f.status}
          defaultValue={defaults.status}
          onChange={(status) => set({ status })}
          options={[{ value: "todo", label: "Cần làm" }, { value: "", label: "Tất cả" }, ...Object.entries(TASK_STATUS_LABEL).map(([value, label]) => ({ value, label }))]}
        />
        <FilterChip label="Biểu" value={f.form} onChange={(form) => set({ form })} options={[{ value: "", label: "Tất cả" }, ...["T01", "T03", "T04", "T05", "T06", "T07", "M01"].map((x) => ({ value: x, label: x }))]} />
      </FilterBar>
      <LoadState s={s}>
        {(d) => {
          const rows = d.items.filter((t) => (f.status === "" ? true : f.status === "todo" ? TODO.has(t.status) && t.open : t.status === f.status));
          const columns: Column<RcTask>[] = [
            { key: "commune", header: "Xã/phường", cell: (t) => communeNameOf(communes, t.commune_code) },
            {
              key: "form",
              header: "Báo cáo",
              cell: (t) => (
                <>
                  <b>{t.form_code}</b>
                  {t.adjustment_reason ? <div style={{ color: "var(--danger)", fontSize: 12 }}>Yêu cầu điều chỉnh: {t.adjustment_reason}</div> : null}
                </>
              ),
            },
            { key: "period", header: "Kỳ", cell: (t) => `${t.period_label} (${t.period_range})` },
            { key: "due", header: "Hạn nộp", cell: (t) => dt(t.due_at) },
            { key: "status", header: "Trạng thái", cell: (t) => <TaskPill status={t.status} label={t.status_label} /> },
            { key: "updated", header: "Cập nhật", cell: (t) => (t.latest_version ? ddmm(t.updated_at) : "—") },
            {
              key: "act",
              header: "Thao tác",
              cell: (t) => {
                const a = taskAction(t);
                return (
                  <Link className={`btn btn-sm ${a === "Nhập" ? "btn-p" : "btn-o"}`} href={`/kn/nhiem-vu/cong-viec/${t.id}`} aria-label={`${a}: ${t.form_code} — ${communeNameOf(communes, t.commune_code)} — ${t.period_label}`}>
                    {a}
                  </Link>
                );
              },
            },
          ];
          return (
            <>
              <div className="row" style={{ gap: 8, marginBottom: 12 }} aria-label="Số đếm công việc">
                <StatusPill tone="info">Cần làm {d.counts.todo}</StatusPill>
                <StatusPill tone="bad">Trễ hạn {d.counts.overdue}</StatusPill>
                <StatusPill tone="warn">Yêu cầu điều chỉnh {d.counts.adjustment_requested}</StatusPill>
              </div>
              {d.last_reminder ? (
                <div className="alert alert-info" role="status" style={{ marginBottom: 14 }}>
                  <Icon name="bell" />
                  <span>
                    Nhắc nhở từ Cấp Tỉnh ({ddmm(d.last_reminder.at)}): &quot;{d.last_reminder.message}&quot;
                  </span>
                </div>
              ) : null}
              <DataTable
                caption="Công việc nộp báo cáo"
                columns={columns}
                rows={rows.slice((page - 1) * PAGE, page * PAGE)}
                rowKey={(t) => t.id}
                page={page}
                pageSize={PAGE}
                total={rows.length}
                onPage={setPage}
                empty="Chưa có công việc."
              />
            </>
          );
        }}
      </LoadState>
    </>
  );
}

interface EntryData {
  detail: RcTaskDetail;
  siblings: RcTaskDetail[];
  def: FormDefinition;
}

/** FN-XA-03.02 Nhập báo cáo theo biểu mẫu (Hình 14/15): mỗi xã phụ trách một tab, chỉ khe kỳ đang mở nhập được. */
export function EntryScreen() {
  const { id } = useParams<{ id: string }>();
  const { cycleApi, api, kn } = useAuth();
  const router = useRouter();
  const communesCtx = kn.context?.communes ?? [];
  const s = useLoad<EntryData>(async () => {
    const detail = await cycleApi.task(id);
    const list = await cycleApi.tasks({ batch_id: detail.task.batch_id, period_id: detail.task.period_id, form_code: detail.task.form_code });
    const siblings = await Promise.all(list.items.filter((t) => t.id !== detail.task.id).map((t) => cycleApi.task(t.id)));
    const form = detail.batch.forms.find((x) => x.template_id === detail.task.template_id)!;
    const lay = await formLayout(api, form.template_id, form.version, { period_label: detail.layout.period_labels, season: detail.layout.season });
    const all = [detail, ...siblings].sort((a, b) => a.task.commune_code.localeCompare(b.task.commune_code));
    return { detail, siblings: all, def: lay.definition as FormDefinition };
  }, [id]);
  const [values, setValues] = useState<GridValues | null>(null);
  const act = useAction();
  const [issues, setIssues] = useState<string[]>([]);
  useBreadcrumbTail(s.data ? [{ label: `${s.data.detail.task.form_code} · ${s.data.detail.period.label}` }] : []);

  const init = useMemo(() => {
    if (!s.data) return null;
    const v: GridValues = {};
    const system: GridValues = {};
    for (const d of s.data.siblings) {
      const c = d.task.commune_code;
      const latest = d.latest;
      for (const r of latest?.rows ?? []) v[r.key] = { ...r.input };
      for (const [k, cells] of Object.entries(d.carried)) v[k] = { ...cells, ...(v[k] ?? {}) };
      system[c] = { ...d.provincial_values };
    }
    return { values: v, system };
  }, [s.data]);
  const cur = values ?? init?.values ?? {};

  return (
    <>
      <BackLink href="/kn/nhiem-vu/cong-viec">Công việc cần làm</BackLink>
      <LoadState s={s}>
        {(d) => {
          const t = d.detail;
          const gridCommunes: GridCommune[] = d.siblings.map((x) => ({ code: x.task.commune_code, name: communeNameOf(communesCtx, x.task.commune_code), region: x.task.region_code }));
          const open = d.siblings.filter((x) => x.task.open);
          const tabStatus = Object.fromEntries(d.siblings.map((x) => [x.task.commune_code, x.task.status_label]));
          const editable = (letter: string) => {
            const col = d.def.columns.find((c) => c.letter === letter);
            return !!col && (col.kind === "input" || (col.kind === "per_period" && t.layout.open_slots.includes(col.period_slot ?? 0)));
          };
          const save = async (kind: "draft" | "submitted") => {
            setIssues([]);
            const targets = open.filter((x) => {
              if (kind === "draft") return true;
              const changed = communeRows(d.def, { code: x.task.commune_code, name: "" }).some((r) => JSON.stringify(cur[r.key] ?? {}) !== JSON.stringify(Object.fromEntries(Object.entries(x.latest_submitted?.rows.find((y) => y.key === r.key)?.input ?? {}))));
              return changed || (x.task.stored_status !== "submitted" && x.task.stored_status !== "confirmed");
            });
            const done: string[] = [];
            for (const x of targets) {
              const rows = communeRows(d.def, { code: x.task.commune_code, name: "" }).map((r) => ({
                ...(r.indicator ? { indicator: r.indicator } : {}),
                input: Object.fromEntries(Object.entries(cur[r.key] ?? {}).filter(([l, v]) => v !== "" && editable(l))),
              }));
              try {
                await cycleApi.submit(x.task.id, { kind, rows });
              } catch (e) {
                const msg = e instanceof Error ? e.message : String(e);
                setIssues((xs) => [...xs, `${communeNameOf(communesCtx, x.task.commune_code)}: không ${kind === "draft" ? "lưu" : "gửi"} được — ${msg}`]);
                if (done.length) s.reload();
                return;
              }
              done.push(communeNameOf(communesCtx, x.task.commune_code));
            }
            act.setNote(done.length ? `${kind === "draft" ? "Đã lưu nháp" : "Đã gửi báo cáo"}: ${done.join(", ")}.` : "Không có thay đổi để gửi.");
            s.reload();
            setValues(null);
          };
          return (
            <>
              <PageHeader title={`${t.task.form_code} · ${t.form_name}`} badge={<TaskPill status={t.task.status} label={t.task.status_label} />}>
                {t.batch.season_label} · {t.period.label} ({t.period.range_label}) · Hạn nộp {dt(t.period.due_at)} · Sheet mẫu “{d.def.sheet_name}”
              </PageHeader>
              <ActionBar>
                <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !open.length} onClick={() => void save("draft")}>
                  Lưu nháp
                </button>
                <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !open.length} onClick={() => void save("submitted")}>
                  <Icon name="send" size={15} /> Gửi báo cáo
                </button>
                <button type="button" className="btn btn-o btn-sm" onClick={() => router.push("/kn/bao-cao/cua-xa")}>
                  Lịch sử / Báo cáo của xã
                </button>
              </ActionBar>
              {!open.length ? (
                <div className="alert alert-warn" role="status">
                  <Icon name="lock" />
                  <span>Kỳ đã chốt — chỉ xem. Cần sửa thì đề nghị Cấp Tỉnh mở lại kỳ.</span>
                </div>
              ) : null}
              {d.siblings.some((x) => x.task.adjustment_reason) ? (
                <div className="alert alert-warn" role="status">
                  <Icon name="alert" />
                  <span>
                    Yêu cầu điều chỉnh:{" "}
                    {d.siblings
                      .filter((x) => x.task.adjustment_reason)
                      .map((x) => `${communeNameOf(communesCtx, x.task.commune_code)} — ${x.task.adjustment_reason}`)
                      .join("; ")}
                  </span>
                </div>
              ) : null}
              <p className="mute small" style={{ margin: "6px 0 10px" }}>
                Web cần kết nối mạng; nhập ngoại tuyến (tự đồng bộ) dùng ứng dụng di động Cán bộ xã. Ô viền xanh đậm là kỳ đang nhập, ô xám nhạt là kỳ khác chỉ xem.
              </p>
              {act.view}
              {issues.length ? (
                <div className="alert alert-bad" role="alert">
                  <Icon name="alert" />
                  <span>{issues.join(" ")}</span>
                </div>
              ) : null}
              {init ? (
                <FormEntry
                  definition={d.def}
                  versionKey={t.task.version_id}
                  communes={gridCommunes}
                  values={cur}
                  onValuesChange={(next) => setValues(next)}
                  evaluate={appEvaluator(api, t.task.version_id)}
                  openSlots={t.layout.open_slots}
                  system={init.system}
                  readOnly={!open.length}
                  tabStatus={tabStatus}
                  ariaLabel={`Nhập ${t.task.form_code}`}
                />
              ) : null}
              <section className="card sec" style={{ marginTop: 16 }} aria-label="Lịch sử sửa">
                <h2>Lịch sử sửa (phiên bản bất biến)</h2>
                <ul className="list-plain">
                  {d.siblings.flatMap((x) =>
                    x.latest
                      ? [
                          <li key={x.task.id}>
                            {communeNameOf(communesCtx, x.task.commune_code)}: phiên bản {x.task.latest_version} — {x.latest.kind === "submitted" ? "Đã gửi" : "Lưu nháp"} lúc {dt(x.latest.recorded_at)}
                            {x.latest.on_behalf_of ? " (Admin nhập thay)" : ""}
                          </li>,
                        ]
                      : [],
                  )}
                </ul>
              </section>
            </>
          );
        }}
      </LoadState>
    </>
  );
}

/** FN-XA-03.07 Báo cáo của xã: tab xã, danh sách báo cáo đã gửi + thống kê của xã. */
export function CommuneReportsScreen() {
  const { cycleApi, kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const [commune, setCommune] = useState(communes[0]?.code ?? "");
  const [page, setPage] = useState(1);
  const list = useLoad(() => cycleApi.communeReports({ commune: commune || undefined }), [commune]);
  const stats = useLoad(commune ? () => cycleApi.communeStats(commune) : null, [commune]);
  const columns: Column<RcTask>[] = [
    { key: "form", header: "Biểu", cell: (t) => <b>{t.form_code}</b> },
    { key: "period", header: "Kỳ", cell: (t) => `${t.period_label} (${t.period_range})` },
    { key: "status", header: "Trạng thái", cell: (t) => <TaskPill status={t.status} label={t.status_label} /> },
    { key: "sent", header: "Gửi lúc", cell: (t) => dt(t.submitted_at) },
    { key: "closed", header: "Chốt kỳ", cell: (t) => (t.open ? "Chưa chốt" : "Đã chốt") },
    {
      key: "act",
      header: "Thao tác",
      cell: (t) => (
        <Link className="btn btn-o btn-sm" href={`/kn/nhiem-vu/cong-viec/${t.id}`}>
          {t.open && t.stored_status !== "confirmed" ? "Sửa" : "Xem"}
        </Link>
      ),
    },
  ];
  return (
    <>
      <PageHeader title="Báo cáo của xã" />
      <div className="dtabs" role="tablist" aria-label="Xã phụ trách">
        {communes.map((c) => (
          <button key={c.code} type="button" role="tab" aria-selected={c.code === commune} onClick={() => (setCommune(c.code), setPage(1))}>
            {c.name}
          </button>
        ))}
      </div>
      <LoadState s={stats}>
        {(st) => (
          <section className="hot-grid" aria-label="Thống kê của xã" style={{ margin: "12px 0" }}>
            <div className="card hot">
              <div className="lb">DT xuống giống / KH (ha)</div>
              <div className="vl">
                {num(st.sown_area.value)} / {num(st.sown_plan.value)}
              </div>
              <div className="dl neutral">{st.sown_pct.value ? `${num(st.sown_pct.value)} % kế hoạch` : "Chưa có số"}</div>
            </div>
            <div className="card hot">
              <div className="lb">DT nhiễm sâu bệnh (ha)</div>
              <div className="vl">{num(st.pest_area.value)}</div>
              <div className="dl neutral">Theo T05 (chờ mẫu Q-01)</div>
            </div>
            <div className="card hot">
              <div className="lb">Báo cáo nộp đúng hạn</div>
              <div className="vl">
                {st.tasks_on_time} / {st.tasks_due}
              </div>
              <div className="dl neutral">
                Đã gửi {st.tasks_submitted}/{st.tasks_total} công việc · {st.batch?.season_label ?? ""}
              </div>
            </div>
          </section>
        )}
      </LoadState>
      <LoadState s={list}>
        {(d) => {
          const rows = d.items.filter((t) => t.latest_version > 0);
          return <DataTable caption="Báo cáo của xã" columns={columns} rows={rows.slice((page - 1) * PAGE, page * PAGE)} rowKey={(t) => t.id} page={page} pageSize={PAGE} total={rows.length} onPage={setPage} empty="Xã chưa gửi báo cáo nào." />;
        }}
      </LoadState>
    </>
  );
}

/** Lịch sử một công việc (khung Log) — dùng ở màn Rà soát. */
export function TaskLog({ taskId }: { taskId: string }) {
  const { cycleApi } = useAuth();
  const s = useLoad(() => cycleApi.taskLog(taskId), [taskId]);
  return s.data ? <LogPanel entries={toLog(s.data.items)} /> : <LogPanel entries={[]} empty="Đang tải…" />;
}
