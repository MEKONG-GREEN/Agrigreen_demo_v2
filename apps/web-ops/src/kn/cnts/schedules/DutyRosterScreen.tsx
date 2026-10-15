"use client";

// Lịch trực KSGM BM-03 (W11.46, BRD CNTS v3.7 UR-TINH-10, FN-15, UR-XA-03):
// - Cấp Tỉnh (cán bộ tổng hợp), Admin: lưới tháng cơ sở × viên chức × ngày, bấm ô đổi ca N → Đ → NĐ → trống, sao chép
//   tháng trước, lưu thành phiên bản mới (khóa lạc quan), cảnh báo không chặn, xuất CSV theo mẫu "Lịch trực ksgm".
// - Cấp Xã: "Lịch trực của tôi" — ca của mình trong tháng và người trực cùng.
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import { ProblemAlert } from "@/components/ProblemAlert";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  SHIFT_LABEL,
  SHIFT_TITLE,
  cellKey,
  createCntsCalApi,
  currentMonth,
  fromGrid,
  nextShift,
  rosterCsv,
  rowsOf,
  shiftMonth,
  toGrid,
  weekdayLabel,
  type CntsCalApi,
  type DutyRoster,
  type Shift,
} from "./api";

export function DutyRosterScreen() {
  const { api, kn } = useAuth();
  const capi = useMemo(() => (api ? createCntsCalApi(api) : null), [api]);
  const role = kn.context?.role;
  const [month, setMonth] = useState(currentMonth());
  const [province, setProvince] = useState("");
  const provinces = useLoad(api && role === "admin" ? () => collectPages(cursor => unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: { cursor, limit: 200 } } }))) : null, [api, role]);
  if (!capi) return null;
  return (
    <div className="page">
      <PageHeader
        title={role === "kn_xa" ? "Lịch trực KSGM của tôi" : "Lịch trực KSGM"}
        actions={
          <div className="row">
            {role === "admin" && <label className="field"><span>Tỉnh của lịch trực</span><select aria-label="Tỉnh của lịch trực" value={province} onChange={e => setProvince(e.target.value)}><option value="">Chọn tỉnh</option>{(provinces.data ?? []).filter(p => p.level === "province").map(p => <option key={p.code} value={p.code}>{p.name}</option>)}<option value="legacy">Lịch chung cũ (chỉ xem)</option></select></label>}
            <button className="btn btn-sm" onClick={() => setMonth(shiftMonth(month, -1))} aria-label="Tháng trước">
              ‹
            </button>
            <input type="month" aria-label="Tháng" value={month} onChange={(e) => e.target.value && setMonth(e.target.value)} />
            <button className="btn btn-sm" onClick={() => setMonth(shiftMonth(month, 1))} aria-label="Tháng sau">
              ›
            </button>
          </div>
        }
      >
        <p className="mute">
          {role === "kn_xa"
            ? "Ca trực của bạn theo lịch Cấp Tỉnh lập. Lịch trực dùng để giao việc và gợi ý người nhập KSGM hằng ngày, không chặn nhập."
            : "N: ca ngày, Đ: ca đêm (qua nửa đêm thuộc ngày bắt đầu ca), NĐ: cả ngày đêm, ô trống: nghỉ. Bấm ô để đổi ca."}
        </p>
      </PageHeader>
      {role === "admin" && provinces.problem && <ProblemAlert problem={provinces.problem} onRetry={provinces.reload} />}
      {role === "admin" && !province ? <p className="muted">Chọn tỉnh để xem hoặc lập lịch trực.</p> : role === "kn_xa" ? <MyDutiesView capi={capi} month={month} /> : <RosterGrid key={`${month}:${province}`} capi={capi} month={month} province={province || undefined} readOnly={!!kn.context?.read_only || province === "legacy"} />}
    </div>
  );
}

