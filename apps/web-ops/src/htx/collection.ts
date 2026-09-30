// Thu gom rơm MKG phía HTX (G3-2, RB-6 chiều mua): lịch gom và bảng kê số cuộn MKG gửi qua
// van công bố; quản lý HTX xác nhận hoặc tranh chấp. Quy tắc giống máy chủ (pf/cropcycle
// domain.CheckResponse) — máy chủ vẫn kiểm lại từng lời gọi.
import type { HtxCollectionSchedule, HtxCollectionStatement } from "@/api/types";

export const SCHEDULE_STATUS: Record<HtxCollectionSchedule["status"], { text: string; cls: string }> = {
  planned: { text: "Dự kiến", cls: "c-info" },
  confirmed: { text: "Đã chốt", cls: "c-ok" },
  in_progress: { text: "Đang gom", cls: "c-warn" },
  completed: { text: "Đã gom xong", cls: "c-ok" },
  cancelled: { text: "Đã hủy", cls: "c-bad" },
};

export type StatementState = "pending" | "confirmed" | "disputed" | "revised";

/** Trạng thái hiển thị: chờ phản hồi / đã xác nhận / đang tranh chấp / MKG đã sửa (cần phản hồi lại). */
export function statementState(s: HtxCollectionStatement): StatementState {
  if (!s.response) return "pending";
  if (s.response.revision !== s.revision) return "revised";
  return s.response.outcome;
}

export const STATEMENT_STATE: Record<StatementState, { text: string; cls: string }> = {
  pending: { text: "Chờ HTX xác nhận", cls: "c-warn" },
  confirmed: { text: "Đã xác nhận", cls: "c-ok" },
  disputed: { text: "Đang tranh chấp", cls: "c-bad" },
  revised: { text: "MKG đã sửa — cần xác nhận lại", cls: "c-warn" },
};

export interface ResponseDraft {
  outcome: "confirmed" | "disputed";
  htxRolls: string;
  reason: string;
}

const PII = /(?:\+?84|0)?\d(?:[\s.-]?\d){8,11}/;

/** null = hợp lệ; ngược lại là thông báo lỗi (tiếng Việt). */
export function validateResponse(s: HtxCollectionStatement, d: ResponseDraft): string | null {
  const reason = d.reason.trim();
  const superseding = !!s.response && s.response.revision === s.revision;
  if (reason && PII.test(reason)) return "Lý do không được chứa số điện thoại hoặc số CCCD.";
  if (reason && (reason.length < 10 || reason.length > 500)) return "Lý do cần từ 10 đến 500 ký tự.";
  if (d.outcome === "disputed") {
    if (!/^\d+$/.test(d.htxRolls.trim())) return "Tranh chấp cần nhập số cuộn HTX đếm được (≥ 0).";
    if (Number(d.htxRolls) === s.rolls) return "Số cuộn HTX bằng số MKG — hãy chọn Xác nhận.";
    if (!reason) return "Tranh chấp cần ghi lý do (10–500 ký tự).";
  }
  if (superseding && !reason) return "Đã có phản hồi cho bảng kê này — ghi lý do thay đổi (10–500 ký tự).";
  return null;
}

/** Thân POST /v1/cropcycle/htx-collection/statements/{ref}/responses. */
export function responseBody(s: HtxCollectionStatement, d: ResponseDraft) {
  const reason = d.reason.trim();
  return {
    revision: s.revision,
    outcome: d.outcome,
    htx_rolls: d.outcome === "disputed" ? Number(d.htxRolls) : null,
    ...(reason ? { reason } : {}),
  };
}

export function summarizeStatements(items: HtxCollectionStatement[]) {
  let pending = 0;
  let disputed = 0;
  let rolls = 0;
  for (const s of items) {
    const st = statementState(s);
    if (st === "pending" || st === "revised") pending++;
    if (st === "disputed") disputed++;
    rolls += s.rolls;
  }
  return { total: items.length, pending, disputed, rolls };
}
