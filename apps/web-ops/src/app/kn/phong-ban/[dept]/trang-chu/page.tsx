"use client";

import { useParams } from "next/navigation";
import { DeptHomeScreen } from "@/kn/depthome/DeptHomeScreen";

/** FN-17 Trang chủ phòng — Admin › Phòng ban › {phòng} (W11.54). */
export default function Page() {
  const { dept } = useParams<{ dept: string }>();
  return <DeptHomeScreen department={dept} />;
}