function MyDutiesView({ capi, month }: { capi: CntsCalApi; month: string }) {
  const s = useLoad(() => capi.myDuties(month), [capi, month]);
  return (
    <section className="card sec" aria-label="Ca trực của tôi">
      <LoadState s={s}>
        {(d) =>
          d.items.length === 0 ? (
            <p className="mute">{d.version === 0 ? "Tháng này chưa có lịch trực." : "Bạn không có ca trực trong tháng."}</p>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Ngày</th>
                  <th>Ca</th>
                  <th>Cơ sở giết mổ</th>
                  <th>Trực cùng</th>
                </tr>
              </thead>
              <tbody>
                {d.items.map((x) => (
                  <tr key={`${x.date}-${x.facility.ref}`}>
                    <td>
                      {weekdayLabel(month, Number(x.date.slice(8)))} {x.date.split("-").reverse().join("/")}
                    </td>
                    <td title={SHIFT_TITLE[x.shift]}>
                      <StatusPill tone={x.shift === "D" ? "info" : x.shift === "ND" ? "warn" : "ok"}>{SHIFT_LABEL[x.shift]}</StatusPill>
                    </td>
                    <td>{x.facility.name ?? x.facility.ref}</td>
                    <td>{x.colleagues.map((c) => c.display ?? c.ref.slice(0, 8)).join(", ") || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        }
      </LoadState>
    </section>
  );
}

function RosterGrid({ capi, month, province, readOnly }: { capi: CntsCalApi; month: string; province?: string; readOnly: boolean }) {
  const s = useLoad(() => capi.roster(month, province), [capi, month, province]);
  return <LoadState s={s}>{(r) => <RosterEditor key={`${r.month}-${r.version}`} capi={capi} roster={r} readOnly={readOnly} onSaved={s.reload} />}</LoadState>;
}

function RosterEditor({ capi, roster, readOnly, onSaved }: { capi: CntsCalApi; roster: DutyRoster; readOnly: boolean; onSaved: () => void }) {
  const [grid, setGrid] = useState<Map<string, Shift>>(() => toGrid(roster.cells));
  const [extra, setExtra] = useState<[string, string][]>([]);
  const [dirty, setDirty] = useState(false);
  const [source, setSource] = useState<"grid" | "copy">("grid");
  const [note, setNote] = useState("");
  const [addF, setAddF] = useState("");
  const [addO, setAddO] = useState("");
  const act = useAction();
  const editable = roster.can_edit && !readOnly;
  const communeName = useCommuneNames(roster.facilities.map((f) => f.commune_code));
  const days = Array.from({ length: roster.days_in_month }, (_, i) => i + 1);
  const rows = rowsOf(fromGrid(grid, roster.days_in_month), extra).sort(
    (a, b) => roster.facilities.findIndex((f) => f.ref === a[0]) - roster.facilities.findIndex((f) => f.ref === b[0]),
  );
  const fname = (ref: string) => roster.facilities.find((f) => f.ref === ref)?.name ?? ref.slice(0, 8);
  const oname = (ref: string) => roster.officers.find((o) => o.ref === ref)?.display ?? ref.slice(0, 8);
  const warnDays = new Map<string, Set<number>>();
  for (const w of roster.warnings) {
    const k = w.code === "double_shift" ? `o:${w.officer_ref}` : w.code === "facility_uncovered" ? `f:${w.facility_ref}` : "";
    if (k) warnDays.set(k, new Set(w.days));
  }

  const toggle = (f: string, o: string, d: number) => {
    if (!editable) return;
    const k = cellKey(f, o, d);
    const n = new Map(grid);
    const v = nextShift(grid.get(k));
    if (v) n.set(k, v);
    else n.delete(k);
    setGrid(n);
    setDirty(true);
  };
  const copyPrevious = async () => {
    const prev = await act.run(() => capi.roster(shiftMonth(roster.month, -1), roster.province_code ?? "legacy"));
    if (!prev) return;
    setGrid(toGrid(prev.cells.filter((c) => c.day <= roster.days_in_month)));
    setSource("copy");
    setDirty(true);
    act.setNote(`Đã chép ${prev.cells.length} ô của tháng ${prev.month} — kiểm tra rồi bấm Lưu.`);
  };
  const save = async () => {
    const r = await act.run(
      () => capi.saveRoster(roster.month, { province_code: roster.province_code ?? undefined, base_version: roster.version, cells: fromGrid(grid, roster.days_in_month), source, ...(note.trim() ? { note: note.trim() } : {}) }),
      (x) => `Đã lưu phiên bản ${x.version}${x.notified_officer_refs.length ? ` — đã thông báo ${x.notified_officer_refs.length} viên chức có ca thay đổi` : ""}.`,
    );
    if (r) {
      setExtra([]);
      setNote("");
      onSaved();
    }
  };
  const exportCsv = () => {
    const blob = new Blob(["﻿" + rosterCsv(roster, grid, rows)], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `lich-truc-ksgm-${roster.month}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  return (
    <>
      {act.view}
      <section className="card sec" aria-label="Thông tin lịch">
        <div className="row">
          <span>
            {roster.version === 0 ? "Chưa lập lịch tháng này." : `Phiên bản ${roster.version} — cập nhật ${dt(roster.updated_at)}`}
            {dirty ? " · Có thay đổi chưa lưu" : ""}
          </span>
          <span className="spacer" />
          {editable ? (
            <>
              <button className="btn btn-sm" onClick={copyPrevious} disabled={act.busy}>
                Sao chép tháng trước
              </button>
              <input aria-label="Ghi chú lần lưu" placeholder="Ghi chú (tùy chọn)" value={note} onChange={(e) => setNote(e.target.value)} maxLength={1000} />
              <button className="btn btn-primary btn-sm" onClick={save} disabled={act.busy || !dirty}>
                Lưu
              </button>
            </>
          ) : null}
          <button className="btn btn-sm" onClick={exportCsv}>
            Xuất CSV
          </button>
        </div>
        {roster.warnings.length ? (
          <ul aria-label="Cảnh báo" className="mute">
            {roster.warnings.map((w, i) => (
              <li key={i}>
                <StatusPill tone="warn">Cảnh báo</StatusPill> {w.message}
                {w.facility_ref ? ` — ${fname(w.facility_ref)}` : ""}
                {w.officer_ref ? ` — ${oname(w.officer_ref)}` : ""}
                {w.days.length && w.days.length <= 10 ? ` (ngày ${w.days.join(", ")})` : ""}
              </li>
            ))}
          </ul>
        ) : null}
      </section>

      <section className="card sec" aria-label="Lưới lịch trực">
        <div style={{ overflowX: "auto" }}>
          <table className="table table-compact">
            <thead>
              <tr>
                <th>Cơ sở</th>
                <th>Viên chức</th>
                {days.map((d) => (
                  <th key={d} title={weekdayLabel(roster.month, d)}>
                    {d}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={days.length + 2} className="mute">
                    Chưa có viên chức nào trong lưới — thêm cặp cơ sở, viên chức bên dưới.
                  </td>
                </tr>
              ) : null}
              {rows.map(([f, o]) => (
                <tr key={`${f}|${o}`}>
                  <td title={communeName(roster.facilities.find((x) => x.ref === f)?.commune_code ?? "")}>{fname(f)}</td>
                  <td>{oname(o)}</td>
                  {days.map((d) => {
                    const v = grid.get(cellKey(f, o, d));
                    const warn = warnDays.get(`o:${o}`)?.has(d);
                    return (
                      <td key={d} style={{ padding: 0, textAlign: "center", background: warn ? "var(--warn-bg, #fff3cd)" : undefined }}>
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm"
                          aria-label={`${fname(f)} ${oname(o)} ngày ${d}: ${v ? SHIFT_LABEL[v] : "nghỉ"}`}
                          title={v ? SHIFT_TITLE[v] : "Nghỉ"}
                          onClick={() => toggle(f, o, d)}
                          disabled={!editable}
                        >
                          {v ? SHIFT_LABEL[v] : "·"}
                        </button>
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {editable ? (
          <div className="row">
            <select aria-label="Cơ sở" value={addF} onChange={(e) => setAddF(e.target.value)}>
              <option value="">— Cơ sở giết mổ —</option>
              {roster.facilities.map((f) => (
                <option key={f.ref} value={f.ref}>
                  {f.name} ({communeName(f.commune_code)})
                </option>
              ))}
            </select>
            <select aria-label="Viên chức" value={addO} onChange={(e) => setAddO(e.target.value)}>
              <option value="">— Viên chức —</option>
              {roster.officers.map((o) => (
                <option key={o.ref} value={o.ref}>
                  {o.display ?? o.ref.slice(0, 8)}
                </option>
              ))}
            </select>
            <button
              className="btn btn-sm"
              disabled={!addF || !addO}
              onClick={() => {
                setExtra([...extra, [addF, addO]]);
                setAddO("");
              }}
            >
              Thêm hàng
            </button>
          </div>
        ) : null}
      </section>

      {roster.versions.length ? (
        <section className="card sec" aria-label="Lịch sử lưu">
          <h2>Lịch sử lưu</h2>
          <ul>
            {roster.versions.map((v) => (
              <li key={v.version}>
                Phiên bản {v.version} · {dt(v.at)} · {v.actor_display ?? v.actor_ref.slice(0, 8)} · {v.cell_count} ô
                {v.source === "copy" ? " · sao chép tháng trước" : ""}
                {v.note ? ` · ${v.note}` : ""}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </>
  );
}
