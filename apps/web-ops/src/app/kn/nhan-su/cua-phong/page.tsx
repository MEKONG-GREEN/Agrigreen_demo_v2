"use client";

import { HrmStaffListScreen } from "@/kn/hrm/StaffScreens";

/** Nhân sự của phòng (Q-067, W11.22): lãnh đạo các phòng xem hồ sơ thuộc đơn vị mình (phạm vi "đơn vị mình" — FN-20). */
export default function Page() {
  return <HrmStaffListScreen mode="unit" />;
}
