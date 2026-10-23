"use client";

// FN-06 — Danh mục Chăn nuôi – Thủy sản (BRD CNTS v3.7, UR-ADM-04; W11.41, pf/masterdata). Admin thêm/sửa trên trang riêng
// (R-A2, BF-SWEEP-CNTS #101): một route chung cho mọi loại danh mục — `…/[loại]/them-moi`, `…/[loại]/[mã]` (chi tiết + các
// giá trị theo ngày hiệu lực), `…/[loại]/[mã]/sua`. Ngừng dùng qua pop-up lý do ngắn; nạp Excel; Cấp Tỉnh chỉ xem
// (Application API). Thuộc tính ảnh hưởng tổng hợp có hiệu lực TỪ NGÀY (BR-03): mỗi lần sửa chọn ngày áp dụng, "Xem tại ngày"
// đọc giá trị có hiệu lực ngày đó.
import Link from "next/link";
import { useParams, usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState, type ChangeEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { fieldErrorMap } from "@/api/problem";
import type { CatalogItem, CatalogName } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { NumberField, SelectField, TextField, parseNumberInput, type NumberRule } from "@/components/form";
import { DetailPage, LabelValueBlock } from "@/components/patterns/detail";
import { DataTable, PageHeader, RowActions, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { toCsv } from "@/htx/rules";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { readUpload, saveBlob } from "@/lib/files";
import { ReasonDialog } from "../ReasonDialog";
import {
  CNTS_TABS,
  CNTS_THRESHOLDS,
  INT_RULE,
  cleanAttrs,
  codes,
  formatDoses,
  normalizeAttrs,
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

/** Gốc route danh mục: `/kn/cnts/danh-muc` hoặc `/kn/phong-ban/[dept]/danh-muc-cnts` (Admin › Phòng ban › Phòng CNTS). */
export function catalogBase(pathname: string | null | undefined): string {
  const m = /^(.*\/danh-muc(?:-cnts)?)(?:\/|$)/.exec(pathname ?? "");
  return m ? m[1]! : "/kn/cnts/danh-muc";
}

function isCntsCatalog(v: string | null | undefined): v is CntsCatalog {
  return !!v && CNTS_TABS.some((t) => t.id === v);
}

const itemHref = (base: string, catalog: string, code: string) => `${base}/${catalog}/${encodeURIComponent(code)}`;

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

/** Tên mục được tham chiếu (loài, bệnh, mô hình, giống, vaccine) để hiển thị thay mã. */
function useRefNames(at: string, nonce = 0) {
  const ca = useCatalogApi();
  return useLoad(
    () =>
      Promise.all((["cnts_species", "cnts_disease", "cnts_model_type", "cnts_breed", "cnts_vaccine"] as CntsCatalog[]).map((c) => ca.list(c, at))).then((ls) =>
        Object.fromEntries(ls.flat().map((i) => [i.code, i.name])) as Record<string, string>,
      ),
    [at, nonce],
  );
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

/** Giá trị hiển thị của một thuộc tính ở trang chi tiết. */
function fieldText(f: Field, value: string | undefined, names: Record<string, string>): string {
  if (!value) return "";
  switch (f.kind) {
    case "select":
      return f.options.find(([v]) => v === value)?.[1] ?? value;
    case "bool":
      return value === "true" ? "Có" : "Không";
    case "ref":
      return codes(value)
        .map((c) => names[c] ?? c)
        .join(", ");
    case "doses":
      return parseDoses(value)
        .map((d) => `${names[d.vaccine] ?? d.vaccine} lần ${d.shot} @ ${d.age_days} ngày tuổi`)
        .join("; ");
    default:
      return value;
  }
}

export function CntsCatalogScreen() {
  const ca = useCatalogApi();
  const base = catalogBase(usePathname());
  const sp = useSearchParams();
  const [tab, setTab] = useState<TabId>(() => {
    const t = sp?.get("tab");
    return t === "thresholds" || isCntsCatalog(t) ? t : "cnts_species";
  });
  const [at, setAt] = useState(vnToday());
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [nonce, setNonce] = useState(0);
  const [retiring, setRetiring] = useState<Item | null>(null);
  const [showImport, setShowImport] = useState(false);
  const act = useAction();
  const catalog = tab === "thresholds" ? null : tab;
  const rows = useLoad(catalog ? () => ca.list(catalog, at) : null, [catalog, at, nonce]);
  const refNames = useRefNames(at, nonce);
  const names = refNames.data ?? {};
  const reload = () => setNonce((n) => n + 1);
  const current = catalog ? tabOf(catalog) : null;

  const setActive = (it: Item, active: boolean, reason?: string) =>
    act
      .run(
        () =>
          unwrap(
            ca.adminApi.POST(active ? "/v1/masterdata/catalogs/{catalog}/items/{code}/reactivate" : "/v1/masterdata/catalogs/{catalog}/items/{code}/deactivate", {
              params: { path: { catalog: it.catalog as CatalogName, code: it.code }, header: { "Idempotency-Key": newIdempotencyKey() } },
              body: active ? {} : { reason: reason ?? "" },
            }),
          ),
        active ? `Đã dùng lại ${it.name}` : `Đã ngừng dùng ${it.name}`,
      )
      .then((r) => {
        if (r) reload();
        return r;
      });

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
              <RowActions
                label={i.name}
                viewHref={itemHref(base, i.catalog, i.code)}
                editHref={ca.canEdit ? `${itemHref(base, i.catalog, i.code)}/sua` : undefined}
              />
              {ca.canEdit ? (
                i.active ? (
                  <button type="button" className="btn btn-o btn-sm" onClick={() => setRetiring(i)}>
                    Ngừng dùng
                  </button>
                ) : (
                  <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void setActive(i, true)}>
                    Dùng lại
                  </button>
                )
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
              <Link className="btn btn-p btn-sm" href={`${base}/${current.id}/them-moi`}>
                Thêm {current.label.toLowerCase()}
              </Link>
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
      {retiring ? (
        <ReasonDialog
          title={`Ngừng dùng ${retiring.name}`}
          confirmLabel="Ngừng dùng"
          busy={act.busy}
          onClose={() => setRetiring(null)}
          onConfirm={(reason) => void setActive(retiring, false, reason).then((r) => r && setRetiring(null))}
        >
          <p className="small">Mục đang dùng trong biểu chỉ được ngừng dùng, không xóa; số liệu cũ giữ nguyên.</p>
        </ReasonDialog>
      ) : null}
    </>
  );
}

/** Đọc loại danh mục + mã mục từ route; mục được tìm trong danh sách hiện hành (Admin: gồm cả mục ngừng dùng). */
function useRouteItem() {
  const params = useParams<{ type?: string; code?: string }>();
  const type = params?.type;
  const code = params?.code ? decodeURIComponent(params.code) : undefined;
  const base = catalogBase(usePathname());
  const ca = useCatalogApi();
  const tab = isCntsCatalog(type) ? tabOf(type) : null;
  const item = useLoad(tab && code ? () => ca.list(tab.id, vnToday()).then((ls) => ({ item: ls.find((i) => i.code === code) ?? null })) : null, [tab?.id, code]);
  return { tab, code, base, ca, item };
}

function NotFound({ base, what }: { base: string; what: string }) {
  return (
    <DetailPage back={{ href: base, label: "Danh mục CNTS" }} title={what}>
      <p className="mute">Không tìm thấy. Có thể mục đã bị xóa khỏi danh mục hoặc đường dẫn sai.</p>
    </DetailPage>
  );
}

/** Trang Thêm / Sửa mục danh mục CNTS (dùng chung mọi loại danh mục). */
export function CntsCatalogItemFormScreen() {
  const { tab, code, base, ca, item } = useRouteItem();
  if (!tab) return <NotFound base={base} what="Danh mục không tồn tại" />;
  if (!ca.canEdit) {
    return (
      <DetailPage back={{ href: `${base}?tab=${tab.id}`, label: tab.label }} title={tab.label}>
        <ReadOnlyNotice>Chỉ xem — Admin quản trị danh mục CNTS (FN-06).</ReadOnlyNotice>
      </DetailPage>
    );
  }
  if (!code) return <ItemForm tab={tab} item={null} base={base} />;
  return (
    <LoadState s={item}>
      {(d) => (d.item ? <ItemForm key={d.item.code} tab={tab} item={d.item} base={base} /> : <NotFound base={`${base}?tab=${tab.id}`} what={`Không có mục ${code}`} />)}
    </LoadState>
  );
}

function ItemForm({ tab, item, base }: { tab: CatalogTab; item: Item | null; base: string }) {
  const ca = useCatalogApi();
  const router = useRouter();
  const [code, setCode] = useState(item?.code ?? "");
  const [name, setName] = useState(item?.name ?? "");
  const [attrs, setAttrs] = useState<Record<string, string>>({ ...(item?.attrs ?? {}) });
  const [validFrom, setValidFrom] = useState(vnToday());
  const [errs, setErrs] = useState<Record<string, string | undefined>>({});
  const [dosesInput, setDosesInput] = useState<string | undefined>();
  const [key] = useState(newIdempotencyKey);
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const listHref = `${base}?tab=${tab.id}`;
  const back = item ? itemHref(base, tab.id, item.code) : listHref;
  const refCatalogs = [...new Set(tab.fields.flatMap((f) => (f.kind === "ref" ? [f.catalog] : f.kind === "doses" ? ["cnts_vaccine" as CntsCatalog] : [])))];
  const opts = useLoad(
    () => Promise.all(refCatalogs.map((c) => ca.list(c, validFrom))).then((ls) => Object.fromEntries(refCatalogs.map((c, i) => [c, ls[i]!])) as Record<CntsCatalog, Item[]>),
    [validFrom],
  );
  const save = async () => {
    const e: Record<string, string> = validateAttrs(tab, attrs);
    if (!name.trim()) e.name = "Bắt buộc nhập.";
    if (!item && !code.trim()) e.code = "Bắt buộc nhập.";
    if (!validFrom) e.valid_from = "Bắt buộc nhập.";
    if (dosesInput) e.doses = dosesInput;
    setErrs(e);
    if (Object.keys(e).length > 0) return;
    const body = { name: name.trim(), attrs: cleanAttrs(normalizeAttrs(tab, attrs)), valid_from: validFrom };
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
    if (r) router.push(item ? back : listHref);
  };
  const err = (k: string) => errs[k] ?? server[k] ?? server[`attrs.${k}`];
  return (
    <DetailPage
      back={{ href: back, label: item ? item.name : tab.label }}
      title={item ? `Sửa ${tab.label.toLowerCase()} ${item.code}` : `Thêm ${tab.label.toLowerCase()}`}
      actions={
        <>
          <Link className="btn btn-o" href={back}>
            Hủy
          </Link>
          <button type="button" className="btn btn-p" disabled={act.busy} onClick={() => void save()}>
            {act.busy ? "Đang lưu…" : "Lưu"}
          </button>
        </>
      }
    >
      {act.view}
      <div className="grid2">
        <TextField label="Mã" required value={code} disabled={!!item} onChange={setCode} error={err("code")} maxLength={40} hint="1–40 ký tự A–Z, 0–9, _ . -" />
        <TextField label="Tên" required value={name} onChange={setName} error={err("name")} maxLength={200} />
      </div>
      <LoadState s={opts}>
        {(o) => (
          <div className="grid2">
            {tab.fields.map((f) => (
              <FieldInput
                key={f.key}
                f={f}
                value={attrs[f.key] ?? ""}
                options={o}
                error={err(f.key)}
                onChange={(v) => setAttrs((a) => ({ ...a, [f.key]: v }))}
                onBlurError={(m) => setErrs((x) => ({ ...x, [f.key]: m }))}
                onDosesInput={setDosesInput}
              />
            ))}
          </div>
        )}
      </LoadState>
      {item?.attrs.facility_ref ? <p className="mute small">Mã định danh cơ sở (cố định): {item.attrs.facility_ref}</p> : null}
      <div className="field">
        <label htmlFor="cnts-valid-from">
          Ngày áp dụng<span className="req" aria-hidden="true"> *</span>
        </label>
        <input
          id="cnts-valid-from"
          className="input"
          type="date"
          aria-required
          aria-invalid={err("valid_from") ? true : undefined}
          value={validFrom}
          onChange={(e) => setValidFrom(e.target.value)}
        />
        {err("valid_from") ? (
          <span className="err" role="alert">
            {err("valid_from")}
          </span>
        ) : (
          <span className="mute hint">Giá trị mới có hiệu lực từ ngày này; kỳ trước giữ giá trị cũ (FN-06 BR-03).</span>
        )}
      </div>
    </DetailPage>
  );
}

/** Trang chi tiết mục danh mục: thuộc tính tại hôm nay + các giá trị theo ngày hiệu lực (Admin). */
export function CntsCatalogItemScreen() {
  const { tab, code, base, ca, item } = useRouteItem();
  const names = useRefNames(vnToday());
  if (!tab || !code) return <NotFound base={base} what="Danh mục không tồn tại" />;
  return (
    <LoadState s={item}>
      {(d) =>
        d.item ? (
          <ItemDetail tab={tab} item={d.item} base={base} canEdit={ca.canEdit} names={names.data ?? {}} />
        ) : (
          <NotFound base={`${base}?tab=${tab.id}`} what={`Không có mục ${code}`} />
        )
      }
    </LoadState>
  );
}

function ItemDetail({ tab, item, base, canEdit, names }: { tab: CatalogTab; item: Item; base: string; canEdit: boolean; names: Record<string, string> }) {
  const ca = useCatalogApi();
  const versions = useLoad(
    canEdit
      ? () =>
          unwrap(
            ca.adminApi.GET("/v1/masterdata/catalogs/{catalog}/items/{code}/versions", {
              params: { path: { catalog: item.catalog as CatalogName, code: item.code } },
            }),
          )
      : null,
    [item.code, canEdit],
  );
  const href = itemHref(base, tab.id, item.code);
  return (
    <DetailPage
      back={{ href: `${base}?tab=${tab.id}`, label: tab.label }}
      title={item.name}
      badge={item.active ? <StatusPill tone="ok">Đang dùng</StatusPill> : <StatusPill>Ngừng dùng</StatusPill>}
      actions={
        canEdit ? (
          <Link className="btn btn-p" href={`${href}/sua`}>
            Sửa
          </Link>
        ) : null
      }
    >
      <LabelValueBlock
        title={tab.label}
        items={[
          { label: "Mã", value: <code>{item.code}</code> },
          { label: "Tên", value: item.name },
          ...tab.fields.map((f) => ({ label: f.label, value: fieldText(f, item.attrs[f.key], names) })),
          ...(item.attrs.facility_ref ? [{ label: "Mã định danh cơ sở", value: item.attrs.facility_ref }] : []),
          { label: "Hiệu lực từ", value: dmy(item.valid_from) },
          ...(item.deactivation_reason ? [{ label: "Lý do ngừng dùng", value: item.deactivation_reason }] : []),
        ]}
      />
      {canEdit ? (
        <section>
          <h2>Các giá trị theo ngày hiệu lực</h2>
          <LoadState s={versions}>
            {(v) =>
              v.items.length === 0 ? (
                <p className="small mute">Mục nạp sẵn chưa sửa lần nào — giá trị hiện tại áp cho mọi kỳ.</p>
              ) : (
                <DataTable
                  caption="Các giá trị có hiệu lực từ ngày"
                  rows={v.items}
                  rowKey={(x) => x.id}
                  columns={[
                    { key: "from", header: "Từ ngày", cell: (x) => dmy(x.valid_from) },
                    { key: "to", header: "Đến trước ngày", cell: (x) => dmy(x.valid_to) },
                    { key: "name", header: "Tên", cell: (x) => x.after.name },
                    { key: "attrs", header: "Thuộc tính", cell: (x) => <span className="small">{summary(tab, { ...item, attrs: x.after.attrs ?? {} }, names)}</span> },
                    { key: "st", header: "Trạng thái", cell: (x) => (x.after.active ? "Đang dùng" : "Ngừng dùng") },
                  ]}
                />
              )
            }
          </LoadState>
        </section>
      ) : null}
    </DetailPage>
  );
}

function FieldInput({
  f,
  value,
  onChange,
  onBlurError,
  onDosesInput,
  options,
  error,
}: {
  f: Field;
  value: string;
  onChange: (v: string) => void;
  onBlurError: (error: string | undefined) => void;
  onDosesInput: (error: string | undefined) => void;
  options: Record<CntsCatalog, Item[]>;
  error?: string;
}) {
  const required = "required" in f && !!f.required;
  switch (f.kind) {
    case "select":
      return <SelectField label={f.label} required={required} value={value} onChange={onChange} error={error} placeholder="—" options={f.options.map(([v, l]) => ({ value: v, label: l }))} />;
    case "bool":
      return (
        <label className="row small">
          <input type="checkbox" aria-label={f.label} checked={value === "true"} onChange={(e) => onChange(e.target.checked ? "true" : "")} />
          {f.label}
        </label>
      );
    case "int":
      return <NumberField label={f.label} value={value} onChange={onChange} onBlurError={onBlurError} error={error} rule={INT_RULE} />;
    case "ref": {
      const opts = (options[f.catalog] ?? []).filter((o) => o.active);
      if (!f.multi)
        return (
          <SelectField
            label={f.label}
            required={required}
            value={value}
            onChange={onChange}
            error={error}
            placeholder="—"
            options={opts.map((o) => ({ value: o.code, label: `${o.name} (${o.code})` }))}
          />
        );
      const picked = codes(value);
      return (
        <fieldset className="field" style={{ border: 0, padding: 0 }}>
          <legend>
            {f.label}
            {required ? (
              <span className="req" aria-hidden="true">
                {" "}*
              </span>
            ) : null}
          </legend>
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
          {error ? (
            <span className="err" role="alert">
              {error}
            </span>
          ) : null}
        </fieldset>
      );
    }
    case "doses":
      return <DosesEditor value={value} onChange={onChange} onInputError={onDosesInput} vaccines={(options.cnts_vaccine ?? []).filter((v) => v.active)} error={error} />;
    case "codes":
      return <TextField label={f.label} value={value} onChange={onChange} error={error} hint={f.hint} />;
    default:
      return <TextField label={f.label} required={required} value={value} onChange={onChange} error={error} maxLength={f.max} />;
  }
}

/** Lần tiêm: số nguyên 1–20; ngày tuổi: số nguyên 0–3650 (như máy chủ). */
const SHOT_RULE: NumberRule = { required: true, integer: true, positive: true, max: 20 };
const AGE_RULE: NumberRule = { required: true, integer: true, max: 3650 };

/** Danh sách mũi của quy trình tiêm: vaccine, lần, ngày tuổi (FN-06; ví dụ gà thương phẩm UR-TINH-10). */
function DosesEditor({
  value,
  onChange,
  onInputError,
  vaccines,
  error,
}: {
  value: string;
  onChange: (v: string) => void;
  onInputError: (error: string | undefined) => void;
  vaccines: Item[];
  error?: string;
}) {
  const ds = parseDoses(value);
  // Chuỗi người đang gõ ở ô Lần / Ngày tuổi (khóa "dòng.trường") và lỗi của ô đó; ô hợp lệ mới ghi vào danh sách mũi.
  const [raw, setRaw] = useState<Record<string, string>>({});
  const [bad, setBad] = useState<Record<string, string>>({});
  const set = (next: Dose[]) => onChange(formatDoses(next));
  const reset = () => {
    setRaw({});
    setBad({});
    onInputError(undefined);
  };
  const numCell = (i: number, k: "shot" | "age_days", label: string, rule: NumberRule) => {
    const id = `${i}.${k}`;
    return (
      <>
        <input
          className="input"
          aria-label={`${label} mũi ${i + 1}`}
          inputMode="numeric"
          aria-invalid={bad[id] ? true : undefined}
          value={raw[id] ?? String(ds[i]![k])}
          onChange={(e) => {
            const t = e.target.value;
            const p = parseNumberInput(t, rule);
            const nextBad = { ...bad };
            if (p.ok) delete nextBad[id];
            else nextBad[id] = p.error;
            setRaw((r) => ({ ...r, [id]: t }));
            setBad(nextBad);
            onInputError(Object.keys(nextBad).length ? "Sửa các ô Lần / Ngày tuổi đang lỗi." : undefined);
            if (p.ok) set(ds.map((x, j) => (j === i ? { ...x, [k]: Number(p.value) } : x)));
          }}
        />
        {bad[id] ? (
          <span className="err" role="alert">
            {bad[id]}
          </span>
        ) : null}
      </>
    );
  };
  return (
    <fieldset className="field" style={{ border: 0, padding: 0, gridColumn: "1 / -1" }}>
      <legend>
        Danh sách mũi (vaccine, lần, ngày tuổi)
        <span className="req" aria-hidden="true">
          {" "}*
        </span>
      </legend>
      <DataTable
        caption="Danh sách mũi"
        rows={ds.map((d, i) => ({ d, i }))}
        rowKey={(r) => String(r.i)}
        empty="Chưa có mũi nào."
        columns={[
          {
            key: "vaccine",
            header: "Vaccine",
            cell: ({ d, i }) => (
              <select className="select" aria-label={`Vaccine mũi ${i + 1}`} value={d.vaccine} onChange={(e) => set(ds.map((x, j) => (j === i ? { ...x, vaccine: e.target.value } : x)))}>
                {vaccines.map((v) => (
                  <option key={v.code} value={v.code}>
                    {v.name}
                  </option>
                ))}
              </select>
            ),
          },
          { key: "shot", header: "Lần", cell: ({ i }) => numCell(i, "shot", "Lần", SHOT_RULE) },
          { key: "age", header: "Ngày tuổi", cell: ({ i }) => numCell(i, "age_days", "Ngày tuổi", AGE_RULE) },
          {
            key: "del",
            header: "",
            cell: ({ i }) => (
              <button type="button" className="btn btn-o btn-sm" aria-label={`Bỏ mũi ${i + 1}`} onClick={() => (reset(), set(ds.filter((_, j) => j !== i)))}>
                <Icon name="trash" size={14} />
              </button>
            ),
          },
        ]}
      />
      <button
        type="button"
        className="btn btn-o btn-sm"
        disabled={vaccines.length === 0}
        onClick={() => set([...ds, { vaccine: vaccines[0]?.code ?? "", shot: 1, age_days: (ds[ds.length - 1]?.age_days ?? 0) + 7 }])}
      >
        Thêm mũi
      </button>
      {error ? (
        <span className="err" role="alert">
          {error}
        </span>
      ) : null}
    </fieldset>
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
