import { Suspense } from "react";
import { PointsScreen } from "@/kn/price/points/PointsScreen";
export default function Page() {
  return (
    <Suspense fallback={<p>Đang tải xã báo giá…</p>}>
      <PointsScreen />
    </Suspense>
  );
}
