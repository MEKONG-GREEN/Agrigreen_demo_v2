import { validateForm, type FieldRule } from "@/components/form";
import type { LogEntry } from "@/components/patterns/detail";
import { CATALOG_KINDS, KIND_ATTRS, TEMPLATE_LABEL, type KhtcCatalogInput, type KhtcCatalogItem, type KhtcCatalogKind } from "./api";

export const CATALOG_BASE = "/kn/phong-ban/ke_hoach_tai_chinh/danh-muc-khtc";
export const CATALOG_SORT_RULE = { integer: true, max: 100000 } as const;
export type CatalogValues = Record<string, string>;

export const isCatalogKind = (value: string): value is KhtcCatalogKind => CATALOG_KINDS.includes(value as KhtcCatalogKind);
export const catalogListHref = (kind: KhtcCatalogKind, retired = false) => `${CATALOG_BASE}?kind=${kind}${retired ? "&retired=1" : ""}`;
// The record segment keeps legal codes such as "them-moi" separate from the create route.
export const catalogItemHref = (kind: KhtcCatalogKind, code: string, retired = false) => `${CATALOG_BASE}/${kind}/muc/${encodeURIComponent(code)}${retired ? "?retired=1" : ""}`;
export const catalogEditHref = (kind: KhtcCatalogKind, code: string, retired = false) => `${CATALOG_BASE}/${kind}/muc/${encodeURIComponent(code)}/sua${retired ? "?retired=1" : ""}`;
export const catalogCreateHref = (kind: KhtcCatalogKind, retired = false) => `${CATALOG_BASE}/${kind}/them-moi${retired ? "?retired=1" : ""}`;

export function catalogValues(kind: KhtcCatalogKind, item?: KhtcCatalogItem): CatalogValues {
  return { code: item?.code ?? "", name: item?.name ?? "", sort_order: String(item?.sort_order ?? 0),
    ...Object.fromEntries((KIND_ATTRS[kind] ?? []).map((f) => [`attrs.${f.key}`, item?.attrs[f.key] ?? ""])) };
}

/** Bound the raw input BEFORE the shared normalizer/Decimal parser, including on blur. */
export function catalogSortInputError(raw: string): string | undefined {
  if (raw.length > 32) return "Thứ tự quá dài; nhập số nguyên từ 0 đến 100000.";
  if (/[eE]/.test(raw)) return "Không dùng số mũ; nhập số nguyên từ 0 đến 100000.";
  return undefined;
}

export function validateCatalogValues(kind: KhtcCatalogKind, values: CatalogValues, defaults: string[], active: boolean): { errors: Record<string, string>; body: KhtcCatalogInput | null } {
  const rules: Record<string, FieldRule> = {
    code: { kind: "text", required: true, maxLength: 40 },
    name: { kind: "text", required: true, maxLength: 500 },
    sort_order: { kind: "number", ...CATALOG_SORT_RULE },
    ...Object.fromEntries((KIND_ATTRS[kind] ?? []).map((f) => [`attrs.${f.key}`, { kind: "text" as const, required: f.required, maxLength: 300 }])),
  };
  const numericError = catalogSortInputError(values.sort_order ?? "");
  const checked = validateForm({ ...values, sort_order: numericError ? "" : (values.sort_order ?? "") }, rules);
  const errors: Record<string, string> = {};
  for (const [field, error] of Object.entries(checked.errors)) if (error) errors[field] = error;
  const code = (checked.values.code ?? "").trim().toLowerCase();
  if (!errors.code && !/^[a-z0-9][a-z0-9._-]{0,39}$/.test(code)) errors.code = "Mã bắt đầu bằng chữ không dấu hoặc số; chỉ dùng chữ, số, dấu chấm, gạch dưới hoặc gạch ngang.";
  if (numericError) errors.sort_order = numericError;
  if (Object.keys(errors).length) return { errors, body: null };
  return { errors, body: {
    code, name: (checked.values.name ?? "").trim(), sort_order: Number(checked.values.sort_order || "0"), active, default_for: defaults,
    attrs: Object.fromEntries((KIND_ATTRS[kind] ?? []).map((f) => [f.key, (checked.values[`attrs.${f.key}`] ?? "").trim()])),
  } };
}

/** The history endpoint supplies real actor names/timestamps and complete immutable versions. */
export function catalogLog(items: KhtcCatalogItem[]): LogEntry[] {
  const versions = [...items].sort((a, b) => a.version - b.version);
  const label = (item: KhtcCatalogItem, key: string): string => {
    if (key === "active") return item.active ? "Đang dùng" : "Ngừng dùng";
    if (key === "default_for") return item.default_for.map((t) => TEMPLATE_LABEL[t] ?? t).join(", ");
    if (key.startsWith("attrs.")) return item.attrs[key.slice(6)] ?? "";
    return String(item[key as "code" | "name" | "sort_order"]);
  };
  return versions.map((item, i) => {
    const previous = versions[i - 1];
    const fields = [{ key: "code", label: "Mã" }, { key: "name", label: item.kind === "signer" ? "Họ tên" : "Tên" },
      ...(KIND_ATTRS[item.kind] ?? []).map((f) => ({ key: `attrs.${f.key}`, label: f.label })),
      { key: "sort_order", label: "Thứ tự" }, { key: "active", label: "Trạng thái" }, { key: "default_for", label: "Mặc định cho mẫu" }];
    return {
      event: `${!previous ? "Thêm mới" : previous.active !== item.active ? (item.active ? "Dùng lại" : "Ngừng dùng") : "Cập nhật"} · v${item.version}`,
      actor_display: item.actor.display ?? "—", at: item.recorded_at,
      changes: fields.flatMap((f) => {
        const old = previous ? label(previous, f.key) : "";
        const next = label(item, f.key);
        return old === next ? [] : [{ field: f.label, old: old || null, new: next || null }];
      }),
    };
  }).reverse();
}
