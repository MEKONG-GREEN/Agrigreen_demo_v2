// Mô hình lịch công tác tuần (P5) trên kiểu SINH từ contracts/openapi/application.yaml:
// trùng thời gian, xếp mục vào lưới tuần, so bản đã duyệt ↔ bản điều chỉnh, nhãn trạng thái/sự kiện.
// Hàm thuần — dùng chung cho màn hình Cấp Xã / Cấp Tỉnh và backend giả lập.
import type { components } from "@/api/generated/application";
import type { PillTone } from "@/components/patterns/list";

type S = components["schemas"];
export type WsStatus = S["ExtWorkScheduleStatus"];
export type WsItemInput = S["ExtWorkScheduleItemInput"];
export type WsItem = S["ExtWorkScheduleItem"];
export type WsSchedule = S["ExtWorkSchedule"];
export type WsListItem = S["ExtWorkScheduleListItem"];
export type WsDetail = S["ExtWorkScheduleDetail"];
export type WsMine = S["ExtMyWorkSchedule"];
export type WsCounts = S["ExtWorkScheduleCounts"];
export type WsUnregistered = S["ExtWorkScheduleUnregistered"];
export type WsWarning = S["ExtWorkScheduleWarning"];
export type WsVersion = S["ExtWorkScheduleVersion"];
export type WsSave = S["ExtWorkScheduleSave"];
export type WsSettings = S["ExtWorkScheduleSettings"];
export type KnLogEntry = S["KNLogEntry"];
export type KnLogPage = S["KNLogPage"];
export type WsSession = WsItemInput["session"];

export const SESSIONS: readonly WsSession[] = ["morning", "afternoon"];
export const SESSION_LABEL: Record<WsSession, string> = { morning: "Sáng", afternoon: "Chiều" };

/** Khung giờ mặc định của buổi khi mục không ghi giờ (dùng để xét trùng thời gian). */
export const SESSION_BOUNDS: Record<WsSession, [string, string]> = { morning: ["07:00", "11:30"], afternoon: ["13:00", "17:00"] };

export const STATUS_LABEL: Record<WsStatus, string> = {
  draft: "Nháp",
  pending: "Chờ duyệt",
  approved: "Đã duyệt",
  changes_requested: "Yêu cầu chỉnh sửa",
};

export const STATUS_TONE: Record<WsStatus, PillTone> = { draft: "info", pending: "warn", approved: "ok", changes_requested: "bad" };

/** Nhãn trạng thái có tính "điều chỉnh": "Chờ duyệt điều chỉnh". */
export function statusLabel(s: Pick<WsSchedule, "status" | "amendment">): string {
  if (s.amendment && s.status === "pending") return "Chờ duyệt điều chỉnh";
  return STATUS_LABEL[s.status];
}

/** Sự kiện Log (KNLogEntry.event) → nhãn tiếng Việt. */
export const LOG_EVENT_LABEL: Record<string, string> = {
  draft_saved: "Lưu nháp",
  copied_previous: "Sao chép tuần trước",
  submitted: "Gửi duyệt",
  amended: "Gửi điều chỉnh lịch đã duyệt",
  approved: "Duyệt lịch",
  change_requested: "Yêu cầu chỉnh sửa",
  locked: "Khóa lịch (tuần đã kết thúc)",
};

export function logEventLabel(event: string): string {
  return LOG_EVENT_LABEL[event] ?? event;
}

// ---------------------------------------------------------------------------
// Thời gian, trùng giờ
// ---------------------------------------------------------------------------
export const TIME_PATTERN = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

function minutes(t: string): number {
  const [h, m] = t.split(":");
  return Number(h) * 60 + Number(m);
}

