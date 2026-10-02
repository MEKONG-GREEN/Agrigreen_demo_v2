"use client";

import { useParams } from "next/navigation";
import { ReviewListScreen } from "@/kn/cycle/screens/TinhScreens";

/** Admin › Phòng ban › Dữ liệu báo cáo các xã (chỉ xem). */
export default function Page() {
  const { dept } = useParams<{ dept: string }>();
  return <ReviewListScreen base={`/kn/phong-ban/${dept}/du-lieu-bao-cao`} />;
}
