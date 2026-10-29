"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { NumberField, TextField } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel } from "@/components/patterns/detail";
import { StatusPill } from "@/components/patterns/list";
import { dt, LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { KIND_ATTRS, KIND_LABEL, TEMPLATED, TEMPLATES, TEMPLATE_LABEL, useKhtcApi, useKhtcRights, type KhtcCatalogItem, type KhtcCatalogKind } from "./api";
import { CATALOG_BASE, CATALOG_SORT_RULE, catalogEditHref, catalogItemHref, catalogListHref, catalogLog, catalogSortInputError, catalogValues, isCatalogKind, validateCatalogValues } from "./catalog";

function useCatalogRoute() {
  const params = useParams<{ kind: string; code: string }>();
  const query = useSearchParams();
  return { kind: isCatalogKind(params.kind ?? "") ? params.kind as KhtcCatalogKind : null, code: params.code ?? "", retired: query.get("retired") === "1" };
}

function UnknownCatalog() {
  return <DetailPage back={{ href: CATALOG_BASE, label: "Danh sách danh mục" }} title="Danh mục KHTC"><p className="mute">Không có loại danh mục này.</p></DetailPage>;
}

function CannotEdit({ kind, retired }: { kind: KhtcCatalogKind; retired: boolean }) {
  return <DetailPage back={{ href: catalogListHref(kind, retired), label: "Danh sách danh mục" }} title={KIND_LABEL[kind]}>
    <ReadOnlyNotice>Chỉ xem — Admin quản lý Danh mục KHTC.</ReadOnlyNotice>
  </DetailPage>;
}

export function KhtcCatalogCreateScreen() {
  const { kind, retired } = useCatalogRoute();
  const rights = useKhtcRights();
  if (!kind) return <UnknownCatalog />;
  if (!rights.catalog) return <CannotEdit kind={kind} retired={retired} />;
  return <CatalogEditor key={kind} kind={kind} retired={retired} history={[]} />;
}

export function KhtcCatalogDetailScreen() { return <ExistingCatalog edit={false} />; }
export function KhtcCatalogEditScreen() { return <ExistingCatalog edit />; }

function ExistingCatalog({ edit }: { edit: boolean }) {
  const { kind, code, retired } = useCatalogRoute();
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const s = useLoad(kind && (!edit || rights.catalog) ? () => api.catalogHistory(kind, code) : null, [api, kind, code, edit, rights.catalog]);
  if (!kind) return <UnknownCatalog />;
  if (edit && !rights.catalog) return <CannotEdit kind={kind} retired={retired} />;
  return <LoadState s={s}>{({ items }) => {
    const item = [...items].sort((a, b) => b.version - a.version)[0];
    if (!item) return <DetailPage back={{ href: catalogListHref(kind, retired), label: "Danh sách danh mục" }} title={KIND_LABEL[kind]}><p className="mute">Không có mục danh mục này.</p></DetailPage>;
    return edit ? <CatalogEditor key={item.version_id} kind={kind} item={item} retired={retired} history={items} /> : <CatalogDetail item={item} retired={retired} history={items} reload={s.reload} />;
  }}</LoadState>;
}

function CatalogDetail({ item, retired, history, reload }: { item: KhtcCatalogItem; retired: boolean; history: KhtcCatalogItem[]; reload: () => void }) {
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const act = useAction();
  const toggle = async () => {
    if (!rights.catalog) return;
    const r = await act.run(() => api.reviseCatalog(item.kind, item.code, { name: item.name, sort_order: item.sort_order, active: !item.active, default_for: item.default_for, attrs: item.attrs }), item.active ? "Đã ngừng dùng mục danh mục." : "Đã dùng lại mục danh mục.");
    if (r) reload();
  };
  return <DetailPage back={{ href: catalogListHref(item.kind, retired), label: "Danh sách danh mục" }} title={item.name}
    badge={<StatusPill tone={item.active ? "ok" : "neutral"}>{item.active ? "Đang dùng" : "Ngừng dùng"}</StatusPill>}
    actions={rights.catalog ? <><Link className="btn btn-p" href={catalogEditHref(item.kind, item.code, retired)}>Sửa</Link><button type="button" className="btn btn-o" disabled={act.busy} onClick={() => void toggle()}>{item.active ? "Ngừng dùng" : "Dùng lại"}</button></> : null}
    log={<LogPanel entries={catalogLog(history)} />}>
    {act.view}
    {!rights.catalog ? <ReadOnlyNotice>Chỉ xem — Admin quản lý Danh mục KHTC.</ReadOnlyNotice> : null}
    <LabelValueBlock title="Thông tin danh mục" items={[
      { label: "Loại danh mục", value: KIND_LABEL[item.kind] }, { label: "Mã", value: item.code }, { label: item.kind === "signer" ? "Họ tên" : "Tên", value: item.name },
      ...(KIND_ATTRS[item.kind] ?? []).map((f) => ({ label: f.label, value: item.attrs[f.key] })),
      { label: "Thứ tự", value: String(item.sort_order) },
      ...(TEMPLATED.includes(item.kind) ? [{ label: "Mặc định cho mẫu", value: item.default_for.map((t) => TEMPLATE_LABEL[t] ?? t).join(", ") }] : []),
      { label: "Phiên bản", value: String(item.version) }, { label: "Cập nhật", value: dt(item.recorded_at) }, { label: "Người cập nhật", value: item.actor.display },
    ]} />
  </DetailPage>;
}

function CatalogEditor({ kind, item, retired, history }: { kind: KhtcCatalogKind; item?: KhtcCatalogItem; retired: boolean; history: KhtcCatalogItem[] }) {
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const router = useRouter();
  const formId = useId();
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const [values, setValues] = useState(() => catalogValues(kind, item));
  const [defaults, setDefaults] = useState(item?.default_for ?? []);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [rejectedSort, setRejectedSort] = useState<string | undefined>();
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  if (act.problem?.code === "extension.khtc_catalog_exists") server.code = "Mã đã có trong danh mục (kể cả mục đã ngừng dùng).";
  if (act.problem?.code === "extension.khtc_default_conflict") server.default_for = "Mẫu đã có người ký mặc định đang dùng.";
  const set = (field: string) => (value: string) => {
    setValues((v) => ({ ...v, [field]: value }));
    setErrors((e) => ({ ...e, [field]: "" }));
  };
  const save = async () => {
    if (!rights.catalog || act.busy) return;
    const checked = validateCatalogValues(kind, values, defaults, item?.active ?? true);
    setErrors({ ...checked.errors, ...(rejectedSort ? { sort_order: rejectedSort } : {}) });
    if (!checked.body || rejectedSort) return;
    const body = checked.body;
    const saved = await act.run(() => item ? api.reviseCatalog(kind, item.code, body) : api.createCatalog(kind, body));
    if (saved && mounted.current) router.push(catalogItemHref(kind, saved.code, retired));
  };
  return <DetailPage back={{ href: catalogListHref(kind, retired), label: "Danh sách danh mục" }} title={`${item ? "Sửa" : "Thêm"} ${KIND_LABEL[kind].toLowerCase()}`}
    actions={<><Link className="btn btn-o" href={catalogListHref(kind, retired)}>Hủy</Link><button type="submit" form={formId} className="btn btn-p" disabled={act.busy}>Lưu</button></>}
    log={<LogPanel entries={catalogLog(history)} empty="Chưa lưu mục danh mục." />}>
    {act.view}
    <form id={formId} noValidate onSubmit={(event) => { event.preventDefault(); void save(); }}>
      <TextField label="Mã" required maxLength={40} value={values.code ?? ""} onChange={set("code")} disabled={!!item || act.busy} error={errors.code || server.code} hint="Bắt đầu bằng chữ không dấu hoặc số; mã không đổi sau khi tạo." />
      <TextField label={kind === "signer" ? "Họ tên" : "Tên"} required maxLength={500} value={values.name ?? ""} onChange={set("name")} disabled={act.busy} error={errors.name || server.name} />
      {(KIND_ATTRS[kind] ?? []).map((field) => <TextField key={field.key} label={field.label} required={field.required} maxLength={300} value={values[`attrs.${field.key}`] ?? ""} onChange={set(`attrs.${field.key}`)} disabled={act.busy} error={errors[`attrs.${field.key}`] || server[`attrs.${field.key}`]} />)}
      <NumberField label="Thứ tự" value={values.sort_order ?? ""} rule={CATALOG_SORT_RULE} hint="Số nguyên từ 0 đến 100000; để trống dùng 0." disabled={act.busy} error={rejectedSort || errors.sort_order || server.sort_order}
        onChange={(raw) => {
          const error = catalogSortInputError(raw);
          setRejectedSort(error);
          // Giữ số an toàn trong NumberField; lỗi nhập bị từ chối vẫn chặn Lưu cho tới khi sửa lại.
          if (!error) set("sort_order")(raw);
        }} onBlurError={(error) => setErrors((e) => ({ ...e, sort_order: error ?? "" }))} />
      {TEMPLATED.includes(kind) ? <fieldset className="field"><legend>Mặc định cho mẫu</legend>
        {TEMPLATES.map((template) => <label key={template} className="row small"><input type="checkbox" checked={defaults.includes(template)} disabled={act.busy} onChange={(e) => setDefaults((v) => e.target.checked ? [...v, template] : v.filter((t) => t !== template))} /> {TEMPLATE_LABEL[template]}</label>)}
        {server.default_for ? <span className="err" role="alert">{server.default_for}</span> : null}
      </fieldset> : null}
    </form>
  </DetailPage>;
}
