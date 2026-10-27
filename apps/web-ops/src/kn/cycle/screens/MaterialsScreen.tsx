"use client";

// BF-TT-MAT (#105, PRD TT-12) — FN-XA-03.06 Vật tư dạng BẢNG NHẬP: lọc Xã + Thời điểm; mỗi dòng Loại vật tư (danh mục
// masterdata `material`), Số lượng, ĐVT (tự điền theo danh mục), Thời điểm, Ghi chú, Xóa; nút Thêm dòng, Lưu, Nạp Excel
// (máy chủ đọc tệp thành dòng — không ghi), Lịch sử (sổ ghi nhận + log từng bản ghi). Sổ vật tư chỉ ghi thêm: sửa bản đã
// lưu là bản ghi bù ở máy chủ, "Xóa" trên bảng chỉ bỏ dòng chưa lưu. Cấp Tỉnh/Quốc gia chỉ xem (máy chủ 403 *.read_only).
import { useMemo, useState } from "react";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { parseNumberInput } from "@/components/form";
import { Icon } from "@/components/Icon";
import { DetailLayout, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, type Column } from "@/components/patterns/list";
import { saveBlob } from "@/lib/files";
import { vnToday } from "@/lib/vnDate";
import { PiiHint } from "@/kn/pii";
import { RawFile, type MaterialImport, type MaterialRecord } from "../api";
import { LoadState, dt, num, useAction, useCommuneNames, useCycleReadOnly, useLoad } from "../common";

/** ĐVT đếm được → số lượng nguyên (như máy chủ material/domain.CountableUnits). */
const COUNTABLE_UNITS = ["bao", "cái", "chai", "gói", "con", "chiếc", "cây", "hộp"];
export const isCountable = (unit: string | undefined) => !!unit && COUNTABLE_UNITS.includes(unit.trim().toLowerCase());

type Field = "material_code" | "quantity" | "recorded_on" | "note";
export interface DraftLine {
  key: string;
  material_code: string;
  quantity: string;
  recorded_on: string;
  note: string;
}
type RowErrors = Partial<Record<Field | "row", string>>;

const TEMPLATE_HEADER = ["Vật tư", "Số lượng", "ĐVT", "Thời điểm", "Ghi chú"];
const XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";

let seq = 0;
const newKey = () => `d${++seq}`;

/** Kiểm các dòng trước khi Lưu; trả lỗi theo dòng và số đã chuẩn hóa. */
export function validateLines(lines: DraftLine[], units: Record<string, string>, today: string): { errors: Record<string, RowErrors>; values: Record<string, string> } {
  const errors: Record<string, RowErrors> = {};
  const values: Record<string, string> = {};
  for (const l of lines) {
    const e: RowErrors = {};
    if (!l.material_code) e.material_code = "Bắt buộc nhập.";
    else if (!units[l.material_code]) e.material_code = "Vật tư không có trong danh mục đang dùng.";
    const q = parseNumberInput(l.quantity, { required: true, maxDecimals: 4, integer: isCountable(units[l.material_code]) });
    if (q.ok) values[l.key] = q.value;
    else e.quantity = q.error;
    if (!l.recorded_on) e.recorded_on = "Bắt buộc nhập.";
    else if (l.recorded_on > today) e.recorded_on = "Không được ở tương lai.";
    if (l.note.length > 1000) e.note = "Tối đa 1000 ký tự.";
    if (Object.keys(e).length) errors[l.key] = e;
  }
  return { errors, values };
}

function downloadTemplate() {
  const csv = [TEMPLATE_HEADER, ["Phân urê", "1,5", "tấn", "30/09/2026", ""]].map((r) => r.join(",")).join("\r\n") + "\r\n";
  saveBlob(new Blob([String.fromCharCode(0xfeff) + csv], { type: "text/csv;charset=utf-8" }), "mau-vat-tu.csv");
}

