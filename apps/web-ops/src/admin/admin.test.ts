import { describe, expect, it } from "vitest";
import { collectPages } from "@/api/pagination";
import type { Org } from "@/api/pending/party";
import { buildPrincipal } from "@/auth/principal";
import { approvalBlockReason, proposeBlockReason } from "./approval";
import { buildOrgTree } from "./orgTree";

const approver = (sub: string, mfa = true) =>
  buildPrincipal({ sub, amr: mfa ? ["pwd", "otp"] : ["pwd"], realm_access: { roles: ["param_approver", "param_setter"] } }, null);

describe("R-56: người phê duyệt ≠ người đặt", () => {
  it("chặn tự phê duyệt", () => {
    expect(approvalBlockReason({ proposed_by: "u1", status: "pending" }, approver("u1"))).toMatch(/khác người đề xuất/);
  });
  it("cho phép người khác duyệt", () => {
    expect(approvalBlockReason({ proposed_by: "u1", status: "pending" }, approver("u2"))).toBeNull();
  });
  it("cần vai param_approver và MFA", () => {
    const setterOnly = buildPrincipal({ sub: "u2", amr: ["otp"], realm_access: { roles: ["param_setter"] } }, null);
    expect(approvalBlockReason({ proposed_by: "u1", status: "pending" }, setterOnly)).toMatch(/param_approver/);
    expect(approvalBlockReason({ proposed_by: "u1", status: "pending" }, approver("u2", false))).toMatch(/hai yếu tố/);
    expect(proposeBlockReason(approver("u2", false))).toMatch(/hai yếu tố/);
    expect(proposeBlockReason(approver("u2"))).toBeNull();
  });
  it("đề xuất đã xử lý không duyệt lại", () => {
    expect(approvalBlockReason({ proposed_by: "u1", status: "approved" }, approver("u2"))).toMatch(/đã được xử lý/);
  });
});

describe("buildOrgTree", () => {
  const o = (ref: string, parent: string | null, name: string): Org => ({
    ref, parent_ref: parent, name, kind: "ttkn_province", code: ref, admin_unit_code: null, active: true,
  });
  it("dựng cây TW → Tỉnh → Xã, nút mồ côi thành gốc", () => {
    const tree = buildOrgTree([o("x1", "p1", "Xã A"), o("p1", "n", "Tỉnh B"), o("n", null, "TW"), o("orphan", "missing", "Lẻ")]);
    expect(tree.map((t) => t.org.ref).sort()).toEqual(["n", "orphan"]);
    const tw = tree.find((t) => t.org.ref === "n");
    expect(tw?.children[0]?.org.ref).toBe("p1");
    expect(tw?.children[0]?.children[0]?.org.ref).toBe("x1");
  });
});

describe("collectPages — cursor (R-53)", () => {
  it("gom tới hết next_cursor", async () => {
    const pages: Record<string, { items: number[]; next_cursor: string | null }> = {
      start: { items: [1, 2], next_cursor: "c2" },
      c2: { items: [3], next_cursor: "c3" },
      c3: { items: [4], next_cursor: null },
    };
    const calls: (string | undefined)[] = [];
    const all = await collectPages(async (c) => {
      calls.push(c);
      return pages[c ?? "start"]!;
    });
    expect(all).toEqual([1, 2, 3, 4]);
    expect(calls).toEqual([undefined, "c2", "c3"]);
  });
  it("dừng khi server lặp cursor và khi đạt max", async () => {
    const loop = await collectPages(async () => ({ items: [1], next_cursor: "same" }));
    expect(loop.length).toBeLessThanOrEqual(2);
    const capped = await collectPages(async () => ({ items: [1, 2, 3], next_cursor: String(Math.random()) }), 5);
    expect(capped).toHaveLength(5);
  });
});
