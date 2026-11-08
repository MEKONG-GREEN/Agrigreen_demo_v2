"use client";

import { useEffect, useRef, useState } from "react";
import type { components } from "@/api/generated/application";
import { unwrap } from "@/api/client";
import { uuidv7 } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";

type Summary = components["schemas"]["KNSupportSummary"];
type Detail = components["schemas"]["KNSupportDetail"];

/** Ownership is part of the React key: a late response cannot display PII after account/context/tenant/MFA changes. */
export default function SupportQueueScreen() {
  const { principal, kn } = useAuth();
  if (!principal?.mfa || !principal.roles.includes("platform_admin") || kn.context?.role !== "admin" || kn.context.read_only || kn.context.support_session_id) return <p role="alert">Chỉ Admin Khuyến nông đã xác thực MFA được xử lý yêu cầu hỗ trợ.</p>;
  const owner = `${principal.tenantId}:${principal.subject}:${kn.context.context_id}:${principal.mfa}`;
  return <Queue key={owner} />;
}

function Queue() {
  const { api } = useAuth();
  const [items, setItems] = useState<Summary[]>([]);
  const [detail, setDetail] = useState<Detail | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [state, setState] = useState("new");
  const [offset, setOffset] = useState(0);
  const [refresh, setRefresh] = useState(0);
  const alive = useRef(false);
  const detailGeneration = useRef(0);
  const resolving = useRef(false);
  const operation = useRef<{ id: string; version: number; key: string } | null>(null);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => {
    let current = true;
    void unwrap(api.GET("/v1/contacts/support-requests", { params: { query: { state: state as "new" | "resolved", offset } }, cache: "no-store" }))
      .then((r) => { if (current) { setItems(r.items); setError(null); } })
      .catch(() => { if (current) { setItems([]); setError("Không tải được yêu cầu. Kiểm tra quyền hiện tại và bấm Tải lại."); } })
      .finally(() => { if (current) setLoading(false); });
    return () => { current = false; };
  }, [api, state, offset, refresh]);

  async function open(id: string) {
    const generation = ++detailGeneration.current;
    setDetail(null); setBusy(true); setError(null); operation.current = null;
    try {
      const r = await unwrap(api.GET("/v1/contacts/support-requests/{id}", { params: { path: { id } }, cache: "no-store" }));
      if (alive.current && generation === detailGeneration.current) setDetail(r);
    } catch { if (alive.current && generation === detailGeneration.current) setError("Không đọc được yêu cầu. Kiểm tra quyền hoặc tải lại danh sách."); }
    finally { if (alive.current && generation === detailGeneration.current) setBusy(false); }
  }
  async function resolve() {
    if (!detail || resolving.current) return;
    const old = detail; const generation = detailGeneration.current;
    if (operation.current?.id !== old.id || operation.current.version !== old.version) operation.current = { id: old.id, version: old.version, key: uuidv7() };
    resolving.current = true; setBusy(true); setError(null);
    try {
      const r = await unwrap(api.PUT("/v1/contacts/support-requests/{id}/resolution", {
        params: { path: { id: old.id }, header: { "Idempotency-Key": operation.current.key } },
        body: { expected_version: old.version }, cache: "no-store",
      }));
      if (alive.current && generation === detailGeneration.current) { setDetail(null); setItems((rows) => rows.map((row) => row.id === r.id ? r : row)); setLoading(true); setRefresh((n) => n + 1); operation.current = null; }
    } catch (e) {
      if (alive.current && generation === detailGeneration.current) {
        if (e instanceof ApiProblem && [401, 403, 404, 409].includes(e.status)) { setDetail(null); operation.current = null; }
        setError(e instanceof ApiProblem && e.status === 409 ? "Yêu cầu đã thay đổi. Tải lại và kiểm tra trạng thái mới." : "Chưa xác nhận được việc xử lý. Bạn có thể thử lại cùng thao tác.");
      }
    } finally { resolving.current = false; if (alive.current && generation === detailGeneration.current) setBusy(false); }
  }
  return <>
    <PageHeader title="Yêu cầu hỗ trợ tài khoản" actions={<button className="btn btn-o" type="button" disabled={loading || busy} onClick={() => { detailGeneration.current++; setDetail(null); setLoading(true); setRefresh((n) => n + 1); }}>Tải lại</button>} />
    <p className="mute">Các yêu cầu gửi từ màn đăng nhập. Mở từng yêu cầu để xem thông tin liên hệ và đánh dấu sau khi xử lý.</p>
    <label>Trạng thái <select className="select" aria-label="Trạng thái" disabled={busy} value={state} onChange={(e) => { detailGeneration.current++; setDetail(null); setState(e.target.value); setOffset(0); setLoading(true); }}><option value="new">Chưa xử lý</option><option value="resolved">Đã xử lý</option></select></label>
    {error ? <p role="alert" className="err">{error}</p> : null}
    {loading ? <p role="status">Đang tải…</p> : <DataTable caption="Yêu cầu hỗ trợ tài khoản" rows={items} rowKey={(r) => r.id} columns={[
      { key: "created", header: "Ngày tiếp nhận", cell: (r) => new Date(r.created_at).toLocaleString("vi-VN") },
      { key: "state", header: "Trạng thái", cell: (r) => <StatusPill tone={r.state === "resolved" ? "ok" : "warn"}>{r.state === "resolved" ? "Đã xử lý" : "Chưa xử lý"}</StatusPill> },
      { key: "open", header: "Thao tác", cell: (r) => <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void open(r.id)}>Xem yêu cầu</button> },
    ]} empty="Chưa có yêu cầu ở trạng thái này." />}
    <div className="row" style={{ gap: 12, marginTop: 12 }}>
      <button type="button" className="btn btn-o btn-sm" disabled={busy || loading || offset === 0} onClick={() => { setOffset((n) => Math.max(0, n - 50)); setDetail(null); setLoading(true); }}>Trang trước</button>
      <button type="button" className="btn btn-o btn-sm" disabled={busy || loading || items.length < 50} onClick={() => { setOffset((n) => n + 50); setDetail(null); setLoading(true); }}>Trang sau</button>
    </div>
    {busy && !detail ? <p role="status">Đang mở yêu cầu…</p> : null}
    {detail ? <section className="card sec" aria-label="Nội dung yêu cầu">
      <h2>Nội dung yêu cầu</h2><dl><dt>Họ tên</dt><dd>{detail.name}</dd><dt>Liên hệ</dt><dd style={{ overflowWrap: "anywhere" }}>{detail.contact}</dd><dt>Nội dung</dt><dd style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{detail.message}</dd></dl>
      <div className="row" style={{ gap: 12, marginTop: 12 }}>{detail.state === "new" ? <button type="button" className="btn btn-p" disabled={busy} onClick={() => void resolve()}>{busy ? "Đang xử lý…" : "Đánh dấu đã xử lý"}</button> : <StatusPill tone="ok">Đã xử lý</StatusPill>}
        <button type="button" className="btn btn-o" disabled={busy} onClick={() => { detailGeneration.current++; setDetail(null); }}>Đóng nội dung</button></div>
    </section> : null}
  </>;
}
