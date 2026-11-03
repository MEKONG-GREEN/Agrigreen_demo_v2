"use client";
import { useRouter, useSearchParams } from "next/navigation";
import { useMemo } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { DetailPage } from "@/components/patterns/detail";
import { createPriceTicketApi, vnToday } from "./api";
import { ReviewScreen } from "./ReviewScreen";
import { TicketReviewDetail } from "./TicketReviewDetail";
import { reviewFilters, reviewHref, reviewRoute } from "./ticket-review-routes";
const clients = new WeakMap<object, number>(); let nextClient = 0;
function clientIdentity(client: object) { let id = clients.get(client); if (id === undefined) { id = ++nextClient; clients.set(client, id); } return id; }
/** Key destroys local fields/requests/idempotency turns when authority or API identity changes. */
export function TicketReviewWorkspace({ path = [] }: { path?: string[] }) {
  const auth = useAuth(), query = useSearchParams(), router = useRouter();
  const ta = useMemo(() => createPriceTicketApi(auth.api), [auth.api]);
  const filters = reviewFilters(query, vnToday()), route = reviewRoute(path), ctx = auth.kn.context;
  const allowed = auth.status === "authenticated" && auth.kn.status === "active" && !!ctx && (ctx.role === "admin" || (ctx.role === "kn_tinh" && ctx.department_code === "thong_tin_truyen_thong"));
  const back = { href: reviewHref(filters), label: "Danh sách kiểm duyệt" };
  if (!allowed) return <DetailPage back={back} title="Phiếu báo giá"><p role="alert">Chọn ngữ cảnh Cấp Tỉnh Phòng Thông tin truyền thông hoặc Admin để xem phiếu và nhật ký.</p></DetailPage>;
  if (route.kind === "invalid") return <DetailPage back={back} title="Không tìm thấy trang phiếu"><p role="alert">Đường dẫn phiếu không hợp lệ.</p></DetailPage>;
  const identity = JSON.stringify([clientIdentity(auth.api), auth.status, auth.principal, auth.kn.status, ctx, auth.kn.support, filters, route]);
  return route.kind === "list" ? <ReviewScreen key={identity} filters={filters} onFilters={f => router.replace(reviewHref(f))} /> : <TicketReviewDetail key={identity} route={route} filters={filters} ta={ta} readOnly={ctx?.read_only === true} />;
}
