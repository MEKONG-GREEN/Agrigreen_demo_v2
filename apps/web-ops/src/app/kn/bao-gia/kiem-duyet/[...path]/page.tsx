import { Suspense } from "react";
import { TicketReviewWorkspace } from "@/kn/price/tickets/TicketReviewWorkspace";
export default async function Page({ params }: { params: Promise<{ path: string[] }> }) { const { path } = await params; return <Suspense fallback={<p>Đang tải phiếu…</p>}><TicketReviewWorkspace path={path} /></Suspense>; }
