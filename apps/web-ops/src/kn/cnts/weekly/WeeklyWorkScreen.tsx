"use client";

// Báo cáo công việc tuần — CNTS Cấp Xã (BF-CN-WEEKLY, PRD UP-06; BRD CNTS UR-XA-09, BM-05):
//  - Phần I, Kết quả tuần trước: hạn 08:00 thứ Hai; "Gợi ý điền" từ KSGM, Nhập liệu tuần và Lịch trực; bảng T2–CN, mỗi ô ≤ 500 ký tự.
//  - Phần II, Kế hoạch tuần sau: đăng ký thứ Năm–thứ Sáu, hạn 17:00 thứ Sáu; sửa sau hạn phải ghi lý do; sao chép tuần trước;
//    Địa điểm bắt buộc khi đi công tác. Kế hoạch đã đăng ký thành Lịch công tác chờ tỉnh duyệt
//    (POST /v1/extension/cnts/weekly-plan/schedule).
//  - Cột phải: thông tin người báo cáo và Log (lịch sử phiên bản bài nộp).
// Nộp qua /v1/forms (profile kn_cnts) — máy chủ kiểm lại mọi quy tắc (reportcycle/domain.ValidateCNTS, lý do trễ).
import Link from "next/link";
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Tabs } from "@/components/Tabs";
import { TextField } from "@/components/form";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LogPanel, type LogEntry } from "@/components/patterns/detail";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { STATUS_LABEL, STATUS_TONE, createCntsPeriodApi, type FormsSubmission, type FormsTask } from "../periods/api";
import {
  INTAKE,
  KSGM,
  MAX_CELL,
  PART_I,
  PART_II,
  TEXT_COLS,
  WORK_OTHER,
  WORK_TYPES,
  applySuggestions,
  buildSuggestions,
  codeOf,
  ddmm,
  pastDue,
  pickTask,
  validateWeekly,
  weekDays,
  weeklyPayload,
  type MyDuty,
  type Part,
} from "./weekly";

const SCHEDULE_ROUTE = "/kn/nhiem-vu/lich-cong-tac";
const today = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};

export function WeeklyWorkScreen() {
  const { api, kn, principal } = useAuth();
  const capi = useMemo(() => (api ? createCntsPeriodApi(api) : null), [api]);
  const [part, setPart] = useState<Part>("I");
  const tasks = useLoad(capi ? async () => [...(await capi.tasks(PART_I)), ...(await capi.tasks(PART_II))] : null, [capi]);
  const [picked, setPicked] = useState<Record<Part, string>>({ I: "", II: "" });
  const communeName = useCommuneNames((tasks.data ?? []).map((t) => t.unit));
  const ctx = kn.context;
  const all = tasks.data ?? [];
  const mine = all.filter((t) => t.template_code === codeOf(part)).sort((a, b) => ((a.period_from ?? "") < (b.period_from ?? "") ? 1 : -1));
  const cur = mine.find((t) => t.id === picked[part]) ?? pickTask(all, part, today());

  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader title="Báo cáo công việc tuần">
        <p className="mute">BM-05 — cán bộ tăng cường: Phần I kết quả tuần trước, Phần II kế hoạch tuần sau (UR-XA-09).</p>
      </PageHeader>
      <div className="detail-grid">
        <div className="card detail-main">
          <Tabs
            label="Phần báo cáo"
            value={part}
            onChange={(v) => setPart(v as Part)}
            tabs={[
              { id: "I", label: "Phần I · Kết quả tuần trước" },
              { id: "II", label: "Phần II · Kế hoạch tuần sau" },
            ]}
          >
            <LoadState s={tasks}>
              {() =>
                !cur ? (
                  <p className="mute">Chưa có kỳ {part === "I" ? "Phần I" : "Phần II"} được phát hành cho xã phụ trách.</p>
                ) : (
                  <>
                    <label className="row" style={{ gap: 8, margin: "12px 0" }}>
                      <span>Kỳ</span>
                      <select className="select" aria-label="Chọn kỳ" value={cur.id} onChange={(e) => setPicked((p) => ({ ...p, [part]: e.target.value }))}>
                        {mine.map((t) => (
                          <option key={t.id} value={t.id}>
                            {t.label} · {communeName(t.unit)}
                          </option>
                        ))}
                      </select>
                    </label>
                    <WeeklyForm key={`${cur.id}:${cur.version}`} part={part} task={cur} unitLabel={communeName(cur.unit)} capi={capi} onSaved={() => tasks.reload()} />
                  </>
                )
              }
            </LoadState>
          </Tabs>
        </div>
        <div>
          <aside className="card log" aria-label="Người báo cáo">
            <h2>Người báo cáo</h2>
            <dl className="lv-grid">
              <dt>Họ tên</dt>
              <dd>{principal?.displayName || "—"}</dd>
              <dt>Phòng ban</dt>
              <dd>{ctx?.department_name ?? "Chăn nuôi – Thủy sản"}</dd>
              <dt>Đơn vị</dt>
              <dd>{ctx?.org_name || "—"}</dd>
              <dt>Xã phụ trách</dt>
              <dd>{ctx?.communes?.length ? ctx.communes.map((c) => c.name).join(", ") : "—"}</dd>
            </dl>
            <p className="mute" style={{ marginTop: 8 }}>
              Hạn: Phần I 08:00 thứ Hai; Phần II đăng ký thứ Năm–thứ Sáu, hạn 17:00 thứ Sáu.
            </p>
          </aside>
          {cur ? <SubmissionLog key={`${cur.id}:${cur.version}`} capi={capi} task={cur} me={principal?.subject ?? ""} myName={principal?.displayName ?? ""} /> : null}
        </div>
      </div>
    </div>
  );
}

