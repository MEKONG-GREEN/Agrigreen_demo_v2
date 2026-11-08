"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import type { ApiClient } from "@/api/client";
import { SelectField } from "@/components/form";
import { DataTable, PageHeader, type Column } from "@/components/patterns/list";
import { dt } from "@/kn/cycle/common";
import { DEPT_LABEL } from "../api";
import { readPending243, type PendingPage243, type PendingFilters245 } from "./api";

export function KhtcPendingInboxScreen() {
  const auth = useAuth();
  const pathname = usePathname();
  const context = auth.kn?.context;
  const expiresAt = Date.parse(context?.expires_at ?? "");
  if (!pathname || auth.status !== "authenticated" || !auth.principal || auth.kn?.status !== "active" || !context || !auth.api || !Number.isFinite(expiresAt))
    return <p role="alert">Chọn ngữ cảnh làm việc còn hiệu lực để xem hộp phiếu.</p>;
  const identity = JSON.stringify([pathname, auth.status, auth.principal, auth.kn.status, context, auth.kn.assignments, auth.kn.support, auth.scopeWarning]);
  const allowAdhoc = !context.read_only && (context.role === "kn_tinh" && context.department_code === "ke_hoach_tai_chinh" || context.role === "admin" && auth.principal.mfa);
  return <ApiBoundary key={identity} api={auth.api} expiresAt={expiresAt} allowAdhoc={allowAdhoc} />;
}

function ApiBoundary({ api, expiresAt, allowAdhoc }: { api: ApiClient; expiresAt: number; allowAdhoc: boolean }) {
  const [client, setClient] = useState({ api, epoch: 0 });
  if (client.api !== api) { setClient({ api, epoch: client.epoch + 1 }); return null; }
  return <Workspace key={client.epoch} api={api} expiresAt={expiresAt} allowAdhoc={allowAdhoc} />;
}

function Workspace({ api, expiresAt, allowAdhoc }: { api: ApiClient; expiresAt: number; allowAdhoc: boolean }) {
  const [filters, setFilters] = useState<PendingFilters245>({});
  const [periodName, setPeriodName] = useState("");
  const changeFilters = (next: PendingFilters245, name = "") => { setPeriodName(name); setFilters(next); };
  return <PagedWorkspace key={JSON.stringify(filters)} api={api} expiresAt={expiresAt} allowAdhoc={allowAdhoc} filters={filters} periodName={periodName} changeFilters={changeFilters} />;
}

type Filters = { filters: PendingFilters245; periodName: string; allowAdhoc: boolean; changeFilters: (next: PendingFilters245, name?: string) => void };
function PagedWorkspace({ api, expiresAt, ...filters }: { api: ApiClient; expiresAt: number } & Filters) {
  const [pages, setPages] = useState<(string | null)[]>([null]);
  const [revision, setRevision] = useState(0);
  const cursor = pages[pages.length - 1]!;
  return <Resource key={`${cursor}:${revision}`} api={api} expiresAt={expiresAt} cursor={cursor} page={pages.length} {...filters}
    previous={pages.length > 1 ? () => setPages(p => p.slice(0, -1)) : undefined}
    next={value => setPages(p => [...p, value])} reload={() => setRevision(v => v + 1)} />;
}

