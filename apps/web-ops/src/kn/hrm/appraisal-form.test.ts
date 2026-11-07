import { describe, expect, it } from "vitest";
import { appraisalPointGuard, appraisalSuggestion, appraisalYear, validateAppraisalProposal, validateAppraisalAnswers, type AppraisalForm } from "./appraisal-form";

const form: AppraisalForm = { code: "phieu_xa", name: "Phiếu xã", group: "can_bo_xa",
  criteria: Array.from({ length: 6 }, (_, i) => ({ code: `tc_${i}`, title: `Tiêu chí ${i}`, maximum: "15", guide: "", group: "" })),
  bands: [{ minimum: "81", rating: "HTXSNV" }, { minimum: "63", rating: "HTTNV" }, { minimum: "45", rating: "HTNV" }, { minimum: "0", rating: "KHTNV" }] };
const answers = form.criteria.map((x, i) => ({ code: x.code, points: i === 5 ? "7" : "15", note: "" }));

describe("W11.26: số được kiểm trước phép tính", () => {
  it.each(["1e100000", "1e-100000", "9".repeat(100000), "-1", "15.01", "1.001", "NaN", "Infinity"])("từ chối đầu vào đối kháng %#", (raw) => {
    expect(appraisalPointGuard(raw, "15")).not.toBeNull();
    expect(validateAppraisalAnswers(form, [{ ...answers[0]!, points: raw }, ...answers.slice(1)], true).valid).toBe(false);
  });
  it("đủ 82/90 chỉ gợi ý HTXSNV; nháp thiếu không sinh gợi ý", () => {
    expect(appraisalSuggestion(form, answers)).toEqual({ total: "82", rating: "HTXSNV" });
    expect(appraisalSuggestion(form, [{ ...answers[0]!, points: "" }, ...answers.slice(1)])).toBeNull();
    expect(validateAppraisalAnswers(form, [], false).valid).toBe(true);
    expect(appraisalPointGuard("15", "15")).toBeNull();
    expect(appraisalPointGuard("14,99", "15")).toBeNull();
    expect(validateAppraisalAnswers(form, [{ ...answers[0]!, points: "14,99" }, ...answers.slice(1)], true).answers[0]?.points).toBe("14.99");
  });
  it("năm chỉ chuyển Number sau kiểm cú pháp/biên", () => {
    for (const raw of ["1e100000", "1e-100000", "9".repeat(100000), "2019", "2101", "2026.0"]) expect(appraisalYear(raw)).toBeNull();
    expect(appraisalYear("2020")).toBe(2020); expect(appraisalYear("2100")).toBe(2100);
  });
});

it("HC2 DG04–06: căn cứ bắt buộc ở một trong hai mức đặc biệt, tối đa 2.000 ký tự",()=>{
 for(const self of ["HTXSNV","HTTNV","HTNV","KHTNV"] as const)for(const collective of ["HTXSNV","HTTNV","HTNV","KHTNV"] as const){const need=[self,collective].some(r=>r==="HTXSNV"||r==="KHTNV");expect(!!validateAppraisalProposal(self,collective,"").reason).toBe(need);expect(validateAppraisalProposal(self,collective,"x")).toEqual({});}
 expect(validateAppraisalProposal("","HTNV","")).toHaveProperty("self_rating");expect(validateAppraisalProposal("HTNV","HTNV","á".repeat(2000))).toEqual({});expect(validateAppraisalProposal("HTNV","HTNV","á".repeat(2001))).toHaveProperty("reason");
});
