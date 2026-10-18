"use client";

import { useParams } from "next/navigation";
import { KhtcReportsScreen } from "@/kn/khtc/reports/KhtcReportsScreen";
import { DepartmentFormsScreen } from "@/kn/khtc/inbox/DepartmentFormsScreen";

/** FN-PN-03.01 Báo cáo cho KHTC của phòng — Admin › Phòng ban › <phòng> (xem, nhập thay khi hỗ trợ; W11.11). */
export default function Page() {
  const { dept } = useParams<{ dept: string }>();
 if (dept === "ke_hoach_tai_chinh") return <KhtcReportsScreen />;
 return <DepartmentFormsScreen view="current" />;
}
