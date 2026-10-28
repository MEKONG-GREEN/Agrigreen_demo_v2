// @vitest-environment node
// W11.53 (BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01): luồng giả lập theo hợp đồng /knqg-docs — lập Mẫu 05 (điền sẵn từ
// dự án) → trình ký → tải bản đã ký; Mẫu 07 chặn gửi khi thiếu Mẫu 05 đã ký (AC-03), tổng hợp điểm; trả lại cần ý kiến;
// Cấp Xã chỉ thấy Mẫu 05. Môi trường node: tải tệp multipart (FormData) như trình duyệt thật.
import { beforeEach, describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsKnqg } from "@/api/mock/kn-cnts-knqg";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsModelApi } from "@/kn/cnts/model/api";
import { createKnqgApi } from "./api";

async function contextOf(personaId: string, assignmentId: string, dept: string | null) {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: dept }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}

const client = (persona: string, ctx: string) =>
  createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => ctx, fetch: (req: Request) => mockFetchNoDelay(req) });

let adminApi: ApiClient;
let xaApi: ApiClient;
beforeEach(async () => {
  resetKnAdmin();
  resetCntsModel();
  resetCntsKnqg();
  const adminCtx = await contextOf("kn-admin", "asg-admin", null);
  const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
  const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
  if (cn.status !== "active") await adm.departmentStatus(cn.id, "active");
  adminApi = client("kn-admin", adminCtx);
  xaApi = client("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san"));
});
describe("luồng giả lập (hợp đồng /knqg-docs)", () => {
  it("Mẫu 05 trình ký → ký; Mẫu 07 chặn khi thiếu Mẫu 05 đã ký; Cấp Xã chỉ thấy Mẫu 05 xã mình", async () => {
    const k = createKnqgApi(adminApi);
    const specs = await k.specs();
    expect(specs.map((s) => s.form)).toEqual(["01", "03", "04", "05", "07"]);
    const project = (await createCntsModelApi(adminApi).projects()).items.find((p) => p.kind === "knqg")!;
    const site = (await createCntsModelApi(adminApi).project(project.ref)).sites[0]!;
    const d7 = await k.create({ form: "07", project_ref: project.ref, period: "2027" });
    expect(d7.blockers.join()).toContain("chưa lập Mẫu 05");
    const filled7 = await k.update(d7.ref, d7.version, { fields: { hop_dong: "12/HĐ-KN", ten_mo_hinh: "Gà", nhan_xet: "Đạt" }, tables: {} });
    await expect(k.act(d7.ref, "send", filled7.version)).rejects.toMatchObject({ code: "extension.cnts_knqg_needs_signed_05" });
    const d5 = await k.create({ form: "05", project_ref: project.ref, site_ref: site.ref });
    expect(d5.content.fields.ten_mo_hinh).toBe(project.name);
    expect(d5.content.tables.chi_tieu?.find((r) => r.ma === "quy_mo")?.yeu_cau).toBe("1500 con");
    await expect(k.act(d5.ref, "request_sign", d5.version)).rejects.toBeInstanceOf(ApiProblem); // thiếu ô bắt buộc
    const e5 = await k.update(d5.ref, d5.version, { ...d5.content, fields: { ...d5.content.fields, don_vi_thuc_hien: "TTKN", danh_gia: "Đạt" } });
    const w5 = await k.act(d5.ref, "request_sign", e5.version);
    expect(w5.actions).toContain("sign");
    const s5 = await k.sign(d5.ref, w5.version, new File(["%PDF-1.4"], "bien-ban.pdf", { type: "application/pdf" }));
    expect(s5.status).toBe("signed");
    expect(s5.signed?.name).toBe("bien-ban.pdf");
    const sent = await k.act(d7.ref, "send", filled7.version);
    expect(sent.status).toBe("sent");
    expect(sent.content.tables.diem).toHaveLength(1);
    await expect(k.act(d7.ref, "return", sent.version)).rejects.toBeInstanceOf(ApiProblem); // cần ý kiến
    const xa = await createKnqgApi(xaApi).list();
    expect(xa.every((d) => d.form === "05")).toBe(true);
  });
});
