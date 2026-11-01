"use client";

// Lịch trực KSGM BM-03 (W11.46, BRD CNTS v3.7 UR-TINH-10, FN-15, UR-XA-03):
// - Cấp Tỉnh (cán bộ tổng hợp), Admin: lưới tháng cơ sở × viên chức × ngày, bấm ô đổi ca N → Đ → NĐ → trống, sao chép
//   tháng trước, lưu thành phiên bản mới (khóa lạc quan), cảnh báo không chặn.
// - BF-CN-DUTY (#171, CN-14): cột Địa chỉ, ĐT viên chức (SĐT che, chỉ người lập lịch); tô ngày cơ sở chưa có người trực;
//   cảnh báo "chưa cấp quyền" có liên kết sang Cấp quyền; nạp Excel đúng mẫu "Lịch trực ksgm" (xem trước kèm lỗi theo
//   dòng, áp vào lưới rồi Lưu với source=excel); xuất .xlsx đúng mẫu (tiêu đề tháng, dòng căn cứ, ghi chú ca).
// - Cấp Xã: "Lịch trực của tôi" — ca của mình trong tháng và người trực cùng.
import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import { readUpload, saveBlob } from "@/lib/files";
import { collectPages } from "@/api/pagination";
import { ProblemAlert } from "@/components/ProblemAlert";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  SHIFT_LABEL,
  SHIFT_TITLE,
  cellKey,
  createCntsCalApi,
  currentMonth,
  fromGrid,
  grantHref,
  importFileError,
  issueText,
  nextShift,
  rowsOf,
  shiftMonth,
  toGrid,
  uncoveredDays,
  weekdayLabel,
  type CntsCalApi,
  type DutyRoster,
  type DutyRosterImport,
  type Shift,
} from "./api";