function SubmissionLog({ capi, task, me, myName }: { capi: ReturnType<typeof createCntsPeriodApi>; task: FormsTask; me: string; myName: string }) {
  const subs = useLoad(task.version > 0 ? () => capi.submissions(task.period_id, task.unit) : null, [capi, task.period_id, task.unit, task.version]);
  const entries: LogEntry[] = (subs.data ?? []).map((s: FormsSubmission) => ({
    event: `${s.version === 1 ? "Gửi" : "Sửa, gửi lại"} — phiên bản ${s.version}${s.late ? " (sau hạn)" : ""}`,
    actor_display: s.submitter_ref === me && myName ? myName : "Cán bộ xã",
    at: s.event_time,
    reason: s.explanation,
  }));
  return <LogPanel entries={entries} empty="Chưa gửi lần nào." />;
}

function WeeklyForm({
  part,
  task,
  unitLabel,
  capi,
  onSaved,
}: {
  part: Part;
  task: FormsTask;
  unitLabel: string;
  capi: ReturnType<typeof createCntsPeriodApi>;
  onSaved: () => void;
}) {
  const { api } = useAuth();
  const [values, setValues] = useState<Record<string, string>>({ ...task.values });
  const [reason, setReason] = useState("");
  const [note, setNote] = useState(task.note);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [scheduleFailed, setScheduleFailed] = useState(false);
  const act = useAction();
  const days = weekDays(task.period_from!);
  const late = pastDue(task);
  const readOnly = !task.open;
  const set = (k: string, v: string) => setValues((x) => ({ ...x, [k]: v }));

  const makeSchedule = async () => {
    const r = await act.run(
      () =>
        unwrap(
          api.POST("/v1/extension/cnts/weekly-plan/schedule", {
            params: { header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { period_id: task.period_id, unit: task.unit },
          }),
        ),
      "Đã tạo Lịch công tác tuần từ kế hoạch — chờ Cấp Tỉnh duyệt.",
    );
    setScheduleFailed(r === null);
  };

  const submit = async () => {
    const fe = validateWeekly(part, values, late, reason);
    setErrs(fe);
    if (Object.keys(fe).length) return;
    try {
      await act.idem(() =>
        capi.submit(task.period_id, {
          unit: task.unit,
          values: weeklyPayload(part, values),
          base_version: task.version,
          nil_report: false,
          note: note.trim() || undefined,
          explanation: reason.trim() || undefined,
        }),
      );
    } catch (e) {
      if (e instanceof ApiProblem) {
        const m: Record<string, string> = {};
        for (const x of e.problem.errors ?? []) if (x.field) m[x.field] = x.reason ?? "";
        setErrs(m);
      }
      await act.run(() => Promise.reject(e));
      return;
    }
    if (part === "II") {
      // Kế hoạch đã đăng ký → Lịch công tác chờ duyệt (UP-06). Lỗi ở bước này không làm mất bài đã đăng ký: cho bấm lại.
      try {
        await unwrap(
          api.POST("/v1/extension/cnts/weekly-plan/schedule", {
            params: { header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { period_id: task.period_id, unit: task.unit },
          }),
        );
        act.setNote("Đã đăng ký kế hoạch tuần và gửi Lịch công tác chờ Cấp Tỉnh duyệt.");
      } catch {
        setScheduleFailed(true);
        act.setNote("Đã đăng ký kế hoạch tuần, nhưng chưa tạo được Lịch công tác — bấm \"Tạo lại Lịch công tác\".");
      }
    } else act.setNote("Đã gửi kết quả công việc tuần.");
    onSaved();
  };

  const suggest = async () => {
    const r = await act.run(async () => {
      const months = [...new Set([days[0]!.date.slice(0, 7), days[6]!.date.slice(0, 7)])];
      const [ksgm, intake, facilities, ...duty] = await Promise.all([
        capi.tasks(KSGM),
        capi.tasks(INTAKE),
        unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_slaughter_facility" }, query: {} } })).then(
          (x) => x.items as CatalogItem[],
          () => [] as CatalogItem[],
        ),
        ...months.map((month) =>
          unwrap(api.GET("/v1/extension/cnts/my-duties", { params: { query: { month } } })).then(
            (x) => x.items as MyDuty[],
            () => [] as MyDuty[],
          ),
        ),
      ]);
      const fname = (t: FormsTask) => facilities.find((f) => f.attrs.facility_ref === t.unit)?.name ?? t.unit;
      return buildSuggestions(days, task.unit, ksgm, intake, duty.flat(), fname);
    });
    if (!r) return;
    const { values: next, filled } = applySuggestions(values, r);
    setValues(next);
    act.setNote(filled ? `Đã gợi ý ${filled} ngày từ KSGM, Nhập liệu tuần và Lịch trực — kiểm tra lại trước khi gửi.` : "Không có dữ liệu KSGM, Nhập liệu tuần hay Lịch trực để gợi ý cho các ô còn trống.");
  };

  const copyPrevious = async () => {
    const p = await act.run(() => capi.previous(task.period_id, task.unit));
    if (p) {
      setValues({ ...p.submission.values });
      act.setNote(`Đã sao chép kế hoạch ${p.label} — sửa lại cho tuần này trước khi đăng ký.`);
    } else if (p === null && !act.problem) act.setNote("Chưa có kế hoạch tuần trước để sao chép.");
  };

  const cols = TEXT_COLS[part];
  return (
    <section aria-label={part === "I" ? "Phần I — Kết quả tuần trước" : "Phần II — Kế hoạch tuần sau"}>
      <p className="mute">
        {unitLabel} · {task.label} · hạn {dt(task.due_at)} · <StatusPill tone={STATUS_TONE[task.status]}>{STATUS_LABEL[task.status]}</StatusPill>
        {task.version > 0 ? ` · phiên bản ${task.version}, gửi lúc ${dt(task.submitted_at)}` : ""}
      </p>
      {part === "II" ? (
        <p className="mute">Đăng ký thứ Năm–thứ Sáu, hạn 17:00 thứ Sáu. Kế hoạch đã đăng ký thành Lịch công tác chờ tỉnh duyệt. Sửa sau hạn phải ghi lý do.</p>
      ) : (
        <p className="mute">Nộp trước 08:00 thứ Hai. Gửi sau hạn phải ghi lý do.</p>
      )}
      {act.view}
      {readOnly ? <p className="err">Kỳ đã chốt hoặc không còn nhận bài — chỉ xem.</p> : null}
      {!readOnly ? (
        <div className="row" style={{ gap: 8, margin: "8px 0" }}>
          {part === "I" ? (
            <button className="btn btn-sm" disabled={act.busy} onClick={suggest}>
              Gợi ý điền (KSGM, Nhập liệu, Lịch trực)
            </button>
          ) : (
            <button className="btn btn-sm" disabled={act.busy} onClick={copyPrevious}>
              Sao chép tuần trước
            </button>
          )}
          {part === "II" && (scheduleFailed || task.version > 0) ? (
            <button className="btn btn-sm" disabled={act.busy || task.version === 0} onClick={makeSchedule}>
              Tạo lại Lịch công tác
            </button>
          ) : null}
          {part === "II" ? (
            <Link className="btn btn-sm" href={SCHEDULE_ROUTE}>
              Xem Lịch công tác
            </Link>
          ) : null}
        </div>
      ) : null}
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Bảng công việc theo ngày">
          <thead>
            <tr>
              <th>Ngày</th>
              <th>Loại công việc</th>
              <th>Công tác khác (ghi rõ)</th>
              {cols.map((c) => (
                <th key={c.suffix}>{c.label}</th>
              ))}
              {part === "II" ? <th>Địa điểm công tác</th> : null}
            </tr>
          </thead>
          <tbody>
            {days.map((d) => {
              const loai = values[`${d.key}_loai`] ?? "";
              const cell = (suffix: string, label: string, required: boolean) => {
                const k = `${d.key}_${suffix}`;
                return (
                  <td key={suffix}>
                    <textarea
                      className="textarea"
                      aria-label={`${d.label} — ${label}`}
                      aria-required={required || undefined}
                      aria-invalid={errs[k] ? true : undefined}
                      maxLength={MAX_CELL}
                      rows={2}
                      disabled={readOnly}
                      value={values[k] ?? ""}
                      onChange={(e) => set(k, e.target.value)}
                    />
                    {errs[k] ? (
                      <span className="err" role="alert">
                        {errs[k]}
                      </span>
                    ) : (
                      <span className="mute hint">
                        {(values[k] ?? "").length}/{MAX_CELL}
                      </span>
                    )}
                  </td>
                );
              };
              const need = d.required || loai !== "";
              return (
                <tr key={d.key}>
                  <th scope="row">
                    {d.label}
                    {d.required ? " *" : ""}
                    <div className="mute">{ddmm(d.date)}</div>
                  </th>
                  <td>
                    <select
                      className="select"
                      aria-label={`${d.label} — Loại công việc`}
                      aria-invalid={errs[`${d.key}_loai`] ? true : undefined}
                      disabled={readOnly}
                      value={loai}
                      onChange={(e) => set(`${d.key}_loai`, e.target.value)}
                    >
                      <option value="">{d.required ? "Chọn…" : "Không làm việc"}</option>
                      {WORK_TYPES.map((w) => (
                        <option key={w} value={w}>
                          {w}
                        </option>
                      ))}
                    </select>
                    {errs[`${d.key}_loai`] ? (
                      <span className="err" role="alert">
                        {errs[`${d.key}_loai`]}
                      </span>
                    ) : null}
                  </td>
                  {cell("cong_tac_khac", "Công tác khác (ghi rõ)", loai === WORK_OTHER)}
                  {cols.map((c) => cell(c.suffix, c.label, need))}
                  {part === "II" ? cell("dia_diem", "Địa điểm công tác", loai === WORK_OTHER) : null}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <TextField label="Ghi chú chung" multiline maxLength={1000} disabled={readOnly} value={note} onChange={setNote} />
      <TextField
        label={part === "II" && task.version > 0 ? "Lý do sửa kế hoạch / gửi sau hạn" : "Lý do gửi sau hạn"}
        required={late}
        multiline
        maxLength={1000}
        disabled={readOnly}
        value={reason}
        onChange={setReason}
        error={errs.explanation}
        hint={late ? "Đã quá hạn nộp — bắt buộc ghi lý do." : undefined}
      />
      {!readOnly ? (
        <div className="row" style={{ gap: 8 }}>
          <button className="btn btn-primary" disabled={act.busy} onClick={submit}>
            {part === "II" ? (task.version > 0 ? "Đăng ký lại kế hoạch" : "Đăng ký kế hoạch") : "Gửi báo cáo"}
          </button>
        </div>
      ) : null}
    </section>
  );
}
