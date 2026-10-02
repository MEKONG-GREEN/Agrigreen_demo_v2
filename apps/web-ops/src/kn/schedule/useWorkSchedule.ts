"use client";

import { useMemo } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { createWorkScheduleApi, type WorkScheduleApi } from "./api";

/** Vai trò trên màn Lịch công tác: Cấp Xã đăng ký, Cấp Tỉnh duyệt + "Lịch của tôi", Admin chỉ xem; Cấp Quốc gia không có quyền. */
export type ScheduleMode = "register" | "approve" | "view" | "none";

export function useWorkSchedule(): { ws: WorkScheduleApi; mode: ScheduleMode } {
  const { api, kn } = useAuth();
  const ws = useMemo(() => createWorkScheduleApi(api), [api]);
  const role = kn.context?.role;
  const mode: ScheduleMode = role === "kn_xa" ? "register" : role === "kn_tinh" ? "approve" : role === "admin" ? "view" : "none";
  return { ws, mode };
}
