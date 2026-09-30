import { describe, expect, it } from "vitest";
import type { HtxCollectionStatement } from "@/api/types";
import { responseBody, statementState, summarizeStatements, validateResponse } from "./collection";

function stmt(over: Partial<HtxCollectionStatement> = {}): HtxCollectionStatement {
  return {
    statement_ref: "s1",
    cooperative_ref: "c1",
    plot_ref: "p1",
    plot_code: "TH0000001",
    season_ref: null,
    collected_on: "2026-09-28",
    rolls: 40,
    revision: 1,
    pending: true,
    response: null,
    published_at: "2026-09-28T10:00:00Z",
    updated_at: "2026-09-28T10:00:00Z",
    ...over,
  };
}

const resp = (revision: number, outcome: "confirmed" | "disputed") => ({ id: "r1", revision, outcome, htx_rolls: 40, reason: null, responded_at: "2026-09-29T00:00:00Z" });

describe("RB-6 chiều mua — phản hồi bảng kê thu gom (G3-2)", () => {
  it("trạng thái: chờ, đã xác nhận, tranh chấp, MKG đã sửa", () => {
    expect(statementState(stmt())).toBe("pending");
    expect(statementState(stmt({ response: resp(1, "confirmed") }))).toBe("confirmed");
    expect(statementState(stmt({ response: resp(1, "disputed") }))).toBe("disputed");
    expect(statementState(stmt({ revision: 2, response: resp(1, "confirmed") }))).toBe("revised");
  });

  it("kiểm như máy chủ", () => {
    const s = stmt();
    expect(validateResponse(s, { outcome: "confirmed", htxRolls: "", reason: "" })).toBeNull();
    expect(validateResponse(s, { outcome: "disputed", htxRolls: "", reason: "Đếm lại thiếu 2 cuộn" })).toMatch(/số cuộn/);
    expect(validateResponse(s, { outcome: "disputed", htxRolls: "40", reason: "Đếm lại thiếu 2 cuộn" })).toMatch(/bằng số MKG/);
    expect(validateResponse(s, { outcome: "disputed", htxRolls: "38", reason: "" })).toMatch(/lý do/);
    expect(validateResponse(s, { outcome: "disputed", htxRolls: "38", reason: "gọi 0912345678" })).toMatch(/điện thoại/);
    expect(validateResponse(s, { outcome: "disputed", htxRolls: "38", reason: "Đếm lại thiếu 2 cuộn" })).toBeNull();
    const answered = stmt({ response: resp(1, "confirmed") });
    expect(validateResponse(answered, { outcome: "confirmed", htxRolls: "", reason: "" })).toMatch(/lý do thay đổi/);
  });

  it("thân POST gửi revision đang xem, htx_rolls chỉ khi tranh chấp", () => {
    expect(responseBody(stmt({ revision: 3 }), { outcome: "confirmed", htxRolls: "12", reason: "" })).toEqual({ revision: 3, outcome: "confirmed", htx_rolls: null });
    expect(responseBody(stmt(), { outcome: "disputed", htxRolls: "38", reason: " Thiếu 2 cuộn ở bờ " })).toEqual({
      revision: 1,
      outcome: "disputed",
      htx_rolls: 38,
      reason: "Thiếu 2 cuộn ở bờ",
    });
  });

  it("tóm tắt", () => {
    const sum = summarizeStatements([stmt(), stmt({ response: resp(1, "disputed") }), stmt({ revision: 2, response: resp(1, "confirmed") })]);
    expect(sum).toEqual({ total: 3, pending: 2, disputed: 1, rolls: 120 });
  });
});
