import { Suspense } from "react";
import { TicketReviewWorkspace } from "@/kn/price/tickets/TicketReviewWorkspace";
export default function Page() { return <Suspense fallback={<p>Đang tải kiểm duyệt…</p>}><TicketReviewWorkspace /></Suspense>; }
