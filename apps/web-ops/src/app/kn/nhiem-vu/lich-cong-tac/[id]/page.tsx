"use client";

import "@/kn/schedule/schedule.css";
import { useParams } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { SCHEDULE_ROUTE } from "@/kn/schedule/ApproveSchedules";
import { ScheduleDetail } from "@/kn/schedule/ScheduleDetail";
import { useWorkSchedule } from "@/kn/schedule/useWorkSchedule";

/** Detail một lịch tuần (FN-TI-02.07): thanh nút Duyệt / Yêu cầu chỉnh sửa, so bản đã duyệt, khung Log. */
export default function WorkScheduleDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { api } = useAuth();
  const { ws, mode } = useWorkSchedule();
  return <ScheduleDetail key={id} ws={ws} api={api} id={id} backHref={SCHEDULE_ROUTE} readOnly={mode !== "approve"} />;
}
