"use client";

import { useAuth } from "@/auth/AuthProvider";
import { ProvinceReportScreen } from "@/kn/cycle/screens/ReportScreens";
import { TasksScreen } from "@/kn/cycle/screens/XaScreens";

/** Báo cáo hàng vụ: Cấp Quốc gia xem báo cáo tỉnh chỉ đọc (FN-TW-01.02, Hình 5); Cấp Xã nhập biểu của loại kỳ (FN-XA-03.02). */
export default function Page() {
  const { kn } = useAuth();
  return kn.context?.role === "kn_xa" ? <TasksScreen periodType="season" title="Báo cáo hàng vụ" /> : <ProvinceReportScreen title="Báo cáo hàng vụ" reportType="seasonal" />;
}
