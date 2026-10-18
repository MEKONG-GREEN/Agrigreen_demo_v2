// ORGUI — logic hộp thoại "Tạo tổ chức": quy tắc khớp party/domain/org.go (AllowedParent, UnitLevel, codeRe).
import { describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import type { AdminUnit, Org } from "@/api/types";
import {
  allowedParent,
  buildOrgInput,
  canCreateOrg,
  codeError,
  EMPTY_FORM,
  fieldErrorsFromProblem,
  KIND_RULES,
  parentOptions,
  unitOptions,
  unitValidOn,
  validateForm,
  vnToday,
} from "./orgCreate";

const org = (ref: string, kind: Org["kind"], unit: string | null, parent: string | null = null, active = true): Org => ({
  ref,
  kind,
  code: ref.toUpperCase(),
  name: `Tổ chức ${ref}`,
  admin_unit_code: unit,
  parent_ref: parent,
  representative_ref: null,
  active,
});
const NAT = org("nat", "ttkn_national", null);
const P82 = org("p82", "ttkn_province", "82", "nat");
const P91 = org("p91", "ttkn_province", "91", "nat");
const POFF = org("poff", "ttkn_province", "96", "nat", false);
const TEAM = org("team", "extension_team", "82101", "p82");
const COOP = org("coop", "cooperative", "82101", "team");
const ORGS = [NAT, P82, P91, POFF, TEAM, COOP];

const unit = (code: string, level: AdminUnit["level"], parent: string | null, from = "2025-07-01", to: string | null = null): AdminUnit => ({
  code,
  name: `ĐV ${code}`,
  level,
  parent_code: parent,
  valid_from: from,
  valid_to: to,
  successor_codes: [],
});
const UNITS = [
  unit("82", "province", null),
  unit("91", "province", null),
  unit("82101", "commune", "82"),
  unit("82102", "commune", "82"),
  unit("82199", "commune", "82", "2020-01-01", "2025-07-01"), // đã sáp nhập
  unit("82200", "commune", "82", "2027-01-01"), // chưa hiệu lực
  unit("91101", "commune", "91"),
];
const DAY = "2026-10-14";

describe("quyền mở hộp thoại", () => {
  it("chỉ platform_admin / registry_admin; KN admin chưa được tạo", () => {
    expect(canCreateOrg(["platform_admin"])).toBe(true);
    expect(canCreateOrg(["registry_admin"])).toBe(true);
    expect(canCreateOrg(["kn_province_admin"])).toBe(false);
    expect(canCreateOrg(["kn_national_admin", "account_admin"])).toBe(false);
    expect(canCreateOrg(undefined)).toBe(false);
  });
});

describe("mã tổ chức (domain.codeRe)", () => {
  it.each([
    ["TTKN-82", null],
    ["a.b_c-1", null],
    ["", "Nhập mã tổ chức."],
    ["có dấu", "Mã 1–40 ký tự, chỉ gồm chữ không dấu, số và . _ -"],
    ["A".repeat(41), "Mã 1–40 ký tự, chỉ gồm chữ không dấu, số và . _ -"],
  ])("%s", (code, want) => expect(codeError(code)).toBe(want));
});

describe("cấp trên (domain.AllowedParent)", () => {
  it("lọc theo loại cấp trên hợp lệ và chỉ tổ chức đang hoạt động", () => {
    expect(parentOptions("extension_team", ORGS).map((o) => o.ref)).toEqual(["p82", "p91"]);
    expect(parentOptions("ttkn_province", ORGS).map((o) => o.ref)).toEqual(["nat"]);
    expect(parentOptions("dcrd", ORGS)).toEqual([]);
  });
  it("gốc / sai loại / ngừng hoạt động", () => {
    expect(allowedParent("ttkn_national", null)).toBe(true);
    expect(allowedParent("ttkn_national", P82)).toBe(false);
    expect(allowedParent("ttkn_province", null)).toBe(true);
    expect(allowedParent("extension_team", null)).toBe(false);
    expect(allowedParent("extension_team", NAT)).toBe(false);
    expect(allowedParent("extension_team", POFF)).toBe(false);
    expect(allowedParent("extension_team", P82)).toBe(true);
    expect(allowedParent("partner", P82)).toBe(false);
  });
});

describe("ĐVHC theo ngày hiệu lực", () => {
  it("valid_to không bao gồm, valid_from bao gồm", () => {
    expect(unitValidOn(unit("x", "commune", "82", "2026-10-14"), DAY)).toBe(true);
    expect(unitValidOn(unit("x", "commune", "82", "2020-01-01", "2026-10-14"), DAY)).toBe(false);
    expect(unitValidOn(unit("x", "commune", "82", "2026-10-15"), DAY)).toBe(false);
  });
  it("xã: đúng cấp, còn hiệu lực, thuộc tỉnh của cấp trên", () => {
    expect(unitOptions("extension_team", UNITS, P82, DAY).map((u) => u.code)).toEqual(["82101", "82102"]);
    expect(unitOptions("extension_team", UNITS, P91, DAY).map((u) => u.code)).toEqual(["91101"]);
  });
  it("tỉnh: chỉ cấp tỉnh; loại không gắn ĐVHC → rỗng", () => {
    expect(unitOptions("ttkn_province", UNITS, NAT, DAY).map((u) => u.code)).toEqual(["82", "91"]);
    expect(unitOptions("dcrd", UNITS, null, DAY)).toEqual([]);
  });
  it("ngày theo giờ Việt Nam (UTC+7)", () => {
    expect(vnToday(new Date("2026-10-13T17:30:00Z"))).toBe("2026-10-14");
    expect(vnToday(new Date("2026-10-13T16:59:00Z"))).toBe("2026-10-13");
  });
});

describe("kiểm form và thân gửi", () => {
  it("trường bắt buộc theo loại", () => {
    expect(validateForm({ ...EMPTY_FORM, kind: "extension_team" }, ORGS)).toEqual({
      code: "Nhập mã tổ chức.",
      name: "Nhập tên tổ chức.",
      parent_ref: "Chọn tổ chức cấp trên.",
      admin_unit_code: "Chọn đơn vị hành chính.",
    });
    expect(validateForm({ ...EMPTY_FORM, kind: "dcrd", code: "CUC2", name: "Cục 2" }, ORGS)).toEqual({});
  });
  it("cấp trên sai loại bị chặn trước khi gửi", () => {
    const f = {
      ...EMPTY_FORM,
      kind: "extension_team" as const,
      code: "T1",
      name: "Tổ",
      parent_ref: "nat",
      admin_unit_code: "82101",
    };
    expect(validateForm(f, ORGS).parent_ref).toBe("Tổ chức cấp trên không hợp lệ cho loại này.");
  });
  it("HTX không gửi từ đây (chuyển Neo HTX)", () => {
    expect(KIND_RULES.cooperative.redirect).toBe("/quan-tri/neo-htx");
    expect(validateForm({ ...EMPTY_FORM, kind: "cooperative" }, ORGS)).toEqual({
      kind: "HTX tạo qua màn Neo HTX theo mã số thuế.",
    });
  });
  it("thân chỉ gồm trường của loại; cắt khoảng trắng", () => {
    expect(
      buildOrgInput({
        kind: "ttkn_province",
        code: " TTKN-99 ",
        name: " TTKN 99 ",
        admin_unit_code: "82",
        parent_ref: "nat",
      }),
    ).toEqual({
      kind: "ttkn_province",
      code: "TTKN-99",
      name: "TTKN 99",
      admin_unit_code: "82",
      parent_ref: "nat",
    });
    expect(
      buildOrgInput({
        kind: "dcrd",
        code: "C",
        name: "Cục",
        admin_unit_code: "82",
        parent_ref: "",
      }),
    ).toEqual({ kind: "dcrd", code: "C", name: "Cục" });
  });
});

describe("lỗi server → trường", () => {
  const p = (status: number, code: string, extra: Record<string, unknown> = {}) =>
    new ApiProblem({
      type: "about:blank",
      title: code,
      status,
      code,
      ...extra,
    });
  it("409 mã trùng → ô Mã", () => {
    expect(fieldErrorsFromProblem(p(409, "party.org_code_taken"))?.code).toMatch(/đã được dùng/);
  });
  it("422 cấp bậc → ô Cấp trên, kèm detail của server", () => {
    expect(
      fieldErrorsFromProblem(
        p(422, "party.invalid_hierarchy", {
          detail: "đơn vị hành chính không thuộc địa bàn của tổ chức cấp trên",
        }),
      ),
    ).toEqual({
      parent_ref: "đơn vị hành chính không thuộc địa bàn của tổ chức cấp trên",
    });
  });
  it("422 validation_failed → theo errors[].field", () => {
    const e = p(422, "validation_failed", {
      errors: [
        {
          field: "admin_unit_code",
          reason: "không có đơn vị hành chính còn hiệu lực với mã này",
        },
      ],
    });
    expect(fieldErrorsFromProblem(e)).toEqual({
      admin_unit_code: "không có đơn vị hành chính còn hiệu lực với mã này",
    });
  });
  it("lỗi khác → null (khung lỗi chung)", () => {
    expect(fieldErrorsFromProblem(p(403, "party.out_of_scope"))).toBeNull();
    expect(
      fieldErrorsFromProblem(
        p(422, "validation_failed", {
          errors: [{ field: "cooperative", reason: "x" }],
        }),
      ),
    ).toBeNull();
  });
});
