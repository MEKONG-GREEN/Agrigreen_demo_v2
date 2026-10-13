"use client";

// FN-TI-02.02 — Kế hoạch năm các chương trình (BRD KHTC v2.2; W11.61, pf/extension). Chọn năm; thanh 6 tab; cây CT/DA →
// nhóm → hạng mục; Phòng KHTC (và Admin) sửa trên bản nháp rồi "Lưu" → phiên bản 1 (ngày hiệu lực mặc định 01/01) hoặc
// "Điều chỉnh theo QĐ" (số, ngày QĐ, ngày hiệu lực bắt buộc) → phiên bản mới; phiên bản cũ xem lại được. "Sao chép từ
// năm trước", "Xuất Excel", "Nhập từ Excel" (thành bản nháp). Danh sách CT/DA chủ (D20) thêm/ngừng ngay trên màn.
// Phòng chuyên môn, lãnh đạo chỉ xem.
import { useMemo, useState, type ChangeEvent } from "react";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { readUpload, saveBlob } from "@/lib/files";
import {
  DEPT_LABEL,
  ENTRY_KIND_LABEL,
  PROGRAM_KINDS,
  PROGRAM_KIND_LABEL,
  TABS,
  TAB_KIND,
  TAB_LABEL,
  blankEntry,
  childrenOf,
  dmy,
  isDecimal,
  normDecimal,
  removeSubtree,
  toSave,
  treeRows,
  useKhtcApi,
  useKhtcRights,
  vnDecimal,
  type KhtcCatalogItem,
  type KhtcPlan,
  type KhtcPlanEntry,
  type KhtcProgram,
  type KhtcProgramKind,
  type KhtcTab,
} from "./api";

function vnYear(): number {
  return new Date(Date.now() + 7 * 3_600_000).getUTCFullYear();
}

interface Loaded {
  plan: KhtcPlan | null;
  programs: KhtcProgram[];
  catalog: KhtcCatalogItem[];
}

export function KhtcPlanScreen() {
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const [year, setYear] = useState(vnYear());
  const [version, setVersion] = useState<number | undefined>(undefined);
  const s = useLoad<Loaded>(
    async () => {
      const [plan, programs, catalog] = await Promise.all([api.plan(year, version), api.programs(true), api.catalog()]);
      return { plan, programs: programs.items, catalog: catalog.items };
    },
    [year, version],
  );
  return (
    <div>
      <PageHeader title="Kế hoạch năm các chương trình">
        Lập, điều chỉnh kế hoạch năm theo quyết định; gán phòng phụ trách cho từng hạng mục. Phiên bản cũ giữ nguyên, xem lại được.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12 }}>
        <button type="button" className="btn btn-o btn-sm" onClick={() => (setYear(year - 1), setVersion(undefined))} aria-label="Năm trước">
          ‹
        </button>
        <strong aria-live="polite">Năm {year}</strong>
        <button type="button" className="btn btn-o btn-sm" onClick={() => (setYear(year + 1), setVersion(undefined))} aria-label="Năm sau">
          ›
        </button>
      </div>
      {!rights.plan ? <ReadOnlyNotice>Chỉ xem — Phòng Kế hoạch – Tài chính lập và điều chỉnh kế hoạch năm.</ReadOnlyNotice> : null}
      <LoadState s={s}>
        {(d) => (
          <Editor
            key={`${year}:${d.plan?.version.version ?? 0}`}
            year={year}
            data={d}
            canEdit={rights.plan && (!version || version === d.plan?.latest_version)}
            onVersion={setVersion}
            reload={s.reload}
          />
        )}
      </LoadState>
    </div>
  );
}

type Dlg =
  | { t: "entry"; entry: KhtcPlanEntry; isNew: boolean }
  | { t: "program" }
  | { t: "newProgram" }
  | { t: "save" }
  | { t: "versions" };

