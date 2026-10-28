"use client";

import { useParams } from "next/navigation";
import { DeptLibraryScreen } from "@/kn/depthome/DeptLibraryScreen";

/** FN-20 Kho văn bản phòng — Admin › Phòng ban › {phòng} (W11.54). */
export default function Page() {
  const { dept } = useParams<{ dept: string }>();
  return <DeptLibraryScreen department={dept} />;
}
