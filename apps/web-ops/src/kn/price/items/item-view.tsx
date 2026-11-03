"use client";

import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import { LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { dmy, itemLabel, KIND_LABEL, STATE_LABEL, validRange, type PriceCatalog, type PriceGroup, type PriceItemDetail } from "./api";
import type { FieldErrors } from "./item-form";

export function itemFieldErrors(problem: ApiProblem | null, local: FieldErrors, dateField: "valid_from" | "valid_to" = "valid_from"): FieldErrors {
  const errors: FieldErrors = problem ? fieldErrorMap(problem) : {};
  if (problem?.code === "price.item_duplicate") errors.name = problem.message;
  if (problem?.code === "price.item_date_invalid") errors[dateField] = problem.message;
  return { ...errors, ...Object.fromEntries(Object.entries(local).filter(([, value]) => !!value)) };
}

export function ItemDateField({ label, value, onChange, error, hint, disabled }: {
  label: string; value: string; onChange: (value: string) => void;
  error?: string; hint?: string; disabled?: boolean;
}) {
  const id = `item-date-${label.replace(/\s+/g, "-")}`;
  return <div className="field"><label htmlFor={id}>{label}</label>
    <input id={id} type="date" className="input" value={value} disabled={disabled}
      aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined}
      onChange={(event) => onChange(event.target.value)} />
    {error ? <span role="alert" className="err" id={`${id}-error`}>{error}</span> : hint ? <span className="mute hint">{hint}</span> : null}
  </div>;
}

export function itemLog(detail: PriceItemDetail): LogEntry[] {
  let previous: PriceItemDetail["history"][number] | undefined;
  return [...detail.history].sort((a, b) => a.version - b.version).map((entry) => {
    const fields: [keyof typeof entry, string][] = [
      ["name", "Tên loại"], ["variety", "Giống / phẩm cấp"], ["group_code", "Mã nhóm hàng"],
      ["unit_code", "Mã đơn vị tính"], ["spec", "Quy cách"], ["sale_place", "Nơi bán"],
      ["kind", "Loại"], ["sort_order", "Thứ tự"], ["valid_from", "Hiệu lực từ"], ["status", "Trạng thái"],
    ];
    const old = previous;
    previous = entry;
    return {
      event: `Phiên bản ${entry.version} · ${entry.status === "retired" ? "Ngưng mặt hàng" : "Cập nhật mặt hàng"}`,
      actor_display: `Mã tham chiếu ${entry.actor_ref}`, at: entry.event_time, reason: entry.note,
      changes: fields.filter(([key]) => !old || old[key] !== entry[key]).map(([key, label]) => ({
        field: label, old: old ? String(old[key] ?? "") || null : null, new: String(entry[key] ?? "") || null,
      })),
    };
  }).reverse();
}
export function ItemLog({ detail }: { detail: PriceItemDetail }) {
  return <LogPanel entries={itemLog(detail)} />;
}
export function GroupLog() {
  return <aside className="card log" aria-label="Log"><h2>Log</h2><p className="mute">Chưa có lịch sử thay đổi nhóm hàng. Phiên bản và hiệu lực hiện tại hiển thị trong thông tin nhóm.</p></aside>;
}
export function GroupSummary({ group }: { group: PriceGroup }) {
  return <LabelValueBlock title="Thông tin nhóm hàng" items={[
    { label: "Mã nhóm", value: group.code }, { label: "Tên nhóm", value: group.name },
    { label: "Thứ tự", value: group.sort_order }, { label: "Số mặt hàng", value: group.items },
    { label: "Phiên bản", value: group.version }, { label: "Hiệu lực từ", value: dmy(group.valid_from) },
  ]} />;
}
export function ItemSummary({ detail: item, catalog }: { detail: PriceItemDetail; catalog: PriceCatalog }) {
  return <>
    <LabelValueBlock title="Thông tin mặt hàng" items={[
      { label: "Mã mặt hàng", value: item.code }, { label: "Tên loại", value: item.name },
      { label: "Giống / phẩm cấp", value: item.variety }, { label: "Nhóm hàng", value: catalog.groups.find((g) => g.code === item.group_code)?.name ?? item.group_code },
      { label: "Đơn vị tính", value: catalog.units.find((u) => u.code === item.unit_code)?.name ?? item.unit_code },
      { label: "Quy cách", value: item.spec }, { label: "Nơi bán", value: item.sale_place },
      { label: "Loại", value: KIND_LABEL[item.kind] }, { label: "Thứ tự", value: item.sort_order },
      { label: "Hiệu lực", value: validRange(item) }, { label: "Trạng thái", value: STATE_LABEL[item.state] },
      { label: "Số xã", value: item.point_count === null ? "Mọi xã" : item.point_count },
      { label: "Thay thế cho", value: item.replaces_code }, { label: "Được thay thế bởi", value: item.replaced_by.join(", ") },
    ]} />
    <DataTable caption="Lịch sử phiên bản mặt hàng" columns={[
      { key: "version", header: "Phiên bản", cell: (row) => row.version },
      { key: "valid", header: "Hiệu lực từ", cell: (row) => dmy(row.valid_from) },
      { key: "name", header: "Mặt hàng", cell: itemLabel },
      { key: "state", header: "Trạng thái", cell: (row) => row.status === "retired" ? "Ngưng" : "Hoạt động" },
    ]} rows={item.history} rowKey={(row) => String(row.version)} page={1} pageSize={item.history.length || 1} total={item.history.length} />
  </>;
}
