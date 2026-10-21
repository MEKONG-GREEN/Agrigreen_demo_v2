// FN-08 Thông tin làm việc (BRD HCTH v3.0; màn `tl`/`tl.d` của prototype HCTH — W11.29). Logic thuần của màn: lọc
// biến động vào làm/nghỉ việc theo tháng, thẻ số liệu, kiểm tra hộp thoại cập nhật và kế hoạch ghi sự kiện. Màn chỉ
// dùng API có sẵn của W11.22 (/v1/hrm/staff, /v1/hrm/staff/{id}/events*): ngày vào làm/nghỉ việc là sự kiện
// work_start/work_end trong sổ chỉ ghi thêm — sửa là "replace" kèm lý do, bỏ ngày nghỉ là "void" kèm lý do.
import type { HrmEventInput, HrmStaffEvent, HrmStaffItem } from "./staff-api";

/** Ngày (YYYY-MM-DD | YYYY-MM | YYYY) thuộc tháng `ym` (YYYY-MM)? Ngày chỉ có năm không thuộc tháng nào. */
export function inMonth(d: string | null | undefined, ym: string): boolean {
  return !!d && d.length >= 7 && d.slice(0, 7) === ym;
}

/** Tháng hiện tại (giờ Việt Nam) dạng YYYY-MM. */
export function vnMonth(now = Date.now()): string {
  return new Date(now + 7 * 3_600_000).toISOString().slice(0, 7);
}

/** "2026-09" → "Tháng 09/2026". */
export function monthLabel(ym: string): string {
  return `Tháng ${ym.slice(5, 7)}/${ym.slice(0, 4)}`;
}

/** Tháng kế tiếp của YYYY-MM. */
export function nextMonth(ym: string): string {
  const y = Number(ym.slice(0, 4));
  const m = Number(ym.slice(5, 7));
  return m === 12 ? `${y + 1}-01` : `${y}-${String(m + 1).padStart(2, "0")}`;
}

/** Danh sách tháng chọn lọc: `n` tháng gần nhất tính tới `ym` (mới nhất trước). */
export function recentMonths(ym: string, n = 12): string[] {
  const out: string[] = [];
  let y = Number(ym.slice(0, 4));
  let m = Number(ym.slice(5, 7));
  for (let i = 0; i < n; i++) {
    out.push(`${y}-${String(m).padStart(2, "0")}`);
    m -= 1;
    if (m === 0) {
      m = 12;
      y -= 1;
    }
  }
  return out;
}

export type WorkMove = "" | "vao" | "nghi";
export interface WorkFilter {
  /** "" = tất cả; "active" đang làm việc; "left" đã nghỉ việc. */
  status: "" | "active" | "left";
  /** Tháng biến động (YYYY-MM); "" = mọi tháng. */
  month: string;
  move: WorkMove;
}

/** Lọc phía màn theo tháng biến động (máy chủ không có tham số tháng — W11.29 giả định). */
export function filterWork(
  items: HrmStaffItem[],
  f: WorkFilter,
  fallbackMonth: string,
): HrmStaffItem[] {
  return items.filter((s) => {
    const c = s.current;
    if (f.status === "active" && c.status !== "active") return false;
    if (f.status === "left" && c.status !== "left") return false;
    if (!f.month && !f.move) return true;
    const m = f.month || fallbackMonth;
    const v = inMonth(c.started_on, m);
    const n = inMonth(c.ended_on, m);
    return f.move === "vao" ? v : f.move === "nghi" ? n : v || n;
  });
}

export interface WorkKpis {
  active: number;
  joined: number;
  left: number;
}

/** Ba thẻ số liệu của màn: đang làm việc, vào làm và nghỉ việc trong tháng `ym`. */
export function workKpis(items: HrmStaffItem[], ym: string): WorkKpis {
  return {
    active: items.filter((s) => s.current.status === "active").length,
    joined: items.filter((s) => inMonth(s.current.started_on, ym)).length,
    left: items.filter((s) => inMonth(s.current.ended_on, ym)).length,
  };
}

/** Ảnh hưởng đến báo cáo Sở (khối "Ảnh hưởng đến báo cáo" của tl.d): NS02 tháng nghỉ, NS01 không còn từ tháng sau. */
export function reportImpact(ended: string | null | undefined): {
  ns02: string;
  ns01: string;
} {
  if (!ended || ended.length < 7)
    return { ns02: "Không", ns01: "Có trong danh sách" };
  const ym = ended.slice(0, 7);
  return {
    ns02: `Có trong ${monthLabel(ym).toLowerCase()}`,
    ns01: `Không còn từ ${monthLabel(nextMonth(ym)).toLowerCase()}`,
  };
}

