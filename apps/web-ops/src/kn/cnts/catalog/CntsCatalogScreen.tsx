"use client";

// FN-06 — Danh mục Chăn nuôi – Thủy sản (BRD CNTS v3.7, UR-ADM-04; W11.41, pf/masterdata). Admin thêm/sửa/ngừng dùng
// trong popup, nạp Excel; Cấp Tỉnh chỉ xem (Application API). Thuộc tính ảnh hưởng tổng hợp có hiệu lực TỪ NGÀY (BR-03):
// mỗi lần sửa chọn ngày áp dụng, "Xem tại ngày" đọc giá trị có hiệu lực ngày đó, "Hiệu lực" liệt kê các giá trị.
import { useState, type ChangeEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { CatalogItem, CatalogName } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { DataTable, PageHeader, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { toCsv } from "@/htx/rules";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { readUpload, saveBlob } from "@/lib/files";
import {
  CNTS_TABS,
  CNTS_THRESHOLDS,
  cleanAttrs,
  codes,
  formatDoses,
  parseDoses,
  tabOf,
  templateRows,
  validateAttrs,
  type CatalogTab,
  type CntsCatalog,
  type Dose,
  type Field,
} from "./schema";

const PAGE = 20;
type TabId = CntsCatalog | "thresholds";
type Item = CatalogItem & { valid_from?: string };

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

function dmy(d?: string | null): string {
  if (!d) return "—";
  const [y, m, dd] = d.slice(0, 10).split("-");
  return `${dd}/${m}/${y}`;
}

/** Truy cập danh mục: Admin (masterdata_admin) qua Admin API, người khác chỉ đọc qua Application API. */
function useCatalogApi() {
  const { api, adminApi, principal } = useAuth();
  const canEdit = hasRole(principal, ROLES.masterdataAdmin, ROLES.platformAdmin);
  const list = async (catalog: CntsCatalog, at: string): Promise<Item[]> => {
    const c = catalog as CatalogName;
    const page = canEdit
      ? await unwrap(adminApi.GET("/v1/masterdata/catalogs/{catalog}/items", { params: { path: { catalog: c }, query: { at, include_inactive: true } } }))
      : await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: c }, query: { at } } }));
    return page.items as Item[];
  };
  return { api, adminApi, canEdit, list };
}

/** Tóm tắt thuộc tính một dòng (cột "Thuộc tính"). */
function summary(tab: CatalogTab, it: Item, names: Record<string, string>): string {
  const a = it.attrs;
  const opt = (key: string) => {
    const f = tab.fields.find((x) => x.key === key);
    return f && f.kind === "select" ? (f.options.find(([v]) => v === a[key])?.[1] ?? a[key]) : a[key];
  };
  const refs = (v?: string) =>
    codes(v)
      .map((c) => names[c] ?? c)
      .join(", ");
  switch (tab.id) {
    case "cnts_species":
      return `${opt("group") ?? ""} · ${a.unit ?? ""}${a.herd_column === "true" ? " · cột tổng đàn" : ""}`;
    case "cnts_breed":
    case "cnts_disease":
      return refs(a.species) || "—";
    case "cnts_vaccine":
      return `${refs(a.species)} · ${opt("report_group") ?? ""}`;
    case "cnts_vaccination_schedule":
      return `${names[a.model_type ?? ""] ?? a.model_type ?? names[a.breed ?? ""] ?? a.breed ?? ""} · ${parseDoses(a.doses).length} mũi`;
    case "cnts_slaughter_facility":
      return `Xã ${a.commune_code ?? "—"} · ${opt("facility_kind") ?? ""} · GS: ${a.stamp_codes_gs || "—"} · GC: ${a.stamp_codes_gc || "—"}`;
    case "cnts_aquaculture_object":
      return opt("scale_unit") ?? "—";
    case "cnts_indicator":
      return `${a.unit ?? ""} · ${opt("value_type") ?? ""}`;
    case "cnts_model_type":
      return `${opt("sector") ?? ""}${a.species ? ` · ${refs(a.species)}` : ""}`;
    case "cnts_chemical":
      return `${a.unit ?? ""}${a.target ? ` · ${opt("target")}` : ""}`;
    default:
      return a.unit ?? a.description ?? "—";
  }
}

