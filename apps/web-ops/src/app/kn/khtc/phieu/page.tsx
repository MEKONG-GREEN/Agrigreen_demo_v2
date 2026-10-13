"use client";

import { DepartmentFormsScreen } from "@/kn/khtc/inbox/DepartmentFormsScreen";

/** /kn/khtc/phieu — cùng màn "Phiếu kỳ này" của W11.11 (FN-PN-03.01); nhập phiếu ở /kn/khtc/phieu/{period_id} (W11.63). */
export default function Page() {
  return <DepartmentFormsScreen view="current" />;
}
