"use client";

// FN-11 Kế hoạch nhân lực năm (BRD HCTH v3.0; W11.27, dom/hrm — kỳ native cap/forms profile hrm_plan). Bố cục theo
// prototype HCTH (Khuyen_nong_so_HCTH.html, SC.kh / SC['kh.d']):
//   - Phòng HCTH/Admin: bảng tổng hợp đơn vị × chỉ tiêu (Hiện có, Dự kiến nghỉ hưu, Dự kiến nghỉ/chuyển khác, Cần bổ sung,
//     Nhu cầu theo kế hoạch, Trạng thái) + dòng "Toàn Trung tâm"; mở kỳ năm, Xuất Excel, Chốt kế hoạch; bấm đơn vị xem chi tiết.
//   - Chi tiết đơn vị: "Số liệu hiện có (tự động)", "Kế hoạch của phòng" (nhập), "Nhu cầu theo vị trí" (thêm/xóa dòng),
//     Lưu nháp / Gửi Phòng HCTH / Sao chép năm trước; HCTH: Xác nhận, Yêu cầu điều chỉnh, Điều chỉnh sau chốt (có lý do);
//     khung lịch sử bên phải.
//   - Lãnh đạo đơn vị (menu "Kế hoạch của phòng"): mở thẳng chi tiết đơn vị mình ở kỳ mới nhất.
// Quyền do máy chủ quyết định (can_edit, can_review, can_adjust, can_manage).
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { BackLink, DetailLayout, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { PageHeader, StatusPill, type PillTone } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import {
  createPlanApi,
  PERIOD_STATUS_LABEL,
  PLAN_STATUS_LABEL,
  planDemand,
  type HrmPlanInput,
  type HrmPlanPosition,
  type HrmPlanStatus,
  type HrmPlanSummary,
  type HrmPlanUnit,
  type PlanApi,
} from "./plan-api";
import { vnDate } from "./staff-api";

const BASE = "/kn/nhan-su/ke-hoach";

function usePlanApi(): PlanApi {
  const { api } = useAuth();
  return useMemo(() => createPlanApi(api), [api]);
}

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

const STATUS_TONE: Record<HrmPlanStatus, PillTone> = {
  not_started: "neutral",
  draft: "info",
  submitted: "info",
  returned: "warn",
  accepted: "ok",
  closed: "ok",
  missed: "bad",
};

export function PlanStatusPill({ s, late }: { s: HrmPlanStatus; late?: boolean }) {
  if (late && (s === "submitted" || s === "draft" || s === "returned"))
    return <StatusPill tone="warn">Trễ hạn</StatusPill>;
  return <StatusPill tone={STATUS_TONE[s]}>{PLAN_STATUS_LABEL[s]}</StatusPill>;
}

// --------------------------------------------------------------------------------------------------------------------
// Bảng tổng hợp (Phòng HCTH, Admin).
// --------------------------------------------------------------------------------------------------------------------

export function HrmPlanCenterScreen() {
  const pa = usePlanApi();
  const [nonce, setNonce] = useState(0);
  const list = useLoad(() => pa.list(), [nonce]);
  const [picked, setPicked] = useState<number | null>(null);
  const [opening, setOpening] = useState(false);
  const years = list.data?.items ?? [];
  const year = picked ?? years[0]?.year ?? null;
  return (
    <>
      <PageHeader
        title={year ? `Kế hoạch nhân lực năm ${year}` : "Kế hoạch nhân lực năm"}
        actions={
          <button type="button" className="btn btn-o btn-sm" onClick={() => setOpening(true)}>
            Mở kỳ kế hoạch
          </button>
        }
      >
        Lãnh đạo các đơn vị lập kế hoạch nhân lực năm; Phòng HCTH tổng hợp toàn Trung tâm: số hiện có, dự kiến nghỉ hưu
        (tự tính từ hồ sơ tại ngày chốt), dự kiến nghỉ/chuyển khác, cần bổ sung, nhu cầu theo kế hoạch.
      </PageHeader>
      <LoadState s={list}>
        {(l) =>
          l.items.length ? (
            <>
              {l.items.length > 1 ? (
                <div className="row" style={{ marginBottom: 12 }}>
                  <label className="small">
                    Năm kế hoạch{" "}
                    <select
                      aria-label="Năm kế hoạch"
                      value={year ?? ""}
                      onChange={(e) => setPicked(Number(e.target.value))}
                    >
                      {l.items.map((p) => (
                        <option key={p.year} value={p.year}>
                          {p.year} — {PERIOD_STATUS_LABEL[p.status] ?? p.status}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
              ) : null}
              {year ? <PlanSummaryView year={year} onChanged={() => setNonce((n) => n + 1)} /> : null}
            </>
          ) : (
            <div className="card">
              <p className="mute">Chưa mở kỳ kế hoạch nhân lực nào. Bấm “Mở kỳ kế hoạch” để bắt đầu.</p>
            </div>
          )
        }
      </LoadState>
      {opening ? (
        <OpenPlanDialog
          onClose={() => setOpening(false)}
          onOpened={(y) => {
            setOpening(false);
            setPicked(y);
            setNonce((n) => n + 1);
          }}
        />
      ) : null}
    </>
  );
}

function OpenPlanDialog({ onClose, onOpened }: { onClose: () => void; onOpened: (year: number) => void }) {
  const pa = usePlanApi();
  const act = useAction();
  const next = new Date().getFullYear() + 1;
  const [year, setYear] = useState(String(next));
  const [due, setDue] = useState(`${next - 1}-11-30`);
  const [cutoff, setCutoff] = useState(vnToday());
  return (
    <Dialog
      title="Mở kỳ kế hoạch nhân lực năm"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={() =>
            void act.run(async () => {
              const p = await pa.open({ year: Number(year), due_on: due, cutoff_on: cutoff });
              onOpened(p.year);
              return p;
            })
          }
        >
          Mở kỳ
        </button>
      }
    >
      {act.view}
      <div className="grid2">
        <div className="field">
          <label htmlFor="kh-year">Năm kế hoạch</label>
          <input id="kh-year" type="number" min={2020} max={2100} value={year} onChange={(e) => setYear(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="kh-due">Hạn các đơn vị gửi</label>
          <input id="kh-due" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="kh-cutoff">Ngày chốt số liệu hiện có</label>
          <input id="kh-cutoff" type="date" value={cutoff} onChange={(e) => setCutoff(e.target.value)} />
        </div>
      </div>
      <p className="mute small">Kỳ mở cho mọi đơn vị nội bộ đang dùng (danh mục đơn vị nội bộ).</p>
    </Dialog>
  );
}

function PlanSummaryView({ year, onChanged }: { year: number; onChanged: () => void }) {
  const pa = usePlanApi();
  const [nonce, setNonce] = useState(0);
  const sum = useLoad(() => pa.summary(year), [year, nonce]);
  const act = useAction();
  const [confirm, setConfirm] = useState(false);
  return (
    <LoadState s={sum}>
      {(s) => {
        const chot = s.period.status === "closed" || s.period.status === "cancelled";
        const pending = s.rows.filter((r) => r.versions === 0);
        return (
          <>
            {act.view}
            <div className="card action-bar" role="toolbar" aria-label="Thao tác">
              <StatusPill tone={chot ? "ok" : "info"}>{PERIOD_STATUS_LABEL[s.period.status] ?? s.period.status}</StatusPill>
              <span className="mute small">
                Hạn các phòng gửi: {vnDate(s.period.due_at.slice(0, 10))} · Ngày chốt số liệu hiện có: {vnDate(s.period.cutoff_on)} ·
                Đã gửi {s.period.submitted}/{s.period.units.length} đơn vị
              </span>
              <span style={{ flex: 1 }} />
              <button
                type="button"
                className="btn btn-o btn-sm"
                disabled={act.busy}
                onClick={() =>
                  void act.run(async () => {
                    const f = await pa.exportFile(year);
                    saveBlob(f.blob, f.name);
                    return f;
                  }, "Đã xuất kế hoạch nhân lực ra Excel.")
                }
              >
                Xuất Excel
              </button>
              {s.can_manage ? (
                <button type="button" className="btn btn-p btn-sm" disabled={chot || act.busy} onClick={() => setConfirm(true)}>
                  Chốt kế hoạch
                </button>
              ) : null}
            </div>
            {!s.figures_available ? (
              <div className="alert alert-warn" role="note">
                Hồ sơ nhân sự chưa bật — số hiện có, dự kiến nghỉ hưu chưa tự tính được.
              </div>
            ) : null}
            <div className="card dt-card">
              <div className="tbl-wrap">
                <table className="dt">
                  <caption className="sr-only">Kế hoạch nhân lực năm {year} theo đơn vị</caption>
                  <thead>
                    <tr>
                      <th>Đơn vị</th>
                      <th className="num">Hiện có</th>
                      <th className="num">Dự kiến nghỉ hưu</th>
                      <th className="num">Dự kiến nghỉ, chuyển khác</th>
                      <th className="num">Cần bổ sung</th>
                      <th className="num">Nhu cầu theo kế hoạch</th>
                      <th>Trạng thái</th>
                    </tr>
                  </thead>
                  <tbody>
                    {s.rows.map((r) => (
                      <tr key={r.unit}>
                        <td>
                          <Link href={`${BASE}/${year}/${r.unit}`}>{r.unit_name}</Link>
                          {r.adjusted ? <span className="mute small"> · đã điều chỉnh</span> : null}
                        </td>
                        <td className="num">{r.values.current}</td>
                        <td className="num">{r.values.retiring}</td>
                        <td className="num">{r.values.leaving}</td>
                        <td className="num">{r.values.additional}</td>
                        <td className="num">
                          <b>{r.values.demand}</b>
                        </td>
                        <td>
                          <PlanStatusPill s={r.status} late={r.late} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                  <tfoot>
                    <tr>
                      <td>
                        <b>Toàn Trung tâm</b>
                      </td>
                      <td className="num">
                        <b>{s.totals.current}</b>
                      </td>
                      <td className="num">
                        <b>{s.totals.retiring}</b>
                      </td>
                      <td className="num">
                        <b>{s.totals.leaving}</b>
                      </td>
                      <td className="num">
                        <b>{s.totals.additional}</b>
                      </td>
                      <td className="num">
                        <b>{s.totals.demand}</b>
                      </td>
                      <td />
                    </tr>
                  </tfoot>
                </table>
              </div>
              <p className="mute small">Bấm vào một đơn vị để xem chi tiết kế hoạch.</p>
            </div>
            {confirm ? (
              <Dialog
                title="Chốt kế hoạch nhân lực"
                onClose={() => setConfirm(false)}
                actions={
                  <button
                    type="button"
                    className="btn btn-p btn-sm"
                    disabled={act.busy}
                    onClick={() =>
                      void act.run(async () => {
                        const r = await pa.close(year);
                        setConfirm(false);
                        setNonce((n) => n + 1);
                        onChanged();
                        return r;
                      }, `Đã chốt kế hoạch nhân lực năm ${year}.`)
                    }
                  >
                    Chốt kế hoạch
                  </button>
                }
              >
                <p>
                  {pending.length ? (
                    <>
                      <b>{pending.length}</b> đơn vị chưa gửi kế hoạch: {pending.map((r) => r.unit_name).join(", ")}.{" "}
                    </>
                  ) : null}
                  Sau khi chốt, bảng tổng hợp không sửa được; chỉ điều chỉnh có lý do. Chốt kế hoạch?
                </p>
              </Dialog>
            ) : null}
          </>
        );
      }}
    </LoadState>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Chi tiết đơn vị.
// --------------------------------------------------------------------------------------------------------------------

export function HrmPlanUnitScreen() {
  const p = useParams<{ year: string; unit: string }>();
  return <PlanUnitView year={Number(p.year)} unit={p.unit} back />;
}

/** Lãnh đạo đơn vị: kế hoạch của đơn vị mình ở kỳ mới nhất. */
export function HrmPlanMyUnitScreen() {
  const pa = usePlanApi();
  const list = useLoad(() => pa.list(), []);
  const year = list.data?.items[0]?.year;
  const sum = useLoad(year ? () => pa.summary(year) : null, [year]);
  return (
    <LoadState s={list}>
      {(l) =>
        !l.items.length ? (
          <>
            <PageHeader title="Kế hoạch của phòng" />
            <div className="card">
              <p className="mute">Phòng HCTH chưa mở kỳ kế hoạch nhân lực năm.</p>
            </div>
          </>
        ) : (
          <LoadState s={sum}>
            {(s: HrmPlanSummary) =>
              s.my_unit ? (
                <PlanUnitView year={s.period.year} unit={s.my_unit} />
              ) : (
                <div className="card">
                  <p className="mute">Phiên làm việc không gắn đơn vị nội bộ — không lập được kế hoạch của phòng.</p>
                </div>
              )
            }
          </LoadState>
        )
      }
    </LoadState>
  );
}

const QUARTERS = (y: number) => [`Quý I/${y}`, `Quý II/${y}`, `Quý III/${y}`, `Quý IV/${y}`];

function emptyInput(): HrmPlanInput {
  return { leaving: 0, additional: 0, positions: [], note: "" };
}

function PlanUnitView({ year, unit, back }: { year: number; unit: string; back?: boolean }) {
  const pa = usePlanApi();
  const [nonce, setNonce] = useState(0);
  const v = useLoad(() => pa.unit(year, unit), [year, unit, nonce]);
  return (
    <LoadState s={v}>
      {(d) => <PlanUnitBody key={`${nonce}`} d={d} back={back} reload={() => setNonce((n) => n + 1)} api={pa} />}
    </LoadState>
  );
}

function PlanUnitBody({ d, back, reload, api: pa }: { d: HrmPlanUnit; back?: boolean; reload: () => void; api: PlanApi }) {
  const year = d.period.year;
  const unit = d.row.unit;
  const start: HrmPlanInput =
    d.draft && d.row.status === "draft"
      ? d.draft.values
      : d.row.source !== "auto"
        ? {
            leaving: d.row.values.leaving,
            additional: d.row.values.additional,
            positions: d.row.values.positions ?? [],
            note: d.row.values.note ?? "",
          }
        : (d.draft?.values ?? emptyInput());
  const [form, setForm] = useState<HrmPlanInput>(start);
  const [addPos, setAddPos] = useState(false);
  const [review, setReview] = useState(false);
  const [adjust, setAdjust] = useState(false);
  const act = useAction();
  const edit = d.can_edit;
  const cur = d.row.source === "auto" ? d.figures.current : d.row.values.current;
  const ret = d.row.source === "auto" ? d.figures.retiring : d.row.values.retiring;
  const demand = planDemand({ current: cur, retiring: ret, leaving: form.leaving, additional: form.additional });
  const positions = form.positions ?? [];
  const setNum = (k: "leaving" | "additional", s: string) =>
    setForm((f) => ({ ...f, [k]: Math.max(0, Math.trunc(Number(s) || 0)) }));
  const title = `Kế hoạch nhân lực ${year} · ${d.row.unit_name}`;
  const log: LogEntry[] = [
    ...d.submissions.flatMap((s) => {
      const out: LogEntry[] = [
        {
          event: s.late ? `Gửi Phòng HCTH lần ${s.version} (trễ hạn)` : `Gửi Phòng HCTH lần ${s.version}`,
          actor_display: d.row.unit_name,
          at: s.submitted_at,
        },
      ];
      if (s.decision && s.reviewed_at)
        out.push({
          event: s.decision === "accepted" ? "Phòng HCTH xác nhận" : "Phòng HCTH yêu cầu điều chỉnh",
          actor_display: "Phòng HCTH",
          at: s.reviewed_at,
          reason: s.review_note || null,
        });
      return out;
    }),
    ...(d.draft ? [{ event: "Lưu nháp kế hoạch", actor_display: d.row.unit_name, at: d.draft.recorded_at }] : []),
    ...d.adjustments.map((a) => ({
      event: `Điều chỉnh sau chốt (lần ${a.seq})`,
      actor_display: "Phòng HCTH",
      at: a.recorded_at,
      reason: a.reason,
      changes: [{ field: "Nhu cầu theo kế hoạch", old: null, new: String(a.values.demand) }],
    })),
  ].sort((a, b) => b.at.localeCompare(a.at));

  const main = (
    <>
      <section className="lv-block">
        <h2>Số liệu hiện có (tự động)</h2>
        <dl className="lv-grid">
          <dt>Số nhân sự hiện có</dt>
          <dd>{cur}</dd>
          <dt>Ngày chốt</dt>
          <dd>{vnDate(d.period.cutoff_on)}</dd>
          <dt>Dự kiến nghỉ hưu năm {year}</dt>
          <dd>
            {ret}
            {d.figures.retirees.length ? (
              <ul className="small" style={{ margin: "4px 0 0", paddingLeft: 18 }}>
                {d.figures.retirees.map((r) => (
                  <li key={r.staff_id}>
                    {r.full_name || r.code} — đủ tuổi {vnDate(r.retire_on)}
                  </li>
                ))}
              </ul>
            ) : null}
            {d.figures.unknown ? (
              <div className="mute small">{d.figures.unknown} người thiếu ngày sinh/giới tính — chưa tính được.</div>
            ) : null}
          </dd>
        </dl>
        {!d.figures.available ? (
          <p className="mute small">Hồ sơ nhân sự chưa bật — số tự tính chưa có.</p>
        ) : null}
      </section>
      <section className="lv-block">
        <h2>Kế hoạch của phòng</h2>
        <dl className="lv-grid">
          <dt>
            <label htmlFor="kh-nk">Dự kiến nghỉ, chuyển công tác khác</label>
          </dt>
          <dd>
            {edit ? (
              <input id="kh-nk" type="number" min={0} value={form.leaving} onChange={(e) => setNum("leaving", e.target.value)} />
            ) : (
              form.leaving
            )}
          </dd>
          <dt>Nhu cầu theo kế hoạch</dt>
          <dd>
            <b>{demand}</b>
          </dd>
          <dt>
            <label htmlFor="kh-bs">Số cần bổ sung</label>
          </dt>
          <dd>
            {edit ? (
              <input id="kh-bs" type="number" min={0} value={form.additional} onChange={(e) => setNum("additional", e.target.value)} />
            ) : (
              form.additional
            )}
          </dd>
          <dt>
            <label htmlFor="kh-note">Ghi chú</label>
          </dt>
          <dd>
            {edit ? (
              <textarea id="kh-note" maxLength={500} value={form.note ?? ""} onChange={(e) => setForm((f) => ({ ...f, note: e.target.value }))} />
            ) : (
              form.note || "—"
            )}
          </dd>
        </dl>
      </section>
      <section className="lv-block">
        <h2>Nhu cầu theo vị trí</h2>
        <div className="tbl-wrap">
          <table className="dt">
            <caption className="sr-only">Nhu cầu theo vị trí</caption>
            <thead>
              <tr>
                <th>Vị trí</th>
                <th>Trình độ yêu cầu</th>
                <th className="num">Số lượng</th>
                <th>Thời điểm cần</th>
                <th>Lý do</th>
                {edit ? <th aria-label="Thao tác" /> : null}
              </tr>
            </thead>
            <tbody>
              {positions.length ? (
                positions.map((p, i) => (
                  <tr key={`${p.title}-${i}`}>
                    <td>{p.title}</td>
                    <td>{p.level || "—"}</td>
                    <td className="num">{p.count}</td>
                    <td>{p.when || "—"}</td>
                    <td>{p.reason || "—"}</td>
                    {edit ? (
                      <td>
                        <button
                          type="button"
                          className="btn-link"
                          aria-label={`Xóa dòng ${p.title}`}
                          onClick={() => setForm((f) => ({ ...f, positions: positions.filter((_, j) => j !== i) }))}
                        >
                          Xóa
                        </button>
                      </td>
                    ) : null}
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={edit ? 6 : 5} className="mute">
                    Chưa có vị trí cần tuyển.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {edit && positions.length < 10 ? (
          <button type="button" className="btn-link" onClick={() => setAddPos(true)}>
            + Thêm vị trí
          </button>
        ) : null}
      </section>
    </>
  );

  const status = <PlanStatusPill s={d.row.status} late={d.row.late} />;
  return (
    <>
      {back ? <BackLink href={BASE}>Kế hoạch nhân lực</BackLink> : null}
      <PageHeader title={title} badge={status}>
        {d.row.review_note && d.row.decision === "returned" ? `Phòng HCTH yêu cầu điều chỉnh: ${d.row.review_note}` : null}
      </PageHeader>
      {act.view}
      <div className="card action-bar" role="toolbar" aria-label="Thao tác">
        {edit ? (
          <>
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(async () => {
                  const r = await pa.previous(year, unit);
                  setForm(r.values);
                  return r;
                }, (r) => `Đã sao chép kế hoạch năm ${r.from_year} — kiểm tra rồi Lưu nháp hoặc Gửi.`)
              }
            >
              Sao chép năm trước
            </button>
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(async () => {
                  const r = await pa.draft(year, unit, form);
                  reload();
                  return r;
                }, "Đã lưu nháp kế hoạch.")
              }
            >
              Lưu nháp
            </button>
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(async () => {
                  const r = await pa.submit(year, unit, { ...form, base_version: d.row.versions });
                  reload();
                  return r;
                }, "Đã gửi kế hoạch cho Phòng HCTH.")
              }
            >
              Gửi Phòng HCTH
            </button>
          </>
        ) : null}
        {d.can_review ? (
          <>
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(async () => {
                  const r = await pa.review(year, unit, "accepted", "");
                  reload();
                  return r;
                }, "Đã xác nhận kế hoạch của đơn vị.")
              }
            >
              Xác nhận
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setReview(true)}>
              Yêu cầu điều chỉnh
            </button>
          </>
        ) : null}
        {d.can_adjust ? (
          <button type="button" className="btn btn-o btn-sm" onClick={() => setAdjust(true)}>
            Điều chỉnh (có lý do)
          </button>
        ) : null}
        {!edit && d.row.status === "closed" && !d.can_adjust ? (
          <span className="mute small">Kế hoạch đã chốt, chỉ điều chỉnh có lý do.</span>
        ) : null}
      </div>
      <DetailLayout main={main} log={<LogPanel title="Lịch sử" entries={log} empty="Chưa có thao tác nào." />} />
      {addPos ? (
        <PositionDialog
          year={year}
          onClose={() => setAddPos(false)}
          onAdd={(p) => {
            setForm((f) => ({ ...f, positions: [...(f.positions ?? []), p] }));
            setAddPos(false);
          }}
        />
      ) : null}
      {review ? (
        <ReasonDialog
          title="Yêu cầu điều chỉnh kế hoạch"
          label="Nội dung cần điều chỉnh"
          min={1}
          ok="Gửi yêu cầu"
          onClose={() => setReview(false)}
          onOk={(note) =>
            act.run(async () => {
              const r = await pa.review(year, unit, "returned", note);
              setReview(false);
              reload();
              return r;
            }, "Đã yêu cầu đơn vị điều chỉnh kế hoạch.")
          }
        />
      ) : null}
      {adjust ? (
        <ReasonDialog
          title="Điều chỉnh kế hoạch đã chốt"
          label="Lý do điều chỉnh (10–500 ký tự)"
          min={10}
          ok="Ghi điều chỉnh"
          onClose={() => setAdjust(false)}
          extra={
            <div className="grid2">
              <div className="field">
                <label htmlFor="adj-nk">Dự kiến nghỉ, chuyển công tác khác</label>
                <input id="adj-nk" type="number" min={0} value={form.leaving} onChange={(e) => setNum("leaving", e.target.value)} />
              </div>
              <div className="field">
                <label htmlFor="adj-bs">Số cần bổ sung</label>
                <input id="adj-bs" type="number" min={0} value={form.additional} onChange={(e) => setNum("additional", e.target.value)} />
              </div>
            </div>
          }
          onOk={(reason) =>
            act.run(async () => {
              const r = await pa.adjust(year, unit, { ...form, reason });
              setAdjust(false);
              reload();
              return r;
            }, "Đã ghi điều chỉnh kế hoạch (lưu lịch sử).")
          }
        />
      ) : null}
    </>
  );
}

function PositionDialog({ year, onClose, onAdd }: { year: number; onClose: () => void; onAdd: (p: HrmPlanPosition) => void }) {
  const [p, setP] = useState<HrmPlanPosition>({ title: "", level: "", count: 1, when: QUARTERS(year)[0], reason: "" });
  const [err, setErr] = useState<string | null>(null);
  return (
    <Dialog
      title="Thêm vị trí cần tuyển"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          onClick={() => {
            if (!p.title.trim() || !(p.count >= 1)) {
              setErr("Nhập vị trí và số lượng (lớn hơn 0).");
              return;
            }
            onAdd({ ...p, title: p.title.trim() });
          }}
        >
          Thêm
        </button>
      }
    >
      {err ? (
        <div className="alert alert-warn" role="alert">
          {err}
        </div>
      ) : null}
      <div className="grid2">
        <div className="field">
          <label htmlFor="vt-title">Vị trí *</label>
          <input id="vt-title" maxLength={200} value={p.title} onChange={(e) => setP({ ...p, title: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="vt-level">Trình độ yêu cầu</label>
          <input id="vt-level" maxLength={100} value={p.level ?? ""} onChange={(e) => setP({ ...p, level: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="vt-count">Số lượng *</label>
          <input
            id="vt-count"
            type="number"
            min={1}
            value={p.count}
            onChange={(e) => setP({ ...p, count: Math.trunc(Number(e.target.value) || 0) })}
          />
        </div>
        <div className="field">
          <label htmlFor="vt-when">Thời điểm cần</label>
          <select id="vt-when" value={p.when ?? ""} onChange={(e) => setP({ ...p, when: e.target.value })}>
            {QUARTERS(year).map((q) => (
              <option key={q} value={q}>
                {q}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="field">
        <label htmlFor="vt-reason">Lý do</label>
        <textarea id="vt-reason" maxLength={500} value={p.reason ?? ""} onChange={(e) => setP({ ...p, reason: e.target.value })} />
      </div>
    </Dialog>
  );
}

function ReasonDialog({
  title,
  label,
  min,
  ok,
  extra,
  onClose,
  onOk,
}: {
  title: string;
  label: string;
  min: number;
  ok: string;
  extra?: ReactNode;
  onClose: () => void;
  onOk: (text: string) => Promise<unknown>;
}) {
  const [text, setText] = useState("");
  const short = text.trim().length < min;
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={short} onClick={() => void onOk(text.trim())}>
          {ok}
        </button>
      }
    >
      {extra}
      <div className="field">
        <label htmlFor="rs-text">{label}</label>
        <textarea id="rs-text" maxLength={500} value={text} onChange={(e) => setText(e.target.value)} />
      </div>
    </Dialog>
  );
}
