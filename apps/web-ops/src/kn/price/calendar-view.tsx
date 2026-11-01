"use client";

import { useId, type ComponentProps } from "react";
import { NumberField } from "@/components/form";
import {
  LabelValueBlock,
  LogPanel,
  type LogEntry,
} from "@/components/patterns/detail";
import { DataTable, StatusPill, type Column } from "@/components/patterns/list";
import { dt } from "@/kn/cycle/common";
import {
  WEEKDAYS,
  RUN_KIND_LABEL,
  type PriceCalendar,
  type PriceCalendarRun,
  type PriceSchedule,
  type PriceThreshold,
  type PriceThresholds,
} from "./api";
import { numberTextError, PRICE_NUMBER_LENGTH } from "./calendar-form";

export const WD_LABEL = Object.fromEntries(
  WEEKDAYS.map((w) => [w.code, w.label]),
) as Record<string, string>;
export function dmy(day: string): string {
  const [y, m, d] = day.split("-");
  return `${d}/${m}/${y}`;
}
export function hm(iso: string | null | undefined): string {
  if (!iso) return "—";
  const at = new Date(Date.parse(iso) + 7 * 3_600_000);
  return `${String(at.getUTCHours()).padStart(2, "0")}:${String(at.getUTCMinutes()).padStart(2, "0")}`;
}
export function nextDay(day: string): string {
  const date = new Date(`${day}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() + 1);
  return date.toISOString().slice(0, 10);
}
export function ErrorLine({ error, id }: { error?: string; id?: string }) {
  return error ? (
    <span className="err" role="alert" id={id}>
      {error}
    </span>
  ) : null;
}

/** UI-KIT chưa có date/time picker: giữ input gốc, bổ sung nhãn và lỗi tại đúng ô. */
export function CalendarField({
  label,
  type,
  value,
  onChange,
  error,
  min,
}: {
  label: string;
  type: "date" | "time";
  value: string;
  onChange: (v: string) => void;
  error?: string;
  min?: string;
}) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>
        {label}{" "}
        <span className="req" aria-hidden="true">
          *
        </span>
      </label>
      <input
        id={id}
        className="input"
        type={type}
        value={value}
        min={min}
        aria-required="true"
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        onChange={(e) => onChange(e.target.value)}
      />
      <ErrorLine error={error} id={`${id}-error`} />
    </div>
  );
}

/** Guard tại feature: bắt capture trước onBlur của NumberField, không sửa shared form.tsx (#163). */
export function PriceNumberField(props: ComponentProps<typeof NumberField>) {
  const issue = numberTextError(props.value, props.rule);
  return (
    <div
      onBlurCapture={(e) => {
        if (issue) {
          e.stopPropagation();
          props.onBlurError?.(issue);
        }
      }}
    >
      <NumberField
        {...props}
        error={issue ?? props.error}
        onChange={(raw) =>
          props.onChange(raw.slice(0, PRICE_NUMBER_LENGTH + 1))
        }
      />
    </div>
  );
}

export function ScheduleSummary({ value }: { value: PriceSchedule }) {
  return (
    <LabelValueBlock
      title="Kỳ báo giá & giờ khóa"
      items={[
        {
          label: "Ngày báo giá",
          value: value.weekdays.map((w) => WD_LABEL[w]).join(", "),
        },
        { label: "Giờ khóa nhập", value: value.lock_at },
        { label: "Hạn phản hồi xác minh", value: value.respond_by },
        {
          label: "Áp dụng từ",
          value: value.valid_from ? dmy(value.valid_from) : "Mặc định",
        },
        { label: "Giờ nhắc lần 1", value: value.remind1_at },
        { label: "Giờ nhắc lần 2", value: value.remind2_at },
        {
          label: "Kênh nhắc",
          value: value.channels
            .map((x) => (x === "email" ? "Email" : "Trong ứng dụng"))
            .join(", "),
        },
        { label: "Nội dung mẫu", value: value.template },
        { label: "Phiên bản", value: value.version },
      ]}
    />
  );
}

export function ThresholdSummary({ value }: { value: PriceThreshold }) {
  return (
    <LabelValueBlock
      title={value.scope === "default" ? "Tham số chung" : "Ngưỡng riêng"}
      items={[
        {
          label: "Áp dụng cho",
          value:
            value.scope === "default"
              ? "Mặc định"
              : `${value.scope === "group" ? "Nhóm hàng" : "Mặt hàng"} ${value.code}`,
        },
        { label: "Ngưỡng ±%", value: value.pct },
        ...(value.scope === "default"
          ? [
              {
                label: "Số ngày tìm giá kỳ trước (N)",
                value: String(value.lookback_days ?? 7),
              },
              { label: "Ngưỡng đi ngang %", value: value.flat_pct ?? "1.00" },
              {
                label: "Ngưỡng phiếu điều tra %",
                value: value.survey_pct ?? "40.00",
              },
            ]
          : []),
        { label: "Phiên bản", value: value.version },
        {
          label: "Trạng thái",
          value:
            value.status === "retired" ? "Đã xóa ngưỡng riêng" : "Đang áp dụng",
        },
      ]}
    />
  );
}

const SCHEDULE_LABELS = {
  weekdays: "Ngày báo giá",
  lock_at: "Giờ khóa",
  respond_by: "Hạn phản hồi",
  remind1_at: "Nhắc lần 1",
  remind2_at: "Nhắc lần 2",
  channels: "Kênh nhắc",
  template: "Nội dung mẫu",
  valid_from: "Áp dụng từ",
};
export function scheduleLog(c: PriceCalendar): LogEntry[] {
  const ordered = [...c.history].sort((a, b) => a.version - b.version);
  return ordered
    .map((entry, i) => {
      const previous = ordered[i - 1];
      return {
        event: `Lưu cấu hình · phiên bản ${entry.version}`,
        actor_display: entry.actor_ref
          ? `Mã cán bộ ${entry.actor_ref}`
          : "API chưa có người thao tác",
        at: entry.recorded_at,
        changes: (
          Object.keys(SCHEDULE_LABELS) as (keyof typeof SCHEDULE_LABELS)[]
        ).flatMap((key) => {
          const text = (s: PriceSchedule | undefined) =>
            !s
              ? null
              : Array.isArray(s[key])
                ? (s[key] as string[]).join(", ")
                : (s[key] ?? null);
          const old = text(previous);
          const value = text(entry);
          return old === value
            ? []
            : [{ field: SCHEDULE_LABELS[key], old, new: value }];
        }),
      };
    })
    .reverse();
}

export function thresholdLog(
  t: PriceThresholds,
  scope: PriceThreshold["scope"],
  code: string,
): LogEntry[] {
  const ordered = t.history
    .filter((h) => h.scope === scope && h.code === code)
    .sort((a, b) => a.version - b.version);
  const labels = {
    pct: "Ngưỡng ±%",
    lookback_days: "Số ngày N",
    flat_pct: "Ngưỡng đi ngang %",
    survey_pct: "Ngưỡng phiếu điều tra %",
    status: "Trạng thái",
  };
  return ordered
    .flatMap((entry, i) => {
      if (!entry.recorded_at) return []; // Không bịa thời điểm/actor cho dữ liệu mặc định chưa có sổ.
      const previous = ordered[i - 1];
      return [
        {
          event: `${entry.status === "retired" ? "Xóa" : "Lưu"} ngưỡng · phiên bản ${entry.version}`,
          actor_display: entry.actor_ref
            ? `Mã cán bộ ${entry.actor_ref}`
            : "API chưa có người thao tác",
          at: entry.recorded_at,
          changes: (Object.keys(labels) as (keyof typeof labels)[]).flatMap(
            (key) => {
              const old =
                previous?.[key] === undefined ? null : String(previous[key]);
              const value =
                entry[key] === undefined ? null : String(entry[key]);
              return old === value
                ? []
                : [{ field: labels[key], old, new: value }];
            },
          ),
        },
      ];
    })
    .reverse();
}

export function ScheduleLog({ c }: { c: PriceCalendar }) {
  return (
    <LogPanel
      entries={scheduleLog(c)}
      empty="Chưa có lần thay đổi nào trong sổ cấu hình."
    />
  );
}

export function RunsTable({ runs }: { runs: PriceCalendarRun[] }) {
  const columns: Column<PriceCalendarRun>[] = [
    { key: "day", header: "Ngày", cell: (r) => dmy(r.day) },
    { key: "kind", header: "Loại", cell: (r) => RUN_KIND_LABEL[r.kind] },
    { key: "scheduled", header: "Theo lịch", cell: (r) => hm(r.scheduled_at) },
    {
      key: "actual",
      header: "Thực tế",
      cell: (r) => (
        <>
          {dt(r.ran_at)}{" "}
          {r.lag_seconds > 60 ? (
            <StatusPill tone="bad">
              trễ {Math.round(r.lag_seconds / 60)} phút
            </StatusPill>
          ) : null}
        </>
      ),
    },
    {
      key: "recipients",
      header: "Người nhận",
      cell: (r) =>
        r.kind === "lock"
          ? "—"
          : r.targets === 0 && r.recipients === 0
            ? "Toàn tenant"
            : r.recipients,
    },
    { key: "note", header: "Ghi chú", cell: (r) => r.note || "—" },
  ];
  return (
    <DataTable
      caption="Nhật ký chạy khóa và nhắc"
      columns={columns}
      rows={runs}
      rowKey={(r) => `${r.day}:${r.kind}`}
      empty="Chưa có lần chạy nào."
    />
  );
}
