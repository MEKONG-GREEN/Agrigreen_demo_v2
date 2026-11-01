"use client";

// AUDIT-LOG-KN (#73): màn Admin "Nhật ký hệ thống" (prototype Admin › Nhật ký thao tác) trên GET /v1/accounts/audit (#70).
// Chỉ phiên Admin KN xem được; MỖI lần tải một trang máy chủ ghi một lượt xem bền (kể cả trang rỗng), nên màn chỉ gọi
// khi mở màn, bấm "Xem" hoặc chuyển trang — không gọi theo từng phím gõ. Bộ lọc có giới hạn: khoảng ngày ≤ 93 ngày (giờ
// VN), phân trang bằng cursor của máy chủ (Trang trước/Trang sau, giữ cùng bộ lọc). Dữ liệu chỉ gồm mã/ref/thời điểm;
// tên người thao tác lấy từ danh sách tài khoản nếu phiên có quyền xem, không có thì hiện mã rút gọn. Bộ lọc Người thao
// tác/Phòng ban không phụ thuộc quyền tra cứu đó: chọn từ mã đã thấy trong các trang đã tải hoặc "Nhập mã…" (kiểm định dạng).
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

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
// Cùng mẫu mã bộ lọc của máy chủ (accounts/app/audit.go auditQueryCode).
const CODE_RE = /^[a-z][a-z0-9_.-]{0,79}$/;
/** Giá trị chọn "Nhập mã…" trong ô chọn Người thao tác/Phòng ban (không bao giờ gửi lên máy chủ). */
export const AUDIT_ENTER = "__enter__";

/** Kiểm bộ lọc trước khi gọi máy chủ; lỗi → không gọi (máy chủ vẫn kiểm lại). */
export function auditFilterError(f: Filters): string | null {
  if (!isIsoDate(f.fromDate) || !isIsoDate(f.toDate)) return "Chọn đủ Từ ngày và Đến ngày.";
  const days = daysBetween(f.fromDate, f.toDate) + 1;
  if (days < 1) return "Từ ngày phải trước hoặc bằng Đến ngày.";
  if (days > AUDIT_MAX_DAYS) return `Khoảng thời gian tối đa ${AUDIT_MAX_DAYS} ngày.`;
  if (f.actor && !UUID_RE.test(f.actor)) return "Mã người thao tác phải là UUID (vd 0192a000-0000-7000-8000-0000000000a1).";
  if (f.department && !CODE_RE.test(f.department)) return "Mã phòng ban không hợp lệ (chữ thường, số, _ . -; tối đa 80 ký tự).";
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

  // Tên tài khoản/phòng ban chỉ để hiển thị: hai API này cần quyền quản trị nền tảng mà Admin KN không bắt buộc có
  // (403 với phiên chỉ có kn_user). Lỗi thì bỏ qua — bộ lọc vẫn chọn được từ các mã đã thấy trong trang đã tải hoặc nhập mã.
  const refs = useLoad(async () => {
    const [users, depts] = await Promise.allSettled([knAdminApi.users(), knAdminApi.departments()]);
    const names = new Map<string, string>();
    if (users.status === "fulfilled") for (const u of users.value.items) names.set(u.id, `${u.last_name} ${u.first_name}`.trim() || u.username);
    const departments = depts.status === "fulfilled" ? depts.value.items.map((d) => ({ value: d.code, label: d.name })) : [];
    return { names, departments, users: users.status === "fulfilled" ? users.value.items : [] };
  }, []);
  const names = useMemo(() => refs.data?.names ?? new Map<string, string>(), [refs.data]);
  // Mã người thao tác/phòng ban đã xuất hiện trong các trang đã tải (dữ liệu máy chủ đã ghi lượt xem) — nguồn lựa chọn
  // không phụ thuộc quyền tra cứu tài khoản/phòng ban.
  const [seen, setSeen] = useState<{ actors: string[]; departments: string[] }>({ actors: [], departments: [] });
  const [entering, setEntering] = useState<{ actor: boolean; department: boolean }>({ actor: false, department: false });

  const rememberSeen = (items: AuditItem[]) => {
    if (!items.length) return;
    setSeen((s) => {
      const actors = new Set(s.actors);
      const departments = new Set(s.departments);
      for (const r of items) {
        actors.add(r.actor_ref);
        if (r.department) departments.add(r.department);
      }
      return actors.size === s.actors.length && departments.size === s.departments.length ? s : { actors: [...actors], departments: [...departments] };
    });
  };

  const page = useLoad<AuditPage>(async () => {
    const p = await knAdminApi.audit(auditQuery(applied, cursors[pageIdx]));
    rememberSeen(p.items);
    return p;
  }, [applied, pageIdx]);
  useEffect(() => {
    if (page.data) rememberAuditPage(page.data.items, names);
  }, [page.data, names]);

  const set = (k: keyof Filters) => (v: string) => setDraft((d) => ({ ...d, [k]: v, ...(k === "module" ? { action: "" } : {}) }));
  const pick = (k: "actor" | "department") => (v: string) => {
    const enter = v === AUDIT_ENTER;
    setEntering((e) => ({ ...e, [k]: enter }));
    set(k)(enter ? "" : v);
  };
  const actorOptions = (() => {
    const ids = new Set<string>([...(refs.data?.users ?? []).map((u) => u.id), ...seen.actors]);
    if (applied.actor) ids.add(applied.actor);
    return [...ids].map((id) => ({ value: id, label: names.get(id) ?? shortRef(id) })).sort((a, b) => a.label.localeCompare(b.label, "vi"));
  })();
  const deptOptions = (() => {
    const known = new Map((refs.data?.departments ?? []).map((d) => [d.value, d.label]));
    const codes = new Set<string>([...known.keys(), ...seen.departments]);
    if (applied.department) codes.add(applied.department);
    return [...codes].map((c) => ({ value: c, label: known.get(c) ?? c })).sort((a, b) => a.label.localeCompare(b.label, "vi"));
  })();
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
    setEntering({ actor: false, department: false });
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
          value={entering.actor ? AUDIT_ENTER : draft.actor}
          onChange={pick("actor")}
          options={[{ value: "", label: "Tất cả" }, ...actorOptions, { value: AUDIT_ENTER, label: "Nhập mã…" }]}
        />
        {entering.actor ? (
          <label className="row small" htmlFor="audit-actor-ref" style={{ gap: 6 }}>
            Mã người thao tác
            <input id="audit-actor-ref" value={draft.actor} placeholder="UUID" maxLength={36} onChange={(e) => set("actor")(e.target.value.trim().toLowerCase())} />
          </label>
        ) : null}
        <FilterChip label="Module" value={draft.module} onChange={set("module")} options={[{ value: "", label: "Tất cả" }, ...AUDIT_MODULES]} />
        <FilterChip label="Thao tác" value={draft.action} onChange={set("action")} options={[{ value: "", label: "Tất cả" }, ...actions.map((a) => ({ value: a.value, label: a.label }))]} />
        <FilterChip
          label="Phòng ban"
          value={entering.department ? AUDIT_ENTER : draft.department}
          onChange={pick("department")}
          options={[{ value: "", label: "Tất cả" }, ...deptOptions, { value: AUDIT_ENTER, label: "Nhập mã…" }]}
        />
        {entering.department ? (
          <label className="row small" htmlFor="audit-dept-code" style={{ gap: 6 }}>
            Mã phòng ban
            <input id="audit-dept-code" value={draft.department} placeholder="vd trong_trot" maxLength={80} onChange={(e) => set("department")(e.target.value.trim())} />
          </label>
        ) : null}
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
