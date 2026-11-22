import { validateForm } from "@/components/form";
import type { PriceCatalog, PriceItem, PriceItemInput } from "./api";
import { isCalendarDay } from "../calendar-routes";
import { isItemCode } from "./item-routes";

export type ItemFields = {
  name: string; variety: string; group_code: string; unit_code: string;
  spec: string; sale_place: string; kind: string; valid_from: string;
  valid_to: string; replaces_code: string; points: string;
};
export type FieldErrors = Record<string, string | undefined>;

export function initialItemFields(catalog: PriceCatalog, item?: PriceItem, replace = false): ItemFields {
  return {
    name: item?.name ?? "", variety: item?.variety ?? "",
    group_code: item?.group_code ?? catalog.groups[0]?.code ?? "",
    unit_code: item?.unit_code ?? catalog.units[0]?.code ?? "",
    spec: item?.spec ?? "", sale_place: item?.sale_place ?? "", kind: item?.kind ?? "basic",
    valid_from: "", valid_to: "", replaces_code: replace ? item?.code ?? "" : "", points: "",
  };
}

export function validateItemFields(fields: ItemFields, catalog: PriceCatalog, create: boolean, locked?: PriceItem) {
  const checked = validateForm(fields, {
    name: { kind: "text", required: true, maxLength: 100 },
    variety: { kind: "text", required: true, maxLength: 100 },
    group_code: { kind: "text", required: true, maxLength: 40 },
    unit_code: { kind: "text", required: true, maxLength: 40 },
    spec: { kind: "text", maxLength: 100 }, sale_place: { kind: "text", maxLength: 200 },
    valid_from: { kind: "text", maxLength: 10 }, valid_to: { kind: "text", maxLength: 10 },
    replaces_code: { kind: "text", maxLength: 7 },
  });
  if (!catalog.groups.some((group) => group.code === fields.group_code) && locked?.group_code !== fields.group_code)
    checked.errors.group_code = "Chọn nhóm hàng trong danh mục.";
  if (!catalog.units.some((unit) => unit.code === fields.unit_code) && locked?.unit_code !== fields.unit_code)
    checked.errors.unit_code = "Chọn đơn vị tính trong danh mục.";
  if (!["basic", "local"].includes(fields.kind)) checked.errors.kind = "Chọn loại mặt hàng.";
  if (fields.valid_from && !isCalendarDay(fields.valid_from)) checked.errors.valid_from = "Ngày không hợp lệ.";
  if (create && fields.valid_to && !isCalendarDay(fields.valid_to)) checked.errors.valid_to = "Ngày không hợp lệ.";
  if (create && fields.valid_to && fields.valid_from && !checked.errors.valid_to && !checked.errors.valid_from && fields.valid_to < fields.valid_from)
    checked.errors.valid_to = "Hiệu lực đến không được trước hiệu lực từ.";
  if (create && fields.replaces_code && !isItemCode(fields.replaces_code)) checked.errors.replaces_code = "Mã mặt hàng phải có dạng MH00001.";
  const points = create && fields.kind === "local" ? fields.points.split(/[\s,;]+/).filter(Boolean) : [];
  if (points.some((point) => !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(point))) checked.errors.points = "Mã xã không hợp lệ (tối đa 64 ký tự mỗi mã).";
  if (locked) {
    for (const key of ["variety", "group_code", "unit_code", "spec", "kind"] as const)
      if (fields[key] !== (locked[key] ?? "")) checked.errors[key] = "Mặt hàng đã có giá: trường này không được sửa.";
  }
  return { ...checked, valid: Object.keys(checked.errors).length === 0 };
}

export function itemBody(fields: ItemFields, create: boolean): PriceItemInput {
  const body: PriceItemInput = {
    name: fields.name.trim(), variety: fields.variety.trim(), group_code: fields.group_code,
    unit_code: fields.unit_code, spec: fields.spec.trim(), sale_place: fields.sale_place.trim(),
    kind: fields.kind as "basic" | "local",
  };
  if (fields.valid_from) body.valid_from = fields.valid_from;
  if (create) {
    if (fields.valid_to) body.valid_to = fields.valid_to;
    if (fields.replaces_code) body.replaces_code = fields.replaces_code;
    const points = fields.kind === "local" ? fields.points.split(/[\s,;]+/).filter(Boolean) : [];
    if (points.length) body.points = [...new Set(points)];
  }
  return body;
}
