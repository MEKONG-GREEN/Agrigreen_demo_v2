"use client";

import { useParams } from "next/navigation";
import { ReviewDetailScreen } from "@/kn/cycle/screens/TinhScreens";

/** Admin › Phòng ban › Dữ liệu báo cáo các xã › một báo cáo (chỉ xem). */
export default function Page() {
  const { dept } = useParams<{ dept: string }>();
  return <ReviewDetailScreen base={`/kn/phong-ban/${dept}/du-lieu-bao-cao`} />;
}