/** Dòng quá trình còn hiệu lực, theo thời gian. */
export function effectiveEvents(evs: HrmStaffEvent[]): HrmStaffEvent[] {
  return evs
    .filter((e) => e.effective && !e.void)
    .sort((a, b) =>
      a.valid_from === b.valid_from
        ? a.version - b.version
        : a.valid_from < b.valid_from
          ? -1
          : 1,
    );
}

/** Lần vào làm gần nhất và dòng nghỉ việc đi sau nó (nếu có). */
export function currentSpell(evs: HrmStaffEvent[]): {
  start: HrmStaffEvent | null;
  end: HrmStaffEvent | null;
} {
  const eff = effectiveEvents(evs);
  let start: HrmStaffEvent | null = null;
  let end: HrmStaffEvent | null = null;
  for (const e of eff) {
    if (e.kind === "work_start") {
      start = e;
      end = null;
    } else if (e.kind === "work_end" && start) end = e;
  }
  return { start, end };
}

export interface WorkForm {
  start: string;
  end: string;
  /** Mã danh mục leave_reason (FN-19). */
  leaveReason: string;
  decisionNo: string;
  /** Lý do sửa/hủy dòng cũ (10–500 ký tự) — bắt buộc khi đổi dữ liệu đã ghi. */
  note: string;
}

export type WorkOp =
  | { op: "add"; body: HrmEventInput }
  | { op: "replace"; event: string; body: HrmEventInput }
  | { op: "void"; event: string; reason: string };

/** Kế hoạch ghi sự kiện từ hộp thoại "Cập nhật thông tin làm việc". Không đổi gì → mảng rỗng. */
export function planWork(evs: HrmStaffEvent[], f: WorkForm): WorkOp[] {
  const { start, end } = currentSpell(evs);
  const ops: WorkOp[] = [];
  const endPayload = (): Record<string, string> =>
    Object.fromEntries(
      Object.entries({
        reason: f.leaveReason,
        decision_no: f.decisionNo.trim(),
      }).filter(([, v]) => v),
    );
  if (!start)
    ops.push({
      op: "add",
      body: { kind: "work_start", valid_from: f.start, payload: {} },
    });
  else if (start.valid_from !== f.start)
    ops.push({
      op: "replace",
      event: start.id,
      body: {
        kind: "work_start",
        valid_from: f.start,
        payload: start.payload,
        reason: f.note.trim(),
      },
    });
  if (f.end && !end)
    ops.push({
      op: "add",
      body: { kind: "work_end", valid_from: f.end, payload: endPayload() },
    });
  else if (f.end && end) {
    const p = endPayload();
    const same =
      end.valid_from === f.end &&
      (end.payload.reason ?? "") === (p.reason ?? "") &&
      (end.payload.decision_no ?? "") === (p.decision_no ?? "");
    if (!same)
      ops.push({
        op: "replace",
        event: end.id,
        body: {
          kind: "work_end",
          valid_from: f.end,
          payload: p,
          reason: f.note.trim(),
        },
      });
  } else if (!f.end && end)
    ops.push({ op: "void", event: end.id, reason: f.note.trim() });
  return ops;
}

/** Kiểm tra hộp thoại (theo prototype `tlModal` + quy tắc sổ: sửa/hủy cần lý do 10–500 ký tự). */
export function validateWork(
  evs: HrmStaffEvent[],
  f: WorkForm,
): { message: string; fields: (keyof WorkForm)[] } | null {
  if (!f.start) return { message: "Nhập ngày vào làm.", fields: ["start"] };
  if (f.end && f.end < f.start)
    return {
      message: "Ngày nghỉ việc không được trước ngày vào làm.",
      fields: ["end"],
    };
  if (f.end && !f.leaveReason)
    return { message: "Chọn lý do nghỉ việc.", fields: ["leaveReason"] };
  const ops = planWork(evs, f);
  if (ops.length === 0)
    return { message: "Không có thay đổi để lưu.", fields: [] };
  const needNote = ops.some((o) => o.op !== "add");
  const n = f.note.trim().length;
  if (needNote && (n < 10 || n > 500))
    return {
      message:
        "Sửa hoặc bỏ ngày đã ghi cần lý do 10–500 ký tự (sổ chỉ ghi thêm, dòng cũ được giữ lại).",
      fields: ["note"],
    };
  return null;
}
