"use client";

import { DepartmentFormsScreen } from "@/kn/khtc/inbox/DepartmentFormsScreen";

/** FN-PN-03.01 Báo cáo cho KHTC của phòng — Admin › Phòng ban › <phòng> (xem, nhập thay khi hỗ trợ; W11.11). */
export default function Page() {
  return <DepartmentFormsScreen view="current" />;
}
