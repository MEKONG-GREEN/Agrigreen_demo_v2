"use client";

import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import {
  LabelValueBlock,
  LogPanel,
  type LogEntry,
} from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import type { PricePointDetail, PriceRegion } from "./api";
import { dmy, officerLabel, OFFICER_ROLE_LABEL } from "./api";
import type { FieldErrors } from "./point-form";

export function pointFieldErrors(
  problem: ApiProblem | null,
  local: FieldErrors,
): FieldErrors {
  const errors: FieldErrors = problem ? fieldErrorMap(problem) : {};
  const code = problem?.code ?? "";
  if (code === "price.valid_from") errors.valid_from = problem!.message;
  if (["price.admin_unit_taken", "price.admin_unit_invalid"].includes(code))
    errors.admin_unit_code = problem!.message;
  if (["price.region_not_found", "price.region_in_use"].includes(code))
    errors.region_code = problem!.message;
  if (["price.officer_invalid", "price.officer_not_found"].includes(code))
    errors.officer_ref = problem!.message;
  return {
    ...errors,
    ...Object.fromEntries(Object.entries(local).filter(([, value]) => !!value)),
  };
}
export function PointDateField({
  label,
  value,
  onChange,
  error,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  error?: string;
}) {
  const id = `point-date-${label.replace(/\s+/g, "-")}`;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        type="date"
        className="input"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
      />
      {error ? (
        <span className="err" role="alert" id={`${id}-error`}>
          {error}
        </span>
      ) : (
        <small className="muted">Để trống = ngày báo giá kế tiếp.</small>
      )}
    </div>
  );
}
export function pointLogEntries(d: PricePointDetail): LogEntry[] {
  const rows = [...d.versions].sort((a, b) => a.version - b.version);
  const events: LogEntry[] = rows.map((v, i) => {
    const previous = rows[i - 1];
    const fields = [
      ["Mã ĐVHC", previous?.admin_unit_code ?? null, v.admin_unit_code],
      ["Tên xã", previous?.name ?? null, v.name],
      ["Nhóm vùng", previous?.region_code ?? null, v.region_code],
      [
        "Hiệu lực từ",
        previous?.valid_from ? dmy(previous.valid_from) : null,
        dmy(v.valid_from),
      ],
      ["Trạng thái", previous?.status ?? null, v.status],
    ] as const;
    return {
      event:
        v.status === "retired"
          ? "Ngưng xã báo giá"
          : i === 0
            ? "Thêm xã báo giá"
            : "Sửa xã báo giá",
      actor_display: `Mã tham chiếu ${v.actor_ref}`,
      at: v.event_time,
      reason: v.note,
      changes: fields
        .filter(([, old, next]) => old !== next)
        .map(([field, old, next]) => ({ field, old, new: next })),
    };
  });
  const previousOfficers = new Map<
    string,
    PricePointDetail["officers"][number]
  >();
  for (const v of [...d.officers].sort((a, b) => a.version - b.version)) {
    const previous = previousOfficers.get(v.role);
    events.push({
      event: v.officer_ref ? "Phân công cán bộ" : "Bỏ phân công",
      actor_display: `Mã tham chiếu ${v.actor_ref}`,
      at: v.event_time,
      changes: [
        {
          field: OFFICER_ROLE_LABEL[v.role],
          old: previous
            ? officerLabel(previous.officer_ref, previous.name, "Bỏ phân công")
            : null,
          new: officerLabel(v.officer_ref, v.name, "Bỏ phân công"),
        },
        {
          field: "Hiệu lực từ",
          old: previous ? dmy(previous.valid_from) : null,
          new: dmy(v.valid_from),
        },
      ],
    });
    previousOfficers.set(v.role, v);
  }
  return events.sort((a, b) => b.at.localeCompare(a.at));
}
export function PointLog({ detail }: { detail: PricePointDetail }) {
  return <LogPanel entries={pointLogEntries(detail)} />;
}
export function RegionLog() {
  return (
    <LogPanel
      entries={[]}
      empty="Chưa có nhật ký thay đổi của nhóm vùng. Hiện chỉ có phiên bản và ngày hiệu lực."
    />
  );
}
export function RegionSummary({ region: r }: { region: PriceRegion }) {
  return (
    <LabelValueBlock
      title="Nhóm vùng"
      items={[
        { label: "Mã", value: r.code },
        { label: "Tên", value: r.name },
        {
          label: "Trạng thái",
          value: r.status === "active" ? "Hoạt động" : "Ngưng",
        },
        { label: "Phiên bản", value: r.version },
        { label: "Hiệu lực từ", value: dmy(r.valid_from) },
        { label: "Số xã đang hoạt động", value: r.points },
      ]}
    />
  );
}
export function OfficerHistory({ detail }: { detail: PricePointDetail }) {
  return (
    <DataTable
      caption="Lịch sử phân công"
      columns={[
        { key: "role", header: "Vai", cell: (v) => OFFICER_ROLE_LABEL[v.role] },
        {
          key: "officer",
          header: "Cán bộ",
          cell: (v) => officerLabel(v.officer_ref, v.name, "Bỏ phân công"),
        },
        { key: "date", header: "Hiệu lực từ", cell: (v) => dmy(v.valid_from) },
      ]}
      rows={detail.officers}
      rowKey={(v) => `${v.role}:${v.version}`}
    />
  );
}
