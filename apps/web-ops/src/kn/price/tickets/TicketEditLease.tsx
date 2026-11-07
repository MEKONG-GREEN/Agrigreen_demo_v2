"use client";
import { useEffect, useState, type ReactNode } from "react";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import { ProblemAlert } from "@/components/ProblemAlert";
import type { PriceEditLease, PriceTicketApi } from "./api";

export function LeaseNotice({ lease }: { lease: PriceEditLease }) {
  return lease.held ? <p role="status">{lease.mine ? "Bạn đang sửa phiếu này." : `Chỉ xem — Đang sửa bởi ${lease.holder_name || "cán bộ khác"}.`}</p> : null;
}

/** A handle belongs to one mounted editor. Cleanup never releases another tab's handle. */
export function TicketEditLease({ id, ta, readOnlyView, children }: { id: string; ta: PriceTicketApi; readOnlyView: ReactNode; children: (handle: string) => ReactNode }) {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<{ lease: PriceEditLease | null; problem: ApiProblem | null; error: boolean }>({ lease: null, problem: null, error: false });
  useEffect(() => {
    const handle = newIdempotencyKey();
    let alive = true, renewing = false;
    let interval: ReturnType<typeof setInterval> | undefined, deadline: ReturnType<typeof setTimeout> | undefined;
    const release = () => { void ta.releaseLease(id, handle).catch(() => {}); };
    const fail = (error: unknown) => {
      if (!alive) return;
      clearInterval(interval); clearTimeout(deadline);
      setState({ lease: null, problem: error instanceof ApiProblem ? error : null, error: true });
    };
    const accept = (lease: PriceEditLease) => {
      if (!alive) { release(); return; }
      setState({ lease, problem: null, error: false });
      clearTimeout(deadline);
      if (lease.mine && lease.handle_id === handle && lease.expires_at) {
        const remaining = Date.parse(lease.expires_at) - Date.parse(lease.now);
        deadline = setTimeout(() => fail(null), Math.max(0, remaining));
      }
    };
    void ta.acquireLease(id, handle).then(lease => {
      accept(lease);
      if (!alive || !lease.mine || lease.handle_id !== handle) return;
      interval = setInterval(() => {
        if (renewing) return;
        renewing = true;
        void ta.renewLease(id, handle).then(accept, fail).finally(() => { renewing = false; });
      }, 30_000);
    }, fail);
    return () => { alive = false; clearInterval(interval); clearTimeout(deadline); release(); };
  }, [id, ta, attempt]);
  const retry = () => { setState({ lease: null, problem: null, error: false }); setAttempt(n => n + 1); };
  if (state.error) return <div>{state.problem && <ProblemAlert problem={state.problem} />}<p role="alert">Chỉ xem — Chưa xác nhận quyền sửa hoặc phiên sửa đã hết hạn. Dữ liệu chưa gửi sẽ được đặt lại khi mở lại phiên sửa.</p><button type="button" className="btn" onClick={retry}>Thử lấy quyền sửa lại</button>{readOnlyView}</div>;
  if (!state.lease) return <p role="status">Đang kiểm tra quyền sửa phiếu…</p>;
  return <><LeaseNotice lease={state.lease} />{state.lease.mine && state.lease.handle_id ? children(state.lease.handle_id) : <button type="button" className="btn" onClick={retry}>Thử lấy quyền sửa lại</button>}{(!state.lease.mine || !state.lease.handle_id) && readOnlyView}</>;
}
