// #277: fixture projection publish → FormsTask từ các hợp đồng mock hiện hữu; native auth kiểm riêng qua PG.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsCampaigns } from "@/api/mock/kn-cnts-campaign";
import { resetCntsDesign } from "@/api/mock/kn-cnts-design";
import { resetCntsLivestock } from "@/api/mock/kn-cnts-livestock";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { createDesignApi } from "@/kn/cnts/design/api";
import { createLivestockApi } from "@/kn/cnts/livestock/api";
import { createCntsPeriodApi } from "@/kn/cnts/periods/api";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCampaignApi } from "./api";

const code = (promise: Promise<unknown>) => promise.then(() => "ok", (e: unknown) => e instanceof ApiProblem ? e.code : String(e));
const raw = (persona: string, ctx: string | null) => createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock." + persona, getContextId: () => ctx, fetch: mockFetchNoDelay });
async function selected(persona: string, assignment_id: string, department_code: string | null = "chan_nuoi_thuy_san") {
  const context = await unwrap(raw(persona, null).POST("/v1/me/context", { params: { header: { "Idempotency-Key": crypto.randomUUID() } }, body: { assignment_id, department_code } }));
  return raw(persona, context.context_id);
}

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-08T05:00:00Z"));
  resetKnAdmin(); resetCntsCampaigns(); resetCntsDesign(); resetCntsLivestock(); resetCntsPeriods();
  const context = await unwrap(raw("kn-admin", null).POST("/v1/me/context", { params: { header: { "Idempotency-Key": crypto.randomUUID() } }, body: { assignment_id: "asg-admin", department_code: null } }));
  const admin = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context.context_id, fetch: mockAdminFetchNoDelay });
  const department = (await admin.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (department.status !== "active") await admin.departmentStatus(department.id, "active");
  // Chuẩn bị tài khoản tổng hợp qua API Admin hiện hữu, không giả selected context hay principal.
  const users = (await admin.users()).items;
  for (const [email, role] of [["an.nv@angiang.gov.vn", "kn_tinh"], ["binh.tt@angiang.gov.vn", "kn_xa"]] as const) {
    const user = users.find((u) => u.email === email)!;
    const assignment = (await admin.assignments(user.id)).items.find((a) => a.role === role)!;
    if (!assignment.departments.some((d) => d.code === "chan_nuoi_thuy_san")) await admin.changeAssignment(user.id, assignment.assignment_id, {
      role, org_ref: assignment.org_ref, department_codes: [...assignment.departments.map((d) => d.code), "chan_nuoi_thuy_san"],
      mgmt_level: "chuyen_vien", ...(role === "kn_xa" ? { commune_codes: assignment.communes.map((c) => c.code) } : {}),
    });
  }
});

async function setup() {
  const tinh = await selected("kn-an", "asg-an-tinh");
  const xa = await selected("kn-an", "asg-an-xa");
  const outside = await selected("kn-binh", "asg-binh-xa");
  const create = async (api: typeof xa, name: string, commune: string) => createLivestockApi(api).create({ owner_name: name, commune_code: commune,
    livestock_type: "nong_ho", animals: [{ species_code: "GA", head_count: 12 }], checkin: { lat: 10.3, lon: 105.4, accuracy_m: "9", mocked: false, captured_at: "2026-11-08T05:00:00Z" } });
  const first = await create(xa, "Hộ tổng hợp 277", "91301");
  const second = await create(outside, "Trang trại tổng hợp 277", "91303");
  const design = await createDesignApi(tinh).create({ code_suffix: "LABEL277", title: "Điều tra hộ 277", fields: [{ key: "dan", label: "Tổng đàn", type: "integer", required: true }] });
  const capi = createCampaignApi(tinh);
  const [owner] = await capi.owners();
  const input = { name: "Đợt theo hộ 277", field: "chan_nuoi" as const, template_code: design.code, template_version: 1, scope_mode: "households" as const,
    households: [first.id, second.id], communes: [], from_date: "2099-10-01", to_date: "2099-10-05", due_at: "2099-10-20T00:00:00Z", owner_ref: owner!.ref };
  return { tinh, xa, outside, first, second, design, capi, input };
}

