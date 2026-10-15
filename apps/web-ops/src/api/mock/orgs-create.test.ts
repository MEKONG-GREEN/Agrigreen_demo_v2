// ORGUI — giả lập Admin POST /v1/orgs: 201 / 409 / 422 / 403 và phát lại Idempotency-Key.
import { afterEach, describe, expect, it } from "vitest";
import { NATIONAL_REF, ORGS, provinceOrgRef } from "./data";
import { mockAdminFetchNoDelay } from "./handler";
import { orgCreateRoutes } from "./orgs-create";
import { findPersona } from "./personas";

const BASE = ORGS.length;
afterEach(() => {
  ORGS.splice(BASE);
});

interface Body {
  org: { ref: string };
  seq: number;
  representative_duplicate: boolean;
  code: string;
  detail?: string;
  errors: { field: string; reason: string }[];
}

let n = 0;
async function create(body: Record<string, unknown>, persona = "setter", key = `orgui-test-${++n}-${Date.now()}`) {
  const res = await mockAdminFetchNoDelay(
    new Request("http://mock/v1/orgs", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${persona}`,
        "Content-Type": "application/json",
        "Idempotency-Key": key,
      },
      body: JSON.stringify(body),
    }),
  );
  return {
    status: res.status,
    body: (await res.json()) as Body,
    replayed: res.headers.get("Idempotent-Replayed"),
    location: res.headers.get("Location"),
  };
}

const TEAM = {
  kind: "extension_team",
  code: "TKN-ORGUI-1",
  name: "Tổ KN thử",
  admin_unit_code: "82101",
  parent_ref: provinceOrgRef(0),
};

describe("POST /v1/orgs (giả lập)", () => {
  it("201: tạo tổ KN xã dưới TTKN tỉnh, có trong GET /v1/orgs", async () => {
    const r = await create(TEAM);
    expect(r.status).toBe(201);
    expect(r.body.org).toMatchObject({
      kind: "extension_team",
      code: "TKN-ORGUI-1",
      admin_unit_code: "82101",
      parent_ref: provinceOrgRef(0),
      active: true,
    });
    expect(r.body.seq).toBe(1);
    expect(r.body.representative_duplicate).toBe(false);
    expect(r.location).toBe(`/v1/orgs/${r.body.org.ref}`);
    expect(ORGS.some((o) => o.ref === r.body.org.ref)).toBe(true);
  });

  it("phát lại cùng khóa trả cùng tổ chức, không tạo bản thứ hai", async () => {
    const a = await create(TEAM, "setter", "orgui-replay-key-1");
    const b = await create(TEAM, "setter", "orgui-replay-key-1");
    expect(b.status).toBe(201);
    expect(b.replayed).toBe("true");
    expect(b.body.org.ref).toBe(a.body.org.ref);
    expect(ORGS.filter((o) => o.code === "TKN-ORGUI-1")).toHaveLength(1);
  });

  it("409 party.org_code_taken khi mã đã có", async () => {
    const r = await create({ ...TEAM, code: "TTKNQG" });
    expect(r.status).toBe(409);
    expect(r.body.code).toBe("party.org_code_taken");
  });

  it("sửa mã sau 409 rồi gửi lại cùng khóa → 201 (lỗi không được lưu để phát lại)", async () => {
    const key = "orgui-retry-key-1";
    expect((await create({ ...TEAM, code: "TTKNQG" }, "setter", key)).status).toBe(409);
    expect((await create(TEAM, "setter", key)).status).toBe(201);
  });

  it("422 party.invalid_hierarchy: tổ KN xã đặt dưới TTKN Quốc gia / ngoài địa bàn cấp trên", async () => {
    const wrongKind = await create({ ...TEAM, parent_ref: NATIONAL_REF });
    expect(wrongKind.status).toBe(422);
    expect(wrongKind.body.code).toBe("party.invalid_hierarchy");
    const otherProvince = await create({
      ...TEAM,
      parent_ref: provinceOrgRef(1),
    });
    expect(otherProvince.body).toMatchObject({
      code: "party.invalid_hierarchy",
      detail: "đơn vị hành chính không thuộc địa bàn của tổ chức cấp trên",
    });
  });

  it("422 validation_failed theo trường", async () => {
    const badCode = await create({ ...TEAM, code: "có dấu" });
    expect(badCode.body.errors).toEqual([{ field: "code", reason: expect.stringContaining("1–40") }]);
    const noUnit = await create({ ...TEAM, admin_unit_code: "" });
    expect(noUnit.body.errors[0]?.field).toBe("admin_unit_code");
    const wrongLevel = await create({ ...TEAM, admin_unit_code: "82" });
    expect(wrongLevel.body.errors[0]?.reason).toMatch(/cấp commune/);
    const unknown = await create({ ...TEAM, admin_unit_code: "99999" });
    expect(unknown.body.errors[0]?.reason).toMatch(/còn hiệu lực/);
    const nationalWithUnit = await create({
      kind: "dcrd",
      code: "CUC-2",
      name: "Cục 2",
      admin_unit_code: "82",
    });
    expect(nationalWithUnit.body.errors[0]?.field).toBe("admin_unit_code");
  });

  it("HTX: mọi yêu cầu tạo qua tổ chức chung phải chuyển sang Neo", async () => {
    const r = await create({
      kind: "cooperative",
      code: "HTX-X",
      name: "HTX thử",
      admin_unit_code: "82101",
    });
    expect(r.status).toBe(422);
    expect(r.body.code).toBe("party.cooperative_requires_anchor");
  });

  it("403: KN tỉnh không tạo tổ chức hoặc HTX qua endpoint chung", async () => {
    // Persona "tinh" (kn_province_admin) chưa qua MFA nên gọi thẳng handler (bỏ qua cổng MFA của mockFetch).
    const tinh = findPersona("tinh")!;
    const call = (body: Record<string, unknown>) =>
      orgCreateRoutes[0]!.handler({ url: new URL("http://mock/v1/orgs"), persona: tinh, params: [], body, request: new Request("http://mock/v1/orgs") });
    const r = call(TEAM);
    expect(r.status).toBe(403);
    expect(await r.json()).toMatchObject({ code: "forbidden", detail: expect.stringContaining("Admin nền tảng") });
    const coop = call({ kind: "cooperative", name: "HTX do KN tỉnh lập", admin_unit_code: "82101", parent_ref: provinceOrgRef(0) });
    expect(coop.status).toBe(403);
  });

  it("403: vai không có quyền; MFA bắt buộc", async () => {
    expect((await create(TEAM, "account-tinh")).body.code).toBe("forbidden");
    expect((await create(TEAM, "admin-no-mfa")).body.code).toBe("mfa_required");
  });
});
