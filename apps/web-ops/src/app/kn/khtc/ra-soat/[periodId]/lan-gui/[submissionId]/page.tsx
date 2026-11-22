import { Suspense } from "react";
import { KhtcFormHistoryScreen } from "@/kn/khtc/forms/KhtcFormHistoryScreen";

export default function Page() {
  return <Suspense fallback={<p role="status">Đang đọc lịch sử phiếu…</p>}><KhtcFormHistoryScreen /></Suspense>;
}
