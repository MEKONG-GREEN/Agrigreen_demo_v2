"use client";
import { useMemo, useRef } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { newIdempotencyKey } from "@/api/idempotency";
import { useAction } from "@/kn/cycle/common";
import { createAppraisalApi } from "./appraisal-api";
import { APPRAISAL_RATINGS, APPRAISAL_RATING_LABEL, type AppraisalRating } from "./appraisal-form";

export const MINE_BASE = "/kn/nhan-su/danh-gia-cua-toi";
export const GROUPS = { vien_chuc: "Viên chức", lanh_dao: "Lãnh đạo, quản lý", can_bo_xa: "Cán bộ khuyến nông cấp xã" } as const;
export function useAppraisalApi() { const { api } = useAuth(); return useMemo(() => createAppraisalApi(api), [api]); }
/** Khóa theo thao tác + dữ liệu, giữ sau mất phản hồi; không lưu PII ra storage. */
export function useAppraisalAction() {
  const action = useAction(), keys = useRef(new Map<string, string>());
  return { ...action, execute: <T,>(op: string, body: unknown, fn: (key: string) => Promise<T>) => {
    const fingerprint = JSON.stringify([op, body]);
    const key = keys.current.get(fingerprint) ?? newIdempotencyKey(); keys.current.set(fingerprint, key);
    return action.run(async () => { const value = await fn(key); keys.current.delete(fingerprint); return value; }, "Đã ghi thay đổi.");
  } };
}
export function RatingSelect({ label, value, onChange, disabled = false }: { label: string; value: AppraisalRating; onChange: (v: AppraisalRating) => void; disabled?: boolean }) {
  return <label className="field">{label}<select aria-label={label} value={value} onChange={e => onChange(e.target.value as AppraisalRating)} disabled={disabled}>
    <option value="">Chưa chọn</option>{APPRAISAL_RATINGS.map(r => <option key={r} value={r}>{APPRAISAL_RATING_LABEL[r]}</option>)}
  </select></label>;
}
export function localDateTime(value: string) { const d = new Date(value); return Number.isNaN(d.valueOf()) ? "" : new Date(d.valueOf() + 7 * 3600000).toISOString().slice(0, 16); }
export function asISO(value: string) { return value ? new Date(value + ":00+07:00").toISOString() : ""; }
export const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;