/** Khoảng [bắt đầu, kết thúc) theo phút; thiếu giờ → biên của buổi. */
export function itemInterval(it: Pick<WsItemInput, "session" | "start_time" | "end_time">): [number, number] {
  const [s0, e0] = SESSION_BOUNDS[it.session];
  // Cùng quy tắc với máy chủ (extension/domain.Conflicts) và field-app: không ghi giờ → cả buổi;
  // có giờ bắt đầu mà không có (hoặc sai) giờ kết thúc → 1 giờ.
  if (!(it.start_time && TIME_PATTERN.test(it.start_time))) return [minutes(s0), minutes(e0)];
  const start = minutes(it.start_time);
  let end = it.end_time && TIME_PATTERN.test(it.end_time) ? minutes(it.end_time) : start + 60;
  if (end <= start) end = start + 60;
  return [start, end];
}

export function overlaps(a: Pick<WsItemInput, "day" | "session" | "start_time" | "end_time">, b: Pick<WsItemInput, "day" | "session" | "start_time" | "end_time">): boolean {
  if (a.day !== b.day) return false;
  const [as, ae] = itemInterval(a);
  const [bs, be] = itemInterval(b);
  return as < be && bs < ae;
}

/**
 * Chỉ số các mục trùng thời gian với ít nhất một mục khác (cùng ngày, khoảng giờ giao nhau).
 * Hai mục cùng buổi không ghi giờ coi như trùng (cả buổi) — chỉ là cảnh báo, không chặn lưu.
 */
export function conflictIndexes(items: readonly Pick<WsItemInput, "day" | "session" | "start_time" | "end_time">[]): Set<number> {
  const out = new Set<number>();
  for (let i = 0; i < items.length; i++) {
    for (let j = i + 1; j < items.length; j++) {
      if (overlaps(items[i]!, items[j]!)) {
        out.add(i);
        out.add(j);
      }
    }
  }
  return out;
}

/** Lỗi giờ của một mục (kết thúc phải sau bắt đầu). */
export function timeError(it: Pick<WsItemInput, "start_time" | "end_time">): string | null {
  if (it.start_time && !TIME_PATTERN.test(it.start_time)) return "Giờ bắt đầu không hợp lệ (HH:MM).";
  if (it.end_time && !TIME_PATTERN.test(it.end_time)) return "Giờ kết thúc không hợp lệ (HH:MM).";
  if (it.start_time && it.end_time && minutes(it.end_time) <= minutes(it.start_time)) return "Giờ kết thúc phải sau giờ bắt đầu.";
  return null;
}

/** RB-3: thành phần phối hợp không ghi SĐT/CCCD — phát hiện dãy ≥ 9 chữ số (bỏ khoảng trắng, dấu chấm, gạch). */
export function looksLikePersonalNumber(text: string | null | undefined): boolean {
  if (!text) return false;
  return /\d{9,}/.test(text.replace(/[\s.\-()+]/g, ""));
}

export function timeRangeLabel(it: Pick<WsItemInput, "start_time" | "end_time">): string {
  if (it.start_time && it.end_time) return `${it.start_time}–${it.end_time}`;
  if (it.start_time) return `từ ${it.start_time}`;
  if (it.end_time) return `đến ${it.end_time}`;
  return "";
}

// ---------------------------------------------------------------------------
// Lưới tuần
// ---------------------------------------------------------------------------
function byStart<T extends Pick<WsItemInput, "session" | "start_time" | "end_time">>(a: T, b: T): number {
  return itemInterval(a)[0] - itemInterval(b)[0];
}

/** Lưới Cấp Xã: ô [buổi][ngày] → các mục, xếp theo giờ bắt đầu. Mục ngoài tuần bị bỏ. */
export function placeBySession<T extends Pick<WsItemInput, "day" | "session" | "start_time" | "end_time">>(
  items: readonly T[],
  days: readonly string[],
): Record<WsSession, T[][]> {
  const grid: Record<WsSession, T[][]> = { morning: days.map(() => []), afternoon: days.map(() => []) };
  for (const it of items) {
    const di = days.indexOf(it.day);
    if (di >= 0) grid[it.session][di]!.push(it);
  }
  for (const s of SESSIONS) grid[s].forEach((cell) => cell.sort(byStart));
  return grid;
}

