"use client";

// AUDIT-LOG-KN (#73): màn Admin "Nhật ký hệ thống" (prototype Admin › Nhật ký thao tác) trên GET /v1/accounts/audit (#70).
// Chỉ phiên Admin KN xem được; MỖI lần tải một trang máy chủ ghi một lượt xem bền (kể cả trang rỗng), nên màn chỉ gọi
// khi mở màn, bấm "Xem" hoặc chuyển trang — không gọi theo từng phím gõ. Bộ lọc có giới hạn: khoảng ngày ≤ 93 ngày (giờ
// VN), phân trang bằng cursor của máy chủ (Trang trước/Trang sau, giữ cùng bộ lọc). Dữ liệu chỉ gồm mã/ref/thời điểm;
// tên người thao tác lấy từ danh sách tài khoản Admin đã được xem, không có thì hiện mã rút gọn.
import { useEffect, useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, FilterBar, FilterChip, PageHeader, RowActions, StatusPill, type Column } from "@/components/patterns/list";
import { formatDateTime } from "@/lib/format";
import { addDays, cutoffOf, daysBetween, isIsoDate, vnToday } from "@/lib/vnDate";
import type { AuditItem, AuditPage, AuditQuery } from "../cycle/api";
import { LoadState, useLoad } from "../cycle/common";
import { AUDIT_ACTIONS, AUDIT_MODULES, OUTCOME, actionLabel, moduleLabel, shortRef, sourceLabel } from "./labels";
import { rememberAuditPage } from "./selection";

export const AUDIT_PAGE_SIZE = 50;
export const AUDIT_MAX_DAYS = 93;
export const AUDIT_BASE = "/kn/quan-tri/nhat-ky-he-thong";

interface Filters {
  fromDate: string;
  toDate: string;
  actor: string;
  module: string;
  department: string;
  action: string;
}

export function defaultAuditFilters(today = vnToday()): Filters {
  return { fromDate: addDays(today, -6), toDate: today, actor: "", module: "", department: "", action: "" };
}

/** Kiểm bộ lọc trước khi gọi máy chủ; lỗi → không gọi (máy chủ vẫn kiểm lại). */
export function auditFilterError(f: Filters): string | null {
  if (!isIsoDate(f.fromDate) || !isIsoDate(f.toDate)) return "Chọn đủ Từ ngày và Đến ngày.";
  const days = daysBetween(f.fromDate, f.toDate) + 1;
  if (days < 1) return "Từ ngày phải trước hoặc bằng Đến ngày.";
  if (days > AUDIT_MAX_DAYS) return `Khoảng thời gian tối đa ${AUDIT_MAX_DAYS} ngày.`;
  return null;
}

/** Truy vấn của một trang: [00:00 Từ ngày, 00:00 ngày sau Đến ngày) giờ VN. */
export function auditQuery(f: Filters, cursor?: string): AuditQuery {
  return {
    from: cutoffOf(addDays(f.fromDate, -1)),
    to: cutoffOf(f.toDate),
    actor_ref: f.actor || undefined,
    module: f.module || undefined,
    department: f.department || undefined,
    action: f.action || undefined,
    limit: AUDIT_PAGE_SIZE,
    cursor,
  };
}

