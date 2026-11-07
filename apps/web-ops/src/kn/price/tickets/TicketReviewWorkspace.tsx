"use client";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { DetailPage } from "@/components/patterns/detail";
import { createPriceTicketApi, vnToday } from "./api";
import { ReviewScreen } from "./ReviewScreen";
import { TicketReviewDetail } from "./TicketReviewDetail";
import { reviewFilters, reviewHref, reviewRoute, type ReviewFilters } from "./ticket-review-routes";
const clients = new WeakMap<object, number>(); let nextClient = 0;
function clientIdentity(client: object) { let id = clients.get(client); if (id === undefined) { id = ++nextClient; clients.set(client, id); } return id; }
/** Key destroys local fields/requests/idempotency turns when authority or API identity changes. */
export function TicketReviewWorkspace({ path = [] }: { path?: string[] }) {
  const auth = useAuth(), query = useSearchParams(), router = useRouter();
  const ta = useMemo(() => createPriceTicketApi(auth.api), [auth.api]);
  const filters = reviewFilters(query, vnToday()), route = reviewRoute(path), ctx = auth.kn.context;
  const allowed = auth.status === "authenticated" && auth.kn.status === "active" && !!ctx && (ctx.role === "admin" || (ctx.role === "kn_tinh" && ctx.department_code === "thong_tin_truyen_thong"));
  const back = { href: reviewHref(filters), label: "Danh sách kiểm duyệt" };
  if (!allowed) return <DetailPage back={back} title="Phiếu báo giá"><p role="alert">Chọn ngữ cảnh Cấp Tỉnh Phòng Thông tin Tuyên truyền hoặc Admin để xem phiếu và nhật ký.</p></DetailPage>;
  if (route.kind === "invalid") return <DetailPage back={back} title="Không tìm thấy trang phiếu"><p role="alert">Đường dẫn phiếu không hợp lệ.</p></DetailPage>;
  const readOnly = ctx?.read_only === true || (ctx?.role === "admin" && auth.principal?.mfa !== true);
  const identity = JSON.stringify([clientIdentity(auth.api), auth.status, auth.principal, auth.kn.status, ctx, auth.kn.support]);
  return route.kind === "list"
    ? <ReviewListWorkspace key={`${identity}:list`} queryFilters={filters} readOnly={readOnly} navigate={f => router.replace(reviewHref(f))} />
    : <TicketReviewDetail key={`${identity}:${JSON.stringify([route, filters])}`} route={route} filters={filters} ta={ta} readOnly={readOnly} />;
}

/** Keep intent outside the filter key: an older query ACK must not remount away a newer selection. */
function ReviewListWorkspace({ queryFilters, readOnly, navigate }: {
  queryFilters: ReviewFilters; readOnly: boolean; navigate: (f: ReviewFilters) => void;
}) {
  const [selected, setSelected] = useState(queryFilters);
  const desired = useRef(queryFilters);
  const pending = useRef(new Set<string>());
  const abandoned = useRef(new Set<string>());
  const latest = useRef<string | null>(null);
  const externalNavigation = useRef(false);
  const navigateRef = useRef(navigate);
  useEffect(() => { navigateRef.current = navigate; }, [navigate]);
  useEffect(() => {
    const pop = () => {
      for (const target of pending.current) abandoned.current.add(target);
      pending.current.clear(); latest.current = null; externalNavigation.current = true;
    };
    window.addEventListener("popstate", pop);
    return () => window.removeEventListener("popstate", pop);
  }, []);
  const { day, mine, tab } = queryFilters;
  useEffect(() => {
    const incoming: ReviewFilters = { day, mine, tab }, key = reviewHref(incoming);
    if (!externalNavigation.current && abandoned.current.has(key) && key !== reviewHref(desired.current)) {
      // A delayed own navigation after Back must not undo the user's history navigation.
      navigateRef.current(desired.current);
      return;
    }
    externalNavigation.current = false;
    if (pending.current.has(key) && key !== latest.current) { pending.current.delete(key); return; }
    pending.current.clear(); latest.current = null;
    desired.current = incoming; setSelected(incoming);
  }, [day, mine, tab]);
  const change = (patch: Partial<ReviewFilters>) => {
    const next = { ...desired.current, ...patch }, target = reviewHref(next);
    desired.current = next; latest.current = target; externalNavigation.current = false;
    abandoned.current.delete(target); pending.current.add(target);
    setSelected(next); navigateRef.current(next);
  };
  return <ReviewScreen key={reviewHref(selected)} filters={selected} readOnly={readOnly} onFilters={change} />;
}
