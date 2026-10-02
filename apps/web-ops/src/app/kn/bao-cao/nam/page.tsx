"use client";

import { ProvinceReportScreen } from "@/kn/cycle/screens/ReportScreens";

/** Báo cáo năm (Cấp Quốc gia, chỉ xem) — chưa có mẫu năm trong 14 mẫu (Q-07, tạm chờ khách). */
export default function Page() {
  return <ProvinceReportScreen title="Báo cáo năm" reportType="annual" />;
}
