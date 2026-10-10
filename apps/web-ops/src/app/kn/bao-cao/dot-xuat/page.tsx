"use client";

import { useAuth } from "@/auth/AuthProvider";
import { ConsolidationScreen, ProvinceReportScreen } from "@/kn/cycle/screens/ReportScreens";
import { TasksScreen } from "@/kn/cycle/screens/XaScreens";

/**
 * FN-AD-04.06 "Báo cáo đột xuất" (W11.02): menu chỉ hiện khi phòng ban có mẫu loại `dot_xuat` (party/domain MenuRequirement).
 * Cấp Xã — công việc nhập các biểu đột xuất (FN-XA-03.01/03.02, kỳ `adhoc`); Cấp Tỉnh — tổng hợp/rà soát đợt đột xuất
 * (FN-TI-02.05); Cấp Quốc gia — xem chỉ đọc (FN-TW-01.02).
 */
export default function Page() {
  const { kn } = useAuth();
  const role = kn.context?.role;
  if (role === "kn_xa") return <TasksScreen periodType="adhoc" title="Báo cáo đột xuất" />;
  if (role === "kn_tw") return <ProvinceReportScreen title="Báo cáo đột xuất" reportType="adhoc" />;
  return <ConsolidationScreen title="Báo cáo đột xuất" reportType="adhoc" />;
}
