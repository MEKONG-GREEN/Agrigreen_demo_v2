// DỮ LIỆU MINH HỌA cho các mẫu trang của khung P6 (đợt 1) — lấy theo wireframe Hình 3c–3f, 13.
// KHÔNG phải số liệu thật và KHÔNG gọi API: màn hình đầy đủ dựng ở đợt 3 (P6-màn) trên hợp đồng P4
// (kn-dashboard, report-tasks, consolidations). Chỉ hiển thị ở chế độ giả lập (NEXT_PUBLIC_API_MOCK=1).
import type { DashboardAlert, PeriodCompareItem, ProgressItem } from "@/components/patterns/dashboard";
import type { LogEntry } from "@/components/patterns/detail";

export const DEMO_HOT = [
  { label: "DT xuống giống lúa (ha)", value: "72.410", delta: "81,7% KH · +6,2% so kỳ trước", trend: "up", series: [40, 44, 47, 50, 55, 58, 63, 66, 70, 72], tone: "ok" },
  { label: "DT đã thu hoạch (ha)", value: "18.240", delta: "+12,4% so kỳ trước", trend: "up", series: [6, 7, 7, 9, 10, 12, 13, 15, 17, 18], tone: "ok" },
  { label: "DT nhiễm sâu bệnh (ha)", value: "3.120", delta: "+8,1% so kỳ trước", trend: "down", series: [22, 25, 23, 27, 26, 29, 27, 30, 29, 31], tone: "bad" },
  { label: "Năng suất lúa khô (t/ha)", value: "6,2", delta: "+0,2 so cùng kỳ", trend: "up", series: [5.8, 6, 5.9, 6.1, 6, 6.2, 6.1, 6.3, 6.2, 6.2], tone: "ok" },
  { label: "Xã đã nộp báo cáo", value: "87/102", delta: "15 xã chưa nộp tuần 36", trend: "neutral", series: [40, 52, 60, 66, 71, 76, 80, 83, 86, 87], tone: "ok" },
] as const;

export const DEMO_PROGRESS: ProgressItem[] = [
  { label: "Long Xuyên", pct: 72, detail: "854 / 1.186 ha" },
  { label: "Châu Đốc", pct: 58, detail: "990 / 1.700 ha" },
  { label: "Tịnh Biên", pct: 90, detail: "4.050 / 4.500 ha" },
  { label: "An Phú", pct: 35, detail: "1.200 / 3.400 ha" },
  { label: "Tri Tôn", pct: 64, detail: "6.400 / 10.000 ha" },
];

export const DEMO_COMPARE: PeriodCompareItem[] = [
  { label: "Xuống giống", current: 72410, previous: 68180 },
  { label: "Thu hoạch", current: 18240, previous: 19020 },
  { label: "Rau màu", current: 9810, previous: 9000 },
  { label: "Sâu bệnh", current: 3120, previous: 3193 },
  { label: "Cây ăn trái", current: 7420, previous: 7175 },
];

export const DEMO_ALERTS: DashboardAlert[] = [
  { id: "a1", level: "urgent", title: "Rầy nâu tăng mạnh tại An Giang", description: "DT nhiễm 900 ha, tăng 35% so tuần trước", at: "2026-09-07T01:15:00Z", href: "/thong-bao" },
  { id: "a2", level: "attention", title: "15 xã chưa nộp báo cáo tuần 36", description: "Hạn nộp 08/09/2026 17:00", at: "2026-09-08T00:00:00Z", href: "/thong-bao" },
  { id: "a3", level: "attention", title: "Tiến độ xuống giống An Phú thấp", description: "Đạt 35% kế hoạch", at: "2026-09-07T09:05:00Z", href: "/thong-bao" },
  { id: "a4", level: "info", title: "Đã cập nhật báo cáo tổng hợp tuần 36", description: "Dữ liệu 87/102 xã", at: "2026-09-08T02:15:00Z", href: "/thong-bao" },
];

export type DemoTaskStatus = "not_started" | "draft" | "adjustment" | "late" | "submitted";

