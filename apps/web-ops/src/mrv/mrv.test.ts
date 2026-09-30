import { describe, expect, it } from "vitest";
import type { MrvComparison } from "@/api/types";
import { completenessSummary, estimateLabel, latestComparison, methodDecisionBlock, PROVISIONAL_LABEL, tallyLines, validateComparison, validateMethodDraft } from "./rules";

const cmp = (id: string, recorded_at: string, supersedes_id: string | null = null): MrvComparison => ({
  id, evidence_id: "e", log_ref: "l", log_id: "l1", season_ref: "s", result: "match", note: null, file_refs: [], compared_by: "u", event_time: recorded_at, recorded_at, supersedes_id, correction_reason: supersedes_id ? "sửa lại cho đúng" : null,
});

describe("MRV — quy tắc màn hình (pf/mrv G3)", () => {
  it("ước tính chỉ chính thức khi máy chủ báo cả status=official và official=true; ngược lại nhãn TẠM", () => {
    expect(estimateLabel({ status: "provisional", official: false, label: "Tạm — chờ khách ký phương pháp" })).toEqual({ official: false, text: PROVISIONAL_LABEL });
    expect(estimateLabel({ status: "official", official: false, label: "x" }).official).toBe(false);
    expect(estimateLabel({ status: "official", official: true, label: "Chính thức (PP v2)" })).toEqual({ official: true, text: "Chính thức (PP v2)" });
    expect(PROVISIONAL_LABEL).toBe("TẠM — chờ khách ký phương pháp");
  });

  it("KN US-MRV-02: Khác biệt bắt buộc ghi chú; sửa kết quả cần lý do ≥ 10 ký tự", () => {
    const base = { result: "mismatch" as const, note: "", evidenceId: "e", supersedesId: null, correctionReason: "" };
    expect(validateComparison(base).map((e) => e.field)).toEqual(["note"]);
    expect(validateComparison({ ...base, note: "Ngày sạ lệch 5 ngày" })).toEqual([]);
    expect(validateComparison({ ...base, result: "match" })).toEqual([]);
    expect(validateComparison({ ...base, result: "match", supersedesId: "c1", correctionReason: "ngắn" }).map((e) => e.field)).toEqual(["correction_reason"]);
    expect(validateComparison({ ...base, result: "", evidenceId: "" }).map((e) => e.field)).toEqual(["evidence_id", "result"]);
  });

  it("kết quả mới nhất là đầu chuỗi bản ghi bù", () => {
    const a = cmp("a", "2026-09-01T00:00:00Z");
    const b = cmp("b", "2026-09-02T00:00:00Z", "a");
    expect(latestComparison({ comparisons: [a, b] })!.id).toBe("b");
    expect(latestComparison({ comparisons: [] })).toBeNull();
    expect(tallyLines([{ result: "match", confirmed: true }, { result: "mismatch", confirmed: false }, { result: "unverified", confirmed: false }])).toEqual({ match: 1, mismatch: 1, unverified: 1, confirmed: 1, total: 3 });
  });

  it("KN US-MRV-05: đủ hồ sơ chỉ khi không thiếu mục và có minh chứng đạt chuẩn", () => {
    const items = [
      { key: "straw_handling", source: "kn_evidence" as const, catalog_version: 2, status: "approved" as const, count: 1, approved_count: 1 },
      { key: "collection_evidence", source: "collection_evidence" as const, catalog_version: null, status: "missing" as const, count: 0, approved_count: 0 },
    ];
    expect(completenessSummary({ items, counts_toward_estimate: true })).toEqual({ done: 1, total: 2, missing: ["collection_evidence"], complete: false });
    expect(completenessSummary({ items: items.slice(0, 1), counts_toward_estimate: true }).complete).toBe(true);
  });

  it("R-56: không tự duyệt phương pháp; bản đã quyết định bất biến", () => {
    expect(methodDecisionBlock({ status: "submitted", setter_ref: "a" }, "a", true)).toMatch(/khác người đề xuất/);
    expect(methodDecisionBlock({ status: "submitted", setter_ref: "a" }, "b", true)).toBeNull();
    expect(methodDecisionBlock({ status: "submitted", setter_ref: "a" }, "b", false)).toMatch(/phê duyệt/);
    expect(methodDecisionBlock({ status: "approved", setter_ref: "a" }, "b", true)).toMatch(/bất biến/);
  });

  it("đề xuất phương pháp: căn cứ ≥ 5 ký tự, 1–20 tham số hợp lệ, không trùng", () => {
    expect(validateMethodDraft("mrv.co2.estimate.v1", "QĐ 01/2027", [{ key: "mrv.co2.open_burning_kg_per_t", version: "1" }])).toEqual([]);
    const errs = validateMethodDraft("", "ab", [
      { key: "mrv.co2.a", version: "0" },
      { key: "mrv.co2.a", version: "1" },
      { key: "Sai Khoa", version: "1" },
    ]).map((e) => e.field);
    expect(errs).toEqual(["rule_version", "source_note", "params[0].version", "params[1].key", "params[2].key"]);
    expect(validateMethodDraft("r", "hợp lệ", []).map((e) => e.field)).toEqual(["params"]);
  });
});