/** Lưới Cấp Tỉnh: một dòng mỗi cán bộ, ô theo ngày (sáng trước chiều, theo giờ). */
export function placeByDay<T extends Pick<WsItemInput, "day" | "session" | "start_time" | "end_time">>(items: readonly T[], days: readonly string[]): T[][] {
  const cells: T[][] = days.map(() => []);
  for (const it of items) {
    const di = days.indexOf(it.day);
    if (di >= 0) cells[di]!.push(it);
  }
  cells.forEach((c) => c.sort((a, b) => (a.session === b.session ? byStart(a, b) : a.session === "morning" ? -1 : 1)));
  return cells;
}

// ---------------------------------------------------------------------------
// So bản đã duyệt ↔ bản điều chỉnh
// ---------------------------------------------------------------------------
export const ITEM_FIELDS = ["day", "session", "start_time", "end_time", "content", "commune_code", "cooperative_ref", "location", "participants", "note"] as const;
export type ItemField = (typeof ITEM_FIELDS)[number];

export const ITEM_FIELD_LABEL: Record<ItemField, string> = {
  day: "Ngày",
  session: "Buổi",
  start_time: "Giờ bắt đầu",
  end_time: "Giờ kết thúc",
  content: "Nội dung",
  commune_code: "Xã/phường",
  cooperative_ref: "HTX",
  location: "Địa điểm",
  participants: "Thành phần phối hợp",
  note: "Ghi chú",
};

function val(v: unknown): string | null {
  return v === undefined || v === null || v === "" ? null : String(v);
}

export function changedFields(a: Partial<WsItemInput>, b: Partial<WsItemInput>): ItemField[] {
  return ITEM_FIELDS.filter((f) => val(a[f]) !== val(b[f]));
}

export interface ItemDiff<A, B = A> {
  added: B[];
  removed: A[];
  modified: { before: A; after: B; fields: ItemField[] }[];
  unchanged: B[];
}

/** So hai bản theo mã mục (id giữ qua các phiên bản). */
export function diffItems<A extends Partial<WsItemInput>, B extends Partial<WsItemInput> = A>(before: readonly A[], after: readonly B[]): ItemDiff<A, B> {
  const prev = new Map(before.filter((x) => x.id).map((x) => [x.id!, x]));
  const out: ItemDiff<A, B> = { added: [], removed: [], modified: [], unchanged: [] };
  const seen = new Set<string>();
  for (const it of after) {
    const old = it.id ? prev.get(it.id) : undefined;
    if (!old) {
      out.added.push(it);
      continue;
    }
    seen.add(it.id!);
    const fields = changedFields(old, it);
    if (fields.length) out.modified.push({ before: old, after: it, fields });
    else out.unchanged.push(it);
  }
  for (const it of before) if (!it.id || !seen.has(it.id)) out.removed.push(it);
  return out;
}

/** Thông tin vắn tắt một mục: "T3 06/10 · Sáng 07:30–10:00 · Thăm đồng". */
export function itemSummary(it: Pick<WsItemInput, "day" | "session" | "start_time" | "end_time" | "content">, dayLabel: (d: string) => string): string {
  const t = timeRangeLabel(it);
  return `${dayLabel(it.day)} · ${SESSION_LABEL[it.session]}${t ? ` ${t}` : ""} · ${it.content}`;
}

/** Bỏ thuộc tính chỉ đọc (changed, conflict) trước khi gửi lại server. */
export function toInput(it: WsItem | WsItemInput): WsItemInput {
  return {
    id: it.id,
    day: it.day,
    session: it.session,
    start_time: it.start_time ?? null,
    end_time: it.end_time ?? null,
    content: it.content,
    commune_code: it.commune_code ?? null,
    cooperative_ref: it.cooperative_ref ?? null,
    location: it.location ?? null,
    participants: it.participants ?? null,
    note: it.note ?? null,
  };
}
