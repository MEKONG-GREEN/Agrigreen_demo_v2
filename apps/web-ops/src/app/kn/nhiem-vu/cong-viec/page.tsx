"use client";

import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { DataTable, FilterBar, FilterChip, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { DEMO_TASKS, TASK_STATUS, type DemoTask } from "@/kn/demo";
import { DemoNotice, RequiresMock } from "@/kn/ui";

const PAGE_SIZE = 6;
const DEFAULTS = { commune: "", kind: "", status: "todo", form: "" };
const TODO = new Set(["not_started", "draft", "adjustment", "late"]);

/** Mẫu List (FN-XA-03.01 Công việc cần làm, Hình 3f/13) — trang mặc định Cấp Xã; dữ liệu minh họa. */
export default function KnTasksPage() {
  const { kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const [f, setF] = useState(DEFAULTS);
  const [page, setPage] = useState(1);
  const set = (patch: Partial<typeof DEFAULTS>) => {
    setF({ ...f, ...patch });
    setPage(1);
  };

  const rows = useMemo(
    () =>
      DEMO_TASKS.filter(
        (t) =>
          (!f.commune || t.commune === f.commune) &&
          (!f.form || t.form.startsWith(f.form)) &&
          (!f.kind || (f.kind === "month" ? t.period.startsWith("Tháng") : t.period.startsWith("Tuần"))) &&
          (f.status === "" || (f.status === "todo" ? TODO.has(t.status) : t.status === f.status)),
      ),
    [f],
  );

  const columns: Column<DemoTask>[] = [
    { key: "commune", header: "Xã/phường", cell: (t) => t.commune },
    {
      key: "form",
      header: "Báo cáo",
      cell: (t) => (
        <>
          {t.form}
          {t.note ? <div style={{ color: "var(--danger)", fontSize: 12 }}>{t.note}</div> : null}
        </>
      ),
    },
    { key: "period", header: "Kỳ", cell: (t) => t.period },
    { key: "due", header: "Hạn nộp", cell: (t) => t.due },
    { key: "status", header: "Trạng thái", cell: (t) => <StatusPill tone={TASK_STATUS[t.status].tone}>{TASK_STATUS[t.status].label}</StatusPill> },
    { key: "updated", header: "Cập nhật", cell: (t) => t.updated ?? "—" },
    {
      key: "act",
      header: "Thao tác",
      cell: (t) => {
        const s = TASK_STATUS[t.status];
        return (
          <button type="button" className={`btn btn-sm ${s.action === "Nhập" ? "btn-p" : "btn-o"}`} aria-label={`${s.action}: ${t.form} — ${t.commune}`}>
            {s.action}
          </button>
        );
      },
    },
  ];

  return (
    <RequiresMock title="Công việc cần làm">
      <PageHeader title="Công việc cần làm" />
      <DemoNotice />
      <FilterBar onReset={() => set(DEFAULTS)}>
        <FilterChip
          label="Xã/phường"
          value={f.commune}
          onChange={(commune) => set({ commune })}
          options={[{ value: "", label: `Tất cả xã phụ trách (${communes.length})` }, ...communes.map((c) => ({ value: c.name, label: c.name }))]}
        />
        <FilterChip
          label="Loại kỳ"
          value={f.kind}
          onChange={(kind) => set({ kind })}
          options={[
            { value: "", label: "Tất cả" },
            { value: "week", label: "Tuần" },
            { value: "month", label: "Tháng" },
          ]}
        />
        <FilterChip
          label="Trạng thái"
          value={f.status}
          defaultValue="todo"
          onChange={(status) => set({ status })}
          options={[
            { value: "todo", label: "Cần làm" },
            { value: "", label: "Tất cả" },
            ...Object.entries(TASK_STATUS).map(([value, s]) => ({ value, label: s.label })),
          ]}
        />
        <FilterChip
          label="Biểu"
          value={f.form}
          onChange={(form) => set({ form })}
          options={[{ value: "", label: "Tất cả" }, ...["T01", "T03", "T04", "T05", "M01"].map((x) => ({ value: x, label: x }))]}
        />
      </FilterBar>
      <div className="alert alert-info" role="status" style={{ marginBottom: 14 }}>
        <Icon name="bell" />
        <span>Nhắc nhở từ Cấp Tỉnh (07/09 08:15): &quot;Đề nghị các xã nộp T05 tuần 36 trước 17h ngày 08/09.&quot;</span>
      </div>
      <DataTable
        caption="Công việc nộp báo cáo"
        columns={columns}
        rows={rows.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)}
        rowKey={(t) => t.id}
        page={page}
        pageSize={PAGE_SIZE}
        total={rows.length}
        onPage={setPage}
      />
    </RequiresMock>
  );
}
