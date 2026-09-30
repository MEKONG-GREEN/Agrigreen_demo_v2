// Quy tắc hiển thị/kiểm trước khi gửi của màn hình MRV (pf/mrv G3) — hàm thuần, có Vitest.
// Máy chủ vẫn là nơi quyết định (R-55); ở đây chỉ báo sớm cho người dùng.
import type { MrvComparisonLine, MrvCompleteness, MrvCompletenessItem, MrvEstimate, MrvMethodVersion } from "@/api/types";

/** Nhãn bắt buộc khi ước tính chưa chính thức (chưa có phương pháp đã ký/duyệt — C-1). */
export const PROVISIONAL_LABEL = "TẠM — chờ khách ký phương pháp";

export function estimateLabel(e: Pick<MrvEstimate, "status" | "official" | "label">): { official: boolean; text: string } {
  // Chỉ coi là chính thức khi máy chủ báo CẢ status = official VÀ official = true.
  const official = e.status === "official" && e.official === true;
  return official ? { official, text: e.label || "Chính thức" } : { official, text: PROVISIONAL_LABEL };
}

export const RESULT_LABEL: Record<"match" | "mismatch" | "unverified", string> = {
  match: "Khớp",
  mismatch: "Khác biệt",
  unverified: "Chưa kiểm chứng",
};

export const RESULT_CHIP: Record<"match" | "mismatch" | "unverified", string> = {
  match: "c-ok",
  mismatch: "c-bad",
  unverified: "c-warn",
};

export interface ComparisonDraft {
  result: "match" | "mismatch" | "";
  note: string;
  evidenceId: string;
  /** Đã có kết quả xác nhận → sửa bằng bản ghi bù. */
  supersedesId: string | null;
  correctionReason: string;
}

/** KN US-MRV-02: mismatch bắt buộc ghi chú; sửa kết quả cần lý do sửa ≥ 10 ký tự. */
export function validateComparison(d: ComparisonDraft): { field: string; reason: string }[] {
  const errors: { field: string; reason: string }[] = [];
  if (!d.evidenceId) errors.push({ field: "evidence_id", reason: "Chọn minh chứng của cán bộ để đối chiếu." });
  if (d.result !== "match" && d.result !== "mismatch") errors.push({ field: "result", reason: "Chọn Khớp hoặc Khác biệt." });
  if (d.result === "mismatch" && !d.note.trim()) errors.push({ field: "note", reason: "Khác biệt bắt buộc ghi chú nội dung sai khác." });
  if (d.note.length > 1000) errors.push({ field: "note", reason: "Ghi chú tối đa 1.000 ký tự." });
  if (d.supersedesId && d.correctionReason.trim().length < 10) errors.push({ field: "correction_reason", reason: "Lý do sửa tối thiểu 10 ký tự." });
  return errors;
}

/** Kết quả cán bộ đã xác nhận mới nhất của dòng (bản hiện hành của chuỗi bản ghi bù). */
export function latestComparison(line: Pick<MrvComparisonLine, "comparisons">): MrvComparisonLine["comparisons"][number] | null {
  const superseded = new Set(line.comparisons.map((c) => c.supersedes_id).filter((x): x is string => !!x));
  const heads = line.comparisons.filter((c) => !superseded.has(c.id));
  return heads.sort((a, b) => b.recorded_at.localeCompare(a.recorded_at))[0] ?? null;
}

export interface ComparisonTally {
  match: number;
  mismatch: number;
  unverified: number;
  confirmed: number;
  total: number;
}

export function tallyLines(lines: readonly Pick<MrvComparisonLine, "result" | "confirmed">[]): ComparisonTally {
  const t: ComparisonTally = { match: 0, mismatch: 0, unverified: 0, confirmed: 0, total: lines.length };
  for (const l of lines) {
    t[l.result]++;
    if (l.confirmed) t.confirmed++;
  }
  return t;
}

export const ITEM_STATUS_LABEL: Record<MrvCompletenessItem["status"], string> = {
  missing: "Thiếu",
  present: "Có, chờ duyệt",
  approved: "Đã duyệt",
  rejected: "Bị từ chối",
};

export const ITEM_STATUS_CHIP: Record<MrvCompletenessItem["status"], string> = {
  missing: "c-bad",
  present: "c-warn",
  approved: "c-ok",
  rejected: "c-bad",
};

export const SOURCE_LABEL: Record<MrvCompletenessItem["source"], string> = {
  kn_evidence: "Minh chứng cán bộ KN",
  htx_farm_log: "Nhật ký HTX",
  collection_evidence: "Bằng chứng thu gom",
};

/** Tóm tắt đủ hồ sơ của một thửa/vụ: số mục có/thiếu, và có đủ để tính vào ước tính không. */
export function completenessSummary(c: Pick<MrvCompleteness, "items" | "counts_toward_estimate">): { done: number; total: number; missing: string[]; complete: boolean } {
  const missing = c.items.filter((i) => i.status === "missing" || i.status === "rejected").map((i) => i.key);
  const total = c.items.length;
  return { done: total - missing.length, total, missing, complete: missing.length === 0 && c.counts_toward_estimate };
}

/** R-56: người duyệt phương pháp phải khác người đề xuất; chỉ duyệt/từ chối bản đang chờ duyệt. */
export function methodDecisionBlock(m: Pick<MrvMethodVersion, "status" | "setter_ref">, actorRef: string | null | undefined, canApprove: boolean): string | null {
  if (m.status !== "submitted") return "Phiên bản đã được quyết định (bất biến).";
  if (!canApprove) return "Cần vai Người phê duyệt tham số.";
  if (actorRef && m.setter_ref === actorRef) return "Người phê duyệt phải khác người đề xuất (R-56).";
  return null;
}

const PARAM_KEY = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$/;

export interface PinDraft {
  key: string;
  version: string;
}

/** Kiểm đề xuất phương pháp: rule_version, văn bản căn cứ ≥ 5 ký tự, 1–20 tham số ghim (key + phiên bản ≥ 1, không trùng). */
export function validateMethodDraft(ruleVersion: string, sourceNote: string, pins: readonly PinDraft[]): { field: string; reason: string }[] {
  const errors: { field: string; reason: string }[] = [];
  if (!ruleVersion.trim()) errors.push({ field: "rule_version", reason: "Nhập mã quy tắc (vd mrv.co2.estimate.v1)." });
  const note = sourceNote.trim();
  if (note.length < 5 || note.length > 1000) errors.push({ field: "source_note", reason: "Văn bản phương pháp đã ký: 5–1.000 ký tự." });
  if (pins.length < 1 || pins.length > 20) errors.push({ field: "params", reason: "Ghim từ 1 đến 20 tham số." });
  const seen = new Set<string>();
  pins.forEach((p, i) => {
    const v = Number(p.version);
    if (!PARAM_KEY.test(p.key.trim())) errors.push({ field: `params[${i}].key`, reason: "Khóa tham số không đúng mẫu (vd mrv.co2.open_burning_kg_per_t)." });
    if (!Number.isInteger(v) || v < 1) errors.push({ field: `params[${i}].version`, reason: "Phiên bản tham số là số nguyên ≥ 1." });
    if (seen.has(p.key.trim())) errors.push({ field: `params[${i}].key`, reason: "Tham số bị ghim trùng." });
    seen.add(p.key.trim());
  });
  return errors;
}
