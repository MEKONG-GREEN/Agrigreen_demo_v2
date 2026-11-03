"use client";

import { Suspense } from "react";
import { ItemsScreen } from "@/kn/price/items/ItemsScreen";

/** UR-XA-06 / BG-ITEM-PROPOSAL: mở lịch sử đề xuất bằng màn nghiệp vụ W11.73. */
export default function Page() {
  return <Suspense fallback={<p>Đang tải đề xuất…</p>}><ItemsScreen initialTab="proposals" /></Suspense>;
}