const LOG_LABEL: Record<string, string> = { recorded: "Ghi nhận", corrected: "Sửa (bản ghi bù)" };
const FIELD_LABEL: Record<string, string> = { quantity: "Số lượng", note: "Ghi chú" };
function toLogEntries(items: { event: string; actor_display: string | null; at: string; changes: { field: string; old: unknown; new: unknown }[]; reason: string | null }[]): LogEntry[] {
  const s = (v: unknown) => (v === null || v === undefined || v === "" ? null : String(v));
  return items.map((l) => ({
    event: LOG_LABEL[l.event] ?? l.event,
    actor_display: l.actor_display || "—",
    at: l.at,
    changes: l.changes.map((c) => ({ field: FIELD_LABEL[c.field] ?? c.field, old: s(c.old), new: s(c.new) })),
    reason: l.reason,
  }));
}

/** FN-XA-03.06 Vật tư (Hình TT-12). */
export function MaterialsScreen() {
  const { collectApi, kn } = useAuth();
  // Chỉ Cấp Xã (và Admin ở chế độ hỗ trợ) ghi nhận vật tư; Cấp Tỉnh xem tổng hợp các xã (máy chủ 403 forbidden).
  const readOnly = useCycleReadOnly() || kn.context?.role === "kn_tinh";
  const communes = kn.context?.communes ?? [];
  const today = vnToday();
  const [commune, setCommune] = useState(readOnly ? "" : (communes[0]?.code ?? ""));
  const [day, setDay] = useState(today);
  const [view, setView] = useState<"entry" | "history">(readOnly ? "history" : "entry");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [matFilter, setMatFilter] = useState("");
  const [page, setPage] = useState(1);
  const [logId, setLogId] = useState("");
  const act = useAction();
  const [rows, setRows] = useState<DraftLine[]>([]);
  const [errors, setErrors] = useState<Record<string, RowErrors>>({});

  const catalog = useLoad(() => collectApi.materialCatalog(), []);
  const items = useMemo(() => (catalog.data?.items ?? []).filter((m) => m.active && m.attrs.unit), [catalog.data]);
  const units = useMemo(() => Object.fromEntries(items.map((m) => [m.code, m.attrs.unit!])), [items]);
  const entry = view === "entry" && !readOnly;
  const q = entry ? { commune: commune || undefined, from: day, to: day } : { commune: commune || undefined, from: from || undefined, to: to || undefined, material_code: matFilter || undefined };
  const list = useLoad(() => collectApi.materials(q), [commune, day, from, to, matFilter, view, act.note]);
  const sum = useLoad(entry ? null : () => collectApi.materialSummary({ commune: commune || undefined, from: from || undefined, to: to || undefined }), [commune, from, to, view, act.note]);
  const log = useLoad(logId ? () => collectApi.materialLog(logId) : null, [logId]);
  const name = useCommuneNames([...(list.data?.items.map((m) => m.commune_code) ?? []), ...(sum.data?.items.map((m) => m.commune_code) ?? [])]);

  const setRow = (key: string, patch: Partial<DraftLine>) => {
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
    setErrors((es) => {
      if (!es[key]) return es;
      const next = { ...es, [key]: { ...es[key] } };
      for (const f of Object.keys(patch)) delete next[key]![f as Field];
      delete next[key]!.row;
      return next;
    });
  };
  const addRow = () => setRows((rs) => [...rs, { key: newKey(), material_code: "", quantity: "", recorded_on: day, note: "" }]);
  const removeRow = (key: string) => {
    setRows((rs) => rs.filter((r) => r.key !== key));
    setErrors((es) => {
      const next = { ...es };
      delete next[key];
      return next;
    });
  };

  const save = async () => {
    const v = validateLines(rows, units, today);
    setErrors(v.errors);
    if (!rows.length || Object.keys(v.errors).length) return;
    // Máy chủ nhận một Thời điểm mỗi lần ghi: gom dòng theo ngày, mỗi nhóm một POST (≤ 200 dòng).
    const groups = new Map<string, DraftLine[]>();
    for (const r of rows) groups.set(r.recorded_on, [...(groups.get(r.recorded_on) ?? []), r]);
    const saved = new Set<string>();
    const ok = await act.run(async (idem) => {
      let n = 0;
      try {
        for (const [on, ls] of groups) {
          for (let i = 0; i < ls.length; i += 200) {
            const chunk = ls.slice(i, i + 200);
            const body = { commune_code: commune, recorded_on: on, lines: chunk.map((l) => ({ material_code: l.material_code, quantity: v.values[l.key]!, ...(l.note.trim() ? { note: l.note.trim() } : {}) })) };
            try {
              await idem(() => collectApi.recordMaterials(body));
            } catch (e) {
              // Lỗi theo trường "lines.<i>.<field>" của nhóm → về đúng dòng trên bảng.
              const fe = e instanceof ApiProblem ? fieldErrorMap(e) : {};
              const mapped: Record<string, RowErrors> = {};
              for (const [f, reason] of Object.entries(fe)) {
                const m = /^lines\.(\d+)\.(material_code|quantity|note)$/.exec(f);
                const line = m ? chunk[Number(m[1])] : undefined;
                if (line) mapped[line.key] = { ...mapped[line.key], [m![2] as Field]: reason };
                else if (f === "recorded_on") for (const l of chunk) mapped[l.key] = { ...mapped[l.key], recorded_on: reason };
              }
              setErrors(mapped);
              throw e;
            }
            for (const l of chunk) saved.add(l.key);
            n += chunk.length;
          }
        }
      } finally {
        // Nhóm đã lưu rời bảng nhập (không lưu lại lần hai khi bấm Lưu sau khi sửa nhóm lỗi).
        if (saved.size) setRows((rs) => rs.filter((r) => !saved.has(r.key)));
      }
      return n;
    }, (n) => `Đã lưu ${n} dòng vật tư cho ${name(commune)}.`);
    if (ok !== null) setErrors({});
  };

  const importFile = async (file: File) => {
    const type = /\.xlsx$/i.test(file.name) ? XLSX : "text/csv; charset=utf-8";
    const r = await act.run(async () => collectApi.importMaterials(new RawFile(await file.arrayBuffer(), type)), (x: MaterialImport) =>
      x.rejected ? `Đã nạp ${x.lines.length} dòng, ${x.rejected} dòng cần sửa — kiểm tra rồi bấm Lưu.` : `Đã nạp ${x.lines.length} dòng — kiểm tra rồi bấm Lưu.`,
    );
    if (!r) return;
    const added: DraftLine[] = [];
    const errs: Record<string, RowErrors> = {};
    for (const l of r.lines) {
      const d: DraftLine = { key: newKey(), material_code: l.material_code ?? "", quantity: l.quantity ?? "", recorded_on: l.recorded_on ?? day, note: l.note ?? "" };
      added.push(d);
      if (l.status === "rejected") errs[d.key] = { row: `Dòng ${l.row} của tệp: ${l.reason ?? "không hợp lệ"}` };
    }
    setRows((rs) => [...rs, ...added]);
    setErrors((es) => ({ ...es, ...errs }));
  };

  const entryCols: Column<DraftLine & { i: number }>[] = [
    {
      key: "mat",
      header: "Loại vật tư *",
      cell: (l) => (
        <>
          <select className="select" aria-label={`Loại vật tư dòng ${l.i + 1}`} aria-required aria-invalid={errors[l.key]?.material_code ? true : undefined} value={l.material_code} onChange={(e) => setRow(l.key, { material_code: e.target.value })}>
            <option value="">Chọn…</option>
            {items.map((m) => (
              <option key={m.code} value={m.code}>
                {m.name}
              </option>
            ))}
          </select>
          <CellError text={errors[l.key]?.material_code} />
          <CellError text={errors[l.key]?.row} />
        </>
      ),
    },
    {
      key: "qty",
      header: "Số lượng *",
      className: "num",
      cell: (l) => (
        <>
          <input
            className="input"
            aria-label={`Số lượng dòng ${l.i + 1}`}
            aria-required
            aria-invalid={errors[l.key]?.quantity ? true : undefined}
            inputMode={isCountable(units[l.material_code]) ? "numeric" : "decimal"}
            value={l.quantity}
            onChange={(e) => setRow(l.key, { quantity: e.target.value })}
            onBlur={() => {
              if (!l.quantity) return;
              const p = parseNumberInput(l.quantity, { maxDecimals: 4, integer: isCountable(units[l.material_code]) });
              if (!p.ok) setErrors((es) => ({ ...es, [l.key]: { ...es[l.key], quantity: p.error } }));
            }}
          />
          <CellError text={errors[l.key]?.quantity} />
        </>
      ),
    },
    { key: "unit", header: "ĐVT", cell: (l) => <span aria-label={`Đơn vị tính dòng ${l.i + 1}`}>{units[l.material_code] ?? "—"}</span> },
    {
      key: "on",
      header: "Thời điểm *",
      cell: (l) => (
        <>
          <input className="input" type="date" max={today} aria-label={`Thời điểm dòng ${l.i + 1}`} aria-required aria-invalid={errors[l.key]?.recorded_on ? true : undefined} value={l.recorded_on} onChange={(e) => setRow(l.key, { recorded_on: e.target.value })} />
          <CellError text={errors[l.key]?.recorded_on} />
        </>
      ),
    },
    {
      key: "note",
      header: "Ghi chú",
      cell: (l) => (
        <>
          <input className="input" aria-label={`Ghi chú dòng ${l.i + 1}`} maxLength={1000} value={l.note} onChange={(e) => setRow(l.key, { note: e.target.value })} />
          <PiiHint text={l.note} />
          <CellError text={errors[l.key]?.note} />
        </>
      ),
    },
    {
      key: "del",
      header: "Xóa",
      cell: (l) => (
        <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa dòng ${l.i + 1}`} onClick={() => removeRow(l.key)}>
          <Icon name="trash" size={15} />
        </button>
      ),
    },
  ];

  const recordCols: Column<MaterialRecord>[] = [
    { key: "on", header: "Thời điểm", cell: (m) => m.recorded_on },
    { key: "commune", header: "Xã/phường", cell: (m) => name(m.commune_code) },
    { key: "mat", header: "Vật tư", cell: (m) => m.material_name },
    { key: "q", header: "Số lượng", className: "num", cell: (m) => num(m.quantity, 4) },
    { key: "unit", header: "ĐVT", cell: (m) => m.unit },
    { key: "note", header: "Ghi chú", cell: (m) => m.note ?? "—" },
    { key: "by", header: "Người ghi", cell: (m) => `${m.actor_display ?? "—"} · ${dt(m.recorded_at)}` },
    {
      key: "log",
      header: "Thao tác",
      cell: (m) => (
        <button type="button" className="btn btn-o btn-sm" aria-pressed={logId === m.id} aria-label={`Lịch sử bản ghi ${m.material_name} ${m.recorded_on}`} onClick={() => setLogId(logId === m.id ? "" : m.id)}>
          <Icon name="clock" size={15} /> Lịch sử
        </button>
      ),
    },
  ];

  const communeChip = (
    <FilterChip
      label="Xã/phường"
      value={commune}
      onChange={(v) => (setCommune(v), setPage(1), setLogId(""))}
      defaultValue={entry ? (communes[0]?.code ?? "") : ""}
      options={[...(entry ? [] : [{ value: "", label: "Tất cả xã phụ trách" }]), ...communes.map((c) => ({ value: c.code, label: c.name }))]}
    />
  );
  const dateInput = (id: string, label: string, value: string, onChange: (v: string) => void) => (
    <label className="row small" htmlFor={id} style={{ gap: 6 }}>
      {label}
      <input id={id} type="date" max={today} value={value} onChange={(e) => (onChange(e.target.value), setPage(1))} />
    </label>
  );

  const records = (
    <LoadState s={list}>
      {(d) => (
        <DataTable
          caption={entry ? "Đã ghi nhận" : "Lịch sử ghi nhận"}
          columns={recordCols}
          rows={d.items.slice((page - 1) * 20, page * 20)}
          rowKey={(m) => m.id}
          page={page}
          pageSize={20}
          total={d.items.length}
          onPage={setPage}
          empty={entry ? "Chưa có ghi nhận vật tư tại thời điểm này." : "Chưa có ghi nhận vật tư."}
        />
      )}
    </LoadState>
  );
  const logPanel = logId ? <LogPanel title="Lịch sử bản ghi" entries={toLogEntries(log.data?.items ?? [])} empty={log.loading ? "Đang tải…" : "Chưa có thay đổi nào."} /> : null;

  return (
    <>
      <PageHeader
        title="Vật tư"
        actions={
          readOnly ? null : entry ? (
            <>
              <button type="button" className="btn btn-o" onClick={downloadTemplate}>
                <Icon name="download" size={15} /> Tệp mẫu
              </button>
              <label className={`btn btn-o${act.busy || !commune ? " disabled" : ""}`}>
                <Icon name="upload" size={15} /> Nạp Excel
                <input
                  type="file"
                  accept=".xlsx,.csv,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                  aria-label="Tệp vật tư (Excel/CSV)"
                  style={{ display: "none" }}
                  disabled={act.busy || !commune}
                  onChange={(e) => {
                    const f = e.target.files?.[0];
                    e.target.value = "";
                    if (f) void importFile(f);
                  }}
                />
              </label>
              <button type="button" className="btn btn-o" onClick={() => (setView("history"), setPage(1), setLogId(""))}>
                <Icon name="clock" size={15} /> Lịch sử
              </button>
            </>
          ) : (
            <button type="button" className="btn btn-o" onClick={() => (setView("entry"), setPage(1), setLogId(""), setCommune(commune || (communes[0]?.code ?? "")))}>
              ← Bảng nhập
            </button>
          )
        }
      />
      {entry ? (
        <FilterBar>
          {communeChip}
          {dateInput("mat-day", "Thời điểm", day, (v) => setDay(v || today))}
        </FilterBar>
      ) : (
        <FilterBar onReset={() => (setCommune(""), setFrom(""), setTo(""), setMatFilter(""), setPage(1))}>
          {communeChip}
          {dateInput("mat-from", "Từ ngày", from, setFrom)}
          {dateInput("mat-to", "Đến ngày", to, setTo)}
          <FilterChip label="Vật tư" value={matFilter} onChange={(v) => (setMatFilter(v), setPage(1))} options={[{ value: "", label: "Tất cả" }, ...items.map((m) => ({ value: m.code, label: m.name }))]} />
        </FilterBar>
      )}
      {act.view}
      {catalog.problem ? <LoadState s={catalog}>{() => null}</LoadState> : null}
      {entry ? (
        <section className="sec" aria-label="Bảng nhập vật tư" style={{ marginBottom: 12 }}>
          <h2>Nhập vật tư — {commune ? name(commune) : "chọn xã"}</h2>
          <DataTable caption="Bảng nhập vật tư" columns={entryCols} rows={rows.map((l, i) => ({ ...l, i }))} rowKey={(l) => l.key} empty="Chưa có dòng nhập — bấm “Thêm dòng” hoặc “Nạp Excel”." />
          <div className="row" style={{ gap: 8, marginTop: 8 }}>
            <button type="button" className="btn btn-o btn-sm" disabled={!commune} onClick={addRow}>
              + Thêm dòng
            </button>
            <button type="button" className="btn btn-p btn-sm" disabled={!commune || !rows.length || act.busy} onClick={() => void save()}>
              Lưu
            </button>
            <span className="mute">Đã lưu thì không xóa được — sửa bằng bản ghi bù có lý do.</span>
          </div>
        </section>
      ) : (
        <section className="card sec" aria-label="Tổng hợp vật tư theo xã" style={{ marginBottom: 12 }}>
          <h2>Tổng hợp theo xã</h2>
          <ul className="list-plain">
            {(sum.data?.items ?? []).map((x) => (
              <li key={`${x.commune_code}-${x.material_code}`}>
                {name(x.commune_code)} · {x.material_name}: <b>{num(x.quantity, 4)}</b> {x.unit} ({x.records} lần)
              </li>
            ))}
            {!sum.data?.items.length ? <li className="mute">Chưa có ghi nhận.</li> : null}
          </ul>
        </section>
      )}
      {logPanel ? <DetailLayout main={records} log={logPanel} /> : records}
    </>
  );
}

function CellError({ text }: { text?: string }) {
  return text ? (
    <span className="err" role="alert">
      {text}
    </span>
  ) : null;
}
