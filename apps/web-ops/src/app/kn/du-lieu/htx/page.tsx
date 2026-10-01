"use client";

import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, FilterBar, FilterChip, PageHeader, RowActions, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { DEMO_COOPS, type DemoCoop } from "@/kn/demo";
import { DemoNotice, RequiresMock, useKnReadOnly } from "@/kn/ui";

const PAGE_SIZE = 10;

/** Mẫu List (FN-XA-03.03 Danh sách HTX, Design Sample "Screens / List") — dữ liệu minh họa. */
export default function KnCoopListPage() {
  const { kn } = useAuth();
  const readOnly = useKnReadOnly();
  const [commune, setCommune] = useState("");
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [removed, setRemoved] = useState<Set<string>>(new Set());
  const rows = useMemo(
    () =>
      DEMO_COOPS.filter(
        (c) => !removed.has(c.id) && (!commune || c.commune === commune) && (!status || c.status === status) && (!q || c.name.toLowerCase().includes(q.toLowerCase())),
      ),
    [commune, status, q, removed],
  );
  const columns: Column<DemoCoop>[] = [
    { key: "name", header: "Tên HTX", cell: (c) => c.name },
    { key: "commune", header: "Xã/phường", cell: (c) => c.commune },
    { key: "rep", header: "Người đại diện", cell: (c) => c.representative },
    { key: "area", header: "Diện tích (ha)", cell: (c) => c.area_ha, className: "num" },
    { key: "members", header: "Nông hộ", cell: (c) => c.members, className: "num" },
    { key: "status", header: "Trạng thái", cell: (c) => <StatusPill tone={c.status === "active" ? "ok" : "neutral"}>{c.status === "active" ? "Hoạt động" : "Tạm dừng"}</StatusPill> },
    {
      key: "act",
      header: "Thao tác",
      cell: (c) => (
        <RowActions
          label={c.name}
          viewHref={`/kn/du-lieu/htx/${c.id}`}
          editHref={readOnly ? undefined : `/kn/du-lieu/htx/${c.id}?sua=1`}
          onDelete={readOnly ? undefined : () => setRemoved((s) => new Set(s).add(c.id))}
        />
      ),
    },
  ];
  const reset = () => {
    setCommune("");
    setStatus("");
    setQ("");
    setPage(1);
  };
  return (
    <RequiresMock title="Danh sách HTX">
      <PageHeader
        title="Danh sách HTX"
        actions={
          readOnly ? null : (
            <>
              <button type="button" className="btn btn-o">
                Nhập từ Excel
              </button>
              <button type="button" className="btn btn-p">
                Thêm HTX
              </button>
            </>
          )
        }
      />
      <DemoNotice> Xóa ở đây chỉ ẩn dòng minh họa — bản thật là vô hiệu hóa có lý do (RB-1).</DemoNotice>
      <FilterBar onReset={reset}>
        <FilterChip
          label="Xã/phường"
          value={commune}
          onChange={(v) => {
            setCommune(v);
            setPage(1);
          }}
          options={[{ value: "", label: "Tất cả" }, ...(kn.context?.communes ?? []).map((c) => ({ value: c.name, label: c.name }))]}
        />
        <FilterChip
          label="Trạng thái"
          value={status}
          onChange={(v) => {
            setStatus(v);
            setPage(1);
          }}
          options={[
            { value: "", label: "Tất cả" },
            { value: "active", label: "Hoạt động" },
            { value: "suspended", label: "Tạm dừng" },
          ]}
        />
        <SearchBox
          value={q}
          onChange={(v) => {
            setQ(v);
            setPage(1);
          }}
          placeholder="Tìm theo tên HTX"
        />
      </FilterBar>
      <DataTable
        caption="Danh sách HTX"
        columns={columns}
        rows={rows.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)}
        rowKey={(c) => c.id}
        page={page}
        pageSize={PAGE_SIZE}
        total={rows.length}
        onPage={setPage}
      />
    </RequiresMock>
  );
}