afterEach(() => { vi.useRealTimers(); });

describe("#277 — projection mock hộ chiến dịch", () => {
  it("giữ template/version/fields đã chọn, parent xã thật, scope và identity gửi; replay/reset không sinh/rò việc", async () => {
    const f = await setup();
    const templates = await f.capi.templates();
    expect(templates.find((t) => t.code === f.design.code)).toMatchObject({ id: f.design.version.template_id, version: 1, fields: [{ key: "dan" }] });
    const draft = await f.capi.create(f.input);
    await createDesignApi(f.tinh).save(f.design.code, 1, "Phiên bản mới", [...f.design.version.fields, { key: "them", label: "Chỉ tiêu mới", type: "text", required: false }]);
    const latest = (await f.capi.templates()).filter((t) => t.code === f.design.code);
    expect(latest).toHaveLength(1);
    expect(latest[0]).toMatchObject({ version: 2, title: "Phiên bản mới" });
    const publishKey = crypto.randomUUID();
    const publish = () => unwrap(f.tinh.POST("/v1/extension/cnts/campaigns/{id}/publish", { params: { path: { id: draft.id }, header: { "Idempotency-Key": publishKey } } }));
    const published = await publish();
    expect((await publish()).forms_period_id).toBe(published.forms_period_id);
    const own = (await createCntsPeriodApi(f.xa).tasks()).filter((t) => t.period_id === published.forms_period_id);
    expect(own).toHaveLength(1);
    expect(own[0]).toMatchObject({ unit: f.first.id, unit_kind: "household", parent: "91301", period_kind: "adhoc", template_id: f.design.version.template_id, template_version: 1, fields: [{ key: "dan" }] });
    expect(own[0]!.fields).toHaveLength(1);
    expect(JSON.stringify(own)).not.toContain("Hộ tổng hợp 277");
    const other = (await createCntsPeriodApi(f.outside).tasks()).filter((t) => t.period_id === published.forms_period_id);
    expect(other.map((t) => [t.unit, t.parent])).toEqual([[f.second.id, "91303"]]);
    expect(await code(createLivestockApi(f.xa).get(f.second.id))).toBe("extension.cnts_livestock_household_not_found");
    expect(await code(createCntsPeriodApi(f.xa).submit(published.forms_period_id!, { unit: f.second.id, values: { dan: "12" }, nil_report: false, base_version: 0 }))).toBe("forbidden");
    const submission = await createCntsPeriodApi(f.xa).submit(published.forms_period_id!, { unit: f.first.id, values: { dan: "12" }, nil_report: false, base_version: 0 });
    expect(submission).toMatchObject({ unit: f.first.id, version: 1, values: { dan: "12" } });
    expect(await code(f.capi.publish(draft.id))).toBe("extension.cnts_campaign_state");
    expect((await createCntsPeriodApi(f.xa).tasks()).filter((t) => t.period_id === published.forms_period_id)).toHaveLength(1);
    resetCntsCampaigns();
    expect(await createCntsPeriodApi(f.xa).tasks()).toEqual([]);
  });

  it("không chiếu hộ mất/phiên bản biểu không có; endpoint template giữ quyền hiện tại", async () => {
    const f = await setup();
    expect(await code(unwrap(f.xa.GET("/v1/forms/templates", { params: { query: { profile: "kn_cnts" } } })))).toBe("forbidden");
    expect(await code(unwrap(f.tinh.GET("/v1/forms/templates", { params: { query: { profile: "other" } } })))).toBe("not_found");
    const missing = await f.capi.create({ ...f.input, households: ["0193a000-0000-7000-8000-00000000ffff"] });
    expect(await code(f.capi.publish(missing.id))).toBe("extension.cnts_campaign_household");
    const badVersion = await f.capi.create({ ...f.input, template_version: 99 });
    expect(await code(f.capi.publish(badVersion.id))).toBe("forms.template_not_found");
    expect(await createCntsPeriodApi(f.xa).tasks()).toEqual([]);
  });
});