function Editor({
  year,
  data,
  canEdit,
  onVersion,
  reload,
}: {
  year: number;
  data: Loaded;
  canEdit: boolean;
  onVersion: (v: number | undefined) => void;
  reload: () => void;
}) {
  const api = useKhtcApi();
  const [entries, setEntries] = useState<KhtcPlanEntry[]>(data.plan?.entries ?? []);
  const [copiedFrom, setCopiedFrom] = useState<number | undefined>(undefined);
  const [dirty, setDirty] = useState(false);
  const [tab, setTab] = useState<KhtcTab>("programs");
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const [problems, setProblems] = useState<{ row: number | null; field: string; reason: string }[]>([]);
  const [programs, setPrograms] = useState(data.programs);
  const act = useAction();
  const plan = data.plan;
  const rows = useMemo(() => treeRows(entries, tab), [entries, tab]);
  const units = data.catalog.filter((c) => c.kind === "unit" && c.active);
  const funding = data.catalog.filter((c) => c.kind === "funding_source" && c.active);
  const progByRef = new Map(programs.map((p) => [p.ref, p]));

  const change = (next: KhtcPlanEntry[]) => {
    setEntries(next);
    setDirty(true);
  };
  const upsert = (e: KhtcPlanEntry) => change(entries.some((x) => x.ref === e.ref) ? entries.map((x) => (x.ref === e.ref ? e : x)) : [...entries, e]);
  const addChild = (parent: KhtcPlanEntry, kind: "group" | "item") =>
    setDlg({ t: "entry", isNew: true, entry: blankEntry(tab, kind, parent, (childrenOf(entries, parent.ref).length + 1) * 10) });

  const copyPrev = async () => {
    const d = await act.run(() => api.copyDraft(year), (r) => `Đã chép ${r.entries.length} dòng còn theo dõi của năm ${r.from_year} (bản nháp — chưa lưu)`);
    if (d) {
      setCopiedFrom(year - 1);
      change(d.entries);
    }
  };
  const exportXlsx = async () => {
    const f = await act.run(() => api.exportXlsx(year, plan?.version.version));
    if (f) saveBlob(f.blob, f.name);
  };
  const importXlsx = async (ev: ChangeEvent<HTMLInputElement>) => {
    const file = ev.target.files?.[0];
    ev.target.value = "";
    if (!file) return;
    const up = await readUpload(file);
    const d = await act.run(() => api.importDraft(year, up), (r) => `Đã đọc ${r.entries.length} dòng từ Excel (bản nháp — chưa lưu)`);
    if (d) {
      setProblems(d.problems);
      change(d.entries);
    }
  };

  return (
    <>
      <div className="row" style={{ marginBottom: 8, flexWrap: "wrap", gap: 8 }}>
        {plan ? (
          <>
            <StatusPill tone="info">
              Phiên bản {plan.version.version}/{plan.latest_version}
            </StatusPill>
            <span className="small">
              {plan.version.decision_no ? `QĐ ${plan.version.decision_no} ngày ${dmy(plan.version.decision_date)} · ` : ""}hiệu lực từ{" "}
              {dmy(plan.version.effective_from)}
            </span>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setDlg({ t: "versions" })}>
              Lịch sử phiên bản
            </button>
            <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={exportXlsx}>
              Xuất Excel
            </button>
          </>
        ) : (
          <StatusPill>Năm {year} chưa có kế hoạch</StatusPill>
        )}
        {canEdit ? (
          <>
            {!plan ? (
              <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={copyPrev}>
                Sao chép từ năm trước
              </button>
            ) : null}
            <label className="btn btn-o btn-sm">
              Nhập từ Excel
              <input type="file" accept=".xlsx,.csv" className="sr-only" onChange={importXlsx} aria-label="Chọn tệp Excel kế hoạch năm" />
            </label>
            <button type="button" className="btn btn-p btn-sm" disabled={!dirty || act.busy} onClick={() => setDlg({ t: "save" })}>
              {plan ? "Điều chỉnh theo QĐ" : "Lưu kế hoạch"}
            </button>
            {dirty ? <span className="small mute">Có thay đổi chưa lưu</span> : null}
          </>
        ) : null}
      </div>
      {act.problem ? <ProblemAlert problem={act.problem} /> : null}
      {act.note ? (
        <div className="alert alert-info" role="status">
          {act.note}
        </div>
      ) : null}
      {problems.length ? (
        <div className="alert alert-info" role="note">
          <strong>Lỗi trong tệp nhập:</strong>
          <ul>
            {problems.slice(0, 20).map((p, i) => (
              <li key={i}>
                {p.row ? `Dòng ${p.row}: ` : ""}
                {p.field} — {p.reason}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      <div className="row" role="tablist" aria-label="Nội dung kế hoạch năm" style={{ flexWrap: "wrap", marginBottom: 8 }}>
        {TABS.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={t === tab}
            className={t === tab ? "btn btn-p btn-sm" : "btn btn-o btn-sm"}
            onClick={() => setTab(t)}
          >
            {TAB_LABEL[t]} ({entries.filter((e) => e.tab === t && e.kind === "program").length})
          </button>
        ))}
      </div>
      <div className="card dt-card">
        {canEdit ? (
          <div className="row" style={{ padding: 8 }}>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setDlg({ t: "program" })}>
              Thêm CT/DA vào tab
            </button>
          </div>
        ) : null}
        <div className="tbl-wrap">
          <table className="dt">
            <caption className="sr-only">{TAB_LABEL[tab]}</caption>
            <thead>
              <tr>
                <th scope="col">Số</th>
                <th scope="col">Nội dung</th>
                <th scope="col">ĐVT</th>
                <th scope="col">Khối lượng KH</th>
                <th scope="col">Quy mô</th>
                <th scope="col">Phòng phụ trách</th>
                <th scope="col">Nguồn số liệu</th>
                <th scope="col">Theo dõi</th>
                {canEdit ? <th scope="col">Thao tác</th> : null}
              </tr>
            </thead>
            <tbody>
              {rows.length ? (
                rows.map(({ e, depth }) => (
                  <tr key={e.ref}>
                    <td>{e.roman ?? (e.kind === "program" ? "•" : "")}</td>
                    <td style={{ paddingLeft: 8 + depth * 20, fontWeight: e.kind === "program" ? 600 : undefined }}>
                      <span className="small mute">{ENTRY_KIND_LABEL[e.kind]} · </span>
                      {e.name || (e.program_ref ? progByRef.get(e.program_ref)?.name : "") || "—"}
                    </td>
                    <td>{e.unit_code ?? ""}</td>
                    <td>{e.planned_qty ? vnDecimal(e.planned_qty) : ""}</td>
                    <td>{e.scale_note ?? ""}</td>
                    <td>{e.department_code ? (DEPT_LABEL[e.department_code] ?? e.department_code) : ""}</td>
                    <td>{e.kind === "item" ? (e.data_source === "module" ? `Module (${e.source_key})` : "Phiếu") : ""}</td>
                    <td>
                      {e.tracking === "stopped" ? <StatusPill tone="warn">Ngừng từ {dmy(e.stopped_from)}</StatusPill> : <StatusPill tone="ok">Đang theo dõi</StatusPill>}
                    </td>
                    {canEdit ? (
                      <td className="row">
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setDlg({ t: "entry", entry: e, isNew: false })}>
                          Sửa
                        </button>
                        {e.kind === "program" ? (
                          <button type="button" className="btn btn-o btn-sm" onClick={() => addChild(e, "group")}>
                            + Nhóm
                          </button>
                        ) : null}
                        {e.kind !== "item" ? (
                          <button type="button" className="btn btn-o btn-sm" onClick={() => addChild(e, "item")}>
                            + Hạng mục
                          </button>
                        ) : null}
                        <button type="button" className="btn btn-o btn-sm" onClick={() => change(removeSubtree(entries, e.ref))}>
                          Xóa
                        </button>
                      </td>
                    ) : null}
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={9} className="empty">
                    Tab này chưa có nội dung.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {dlg?.t === "entry" ? (
        <EntryDialog
          entry={dlg.entry}
          isNew={dlg.isNew}
          units={units}
          funding={funding}
          year={year}
          onClose={() => setDlg(null)}
          onDone={(e) => {
            upsert(e);
            setDlg(null);
          }}
        />
      ) : null}
      {dlg?.t === "program" ? (
        <ProgramPicker
          tab={tab}
          programs={programs}
          used={new Set(entries.filter((e) => e.tab === tab && e.kind === "program").map((e) => e.program_ref ?? ""))}
          onNew={() => setDlg({ t: "newProgram" })}
          onClose={() => setDlg(null)}
          onPick={(p) => {
            const e = blankEntry(tab, "program", null, (childrenOf(entries.filter((x) => x.tab === tab), null).length + 1) * 10);
            upsert({ ...e, program_ref: p.ref, program_code: p.code, program_name: p.name, name: p.name });
            setDlg(null);
          }}
        />
      ) : null}
      {dlg?.t === "newProgram" ? (
        <NewProgramDialog
          kind={TAB_KIND[tab] ?? "program"}
          onClose={() => setDlg({ t: "program" })}
          onCreated={(p) => {
            setPrograms([...programs, p]);
            setDlg({ t: "program" });
          }}
        />
      ) : null}
      {dlg?.t === "save" ? (
        <SaveDialog
          year={year}
          adjust={!!plan}
          onClose={() => setDlg(null)}
          onSave={async (head) => {
            const r = await act.run(
              () => api.save(year, toSave(entries, { base_version: plan?.latest_version ?? 0, copied_from_year: copiedFrom, ...head })),
              (p) => `Đã lưu phiên bản ${p.version.version}`,
            );
            setDlg(null);
            if (r) {
              setDirty(false);
              setProblems([]);
              onVersion(undefined);
              reload();
            }
          }}
        />
      ) : null}
      {dlg?.t === "versions" ? <VersionsDialog year={year} onClose={() => setDlg(null)} onPick={(v) => (setDlg(null), onVersion(v))} /> : null}
    </>
  );
}

function EntryDialog({
  entry,
  isNew,
  units,
  funding,
  year,
  onClose,
  onDone,
}: {
  entry: KhtcPlanEntry;
  isNew: boolean;
  units: KhtcCatalogItem[];
  funding: KhtcCatalogItem[];
  year: number;
  onClose: () => void;
  onDone: (e: KhtcPlanEntry) => void;
}) {
  const [e, setE] = useState<KhtcPlanEntry>(entry);
  const [qty, setQty] = useState(entry.planned_qty ? vnDecimal(entry.planned_qty) : "");
  const [budget, setBudget] = useState(entry.budget ?? "");
  const [communes, setCommunes] = useState(entry.communes.join(", "));
  const item = e.kind === "item";
  const q = normDecimal(qty);
  const b = normDecimal(budget);
  const errors: string[] = [];
  if (!e.name.trim()) errors.push("Nhập tên");
  if (!isDecimal(q, 4)) errors.push("Khối lượng KH: số ≥ 0, tối đa 4 chữ số thập phân");
  if (!isDecimal(b, 2)) errors.push("Kinh phí KH: số ≥ 0, tối đa 2 chữ số thập phân");
  if (item && !e.department_code) errors.push("Hạng mục có đúng một phòng phụ trách");
  if (item && e.data_source === "module" && !e.source_key?.trim()) errors.push("Nguồn module cần khóa nguồn");
  if (e.tracking === "stopped" && !e.stopped_from?.startsWith(String(year))) errors.push(`Ngày ngừng trong năm ${year}`);
  const set = (p: Partial<KhtcPlanEntry>) => setE({ ...e, ...p });
  const done = () =>
    onDone({
      ...e,
      planned_qty: q,
      budget: b,
      communes: communes
        .split(/[,;\s]+/)
        .map((c) => c.trim())
        .filter(Boolean),
    });
  const txt = (label: string, k: keyof KhtcPlanEntry, id: string) => (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input id={id} className="input" value={(e[k] as string | null) ?? ""} onChange={(ev) => set({ [k]: ev.target.value || null } as Partial<KhtcPlanEntry>)} />
    </div>
  );
  return (
    <Dialog
      title={`${isNew ? "Thêm" : "Sửa"} ${ENTRY_KIND_LABEL[e.kind].toLowerCase()}`}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={errors.length > 0} onClick={done}>
          Xong
        </button>
      }
    >
      {errors.length ? (
        <p className="small mute" role="status">
          {errors.join(" · ")}
        </p>
      ) : null}
      <div className="field">
        <label htmlFor="ke-name" className="req">
          Tên
        </label>
        <input id="ke-name" className="input" value={e.name} onChange={(ev) => set({ name: ev.target.value })} />
      </div>
      <div className="grid2">
        {txt("Mã / số hiệu", "code", "ke-code")}
        <div className="field">
          <label htmlFor="ke-sort">Thứ tự</label>
          <input id="ke-sort" className="input" inputMode="numeric" value={e.sort_order} onChange={(ev) => set({ sort_order: Number.parseInt(ev.target.value, 10) || 0 })} />
        </div>
      </div>
      {e.kind !== "program" ? (
        <>
          <div className="grid2">
            <div className="field">
              <label htmlFor="ke-unit">ĐVT</label>
              <select id="ke-unit" className="select" value={e.unit_code ?? ""} onChange={(ev) => set({ unit_code: ev.target.value || null })}>
                <option value="">—</option>
                {units.map((u) => (
                  <option key={u.code} value={u.code}>
                    {u.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="ke-qty">Khối lượng KH</label>
              <input id="ke-qty" className="input" inputMode="decimal" value={qty} onChange={(ev) => setQty(ev.target.value)} />
            </div>
          </div>
          {txt("Quy mô (ghi chú)", "scale_note", "ke-scale")}
          <div className="grid2">
            <div className="field">
              <label htmlFor="ke-budget">Kinh phí KH (đồng)</label>
              <input id="ke-budget" className="input" inputMode="decimal" value={budget} onChange={(ev) => setBudget(ev.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="ke-fund">Nguồn kinh phí</label>
              <select id="ke-fund" className="select" value={e.funding_code ?? ""} onChange={(ev) => set({ funding_code: ev.target.value || null })}>
                <option value="">—</option>
                {funding.map((u) => (
                  <option key={u.code} value={u.code}>
                    {u.name}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </>
      ) : null}
      {item ? (
        <>
          <div className="grid2">
            <div className="field">
              <label htmlFor="ke-dept" className="req">
                Phòng phụ trách
              </label>
              <select id="ke-dept" className="select" value={e.department_code ?? ""} onChange={(ev) => set({ department_code: ev.target.value || null })}>
                <option value="">—</option>
                {Object.entries(DEPT_LABEL).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="ke-src">Nguồn số liệu</label>
              <select id="ke-src" className="select" value={e.data_source ?? "form"} onChange={(ev) => set({ data_source: ev.target.value as "form" | "module" })}>
                <option value="form">Phiếu của phòng</option>
                <option value="module">Lấy từ module</option>
              </select>
            </div>
          </div>
          {e.data_source === "module" ? txt("Khóa nguồn module", "source_key", "ke-skey") : null}
          <div className="grid2">
            <div className="field">
              <label htmlFor="ke-communes">Xã/điểm (mã, cách nhau dấu phẩy)</label>
              <input id="ke-communes" className="input" value={communes} onChange={(ev) => setCommunes(ev.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="ke-points">Số điểm</label>
              <input
                id="ke-points"
                className="input"
                inputMode="numeric"
                value={e.points_count ?? ""}
                onChange={(ev) => set({ points_count: ev.target.value === "" ? null : Number.parseInt(ev.target.value, 10) || 0 })}
              />
            </div>
          </div>
        </>
      ) : null}
      <div className="grid2">
        <div className="field">
          <label htmlFor="ke-track">Theo dõi</label>
          <select
            id="ke-track"
            className="select"
            value={e.tracking}
            onChange={(ev) => set({ tracking: ev.target.value as "tracking" | "stopped", stopped_from: ev.target.value === "stopped" ? e.stopped_from : null })}
          >
            <option value="tracking">Đang theo dõi</option>
            <option value="stopped">Ngừng theo dõi (theo QĐ)</option>
          </select>
        </div>
        {e.tracking === "stopped" ? (
          <div className="field">
            <label htmlFor="ke-stop" className="req">
              Ngừng từ ngày
            </label>
            <input id="ke-stop" type="date" className="input" value={e.stopped_from ?? ""} onChange={(ev) => set({ stopped_from: ev.target.value || null })} />
          </div>
        ) : null}
      </div>
      {e.tracking === "stopped" ? txt("Số QĐ ngừng", "stop_decision", "ke-stopqd") : null}
      {txt("Ghi chú", "note", "ke-note")}
    </Dialog>
  );
}

function ProgramPicker({
  tab,
  programs,
  used,
  onPick,
  onNew,
  onClose,
}: {
  tab: KhtcTab;
  programs: KhtcProgram[];
  used: Set<string>;
  onPick: (p: KhtcProgram) => void;
  onNew: () => void;
  onClose: () => void;
}) {
  const kind = TAB_KIND[tab];
  const [all, setAll] = useState(kind === null);
  const list = programs.filter((p) => p.active && !used.has(p.ref) && (all || p.kind === kind));
  return (
    <Dialog
      title={`Thêm CT/DA vào tab "${TAB_LABEL[tab]}"`}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-o btn-sm" onClick={onNew}>
          CT/DA mới…
        </button>
      }
    >
      <p className="small mute">Danh sách CT/DA chủ do Phòng KHTC quản lý — CNTS gắn điểm, hộ vào CT/DA trong danh sách này.</p>
      {kind ? (
        <label className="row small">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> Hiện mọi loại
        </label>
      ) : null}
      <ul>
        {list.map((p) => (
          <li key={p.ref} className="row">
            <button type="button" className="btn btn-o btn-sm" onClick={() => onPick(p)}>
              Chọn
            </button>
            <span>
              {p.code} — {p.name} <span className="small mute">({PROGRAM_KIND_LABEL[p.kind]})</span>
            </span>
          </li>
        ))}
        {!list.length ? <li className="mute">Không còn CT/DA phù hợp — tạo CT/DA mới.</li> : null}
      </ul>
    </Dialog>
  );
}

function NewProgramDialog({ kind, onClose, onCreated }: { kind: KhtcProgramKind; onClose: () => void; onCreated: (p: KhtcProgram) => void }) {
  const api = useKhtcApi();
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [k, setK] = useState<KhtcProgramKind>(kind);
  const act = useAction();
  const save = async () => {
    const p = await act.run(() => api.createProgram({ code, name, kind: k }));
    if (p) onCreated(p);
  };
  return (
    <Dialog
      title="CT/DA mới"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={!code.trim() || !name.trim() || act.busy} onClick={save}>
          Lưu
        </button>
      }
    >
      {act.problem ? <ProblemAlert problem={act.problem} /> : null}
      <div className="field">
        <label htmlFor="np-code" className="req">
          Mã
        </label>
        <input id="np-code" className="input" value={code} onChange={(e) => setCode(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="np-name" className="req">
          Tên
        </label>
        <input id="np-name" className="input" value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="np-kind">Loại</label>
        <select id="np-kind" className="select" value={k} onChange={(e) => setK(e.target.value as KhtcProgramKind)}>
          {PROGRAM_KINDS.map((x) => (
            <option key={x} value={x}>
              {PROGRAM_KIND_LABEL[x]}
            </option>
          ))}
        </select>
      </div>
    </Dialog>
  );
}

function SaveDialog({
  year,
  adjust,
  onClose,
  onSave,
}: {
  year: number;
  adjust: boolean;
  onClose: () => void;
  onSave: (h: { decision_no?: string; decision_date?: string; effective_from?: string; note?: string }) => void;
}) {
  const [no, setNo] = useState("");
  const [date, setDate] = useState("");
  const [eff, setEff] = useState(adjust ? "" : `${year}-01-01`);
  const [note, setNote] = useState("");
  const missing = adjust && (!no.trim() || !date || !eff);
  const badEff = !!eff && !eff.startsWith(String(year));
  return (
    <Dialog
      title={adjust ? "Điều chỉnh theo quyết định" : `Lưu kế hoạch năm ${year} (phiên bản 1)`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={missing || badEff}
          onClick={() => onSave({ decision_no: no || undefined, decision_date: date || undefined, effective_from: eff || undefined, note: note || undefined })}
        >
          Lưu phiên bản
        </button>
      }
    >
      <div className="field">
        <label htmlFor="sv-no" className={adjust ? "req" : undefined}>
          Số quyết định
        </label>
        <input id="sv-no" className="input" value={no} onChange={(e) => setNo(e.target.value)} />
      </div>
      <div className="grid2">
        <div className="field">
          <label htmlFor="sv-date" className={adjust ? "req" : undefined}>
            Ngày quyết định
          </label>
          <input id="sv-date" type="date" className="input" value={date} onChange={(e) => setDate(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="sv-eff" className={adjust ? "req" : undefined}>
            Ngày hiệu lực
          </label>
          <input id="sv-eff" type="date" className="input" value={eff} onChange={(e) => setEff(e.target.value)} />
        </div>
      </div>
      {badEff ? <p className="small mute">Ngày hiệu lực phải trong năm {year}.</p> : null}
      <div className="field">
        <label htmlFor="sv-note">Ghi chú</label>
        <input id="sv-note" className="input" value={note} onChange={(e) => setNote(e.target.value)} />
      </div>
    </Dialog>
  );
}

function VersionsDialog({ year, onClose, onPick }: { year: number; onClose: () => void; onPick: (v: number) => void }) {
  const api = useKhtcApi();
  const s = useLoad(() => api.versions(year), [year]);
  return (
    <Dialog title={`Lịch sử phiên bản — năm ${year}`} onClose={onClose}>
      <LoadState s={s}>
        {(d) => (
          <table className="dt">
            <thead>
              <tr>
                <th scope="col">Phiên bản</th>
                <th scope="col">Quyết định</th>
                <th scope="col">Hiệu lực</th>
                <th scope="col">Số dòng</th>
                <th scope="col"></th>
              </tr>
            </thead>
            <tbody>
              {[...d.items].reverse().map((v) => (
                <tr key={v.id}>
                  <td>v{v.version}</td>
                  <td>{v.decision_no ? `${v.decision_no} (${dmy(v.decision_date)})` : "—"}</td>
                  <td>{dmy(v.effective_from)}</td>
                  <td>{v.entries_count}</td>
                  <td>
                    <button type="button" className="btn btn-o btn-sm" onClick={() => onPick(v.version)}>
                      Xem
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </LoadState>
    </Dialog>
  );
}
