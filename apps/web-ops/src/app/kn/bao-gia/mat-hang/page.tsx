"use client";

import { Suspense } from "react";
import { ItemsScreen } from "@/kn/price/items/ItemsScreen";

/** FN-AD-04.03 Danh mục mặt hàng, nhóm hàng theo thời kỳ (Báo giá nông sản — W11.73). */
export default function Page() {
  return <Suspense fallback={<p>Đang tải mặt hàng…</p>}><ItemsScreen /></Suspense>;
}