export interface DemoTask {
  id: string;
  commune: string;
  form: string;
  note?: string;
  period: string;
  due: string;
  status: DemoTaskStatus;
  updated: string | null;
}

export const DEMO_TASKS: DemoTask[] = [
  { id: "t1", commune: "Mỹ Hòa Hưng", form: "T05 · Sâu bệnh hại lúa", period: "Tuần 36", due: "08/09 17:00", status: "not_started", updated: null },
  { id: "t2", commune: "Mỹ Hòa Hưng", form: "T01 · Tiến độ xuống giống lúa", period: "Tuần 36", due: "08/09 17:00", status: "draft", updated: "07/09 15:20" },
  { id: "t3", commune: "Mỹ Hòa Hưng", form: "T04 · Rau màu", note: "Tỉnh: \"NS dưa hấu 85 tấn/ha bất thường\"", period: "Tuần 35", due: "01/09 17:00", status: "adjustment", updated: "06/09 09:02" },
  { id: "t4", commune: "Bình Đức", form: "T01 · Tiến độ xuống giống lúa", period: "Tuần 35", due: "01/09 17:00", status: "late", updated: null },
  { id: "t5", commune: "Bình Đức", form: "T05 · Sâu bệnh hại lúa", period: "Tuần 36", due: "08/09 17:00", status: "not_started", updated: null },
  { id: "t6", commune: "Bình Đức", form: "M01 · Cây ăn trái, cây CN", period: "Tháng 08/2026", due: "10/09", status: "not_started", updated: null },
  { id: "t7", commune: "Mỹ Hòa Hưng", form: "T03 · Phân trà lúa", period: "Tuần 35", due: "01/09 17:00", status: "submitted", updated: "01/09 10:12" },
];

export const TASK_STATUS: Record<DemoTaskStatus, { label: string; tone: "neutral" | "ok" | "warn" | "bad" | "info"; action: string }> = {
  not_started: { label: "Chưa nhập", tone: "neutral", action: "Nhập" },
  draft: { label: "Lưu nháp", tone: "info", action: "Tiếp tục" },
  adjustment: { label: "Yêu cầu điều chỉnh", tone: "bad", action: "Sửa" },
  late: { label: "Trễ hạn", tone: "warn", action: "Nhập" },
  submitted: { label: "Đã nộp", tone: "ok", action: "Xem" },
};

export interface DemoCoop {
  id: string;
  name: string;
  commune: string;
  representative: string;
  area_ha: string;
  members: number;
  status: "active" | "suspended";
}

export const DEMO_COOPS: DemoCoop[] = Array.from({ length: 23 }, (_, i) => ({
  id: `htx-${String(i + 1).padStart(2, "0")}`,
  name: `HTX Nông nghiệp ${["Mỹ Hòa", "Bình Đức", "Tân Phú", "Hòa An", "Phú Thạnh"][i % 5]} ${Math.floor(i / 5) + 1}`,
  commune: i % 2 ? "Bình Đức" : "Mỹ Hòa Hưng",
  representative: ["Nguyễn Văn Hùng", "Trần Thị Lan", "Lê Văn Tâm", "Phạm Thị Hoa"][i % 4]!,
  area_ha: (120 + i * 13.5).toLocaleString("vi-VN", { maximumFractionDigits: 1 }),
  members: 40 + ((i * 7) % 60),
  status: i % 9 === 8 ? "suspended" : "active",
}));

export const DEMO_COOP_LOG: LogEntry[] = [
  { event: "Cập nhật diện tích", actor_display: "Nguyễn Văn An", at: "2026-09-07T03:20:00Z", changes: [{ field: "Diện tích (ha)", old: "118", new: "120" }] },
  { event: "Thêm nông hộ thành viên", actor_display: "Nguyễn Văn An", at: "2026-09-05T08:02:00Z" },
  { event: "Đổi người đại diện", actor_display: "Trần Thị Bình", at: "2026-08-28T02:40:00Z", reason: "Bầu lại Ban giám đốc" },
  { event: "Tạo HTX", actor_display: "admin.mekong", at: "2026-08-01T01:00:00Z" },
];