export function AuditLogScreen() {
  const { knAdminApi } = useAuth();
  const [draft, setDraft] = useState<Filters>(() => defaultAuditFilters());
  const [applied, setApplied] = useState<Filters>(draft);
  // cursors[i] = cursor để tải trang i (trang 0 không cursor); đổi bộ lọc → về trang đầu.
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [pageIdx, setPageIdx] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const refs = useLoad(async () => {
    const [users, depts] = await Promise.allSettled([knAdminApi.users(), knAdminApi.departments()]);
    const names = new Map<string, string>();
    if (users.status === "fulfilled") for (const u of users.value.items) names.set(u.id, `${u.last_name} ${u.first_name}`.trim() || u.username);
    const departments = depts.status === "fulfilled" ? depts.value.items.map((d) => ({ value: d.code, label: d.name })) : [];
    return { names, departments, users: users.status === "fulfilled" ? users.value.items : [] };
  }, []);
  const names = useMemo(() => refs.data?.names ?? new Map<string, string>(), [refs.data]);

  const page = useLoad<AuditPage>(() => knAdminApi.audit(auditQuery(applied, cursors[pageIdx])), [applied, pageIdx]);
  useEffect(() => {
    if (page.data) rememberAuditPage(page.data.items, names);
  }, [page.data, names]);

  const set = (k: keyof Filters) => (v: string) => setDraft((d) => ({ ...d, [k]: v, ...(k === "module" ? { action: "" } : {}) }));
  const apply = () => {
    const e = auditFilterError(draft);
    setError(e);
    if (e) return;
    setCursors([undefined]);
    setPageIdx(0);
    setApplied({ ...draft });
  };
  const reset = () => {
    const d = defaultAuditFilters();
    setDraft(d);
    setError(null);
    setCursors([undefined]);
    setPageIdx(0);
    setApplied(d);
  };
  const next = (cursor: string) => {
    setCursors((c) => [...c.slice(0, pageIdx + 1), cursor]);
    setPageIdx((i) => i + 1);
  };

  const cols: Column<AuditItem>[] = [
    { key: "at", header: "Thời điểm", cell: (r) => <span className="small">{formatDateTime(r.recorded_at)}</span> },
    { key: "actor", header: "Người thao tác", cell: (r) => names.get(r.actor_ref) ?? <span className="small">{shortRef(r.actor_ref)}</span> },
    { key: "dept", header: "Phòng ban", cell: (r) => (r.department ? (refs.data?.departments.find((d) => d.value === r.department)?.label ?? r.department) : "—") },
    { key: "module", header: "Module", cell: (r) => moduleLabel(r.module) },
    { key: "action", header: "Thao tác", cell: (r) => actionLabel(r.action) },
    { key: "outcome", header: "Kết quả", cell: (r) => <StatusPill tone={OUTCOME[r.outcome].tone}>{OUTCOME[r.outcome].label}</StatusPill> },
    { key: "entity", header: "Đối tượng", cell: (r) => <span className="small">{shortRef(r.entity_ref)}</span> },
    { key: "view", header: "", cell: (r) => <RowActions label={`${actionLabel(r.action)} lúc ${formatDateTime(r.recorded_at)}`} viewHref={`${AUDIT_BASE}/${r.event_ref}`} /> },
  ];

  const actions = draft.module ? (AUDIT_ACTIONS[draft.module] ?? []) : Object.values(AUDIT_ACTIONS).flat();
  return (
    <>
      <PageHeader title="Nhật ký hệ thống">
        Thao tác ghi trong các sổ đã đăng ký, mới nhất trước. Mỗi lần xem nhật ký cũng được ghi lại.
      </PageHeader>
      <FilterBar onReset={reset}>
        <label className="row small" htmlFor="audit-from" style={{ gap: 6 }}>
          Từ ngày*
          <input id="audit-from" type="date" value={draft.fromDate} max={draft.toDate || undefined} onChange={(e) => set("fromDate")(e.target.value)} />
        </label>
        <label className="row small" htmlFor="audit-to" style={{ gap: 6 }}>
          Đến ngày*
          <input id="audit-to" type="date" value={draft.toDate} min={draft.fromDate || undefined} onChange={(e) => set("toDate")(e.target.value)} />
        </label>
        <FilterChip
          label="Người thao tác"
          value={draft.actor}
          onChange={set("actor")}
          options={[{ value: "", label: "Tất cả" }, ...(refs.data?.users ?? []).map((u) => ({ value: u.id, label: names.get(u.id) ?? u.username }))]}
        />
        <FilterChip label="Module" value={draft.module} onChange={set("module")} options={[{ value: "", label: "Tất cả" }, ...AUDIT_MODULES]} />
        <FilterChip label="Thao tác" value={draft.action} onChange={set("action")} options={[{ value: "", label: "Tất cả" }, ...actions.map((a) => ({ value: a.value, label: a.label }))]} />
        <FilterChip label="Phòng ban" value={draft.department} onChange={set("department")} options={[{ value: "", label: "Tất cả" }, ...(refs.data?.departments ?? [])]} />
        <button type="button" className="btn btn-p" onClick={apply}>
          Xem
        </button>
      </FilterBar>
      {error ? (
        <p className="err" role="alert">
          {error}
        </p>
      ) : null}
      <LoadState s={page}>
        {(d) => (
          <>
            <p className="small mute" data-testid="audit-sources">
              Nguồn: {d.sources.map(sourceLabel).join(", ")}. Nhật ký chỉ gồm các sổ đã đăng ký, không phải mọi thao tác của hệ thống.
            </p>
            <DataTable caption="Nhật ký hệ thống" columns={cols} rows={d.items} rowKey={(r) => `${r.source}:${r.event_ref}`} page={pageIdx + 1} pageSize={AUDIT_PAGE_SIZE} />
            <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
              <span className="small mute">Trang {pageIdx + 1}</span>
              <button type="button" className="btn" disabled={pageIdx === 0} onClick={() => setPageIdx((i) => i - 1)}>
                Trang trước
              </button>
              <button type="button" className="btn" disabled={!d.next_cursor} onClick={() => d.next_cursor && next(d.next_cursor)}>
                Trang sau
              </button>
            </div>
          </>
        )}
      </LoadState>
    </>
  );
}
