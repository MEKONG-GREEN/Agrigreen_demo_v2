"use client";

// Khai thác thủy sản tuần (BF-CN-CATCH #112 — PRD UP-09, BRD CNTS v3.7 UR-XA-08 BM-17; menu CNTS xã "Khai thác thủy sản tuần",
// CNTS tỉnh "Số liệu cấp xã › Khai thác thủy sản"): đúng 4 sheet của biểu gốc "ĐÁNH BẮT (nhập liệu theo xã).xlsx" — cá các
// loại, tôm các loại, mực, thủy sản khác; dòng = xã/phường, cột = tuần (tấn), dòng TỔNG. Cấp Xã nhập cột của tuần đang mở
// ngay trên sheet (một bài BM-17 gồm cả 4 nhóm; ô mặc định 0, không âm), báo không phát sinh, lý do trễ khi quá hạn; khóa lạc
// quan theo phiên bản bài nộp. Cấp Tỉnh xem 4 sheet của mọi xã; tổng hợp theo "Bảng tổng hợp thực hiện chỉ tiêu thủy sản"
// ở màn Phụ lục 2 (BM-18).
import Link from "next/link";
import { Fragment, useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, num, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { createCntsPeriodApi, fieldErrors, isLate, payloadValues, STATUS_LABEL, STATUS_TONE, type CntsPeriodApi, type FormsTask } from "../periods/api";
import { CATCH_CODE, catchMatrix, draftOf, editableWeeks, sheetTotal, sheetValue, SHEETS, unitTotal, type CatchMatrix, type CatchSheet } from "./catch";

const LIMITS = [
  [4, "4 tuần gần nhất"],
  [8, "8 tuần gần nhất"],
  [13, "13 tuần gần nhất"],
  [0, "Tất cả các tuần"],
] as const;

type Key = CatchSheet["key"];

export function CatchScreen() {
  const { api, kn } = useAuth();
  const writer = kn.context?.role === "kn_xa";
  const capi = useMemo(() => (api ? createCntsPeriodApi(api) : null), [api]);
  const tasks = useLoad(capi ? () => capi.tasks(CATCH_CODE) : null, [capi]);
  const [limit, setLimit] = useState<number>(8);
  // Thông báo / lỗi giữ ở đây: gửi xong tải lại công việc thì phần thân dựng lại (khóa theo phiên bản bài nộp).
  const act = useAction();
  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader title="Khai thác thủy sản tuần">
        <span className="mute">
          Sản lượng đánh bắt theo xã, theo tuần (BM-17) — đúng 4 sheet của biểu &quot;ĐÁNH BẮT (nhập liệu theo xã)&quot;. Đơn vị: tấn.
          {writer ? " Nhập cột của tuần đang mở; một tuần của một xã là một bài gồm cả 4 nhóm." : null}{" "}
          <Link href="/kn/cnts/phu-luc-2">Tổng hợp tỉnh: Phụ lục 2 (Bảng tổng hợp thực hiện chỉ tiêu thủy sản)</Link>
        </span>
      </PageHeader>
      <div className="toolbar" role="group" aria-label="Bộ lọc">
        <select className="select" aria-label="Số tuần hiển thị" value={limit} onChange={(e) => setLimit(Number(e.target.value))}>
          {LIMITS.map(([v, l]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </select>
      </div>
      {act.view}
      <LoadState s={tasks}>
        {(ts) => (
          <CatchBody key={ts.map((t) => `${t.id}:${t.version}`).join(",")} tasks={ts} limit={limit} writer={writer} capi={capi} act={act} reload={tasks.reload} />
        )}
      </LoadState>
    </div>
  );
}

function CatchBody({
  tasks,
  limit,
  writer,
  capi,
  act,
  reload,
}: {
  tasks: FormsTask[];
  limit: number;
  writer: boolean;
  capi: CntsPeriodApi;
  act: ReturnType<typeof useAction>;
  reload: () => void;
}) {
  const full = useMemo(() => catchMatrix(tasks), [tasks]);
  const weeks = writer ? editableWeeks(full) : [];
  const [week, setWeek] = useState(weeks[0]?.periodId ?? "");
  const m = useMemo(() => {
    const shown = catchMatrix(tasks, limit);
    // Tuần đang nhập luôn hiện, kể cả khi ngoài số tuần đã chọn.
    const w = full.weeks.find((x) => x.periodId === week);
    return w && !shown.weeks.some((x) => x.periodId === week) ? { ...shown, weeks: [...shown.weeks, w] } : shown;
  }, [tasks, limit, full, week]);
  const name = useCommuneNames(m.units);
  const [sheet, setSheet] = useState<Key>("ca");
  const open = writer ? m.units.map((u) => full.cell(week, u)).filter((t): t is FormsTask => !!t?.open) : [];
  const [drafts, setDrafts] = useState<Record<string, Record<Key, string>>>(() => Object.fromEntries(open.map((t) => [t.unit, draftOf(t)])));
  const [nil, setNil] = useState<Record<string, boolean>>(() => Object.fromEntries(open.map((t) => [t.unit, t.nil_report])));
  const [explanation, setExplanation] = useState("");
  const [note, setNote] = useState("");
  const [errs, setErrs] = useState<Record<string, string>>({});
  const late = open.some((t) => isLate(t));
  const pickWeek = (id: string) => {
    const ts = m.units.map((u) => full.cell(id, u)).filter((t): t is FormsTask => !!t?.open);
    setWeek(id);
    setDrafts(Object.fromEntries(ts.map((t) => [t.unit, draftOf(t)])));
    setNil(Object.fromEntries(ts.map((t) => [t.unit, t.nil_report])));
    setErrs({});
  };
  const submit = async () => {
    const e: Record<string, string> = {};
    for (const t of open) {
      const fe = fieldErrors(t.fields, drafts[t.unit] ?? {}, !!nil[t.unit]);
      for (const [key, reason] of Object.entries(fe)) e[`${t.unit}.${key}`] = reason;
    }
    if (late && !explanation.trim()) e.explanation = "Gửi sau hạn nộp phải ghi lý do trễ.";
    setErrs(e);
    if (Object.keys(e).length) return;
    const done = await act.run(async () => {
      let n = 0;
      for (const t of open) {
        await capi.submit(t.period_id, {
          unit: t.unit,
          values: payloadValues(t.fields, drafts[t.unit] ?? {}, !!nil[t.unit]),
          base_version: t.version,
          nil_report: !!nil[t.unit],
          ...(note.trim() ? { note: note.trim() } : {}),
          ...(explanation.trim() ? { explanation: explanation.trim() } : {}),
        });
        n++;
      }
      return n;
    }, (n) => `Đã gửi số liệu khai thác ${n} xã của tuần.`);
    if (done !== null) reload();
  };
  const sheetErrs = (key: Key) => Object.keys(errs).filter((x) => x.endsWith(`.${key}`)).length;
  if (!m.weeks.length) {
    return <p className="empty">Chưa có kỳ BM-17 (khai thác thủy sản) được phát hành cho xã trong phạm vi.</p>;
  }
  return (
    <>
      {writer ? (
        <section className="card sec" aria-label="Nhập tuần">
          <h2>Nhập số liệu tuần</h2>
          {weeks.length ? (
            <>
              <label>
                Tuần đang nhập{" "}
                <select className="select" aria-label="Tuần đang nhập" value={week} onChange={(e) => pickWeek(e.target.value)}>
                  {weeks.map((w) => (
                    <option key={w.periodId} value={w.periodId}>
                      {w.label} — hạn {new Date(w.dueAt).toLocaleString("vi-VN")}
                    </option>
                  ))}
                </select>
              </label>
              <ul className="list-plain">
                {open.map((t) => (
                  <li key={t.unit}>
                    <strong>{name(t.unit)}</strong> <StatusPill tone={STATUS_TONE[t.status]}>{STATUS_LABEL[t.status]}</StatusPill>{" "}
                    <label>
                      <input
                        type="checkbox"
                        aria-label={`Không phát sinh khai thác trong tuần — ${name(t.unit)}`}
                        checked={!!nil[t.unit]}
                        onChange={(e) => setNil({ ...nil, [t.unit]: e.target.checked })}
                      />{" "}
                      Không phát sinh khai thác trong tuần
                    </label>
                    {t.return_note ? <span className="warn"> Yêu cầu điều chỉnh: {t.return_note}</span> : null}
                  </li>
                ))}
              </ul>
              {late ? (
                <label>
                  Lý do trễ (bắt buộc khi gửi sau hạn)
                  <input
                    className="input"
                    aria-label="Lý do trễ"
                    value={explanation}
                    maxLength={1000}
                    aria-invalid={errs.explanation ? true : undefined}
                    onChange={(e) => setExplanation(e.target.value)}
                  />
                  {errs.explanation ? (
                    <span className="err" role="alert">
                      {errs.explanation}
                    </span>
                  ) : null}
                </label>
              ) : null}
              <label>
                Ghi chú
                <input className="input" aria-label="Ghi chú" value={note} maxLength={1000} onChange={(e) => setNote(e.target.value)} />
              </label>
              {Object.keys(errs).some((x) => x !== "explanation") ? (
                <p className="err" role="alert">
                  Còn ô chưa hợp lệ ở:{" "}
                  {SHEETS.filter((s) => sheetErrs(s.key))
                    .map((s) => s.tab)
                    .join(", ")}
                </p>
              ) : null}
              <button className="btn btn-primary" disabled={act.busy || !open.length} onClick={() => void submit()}>
                Gửi tuần
              </button>
            </>
          ) : (
            <p className="mute">Không có tuần nào đang nhận số liệu.</p>
          )}
        </section>
      ) : null}
      <div className="tabs" role="tablist" aria-label="Sheet">
        {SHEETS.map((s) => (
          <button key={s.key} role="tab" aria-selected={sheet === s.key} className={sheet === s.key ? "tab active" : "tab"} onClick={() => setSheet(s.key)}>
            {s.tab}
            {sheetErrs(s.key) ? ` (${sheetErrs(s.key)} lỗi)` : ""}
          </button>
        ))}
      </div>
      {SHEETS.filter((s) => s.key === sheet).map((s) => (
        <SheetTable
          key={s.key}
          s={s}
          m={m}
          name={name}
          editWeek={writer ? week : ""}
          drafts={drafts}
          nil={nil}
          errs={errs}
          onChange={(unit, v) => setDrafts({ ...drafts, [unit]: { ...(drafts[unit] ?? ({} as Record<Key, string>)), [s.key]: v } })}
        />
      ))}
      <section className="card sec">
        <h2>Tổng sản lượng khai thác theo xã (4 nhóm, tấn)</h2>
        <table className="dt" aria-label="Tổng khai thác theo xã">
          <thead>
            <tr>
              <th>STT</th>
              <th>Xã, phường</th>
              {m.weeks.map((w) => (
                <th key={w.periodId} className="num">
                  {w.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {m.units.map((u, i) => (
              <tr key={u}>
                <td>{i + 1}</td>
                <td>{name(u)}</td>
                {m.weeks.map((w) => (
                  <td key={w.periodId} className="num">
                    {num(unitTotal(m.cell(w.periodId, u)), 3)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </>
  );
}

function SheetTable({
  s,
  m,
  name,
  editWeek,
  drafts,
  nil,
  errs,
  onChange,
}: {
  s: CatchSheet;
  m: CatchMatrix;
  name: (code: string) => string;
  editWeek: string;
  drafts: Record<string, Record<Key, string>>;
  nil: Record<string, boolean>;
  errs: Record<string, string>;
  onChange: (unit: string, v: string) => void;
}) {
  return (
    <section className="card sec" role="tabpanel" aria-label={s.tab}>
      <h2>{s.heading}</h2>
      <div className="table-scroll">
        <table className="dt" aria-label={s.heading}>
          <thead>
            <tr>
              <th>STT</th>
              <th>Xã, phường</th>
              {m.weeks.map((w) => (
                <th key={w.periodId} className="num">
                  Trong kỳ báo cáo ({w.label}) (Tấn)
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {m.units.map((u, i) => (
              <tr key={u}>
                <td>{i + 1}</td>
                <td>{name(u)}</td>
                {m.weeks.map((w) => {
                  const t = m.cell(w.periodId, u);
                  const editing = w.periodId === editWeek && t?.open && drafts[u] !== undefined;
                  const err = errs[`${u}.${s.key}`];
                  return (
                    <td key={w.periodId} className="num">
                      {editing ? (
                        <>
                          <input
                            className="input input-sm"
                            inputMode="decimal"
                            aria-label={`${s.tab} ${name(u)} ${w.label} (tấn)`}
                            aria-invalid={err ? true : undefined}
                            disabled={!!nil[u]}
                            value={nil[u] ? "0" : (drafts[u]?.[s.key] ?? "")}
                            onChange={(e) => onChange(u, e.target.value)}
                          />
                          {err ? (
                            <span className="err" role="alert">
                              {err}
                            </span>
                          ) : null}
                        </>
                      ) : !t ? (
                        <span className="mute">—</span>
                      ) : t.version === 0 ? (
                        <span className="mute" title={STATUS_LABEL[t.status]}>
                          {t.status === "overdue" || t.status === "missed" ? <StatusPill tone="bad">{STATUS_LABEL[t.status]}</StatusPill> : "·"}
                        </span>
                      ) : (
                        <Fragment>
                          {num(sheetValue(t, s.key), 3)}
                          {t.nil_report ? <span className="mute"> (không phát sinh)</span> : null}
                        </Fragment>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
            <tr className="row-sub">
              <td colSpan={2}>
                <strong>TỔNG</strong>
              </td>
              {m.weeks.map((w) => (
                <td key={w.periodId} className="num">
                  <strong>{num(sheetTotal(m, w.periodId, s.key), 3)}</strong>
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  );
}