export function CntsCatalogScreen() {
  const ca = useCatalogApi();
  const [tab, setTab] = useState<TabId>("cnts_species");
  const [at, setAt] = useState(vnToday());
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [nonce, setNonce] = useState(0);
  const [editing, setEditing] = useState<Item | "new" | null>(null);
  const [retiring, setRetiring] = useState<Item | null>(null);
  const [versionsOf, setVersionsOf] = useState<Item | null>(null);
  const [showImport, setShowImport] = useState(false);
  const act = useAction();
  const catalog = tab === "thresholds" ? null : tab;
  const rows = useLoad(catalog ? () => ca.list(catalog, at) : null, [catalog, at, nonce]);
  // Tên mục được tham chiếu (loài, bệnh, mô hình, giống) để hiển thị thay mã.
  const refNames = useLoad(
    () =>
      Promise.all((["cnts_species", "cnts_disease", "cnts_model_type", "cnts_breed", "cnts_vaccine"] as CntsCatalog[]).map((c) => ca.list(c, at))).then((ls) =>
        Object.fromEntries(ls.flat().map((i) => [i.code, i.name])),
      ),
    [at, nonce],
  );
  const names = refNames.data ?? {};
  const reload = () => setNonce((n) => n + 1);
  const current = catalog ? tabOf(catalog) : null;

  const reactivate = (it: Item) =>
    void act
      .run(
        () =>
          unwrap(
            ca.adminApi.POST("/v1/masterdata/catalogs/{catalog}/items/{code}/reactivate", {
              params: { path: { catalog: it.catalog as CatalogName, code: it.code }, header: { "Idempotency-Key": newIdempotencyKey() } },
              body: {},
            }),
          ),
        `Đã dùng lại ${it.name}`,
      )
      .then(reload);

  const columns: Column<Item>[] = current
    ? [
        { key: "code", header: "Mã", cell: (i) => <code>{i.code}</code> },
        { key: "name", header: "Tên", cell: (i) => (i.active ? <b>{i.name}</b> : <span className="mute">{i.name}</span>) },
        { key: "attrs", header: current.summary, cell: (i) => <span className="small">{summary(current, i, names)}</span> },
        { key: "from", header: "Hiệu lực từ", cell: (i) => dmy(i.valid_from) },
        { key: "st", header: "Trạng thái", cell: (i) => (i.active ? <StatusPill tone="ok">Đang dùng</StatusPill> : <StatusPill>Ngừng dùng</StatusPill>) },
        {
          key: "act",
          header: "",
          cell: (i) => (
            <span className="row">
              {ca.canEdit ? (
                <button type="button" className="btn btn-o btn-sm" aria-label={`Sửa ${i.name}`} onClick={() => setEditing(i)}>
                  <Icon name="edit" size={15} />
                </button>
              ) : null}
              {ca.canEdit ? (
                i.active ? (
                  <button type="button" className="btn btn-o btn-sm" onClick={() => setRetiring(i)}>
                    Ngừng dùng
                  </button>
                ) : (
                  <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => reactivate(i)}>
                    Dùng lại
                  </button>
                )
              ) : null}
              {ca.canEdit ? (
                <button type="button" className="btn btn-o btn-sm" onClick={() => setVersionsOf(i)}>
                  Hiệu lực
                </button>
              ) : null}
            </span>
          ),
        },
      ]
    : [];

  return (
    <>
      <PageHeader
        title="Danh mục Chăn nuôi – Thủy sản"
        actions={
          ca.canEdit && current ? (
            <span className="row">
              <button type="button" className="btn btn-o btn-sm" aria-expanded={showImport} onClick={() => setShowImport((v) => !v)}>
                <Icon name="upload" size={15} />
                Nạp Excel
              </button>
              <button type="button" className="btn btn-p btn-sm" onClick={() => setEditing("new")}>
                Thêm {current.label.toLowerCase()}
              </button>
            </span>
          ) : null
        }
      >
        Vật nuôi, giống, vaccine (kèm quy trình tiêm theo ngày tuổi và ánh xạ loài so sánh), bệnh, hóa chất, cơ sở giết mổ (nhiều
        mã mộc dấu), đối tượng nuôi thủy sản, nhóm khai thác, chỉ tiêu theo dõi, loại mô hình, loại văn bản. Sửa thuộc tính là tạo
        giá trị mới từ ngày áp dụng, không ghi đè giá trị cũ; báo cáo đã gửi giữ giá trị lúc gửi.
      </PageHeader>
      {!ca.canEdit ? <ReadOnlyNotice>Chỉ xem — Admin quản trị danh mục CNTS (FN-06).</ReadOnlyNotice> : null}
      {act.view}
      <div className="dtabs" role="tablist" aria-label="Danh mục CNTS">
        {[...CNTS_TABS.map((t) => ({ id: t.id as TabId, label: t.label })), { id: "thresholds" as TabId, label: "Ngưỡng mặc định" }].map((t) => (
          <button key={t.id} type="button" role="tab" aria-selected={t.id === tab} onClick={() => (setTab(t.id), setPage(1), setShowImport(false))}>
            {t.label}
          </button>
        ))}
      </div>
      <div className="row" style={{ margin: "12px 0", gap: 12, flexWrap: "wrap" }}>
        <label className="row small">
          Xem tại ngày
          <input type="date" aria-label="Xem tại ngày" value={at} onChange={(e) => (setAt(e.target.value || vnToday()), setPage(1))} />
        </label>
        {current ? <SearchBox value={q} onChange={(v) => (setQ(v), setPage(1))} placeholder="Tìm mã, tên" /> : null}
      </div>
      {showImport && current ? <ImportPanel tab={current} onDone={reload} /> : null}
      {current ? (
        <LoadState s={rows}>
          {(d) => {
            const list = d.filter((i) => !q || `${i.code} ${i.name}`.toLowerCase().includes(q.toLowerCase()));
            return (
              <DataTable
                caption={current.label}
                columns={columns}
                rows={list.slice((page - 1) * PAGE, page * PAGE)}
                rowKey={(i) => i.code}
                page={page}
                pageSize={PAGE}
                total={list.length}
                onPage={setPage}
                empty="Danh mục chưa có mục có hiệu lực tại ngày này."
              />
            );
          }}
        </LoadState>
      ) : (
        <Thresholds at={at} />
      )}
      {editing && current ? (
        <ItemDialog
          tab={current}
          item={editing === "new" ? null : editing}
          at={at}
          names={names}
          onClose={() => setEditing(null)}
          onSaved={() => (setEditing(null), reload())}
        />
      ) : null}
      {retiring ? <RetireDialog item={retiring} onClose={() => setRetiring(null)} onDone={() => (setRetiring(null), reload())} /> : null}
      {versionsOf ? <VersionsDialog item={versionsOf} names={names} onClose={() => setVersionsOf(null)} /> : null}
    </>
  );
}