const BASIS_KEY = "kn.cnts.duty.basis";
function loadBasis(): string {
  try {
    return localStorage.getItem(BASIS_KEY) ?? "";
  } catch {
    return "";
  }
}
function storeBasis(v: string) {
  try {
    localStorage.setItem(BASIS_KEY, v);
  } catch {
    // trình duyệt chặn lưu trữ: chỉ mất phần ghi nhớ
  }
}

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
            <DataTable
              caption="Ca trực của tôi"
              rows={d.items}
              rowKey={(x) => `${x.date}-${x.facility.ref}`}
              columns={[
                { key: "date", header: "Ngày", cell: (x) => `${weekdayLabel(month, Number(x.date.slice(8)))} ${x.date.split("-").reverse().join("/")}` },
                {
                  key: "shift",
                  header: "Ca",
                  cell: (x) => (
                    <span title={SHIFT_TITLE[x.shift]}>
                      <StatusPill tone={x.shift === "D" ? "info" : x.shift === "ND" ? "warn" : "ok"}>{SHIFT_LABEL[x.shift]}</StatusPill>
                    </span>
                  ),
                },
                { key: "fac", header: "Cơ sở giết mổ", cell: (x) => x.facility.name ?? x.facility.ref },
                { key: "with", header: "Trực cùng", cell: (x) => x.colleagues.map((c) => c.display ?? c.ref.slice(0, 8)).join(", ") || "—" },
              ]}
            />
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
  const [source, setSource] = useState<"grid" | "copy" | "excel">("grid");
  const [note, setNote] = useState("");
  const [addF, setAddF] = useState("");
  const [addO, setAddO] = useState("");
  const [basis, setBasis] = useState(loadBasis);
  const [preview, setPreview] = useState<DutyRosterImport | null>(null);
  const [fileErr, setFileErr] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const act = useAction();
  const editable = roster.can_edit && !readOnly;
  const communeName = useCommuneNames(roster.facilities.map((f) => f.commune_code));
  const days = Array.from({ length: roster.days_in_month }, (_, i) => i + 1);
  const rows = rowsOf(fromGrid(grid, roster.days_in_month), extra).sort(
    (a, b) => roster.facilities.findIndex((f) => f.ref === a[0]) - roster.facilities.findIndex((f) => f.ref === b[0]),
  );
  const fac = (ref: string) => roster.facilities.find((f) => f.ref === ref);
  const fname = (ref: string) => fac(ref)?.name ?? ref.slice(0, 8);
  const faddr = (ref: string) => fac(ref)?.address || communeName(fac(ref)?.commune_code ?? "");
  const oname = (ref: string) => roster.officers.find((o) => o.ref === ref)?.display ?? ref.slice(0, 8);
  const ophone = (ref: string) => roster.officers.find((o) => o.ref === ref)?.phone_masked ?? "";
  const warnDays = new Map<string, Set<number>>();
  for (const w of roster.warnings) if (w.code === "double_shift") warnDays.set(`o:${w.officer_ref}`, new Set(w.days));
  const uncovered = uncoveredDays(grid, [...new Set(rows.map(([f]) => f))], roster.days_in_month);
  const phoneCol = roster.can_view_phone;
  const infoCols = phoneCol ? 4 : 3;

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
  const exportXlsx = async () => {
    storeBasis(basis);
    const r = await act.run(() => capi.exportRoster(roster.month, roster.province_code ?? "legacy", basis));
    if (r) saveBlob(r.blob, r.name);
  };
  const pickFile = async (f: File | undefined) => {
    setFileErr(null);
    setPreview(null);
    if (!f) return;
    const up = await readUpload(f);
    const err = importFileError(up);
    if (err) {
      setFileErr(err);
      return;
    }
    const r = await act.run(() => capi.importRoster(roster.month, up, roster.province_code ?? undefined));
    if (r) setPreview(r);
  };
  const applyImport = (p: DutyRosterImport) => {
    setGrid(toGrid(p.cells));
    setExtra([]);
    setSource("excel");
    setDirty(true);
    setPreview(null);
    act.setNote(`Đã áp dụng ${p.cells.length} ô từ tệp Excel (sheet "${p.sheet}")${p.issues.length ? `, bỏ ${p.issues.length} lỗi` : ""} — kiểm tra rồi bấm Lưu.`);
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
              <button className="btn btn-sm" onClick={() => fileRef.current?.click()} disabled={act.busy}>
                Nạp Excel
              </button>
              <input
                ref={fileRef}
                type="file"
                hidden
                aria-label="Tệp Excel lịch trực"
                accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                onChange={(e) => {
                  void pickFile(e.target.files?.[0]);
                  e.target.value = "";
                }}
              />
              <input aria-label="Ghi chú lần lưu" placeholder="Ghi chú (tùy chọn)" value={note} onChange={(e) => setNote(e.target.value)} maxLength={1000} />
              <button className="btn btn-primary btn-sm" onClick={save} disabled={act.busy || !dirty}>
                Lưu
              </button>
            </>
          ) : null}
        </div>
        <div className="row">
          <input
            aria-label="Dòng căn cứ (xuất Excel)"
            placeholder="Dòng căn cứ, vd: theo thông báo số …/SNNMT ngày …"
            value={basis}
            onChange={(e) => setBasis(e.target.value)}
            maxLength={300}
            style={{ flex: 1, minWidth: 0 }}
          />
          <button className="btn btn-sm" onClick={exportXlsx} disabled={act.busy || dirty} title={dirty ? "Lưu thay đổi trước khi xuất" : "Xuất bản đã lưu theo mẫu Lịch trực KSGM"}>
            Xuất Excel
          </button>
        </div>
        {fileErr ? (
          <p className="err" role="alert">
            {fileErr}
          </p>
        ) : null}
        {roster.warnings.length ? (
          <ul aria-label="Cảnh báo" className="mute">
            {roster.warnings.map((w, i) => (
              <li key={i}>
                <StatusPill tone="warn">Cảnh báo</StatusPill> {w.message}
                {w.facility_ref ? ` — ${fname(w.facility_ref)}` : ""}
                {w.officer_ref ? ` — ${oname(w.officer_ref)}` : ""}
                {w.days.length && w.days.length <= 10 ? ` (ngày ${w.days.join(", ")})` : ""}
                {w.code === "officer_not_granted" && w.officer_ref && w.facility_ref ? (
                  <>
                    {" "}
                    <Link href={grantHref(w.officer_ref, w.facility_ref, fac(w.facility_ref)?.commune_code)}>Cấp quyền</Link>
                  </>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
      </section>

      {preview ? <ImportPreview p={preview} fname={fname} oname={oname} onApply={() => applyImport(preview)} onCancel={() => setPreview(null)} /> : null}

      {/* Lưới ca trực kiểu bảng tính (bấm ô đổi ca) — giữ nguyên như lưới nhập Excel, không đổi sang DataTable. */}
      <section className="card sec" aria-label="Lưới lịch trực">
        <div style={{ overflowX: "auto" }}>
          <table className="table table-compact">
            <thead>
              <tr>
                <th>Cơ sở</th>
                <th>Địa chỉ</th>
                <th>Viên chức</th>
                {phoneCol ? <th>ĐT</th> : null}
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
                  <td colSpan={days.length + infoCols} className="mute">
                    Chưa có viên chức nào trong lưới — thêm cặp cơ sở, viên chức bên dưới.
                  </td>
                </tr>
              ) : null}
              {rows.map(([f, o]) => (
                <tr key={`${f}|${o}`}>
                  <td>{fname(f)}</td>
                  <td>{faddr(f)}</td>
                  <td>{oname(o)}</td>
                  {phoneCol ? <td>{ophone(o) || "—"}</td> : null}
                  {days.map((d) => {
                    const v = grid.get(cellKey(f, o, d));
                    const warn = warnDays.get(`o:${o}`)?.has(d);
                    const empty = uncovered.get(f)?.has(d);
                    return (
                      <td
                        key={d}
                        data-uncovered={empty ? "true" : undefined}
                        style={{ padding: 0, textAlign: "center", background: warn ? "var(--warn-bg, #fff3cd)" : empty ? "var(--bad-bg, #fde2e1)" : undefined }}
                      >
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm"
                          aria-label={`${fname(f)} ${oname(o)} ngày ${d}: ${v ? SHIFT_LABEL[v] : "nghỉ"}${empty ? " (cơ sở chưa có người trực)" : ""}`}
                          title={v ? SHIFT_TITLE[v] : empty ? "Nghỉ — cơ sở chưa có ai trực ngày này" : "Nghỉ"}
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
                {v.source === "copy" ? " · sao chép tháng trước" : v.source === "excel" ? " · nạp Excel" : ""}
                {v.note ? ` · ${v.note}` : ""}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </>
  );
}

const MAX_ISSUES_SHOWN = 50;

/** Xem trước tệp nạp: số dòng/ô đọc được, lỗi theo dòng, cảnh báo như khi lưu; áp vào lưới (chưa lưu). */
function ImportPreview({
  p,
  fname,
  oname,
  onApply,
  onCancel,
}: {
  p: DutyRosterImport;
  fname: (ref: string) => string;
  oname: (ref: string) => string;
  onApply: () => void;
  onCancel: () => void;
}) {
  const rest = p.issues.length - MAX_ISSUES_SHOWN;
  return (
    <section className="card sec" aria-label="Xem trước tệp nạp">
      <h2>Xem trước tệp nạp</h2>
      <p>
        Sheet &quot;{p.sheet}&quot;: {p.row_count} dòng viên chức, {p.cells.length} ô ca trực ghép được.
        {p.basis ? ` Căn cứ trong tệp: ${p.basis}` : ""}
      </p>
      {p.issues.length ? (
        <>
          <p className="err" role="alert">
            {p.issues.length} lỗi — dòng không khớp cơ sở hoặc viên chức bị bỏ cả dòng, ô ký hiệu lạ bị bỏ ô. Sửa tệp rồi nạp lại, hoặc áp dụng phần hợp lệ.
          </p>
          <ul aria-label="Lỗi theo dòng">
            {p.issues.slice(0, MAX_ISSUES_SHOWN).map((i, k) => (
              <li key={k}>{issueText(i)}</li>
            ))}
            {rest > 0 ? <li>… và {rest} lỗi khác</li> : null}
          </ul>
        </>
      ) : (
        <p>Không có lỗi.</p>
      )}
      {p.warnings.length ? (
        <ul aria-label="Cảnh báo của tệp nạp" className="mute">
          {p.warnings.map((w, i) => (
            <li key={i}>
              <StatusPill tone="warn">Cảnh báo</StatusPill> {w.message}
              {w.facility_ref ? ` — ${fname(w.facility_ref)}` : ""}
              {w.officer_ref ? ` — ${oname(w.officer_ref)}` : ""}
            </li>
          ))}
        </ul>
      ) : null}
      <div className="row">
        <button className="btn btn-primary btn-sm" onClick={onApply} disabled={!p.cells.length}>
          {p.issues.length ? `Áp dụng ${p.cells.length} ô hợp lệ vào lưới` : "Áp dụng vào lưới"}
        </button>
        <button className="btn btn-sm" onClick={onCancel}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}
