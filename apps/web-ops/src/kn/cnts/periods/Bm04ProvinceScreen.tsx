"use client";

// Số liệu cấp xã › Nhập liệu tuần — Cấp Tỉnh Phòng CNTS (BF-CN-XA-DATA #175, BRD CNTS v3.7 FN-09, BM-04): chọn năm, tuần;
// lọc khu vực, xã, trạng thái nộp; bảng một dòng một xã + dòng TỔNG theo đúng cột, thứ tự, đơn vị của biểu BM-04 (AC-01);
// chế độ Lũy kế từ đầu năm/quý (BR-01: kỳ thiếu tính 0, hiện "Thiếu kỳ" — AC-03). Chi tiết một xã: số kỳ này, lịch sử
// phiên bản gửi (cũ → mới, giá trị cũ → mới, lý do sửa sau hạn — AC-05), cờ "Đã sửa sau xác thực" (AC-06); người phụ trách
// biểu Xác thực / Yêu cầu điều chỉnh có lý do (POST /v1/forms/submissions/{id}/review). Chỉ đọc số liệu xã — Tỉnh không sửa.
// Dữ liệu: GET /v1/forms/tasks (máy chủ lọc theo phạm vi người gọi, tối đa 200 kỳ một năm), period-tracking (người phụ trách).
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { ReasonBox } from "@/kn/cnts/model/parts";
import { dt, LoadState, num, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { createCntsPeriodApi, STATUS_LABEL, STATUS_TONE, type CntsPeriodApi, type FormsTask, type TaskStatus } from "./api";
import { BM04_CODE, bm04Data, cellValue, changesBetween, columnGroups, columnTotal, cumulative, editedAfterAccept, isFlow, type Bm04Data, type CumBasis } from "./bm04";

type Mode = "week" | CumBasis;

const MODE_LABEL: Record<Mode, string> = { week: "Số liệu tuần", year: "Lũy kế từ đầu năm", quarter: "Lũy kế từ đầu quý" };

const thisYear = () => new Date().getFullYear();

export function Bm04ProvinceScreen() {
  const { api } = useAuth();
  const capi = useMemo(() => (api ? createCntsPeriodApi(api) : null), [api]);
  const [year, setYear] = useState(thisYear());
  const tasks = useLoad(capi ? () => capi.tasksIn(BM04_CODE, `${year}-01-01`, `${year}-12-31`) : null, [capi, year]);
  // Xã của tỉnh kèm khu vực (Xem trước "Tất cả xã" — không ghi gì); không lấy được thì bỏ lọc khu vực.
  const regions = useLoad(
    capi
      ? async () => {
          try {
            const today = new Date().toISOString().slice(0, 10);
            return (await capi.preview({ code: BM04_CODE, from_date: today, to_date: today, target: "province", units: [] })).communes ?? [];
          } catch {
            return [];
          }
        }
      : null,
    [capi],
  );
  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader
        title="Nhập liệu tuần (BM-04) — số liệu cấp xã"
        actions={
          <button className="btn btn-sm" onClick={tasks.reload} disabled={tasks.loading}>
            Tải lại
          </button>
        }
      >
        <span className="mute">
          Số liệu chăn nuôi tuần các xã gửi (đọc trực tiếp, chỉ xem). Người phụ trách biểu xác thực hoặc yêu cầu điều chỉnh.{" "}
          <Link href="/kn/cnts/bieu-mau">Phát hành, theo dõi nộp, nhắc xã</Link>
        </span>
      </PageHeader>
      <LoadState s={tasks}>
        {(ts) => (
          <Body
            key={year}
            data={bm04Data(ts.filter((t) => (t.period_key || "").startsWith(`${year}-`) || (t.period_to ?? "").startsWith(`${year}-`)))}
            year={year}
            setYear={setYear}
            regionOf={new Map((regions.data ?? []).map((c) => [c.commune_code, { code: c.region_code ?? "", name: c.region_name ?? "" }]))}
            capi={capi}
            reload={tasks.reload}
          />
        )}
      </LoadState>
    </div>
  );
}

function defaultWeek(d: Bm04Data): string {
  const today = new Date().toISOString().slice(0, 10);
  const past = d.weeks.filter((w) => w.from <= today);
  return (past.at(-1) ?? d.weeks.at(-1))?.key ?? "";
}

