"use client";

import "@/kn/schedule/schedule.css";
import { useAuth } from "@/auth/AuthProvider";
import { ApproveSchedules } from "@/kn/schedule/ApproveSchedules";
import { RegisterSchedule } from "@/kn/schedule/RegisterSchedule";
import { useWorkSchedule } from "@/kn/schedule/useWorkSchedule";

/**
 * Điều hành › Nhiệm vụ › Lịch công tác (P5). Một đường dẫn cho mọi vai có mục menu:
 * Cấp Xã → đăng ký lịch tuần (FN-XA-03.08, Hình 21); Cấp Tỉnh → duyệt lịch Cấp Xã + Lịch của tôi (FN-TI-02.07, Hình 12);
 * Admin → chỉ xem. Cấp Quốc gia không có mục menu này (ma trận §5) — khung chặn đường dẫn.
 */
export default function WorkSchedulePage() {
  const { api, kn } = useAuth();
  const { ws, mode } = useWorkSchedule();
  if (mode === "register") return <RegisterSchedule ws={ws} api={api} communes={kn.context?.communes ?? []} />;
  if (mode === "approve") return <ApproveSchedules ws={ws} api={api} />;
  if (mode === "view") return <ApproveSchedules ws={ws} api={api} readOnly />;
  return (
    <div className="card sec" role="alert">
      <b>Không có quyền xem lịch công tác.</b>
      <p className="mute">Chức năng dành cho Cấp Tỉnh, Cấp Xã của phòng ban và Admin.</p>
    </div>
  );
}
