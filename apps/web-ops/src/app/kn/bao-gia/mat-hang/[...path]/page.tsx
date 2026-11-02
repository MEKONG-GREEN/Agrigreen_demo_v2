import { Suspense } from "react";
import { notFound } from "next/navigation";
import { PriceItemDetailScreen } from "@/kn/price/items/ItemDetail";
import { parseItemRoute } from "@/kn/price/items/item-routes";

export default async function Page({ params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  const route = parseItemRoute(path);
  if (!route) notFound();
  return <Suspense fallback={<p>Đang tải danh mục mặt hàng…</p>}><PriceItemDetailScreen route={route} /></Suspense>;
}
