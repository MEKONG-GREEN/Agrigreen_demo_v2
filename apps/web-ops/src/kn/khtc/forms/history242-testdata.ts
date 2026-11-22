import type { SubmissionHistory, SubmissionSnapshot } from "./submission-history";

export const PERIOD242 = "11111111-1111-4111-8111-111111111111";
export const FIRST242 = "22222222-2222-4222-8222-222222222222";
export const SECOND242 = "33333333-3333-4333-8333-333333333333";
export const ACTOR242 = "44444444-4444-4444-8444-444444444444";
export const PLAN242 = "55555555-5555-4555-8555-555555555555";
export const ITEM242 = "66666666-6666-4666-8666-666666666666";
export const DEPT242 = "trong_trot";
export function snapshot242(id = FIRST242, value = "10"): SubmissionSnapshot {
  return {
    period: { id: PERIOD242, name: "Kỳ kiểm thử", report_type: "week", year: 2030 },
    department: DEPT242, department_name: "Phòng Trồng trọt",
    submission: { id, attempt: id === FIRST242 ? 1 : 2, submitted_at: "2030-06-01T08:00:00Z", submitter_ref: ACTOR242,
      native_late: false, decision: "returned", review_note: "Bổ sung diện tích", reviewer_ref: PLAN242, reviewed_at: "2030-06-02T08:00:00Z" },
    sheet_id: id, content_hash: (id === FIRST242 ? "a" : "b").repeat(64), plan_version_ref: PLAN242,
    plan: { version_id: PLAN242, version: 1, decision_no: "242/QĐ" },
    template_code: "KH-TT", template_version: 1,
    content: { cells: { [ITEM242]: { done: value, people: "30" } }, texts: { summary: "Lời văn đã gửi" }, adjustments: [] },
    rows: [{ entry_ref: ITEM242, name: "Diện tích mô hình", unit: "ha" }], sections: [{ key: "summary", title: "Kết quả", required: true }],
  };
}
export function history242(): SubmissionHistory {
  const first = snapshot242(), second = snapshot242(SECOND242, "12");
  return { period: first.period, department: DEPT242, department_name: first.department_name, items: [second.submission, first.submission] };
}
