import { Suspense } from "react";
import { notFound } from "next/navigation";
import { PricePointDetailScreen } from "@/kn/price/points/PointDetail";
import { parsePointRoute } from "@/kn/price/points/point-routes";
export default async function Page({
  params,
}: {
  params: Promise<{ path: string[] }>;
}) {
  const { path } = await params;
  const route = parsePointRoute(path);
  if (!route) notFound();
  return (
    <Suspense fallback={<p>Đang tải xã báo giá…</p>}>
      <PricePointDetailScreen route={route} />
    </Suspense>
  );
}
