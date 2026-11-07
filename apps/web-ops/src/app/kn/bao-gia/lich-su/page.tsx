import { Suspense } from "react";
import { HistoryScreen } from "@/kn/price/tickets/XaScreens";

/** FN-XA-03.03 — lịch sử chỉ đọc theo BRD Báo giá v2.0. */
export default function Page() {
  return <Suspense fallback={<p>Đang tải lịch sử báo giá…</p>}><HistoryScreen /></Suspense>;
}