function Resource({ api, expiresAt, cursor, page, previous, next, reload, filters, periodName, allowAdhoc, changeFilters }: {
  api: ApiClient; expiresAt: number; cursor: string | null; page: number;
  previous?: () => void; next: (cursor: string) => void; reload: () => void;
} & Filters) {
  const [data, setData] = useState<PendingPage243 | null>(null);
  const [failed, setFailed] = useState(false);
  const [expired, setExpired] = useState(false);
  useEffect(() => {
    let active = true;
    const abort = new AbortController();
    let timer: number | undefined;
    const expire = () => {
      const remaining = expiresAt - Date.now();
      if (remaining <= 0) { abort.abort(); if (active) { setData(null); setExpired(true); } return; }
      timer = window.setTimeout(expire, Math.min(remaining, 2_147_483_647));
    };
    expire();
    if (expiresAt > Date.now()) void readPending243(api, cursor, abort.signal, filters).then(value => {
      if (active && Date.now() < expiresAt) setData(value);
    }, () => { if (active && Date.now() < expiresAt) setFailed(true); });
    return () => { active = false; abort.abort(); window.clearTimeout(timer); };
  }, [api, cursor, expiresAt, filters]);
  const columns: Column<PendingPage243["rows"][number]>[] = [
    { key: "period", header: "Kỳ báo cáo", cell: row => row.period_name },
    { key: "department", header: "Phòng", cell: row => row.department_name || DEPT_LABEL[row.department] || row.department },
    { key: "attempt", header: "Lần gửi", cell: row => row.attempt },
    { key: "at", header: "Gửi lúc", cell: row => dt(row.submitted_at) },
    { key: "open", header: "Thao tác", cell: row => <Link className="btn btn-o" href={`/kn/khtc/ra-soat/${row.period_id}?department=${encodeURIComponent(row.department)}`}>Xem phiếu</Link> },
  ];
  const periods = new Map(data?.rows.map(row => [row.period_id, row.period_name]));
  if (filters.period_id) periods.set(filters.period_id, periodName);
  return <>
    <PageHeader title="Phiếu số liệu các phòng" />
    <p>Phiếu đã gửi chờ rà soát, theo từng nhóm kỳ chưa đóng hoặc hủy. Trạng thái và quyền được kiểm tra lại khi mở phiếu.</p>
    {expired ? <p role="alert">Ngữ cảnh đã hết hạn. Chọn lại ngữ cảnh làm việc để đọc phiếu.</p> : <>
      <div className="row">
        <SelectField label="Loại phiếu" value={filters.report_type ?? "regular"} placeholder="Chọn loại phiếu"
          options={[{ value: "regular", label: "Phiếu thường niên" }, ...(allowAdhoc ? [{ value: "adhoc", label: "Phiếu phát sinh" }] : [])]}
          onChange={mode => changeFilters({ ...(mode === "adhoc" ? { report_type: "adhoc" as const } : {}), ...(filters.department ? { department: filters.department } : {}) })} />
        <SelectField label="Phòng" value={filters.department ?? ""} placeholder="Tất cả phòng"
          options={Object.entries(DEPT_LABEL).filter(([code]) => code !== "ke_hoach_tai_chinh").map(([value, label]) => ({ value, label }))}
          onChange={department => changeFilters({ ...(filters.report_type ? { report_type: filters.report_type } : {}), ...(department ? { department } : {}) })} />
        <SelectField label="Kỳ báo cáo trong nhóm" value={filters.period_id ?? ""} placeholder="Tất cả kỳ"
          hint="Chọn kỳ từ nhóm phiếu hiện tại. Bỏ lọc kỳ để tiếp tục phân trang."
          options={[...periods].map(([value, label]) => ({ value, label }))}
          onChange={period => { const { period_id: _period, ...rest } = filters; void _period; changeFilters({ ...rest, ...(period ? { period_id: period } : {}) }, periods.get(period)); }} />
      </div>
      <button type="button" className="btn btn-o" onClick={reload}>Làm mới</button>
      {failed ? <p role="alert">Không đọc được hộp phiếu trong ngữ cảnh hiện tại. Kiểm tra quyền hoặc thử làm mới.</p> : data ? <>
        <DataTable caption={`Phiếu chờ rà soát · Trang ${page}`} rows={data.rows} columns={columns} rowKey={row => `${row.period_id}:${row.department}`} />
        {!data.rows.length && <p role="status">Chưa có phiếu chờ rà soát trong nhóm kỳ này.{data.next_cursor ? " Tiếp tục sang nhóm kỳ trước." : ""}</p>}
        <nav aria-label="Phân trang hộp phiếu" className="row">
          <button type="button" className="btn btn-o" disabled={!previous} onClick={previous}>Trang trước</button>
          <span>Trang {page}</span>
          <button type="button" className="btn btn-o" disabled={!data.next_cursor} onClick={() => { if (data.next_cursor) next(data.next_cursor); }}>Trang tiếp</button>
        </nav>
      </> : <p role="status">Đang đọc hộp phiếu…</p>}
    </>}
  </>;
}
