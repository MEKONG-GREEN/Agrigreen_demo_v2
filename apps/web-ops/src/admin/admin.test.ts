import { describe, expect, it } from "vitest";
import { collectPages } from "@/api/pagination";
import type { Org, ParamVersion } from "@/api/types";
import { buildPrincipal } from "@/auth/principal";
import { approvalBlockReason, proposeBlockReason, submitBlockReason } from "./approval";
import { precheckCsv } from "./households";
import { buildOrgTree } from "./orgTree";
import { actorLabel, groupParamVersions } from "./params";

const approver = (sub: string, mfa = true) =>
  buildPrincipal({ sub, amr: mfa ? ["pwd", "otp"] : ["pwd"], realm_access: { roles: ["param_approver", "param_setter"] } }, null);

describe("R-56: người phê duyệt ≠ người đặt", () => {
  it("chặn tự phê duyệt", () => {
    expect(approvalBlockReason({ setter_ref: "u1", status: "submitted" }, approver("u1"))).toMatch(/khác người đề xuất/);
  });
  it("cho phép người khác duyệt", () => {
    expect(approvalBlockReason({ setter_ref: "u1", status: "submitted" }, approver("u2"))).toBeNull();
  });
  it("cần vai param_approver và MFA", () => {
    const setterOnly = buildPrincipal({ sub: "u2", amr: ["otp"], realm_access: { roles: ["param_setter"] } }, null);
    expect(approvalBlockReason({ setter_ref: "u1", status: "submitted" }, setterOnly)).toMatch(/param_approver/);
    expect(approvalBlockReason({ setter_ref: "u1", status: "submitted" }, approver("u2", false))).toMatch(/hai yếu tố/);
    expect(proposeBlockReason(approver("u2", false))).toMatch(/hai yếu tố/);
    expect(proposeBlockReason(approver("u2"))).toBeNull();
  });
  it("nháp chưa gửi và bản đã quyết định không duyệt được", () => {
    expect(approvalBlockReason({ setter_ref: "u1", status: "draft" }, approver("u2"))).toMatch(/nháp/);
    expect(approvalBlockReason({ setter_ref: "u1", status: "approved" }, approver("u2"))).toMatch(/đã được xử lý/);
  });
  it("chỉ người đặt gửi duyệt bản nháp", () => {
    expect(submitBlockReason({ setter_ref: "u1", status: "draft" }, approver("u1"))).toBeNull();
    expect(submitBlockReason({ setter_ref: "u1", status: "draft" }, approver("u2"))).toMatch(/người đặt/);
  });
});

describe("groupParamVersions — gom phiên bản theo khóa", () => {
  const v = (p: Partial<ParamVersion> & Pick<ParamVersion, "id" | "version" | "status" | "effective_from">): ParamVersion => ({
    key: "cgh.norm_ha_per_machine:MGD:thu_hoach",
    value: "90",
    text: null,
    unit: "ha/máy/vụ",
    source_doc: "QĐ 1",
    setter_ref: "u1",
    created_at: "2026-01-01T00:00:00Z",
    ...p,
  });
  const now = Date.parse("2026-09-27T00:00:00Z");

  it("hiện hành = bản đã duyệt mới nhất có hiệu lực tại now; bản tương lai tách riêng", () => {
    const [g] = groupParamVersions(
      [
        v({ id: "a", version: 1, status: "approved", effective_from: "2026-01-01T00:00:00+07:00" }),
        v({ id: "b", version: 2, status: "approved", effective_from: "2026-04-01T00:00:00+07:00", value: "85.5000" }),
        v({ id: "c", version: 3, status: "approved", effective_from: "2026-11-01T00:00:00+07:00" }),
        v({ id: "d", version: 4, status: "submitted", effective_from: "2027-01-01T00:00:00+07:00" }),
        v({ id: "e", version: 5, status: "draft", effective_from: "2027-01-01T00:00:00+07:00" }),
        v({ id: "f", version: 6, status: "rejected", effective_from: "2026-05-01T00:00:00+07:00" }),
      ],
      [{ key: "cgh.norm_ha_per_machine:MGD:thu_hoach", name: "Định mức máy gặt — thu hoạch", unit: "ha/máy/vụ", kind: "decimal", min: "1", max: "5000", current: null, submitted_count: 1 }],
      now,
    );
    expect(g?.current?.id).toBe("b");
    // Giá trị giữ nguyên chuỗi thập phân (R-10).
    expect(g?.current?.value).toBe("85.5000");
    expect(g?.upcoming.map((x) => x.id)).toEqual(["c"]);
    expect(g?.submitted.map((x) => x.id)).toEqual(["d"]);
    expect(g?.drafts.map((x) => x.id)).toEqual(["e"]);
    // Tên và khoảng hợp lệ từ sổ đăng ký tham số (G-07), không viết cứng ở web.
    expect(g?.label).toBe("Định mức máy gặt — thu hoạch");
    expect(g?.max).toBe("5000");
  });

  it("cùng ngày hiệu lực → bản ghi bù (số phiên bản lớn hơn) thắng", () => {
    const [g] = groupParamVersions(
      [
        v({ id: "a", version: 1, status: "approved", effective_from: "2026-01-01T00:00:00+07:00" }),
        v({ id: "a2", version: 2, status: "approved", effective_from: "2026-01-01T00:00:00+07:00", correction_reason: "Sửa lỗi đánh máy" }),
      ],
      [],
      now,
    );
    expect(g?.current?.id).toBe("a2");
  });

  it("khóa không có trong sổ → hiện chính khóa; khóa trong sổ chưa có phiên bản vẫn hiện", () => {
    const list = groupParamVersions(
      [v({ id: "x", key: "foo.bar", version: 1, status: "draft", effective_from: "2027-01-01T00:00:00+07:00" })],
      [{ key: "sync.conflict_sla_hours", name: "SLA xử lý xung đột", unit: "giờ", kind: "decimal", min: "1", max: "720", current: null, submitted_count: 0 }],
      now,
    );
    expect(list.map((p) => [p.key, p.label])).toEqual([
      ["foo.bar", "foo.bar"],
      ["sync.conflict_sla_hours", "SLA xử lý xung đột"],
    ]);
  });

  it("tên người đặt/duyệt (G-07) — thiếu tên thì hiện mã", () => {
    expect(actorLabel("Quản trị viên A", "u1")).toBe("Quản trị viên A");
    expect(actorLabel(null, "u1")).toBe("u1");
  });
});

describe("precheckCsv — nhập nông hộ", () => {
  it("nhận tiêu đề tiếng Việt hoặc tiếng Anh", () => {
    expect(precheckCsv("ho_ten,so_dien_thoai,ma_htx\nA,0900000000,HTX1\n").missingColumns).toEqual([]);
    expect(precheckCsv("﻿full_name,phone,cooperative_code\r\nA,0900000000,HTX1").rows).toBe(1);
  });
  it("báo thiếu cột bắt buộc và quá 200 dòng", () => {
    expect(precheckCsv("full_name,phone\nA,1").missingColumns).toEqual(["cooperative_code | ma_htx"]);
    const many = ["full_name,phone,cooperative_code", ...Array.from({ length: 201 }, () => "A,0900000000,H")].join("\n");
    expect(precheckCsv(many).tooMany).toBe(true);
  });
});

describe("buildOrgTree", () => {
  const o = (ref: string, parent: string | null, name: string): Org => ({
    ref, parent_ref: parent, name, kind: "ttkn_province", code: ref, admin_unit_code: null, representative_ref: null, active: true,
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