function FieldInput({
  f,
  value,
  onChange,
  options,
  error,
}: {
  f: Field;
  value: string;
  onChange: (v: string) => void;
  options: Record<CntsCatalog, Item[]>;
  error?: string;
}) {
  const label = `${f.label}${"required" in f && f.required ? " *" : ""}`;
  const err = error ? <span className="err small">{error}</span> : null;
  switch (f.kind) {
    case "select":
      return (
        <label className="stack small">
          {label}
          <select aria-label={f.label} value={value} onChange={(e) => onChange(e.target.value)}>
            <option value="">—</option>
            {f.options.map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
          {err}
        </label>
      );
    case "bool":
      return (
        <label className="row small">
          <input type="checkbox" aria-label={f.label} checked={value === "true"} onChange={(e) => onChange(e.target.checked ? "true" : "")} />
          {f.label}
        </label>
      );
    case "ref": {
      const opts = (options[f.catalog] ?? []).filter((o) => o.active);
      if (!f.multi)
        return (
          <label className="stack small">
            {label}
            <select aria-label={f.label} value={value} onChange={(e) => onChange(e.target.value)}>
              <option value="">—</option>
              {opts.map((o) => (
                <option key={o.code} value={o.code}>
                  {o.name} ({o.code})
                </option>
              ))}
            </select>
            {err}
          </label>
        );
      const picked = codes(value);
      return (
        <fieldset className="stack small" style={{ border: 0, padding: 0 }}>
          <legend>{label}</legend>
          <span className="row" style={{ flexWrap: "wrap", gap: 8 }}>
            {opts.map((o) => (
              <label key={o.code} className="row">
                <input
                  type="checkbox"
                  aria-label={`${f.label}: ${o.name}`}
                  checked={picked.includes(o.code)}
                  onChange={(e) => onChange((e.target.checked ? [...picked, o.code] : picked.filter((c) => c !== o.code)).join(","))}
                />
                {o.name}
              </label>
            ))}
          </span>
          {err}
        </fieldset>
      );
    }
    case "doses":
      return <DosesEditor value={value} onChange={onChange} vaccines={(options.cnts_vaccine ?? []).filter((v) => v.active)} error={error} />;
    default:
      return (
        <label className="stack small">
          {label}
          <input aria-label={f.label} value={value} inputMode={f.kind === "int" ? "numeric" : undefined} onChange={(e) => onChange(e.target.value)} />
          {f.kind === "codes" ? <span className="mute">{f.hint}</span> : null}
          {err}
        </label>
      );
  }
}

/** Danh sách mũi của quy trình tiêm: vaccine, lần, ngày tuổi (FN-06; ví dụ gà thương phẩm UR-TINH-10). */
function DosesEditor({ value, onChange, vaccines, error }: { value: string; onChange: (v: string) => void; vaccines: Item[]; error?: string }) {
  const ds = parseDoses(value);
  const set = (next: Dose[]) => onChange(formatDoses(next));
  return (
    <fieldset className="stack small" style={{ border: 0, padding: 0 }}>
      <legend>Danh sách mũi (vaccine, lần, ngày tuổi) *</legend>
      <table className="tbl">
        <thead>
          <tr>
            <th scope="col">Vaccine</th>
            <th scope="col">Lần</th>
            <th scope="col">Ngày tuổi</th>
            <th scope="col" />
          </tr>
        </thead>
        <tbody>
          {ds.map((d, i) => (
            <tr key={i}>
              <td>
                <select aria-label={`Vaccine mũi ${i + 1}`} value={d.vaccine} onChange={(e) => set(ds.map((x, j) => (j === i ? { ...x, vaccine: e.target.value } : x)))}>
                  {vaccines.map((v) => (
                    <option key={v.code} value={v.code}>
                      {v.name}
                    </option>
                  ))}
                </select>
              </td>
              <td>
                <input
                  aria-label={`Lần mũi ${i + 1}`}
                  type="number"
                  min={1}
                  max={20}
                  value={d.shot}
                  onChange={(e) => set(ds.map((x, j) => (j === i ? { ...x, shot: Math.max(1, Number(e.target.value) || 1) } : x)))}
                />
              </td>
              <td>
                <input
                  aria-label={`Ngày tuổi mũi ${i + 1}`}
                  type="number"
                  min={0}
                  max={3650}
                  value={d.age_days}
                  onChange={(e) => set(ds.map((x, j) => (j === i ? { ...x, age_days: Math.max(0, Number(e.target.value) || 0) } : x)))}
                />
              </td>
              <td>
                <button type="button" className="btn btn-o btn-sm" aria-label={`Bỏ mũi ${i + 1}`} onClick={() => set(ds.filter((_, j) => j !== i))}>
                  <Icon name="trash" size={14} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <button
        type="button"
        className="btn btn-o btn-sm"
        disabled={vaccines.length === 0}
        onClick={() => set([...ds, { vaccine: vaccines[0]?.code ?? "", shot: 1, age_days: (ds[ds.length - 1]?.age_days ?? 0) + 7 }])}
      >
        Thêm mũi
      </button>
      {error ? <span className="err small">{error}</span> : null}
    </fieldset>
  );
}

function ItemDialog({
  tab,
  item,
  at,
  onClose,
  onSaved,
}: {
  tab: CatalogTab;
  item: Item | null;
  at: string;
  names: Record<string, string>;
  onClose: () => void;
  onSaved: () => void;
}) {
  const ca = useCatalogApi();
  const [code, setCode] = useState(item?.code ?? "");
  const [name, setName] = useState(item?.name ?? "");
  const [attrs, setAttrs] = useState<Record<string, string>>({ ...(item?.attrs ?? {}) });
  const [validFrom, setValidFrom] = useState(item ? vnToday() : at);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [key] = useState(newIdempotencyKey);
  const act = useAction();
  const refCatalogs = [...new Set(tab.fields.flatMap((f) => (f.kind === "ref" ? [f.catalog] : f.kind === "doses" ? ["cnts_vaccine" as CntsCatalog] : [])))];
  const opts = useLoad(
    () => Promise.all(refCatalogs.map((c) => ca.list(c, validFrom))).then((ls) => Object.fromEntries(refCatalogs.map((c, i) => [c, ls[i]!])) as Record<CntsCatalog, Item[]>),
    [validFrom],
  );
  const save = async () => {
    const e = validateAttrs(tab, attrs);
    if (!name.trim()) e.name = "Bắt buộc";
    if (!item && !code.trim()) e.code = "Bắt buộc";
    setErrs(e);
    if (Object.keys(e).length > 0) return;
    const body = { name: name.trim(), attrs: cleanAttrs(attrs), valid_from: validFrom };
    const c = tab.id as CatalogName;
    const r = await act.run(() =>
      item
        ? unwrap(ca.adminApi.PUT("/v1/masterdata/catalogs/{catalog}/items/{code}", { params: { path: { catalog: c, code: item.code } }, body }))
        : unwrap(
            ca.adminApi.POST("/v1/masterdata/catalogs/{catalog}/items", {
              params: { path: { catalog: c }, header: { "Idempotency-Key": key } },
              body: { ...body, code: code.trim() },
            }),
          ),
    );
    if (r) onSaved();
  };
  return (
    <Dialog
      title={item ? `Sửa ${tab.label.toLowerCase()} ${item.code}` : `Thêm ${tab.label.toLowerCase()}`}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void save()}>
          {act.busy ? "Đang lưu…" : "Lưu"}
        </button>
      }
    >
      <div className="stack" style={{ gap: 10, minWidth: 420 }}>
        {act.view}
        <label className="stack small">
          Mã *
          <input aria-label="Mã" value={code} disabled={!!item} onChange={(e) => setCode(e.target.value)} />
          {errs.code ? <span className="err small">{errs.code}</span> : null}
        </label>
        <label className="stack small">
          Tên *
          <input aria-label="Tên" value={name} onChange={(e) => setName(e.target.value)} />
          {errs.name ? <span className="err small">{errs.name}</span> : null}
        </label>
        <LoadState s={opts}>
          {(o) => (
            <>
              {tab.fields.map((f) => (
                <FieldInput key={f.key} f={f} value={attrs[f.key] ?? ""} options={o} error={errs[f.key]} onChange={(v) => setAttrs((a) => ({ ...a, [f.key]: v }))} />
              ))}
            </>
          )}
        </LoadState>
        {item?.attrs.facility_ref ? <span className="mute small">Mã định danh cơ sở (cố định): {item.attrs.facility_ref}</span> : null}
        <label className="stack small">
          Ngày áp dụng *
          <input type="date" aria-label="Ngày áp dụng" value={validFrom} onChange={(e) => setValidFrom(e.target.value || vnToday())} />
          <span className="mute">Giá trị mới có hiệu lực từ ngày này; kỳ trước giữ giá trị cũ (FN-06 BR-03).</span>
        </label>
      </div>
    </Dialog>
  );
}

function RetireDialog({ item, onClose, onDone }: { item: Item; onClose: () => void; onDone: () => void }) {
  const ca = useCatalogApi();
  const [reason, setReason] = useState("");
  const [key] = useState(newIdempotencyKey);
  const act = useAction();
  const ok = reason.trim().length >= 10;
  const submit = async () => {
    const r = await act.run(() =>
      unwrap(
        ca.adminApi.POST("/v1/masterdata/catalogs/{catalog}/items/{code}/deactivate", {
          params: { path: { catalog: item.catalog as CatalogName, code: item.code }, header: { "Idempotency-Key": key } },
          body: { reason: reason.trim() },
        }),
      ),
    );
    if (r) onDone();
  };
  return (
    <Dialog
      title={`Ngừng dùng ${item.name}`}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={!ok || act.busy} onClick={() => void submit()}>
          Ngừng dùng
        </button>
      }
    >
      {act.view}
      <p className="small">Mục đang dùng trong biểu chỉ được ngừng dùng, không xóa; số liệu cũ giữ nguyên.</p>
      <label className="stack small">
        Lý do (tối thiểu 10 ký tự)
        <textarea aria-label="Lý do ngừng dùng" value={reason} onChange={(e) => setReason(e.target.value)} />
      </label>
    </Dialog>
  );
}

function VersionsDialog({ item, names, onClose }: { item: Item; names: Record<string, string>; onClose: () => void }) {
  const ca = useCatalogApi();
  const tab = tabOf(item.catalog as CntsCatalog);
  const s = useLoad(
    () =>
      unwrap(
        ca.adminApi.GET("/v1/masterdata/catalogs/{catalog}/items/{code}/versions", {
          params: { path: { catalog: item.catalog as CatalogName, code: item.code } },
        }),
      ),
    [item.code],
  );
  return (
    <Dialog title={`Hiệu lực của ${item.name}`} onClose={onClose}>
      <LoadState s={s}>
        {(d) =>
          d.items.length === 0 ? (
            <p className="small mute">Mục nạp sẵn chưa sửa lần nào — giá trị hiện tại áp cho mọi kỳ.</p>
          ) : (
            <table className="tbl">
              <caption className="sr-only">Các giá trị có hiệu lực từ ngày</caption>
              <thead>
                <tr>
                  <th scope="col">Từ ngày</th>
                  <th scope="col">Đến trước ngày</th>
                  <th scope="col">Tên</th>
                  <th scope="col">Thuộc tính</th>
                  <th scope="col">Trạng thái</th>
                </tr>
              </thead>
              <tbody>
                {d.items.map((v) => (
                  <tr key={v.id}>
                    <td>{dmy(v.valid_from)}</td>
                    <td>{dmy(v.valid_to)}</td>
                    <td>{v.after.name}</td>
                    <td className="small">{summary(tab, { ...item, attrs: v.after.attrs ?? {} }, names)}</td>
                    <td>{v.after.active ? "Đang dùng" : "Ngừng dùng"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        }
      </LoadState>
    </Dialog>
  );
}

/** Ngưỡng mặc định (tham số có phiên bản, sửa qua quy trình đặt → duyệt ở "Tham số"). */
function Thresholds({ at }: { at: string }) {
  const { api, principal } = useAuth();
  const s = useLoad(
    () =>
      Promise.all(
        CNTS_THRESHOLDS.map((t) =>
          unwrap(api.GET("/v1/masterdata/params/value", { params: { query: { key: t.key, at } } }))
            .then((v) => ({ ...t, value: v.value ?? v.text ?? "—", from: v.effective_from, doc: v.source_doc }))
            .catch(() => ({ ...t, value: "—", from: "", doc: "Chưa có giá trị đã duyệt" })),
        ),
      ),
    [at],
  );
  const setter = hasRole(principal, ROLES.paramSetter);
  return (
    <>
      <div className="alert alert-info" style={{ marginBottom: 12 }}>
        <Icon name="info" />
        <span>
          Ngưỡng là tham số có phiên bản (người đặt ≠ người duyệt); ngưỡng mới áp dụng cho số liệu có ngày từ ngày hiệu lực.
          {setter ? (
            <>
              {" "}
              <a href="/quan-tri/tham-so?prefix=cnts.">Đề xuất giá trị mới</a>
            </>
          ) : null}
        </span>
      </div>
      <LoadState s={s}>
        {(rows) => (
          <DataTable
            caption="Ngưỡng mặc định CNTS"
            columns={[
              { key: "label", header: "Ngưỡng", cell: (r) => r.label },
              { key: "value", header: "Giá trị", cell: (r) => <b>{`${r.value} ${r.unit}`.trim()}</b> },
              { key: "key", header: "Khóa tham số", cell: (r) => <code className="small">{r.key}</code> },
              { key: "from", header: "Hiệu lực từ", cell: (r) => dmy(r.from) },
              { key: "doc", header: "Căn cứ", cell: (r) => <span className="small">{r.doc}</span> },
            ]}
            rows={rows}
            rowKey={(r) => r.key}
            page={1}
            pageSize={rows.length || 1}
            total={rows.length}
            onPage={() => undefined}
          />
        )}
      </LoadState>
    </>
  );
}

/** Nạp Excel/CSV danh sách ban đầu (FN-06 B3) qua POST /v1/masterdata/catalog-imports. */
function ImportPanel({ tab, onDone }: { tab: CatalogTab; onDone: () => void }) {
  const { adminApi } = useAuth();
  const [key, setKey] = useState(newIdempotencyKey);
  const [file, setFile] = useState<Awaited<ReturnType<typeof readUpload>> | null>(null);
  const act = useAction();
  const ok = !!file && (file.kind === "xlsx" || file.kind === "csv");
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setFile(f ? await readUpload(f) : null);
    setKey(newIdempotencyKey());
  };
  const submit = async () => {
    if (!file || !ok) return;
    const r = await act.run(
      () =>
        unwrap(
          adminApi.POST("/v1/masterdata/catalog-imports", {
            params: { header: { "Idempotency-Key": key } },
            body: file.body as unknown as string,
            bodySerializer: (b: unknown) => b as BodyInit,
            headers: { "Content-Type": file.contentType },
          }),
        ),
      (res) => `${res.created} dòng đã tạo · ${res.skipped} bỏ qua · ${res.rejected} dòng lỗi (tổng ${res.total})`,
    );
    if (r) onDone();
  };
  return (
    <div className="panel stack" style={{ gap: 8, marginBottom: 12 }} aria-label="Nạp danh mục từ tệp">
      <b>Nạp {tab.label.toLowerCase()} từ Excel/CSV (tối đa 500 dòng)</b>
      <span className="small mute">
        Cột catalog ({tab.id}), code, name, valid_from (YYYY-MM-DD, trống = hôm nay) và các cột thuộc tính. Mục đã có cùng nội dung được bỏ qua.
      </span>
      <div className="row">
        <input type="file" aria-label="Tệp danh mục CNTS" accept=".xlsx,.csv" onChange={(e) => void onFile(e)} />
        <button type="button" className="btn btn-p btn-sm" disabled={!ok || act.busy} onClick={() => void submit()}>
          Nạp
        </button>
        <button
          type="button"
          className="btn btn-o btn-sm"
          onClick={() => saveBlob(new Blob([toCsv(templateRows(tab))], { type: "text/csv;charset=utf-8" }), `mau-${tab.id}.csv`)}
        >
          <Icon name="download" size={15} />
          Tải tệp mẫu
        </button>
      </div>
      {act.view}
    </div>
  );
}