function Body({
  data,
  year,
  setYear,
  regionOf,
  capi,
  reload,
}: {
  data: Bm04Data;
  year: number;
  setYear: (y: number) => void;
  regionOf: Map<string, { code: string; name: string }>;
  capi: CntsPeriodApi;
  reload: () => void;
}) {
  const { kn, principal } = useAuth();
  const [week, setWeek] = useState(() => defaultWeek(data));
  const [region, setRegion] = useState("");
  const [commune, setCommune] = useState("");
  const [status, setStatus] = useState<TaskStatus | "">("");
  const [mode, setMode] = useState<Mode>("week");
  const [open, setOpen] = useState<string | null>(null);
  const name = useCommuneNames(data.units);
  const weekTasks = data.units.map((u) => data.cell(week, u)).filter((t): t is FormsTask => !!t);
  const periodIds = [...new Set(weekTasks.map((t) => t.period_id))];
  const tracking = useLoad(periodIds.length ? () => capi.tracking(BM04_CODE, periodIds) : null, [capi, periodIds.join(",")]);
  const ownerOf = new Map((tracking.data?.periods ?? []).map((p) => [p.period_id, p]));
  const regionList = [...new Map([...regionOf.values()].filter((r) => r.code).map((r) => [r.code, r.name])).entries()];
  const rows = data.units.filter((u) => {
    const t = data.cell(week, u);
    if (!t) return false;
    if (region && regionOf.get(u)?.code !== region) return false;
    if (commune && u !== commune) return false;
    return !status || t.status === status;
  });
  const fields = mode === "week" ? data.fields : data.fields.filter((f) => isFlow(f.key));
  const cum = mode === "week" ? null : new Map(rows.map((u) => [u, cumulative(data, u, week, mode)]));
  const valueOf = (u: string, key: string) => (cum ? (cum.get(u)?.values[key] ?? null) : cellValue(data.cell(week, u), key));
  const years = [thisYear() + 1, thisYear(), thisYear() - 1, thisYear() - 2];
  const me = principal?.subject ?? "";
  const tinh = kn.context?.role === "kn_tinh";
  const cur = open ? data.cell(week, open) : undefined;
  const owner = cur ? ownerOf.get(cur.period_id) : undefined;
  return (
    <>
      <div className="toolbar" role="group" aria-label="Bộ lọc">
        <select className="select" aria-label="Năm" value={year} onChange={(e) => setYear(Number(e.target.value))}>
          {years.map((y) => (
            <option key={y} value={y}>
              Năm {y}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Tuần" value={week} onChange={(e) => (setWeek(e.target.value), setOpen(null))}>
          {[...data.weeks].reverse().map((w) => (
            <option key={w.key} value={w.key}>
              {w.label}
            </option>
          ))}
        </select>
        {regionList.length ? (
          <select className="select" aria-label="Khu vực" value={region} onChange={(e) => setRegion(e.target.value)}>
            <option value="">Mọi khu vực</option>
            {regionList.map(([c, n]) => (
              <option key={c} value={c}>
                {n}
              </option>
            ))}
          </select>
        ) : null}
        <select className="select" aria-label="Xã" value={commune} onChange={(e) => setCommune(e.target.value)}>
          <option value="">Mọi xã</option>
          {data.units.map((u) => (
            <option key={u} value={u}>
              {name(u)}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Trạng thái nộp" value={status} onChange={(e) => setStatus(e.target.value as TaskStatus | "")}>
          <option value="">Mọi trạng thái</option>
          {(Object.keys(STATUS_LABEL) as TaskStatus[]).map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Hiển thị" value={mode} onChange={(e) => setMode(e.target.value as Mode)}>
          {(Object.keys(MODE_LABEL) as Mode[]).map((m) => (
            <option key={m} value={m}>
              {MODE_LABEL[m]}
            </option>
          ))}
        </select>
      </div>
      {!data.weeks.length ? (
        <p className="empty">Chưa có kỳ BM-04 (nhập liệu tuần chăn nuôi) được phát hành trong năm {year} cho xã trong phạm vi.</p>
      ) : (
        <section className="card sec">
          <h2>
            {data.weeks.find((w) => w.key === week)?.label} · {MODE_LABEL[mode]}
          </h2>
          {mode !== "week" ? (
            <p className="mute">
              Lũy kế = tổng các tuần có số liệu hợp lệ tới tuần đang xem (BR-01); tuần chưa nộp tính 0 và hiện &quot;Thiếu kỳ&quot;; tuần bị yêu cầu điều chỉnh
              chưa cộng. Chỉ cột phát sinh trong tuần (tiêm phòng, hóa chất, KSGM) có lũy kế.
            </p>
          ) : null}
          <div className="table-scroll">
            <table className="dt" aria-label="Số liệu BM-04 theo xã">
              <thead>
                <tr>
                  <th rowSpan={2}>STT</th>
                  <th rowSpan={2}>Xã, phường</th>
                  <th rowSpan={2}>Trạng thái</th>
                  {columnGroups(fields).map((g, i) => (
                    <th key={`${g.group}-${i}`} colSpan={g.span} className="num">
                      {g.group}
                    </th>
                  ))}
                  {mode !== "week" ? <th rowSpan={2}>Thiếu kỳ</th> : null}
                  <th rowSpan={2} />
                </tr>
                <tr>
                  {fields.map((f) => (
                    <th key={f.key} className="num">
                      {f.label}
                      {f.unit ? ` (${f.unit})` : ""}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.length ? (
                  rows.map((u, i) => {
                    const t = data.cell(week, u)!;
                    const c = cum?.get(u);
                    return (
                      <tr key={u} className={open === u ? "selected" : undefined}>
                        <td>{i + 1}</td>
                        <td>{name(u)}</td>
                        <td>
                          <StatusPill tone={STATUS_TONE[t.status]}>{STATUS_LABEL[t.status]}</StatusPill>
                          {t.late ? <span className="mute"> · nộp trễ</span> : null}
                          {t.nil_report ? <span className="mute"> · không phát sinh</span> : null}
                        </td>
                        {fields.map((f) => (
                          <td key={f.key} className="num">
                            {num(valueOf(u, f.key), f.type === "decimal" ? 3 : 0)}
                          </td>
                        ))}
                        {c ? (
                          <td>
                            {c.missing.length ? <span className="warn">Thiếu kỳ {c.missing.map((w) => weekNo(w.key)).join(", ")}</span> : "—"}
                            {c.returned.length ? <span className="mute"> · chờ điều chỉnh {c.returned.map((w) => weekNo(w.key)).join(", ")}</span> : null}
                          </td>
                        ) : null}
                        <td>
                          <button className="btn btn-sm" aria-label={`Chi tiết ${name(u)}`} onClick={() => setOpen(open === u ? null : u)}>
                            {open === u ? "Đóng" : "Chi tiết"}
                          </button>
                        </td>
                      </tr>
                    );
                  })
                ) : (
                  <tr>
                    <td colSpan={fields.length + 4 + (mode !== "week" ? 1 : 0)} className="empty">
                      Không có xã khớp bộ lọc.
                    </td>
                  </tr>
                )}
                {rows.length ? (
                  <tr className="row-sub">
                    <td colSpan={3}>
                      <strong>TỔNG</strong>
                    </td>
                    {fields.map((f) => (
                      <td key={f.key} className="num">
                        <strong>{num(columnTotal(rows.map((u) => valueOf(u, f.key))), f.type === "decimal" ? 3 : 0)}</strong>
                      </td>
                    ))}
                    {mode !== "week" ? <td /> : null}
                    <td />
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {open && cur ? (
        <Detail
          key={`${cur.id}:${cur.version}:${cur.status}`}
          task={cur}
          unitName={name(open)}
          fields={data.fields}
          capi={capi}
          ownerName={owner?.owner_name ?? null}
          canReview={tinh && !!tracking.data && (owner?.owner_ref ? owner.owner_ref === me : true)}
          isOwnerSet={!!owner?.owner_ref}
          onReviewed={reload}
        />
      ) : null}
    </>
  );
}

const weekNo = (key: string) => /-W(\d+)$/.exec(key)?.[1]?.replace(/^0/, "") ?? key;

function Detail({
  task,
  unitName,
  fields,
  capi,
  ownerName,
  canReview,
  isOwnerSet,
  onReviewed,
}: {
  task: FormsTask;
  unitName: string;
  fields: Bm04Data["fields"];
  capi: CntsPeriodApi;
  ownerName: string | null;
  canReview: boolean;
  isOwnerSet: boolean;
  onReviewed: () => void;
}) {
  const subs = useLoad(task.version > 0 ? () => capi.submissions(task.period_id, task.unit) : null, [capi, task.period_id, task.unit, task.version]);
  const act = useAction();
  const [returning, setReturning] = useState(false);
  const reviewable = canReview && task.status === "submitted" && !!task.submission_id && (task.period_status === "open" || task.period_status === "reopened");
  const all = task.fields.filter((f) => f.type === "text");
  return (
    <section className="card sec" aria-label={`Chi tiết ${unitName}`}>
      <h2>
        {unitName} · {task.label} <StatusPill tone={STATUS_TONE[task.status]}>{STATUS_LABEL[task.status]}</StatusPill>
      </h2>
      <p className="mute">
        Hạn nộp {dt(task.due_at)}
        {task.version > 0 ? ` · phiên bản ${task.version}, gửi lúc ${dt(task.submitted_at)}` : " · chưa gửi"}
        {task.late ? " · nộp trễ" : ""} · Người phụ trách biểu: {ownerName ?? (isOwnerSet ? "—" : "chưa phân công")}
      </p>
      {act.view}
      {task.return_note ? <p className="alert alert-warn">Yêu cầu điều chỉnh: {task.return_note}</p> : null}
      {subs.data && editedAfterAccept(subs.data) ? (
        <p className="alert alert-warn" role="status">
          <strong>Đã sửa sau xác thực</strong> — bản đã xác thực được xã sửa và gửi lại; báo cáo về trạng thái Đã nộp, chờ xác thực lại.
        </p>
      ) : null}
      {task.version > 0 ? (
        <table className="dt" aria-label="Số liệu tuần của xã">
          <thead>
            <tr>
              <th>Nhóm</th>
              <th>Chỉ tiêu</th>
              <th className="num">Số liệu</th>
            </tr>
          </thead>
          <tbody>
            {fields.map((f) => (
              <tr key={f.key}>
                <td>{f.group}</td>
                <td>
                  {f.label}
                  {f.unit ? ` (${f.unit})` : ""}
                </td>
                <td className="num">{num(cellValue(task, f.key), f.type === "decimal" ? 3 : 0)}</td>
              </tr>
            ))}
            {all.map((f) => (
              <tr key={f.key}>
                <td colSpan={2}>{f.label}</td>
                <td>{task.values[f.key] || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="mute">Xã chưa gửi số liệu tuần này.</p>
      )}
      {task.note ? <p>H08 Ghi chú: {task.note}</p> : null}
      {task.explanation ? <p>H09 Giải trình / lý do sửa sau hạn: {task.explanation}</p> : null}
      {reviewable ? (
        returning ? (
          <ReasonBox
            label="Yêu cầu điều chỉnh"
            busy={act.busy}
            onCancel={() => setReturning(false)}
            onConfirm={async (reason) => {
              if (await act.run(() => capi.review(task.submission_id!, "returned", reason), "Đã yêu cầu xã điều chỉnh.")) {
                setReturning(false);
                onReviewed();
              }
            }}
          />
        ) : (
          <div className="row">
            <button
              className="btn btn-primary"
              disabled={act.busy}
              onClick={async () => {
                if (await act.run(() => capi.review(task.submission_id!, "accepted"), "Đã xác thực số liệu tuần của xã.")) onReviewed();
              }}
            >
              Xác thực
            </button>
            <button className="btn" disabled={act.busy} onClick={() => setReturning(true)}>
              Yêu cầu điều chỉnh
            </button>
          </div>
        )
      ) : task.status === "submitted" && !canReview ? (
        <p className="mute">Chỉ người phụ trách biểu xác thực hoặc yêu cầu điều chỉnh; bạn chỉ xem.</p>
      ) : null}
      {task.version > 0 ? (
        <LoadState s={subs}>
          {(list) => {
            const old = [...list].reverse();
            return (
              <details open>
                <summary>Lịch sử phiên bản gửi ({old.length})</summary>
                <ol className="list-plain">
                  {old.map((s, i) => {
                    const ch = changesBetween(fields, old[i - 1], s);
                    return (
                      <li key={s.id}>
                        <strong>Phiên bản {s.version}</strong> · gửi lúc {dt(s.event_time)}
                        {s.late ? " · trễ hạn" : ""}
                        {s.nil_report ? " · không phát sinh" : ""}
                        {s.review ? ` · ${s.review.decision === "accepted" ? "Đã xác thực" : "Yêu cầu điều chỉnh"} lúc ${dt(s.review.event_time)}` : ""}
                        {s.review?.note ? ` (${s.review.note})` : ""}
                        {s.explanation ? <div className="mute">Lý do: {s.explanation}</div> : null}
                        {i > 0 ? (
                          ch.length ? (
                            <ul>
                              {ch.map((c) => (
                                <li key={c.key}>
                                  {c.label}: {c.old ?? "—"} → {c.new ?? "—"}
                                </li>
                              ))}
                            </ul>
                          ) : (
                            <div className="mute">Không đổi số liệu.</div>
                          )
                        ) : null}
                      </li>
                    );
                  })}
                </ol>
              </details>
            );
          }}
        </LoadState>
      ) : null}
    </section>
  );
}
