// @vitest-environment node
// W11.53 (BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01): luồng giả lập theo hợp đồng /knqg-docs — lập Mẫu 05 (điền sẵn từ
// dự án) → trình ký → tải bản đã ký; Mẫu 07 chặn gửi khi thiếu Mẫu 05 đã ký (AC-03), tổng hợp điểm; trả lại cần ý kiến;
// Cấp Xã chỉ thấy Mẫu 05. Môi trường node: tải tệp multipart (FormData) như trình duyệt thật.
import { beforeEach, describe, expect, it } from "vitest";
import { IdempotentOperation, withIdempotentOperation } from "@/api/idempotency";
import { stableMultipart } from "@/api/multipart";
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

describe("#29 P2: thử lại sau mất phản hồi giữ cùng Idempotency-Key (client thật + middleware thật)", () => {
  /** Client thật (createApiClient) trên mock: ghi lại khóa của từng POST; `lose` → máy chủ xử lý xong nhưng phản hồi mất. */
  async function flaky() {
    const ctx = await contextOf("kn-admin", "asg-admin", null);
    const sent: { path: string; key: string | null }[] = [];
    let lose = false;
    const api = createApiClient({
      baseUrl: "http://mock",
      getAccessToken: () => "mock.kn-admin",
      getContextId: () => ctx,
      fetch: async (req: Request) => {
        if (req.method === "POST") sent.push({ path: new URL(req.url).pathname, key: req.headers.get("Idempotency-Key") });
        const res = await mockFetchNoDelay(req);
        if (lose) {
          lose = false;
          throw new TypeError("Failed to fetch"); // đã ghi ở máy chủ, phản hồi không về
        }
        return res;
      },
    });
    return { k: createKnqgApi(api), sent, loseNext: () => (lose = true) };
  }

  async function awaitingSign(k: ReturnType<typeof createKnqgApi>) {
    const project = (await createCntsModelApi(adminApi).projects()).items.find((p) => p.kind === "knqg")!;
    const site = (await createCntsModelApi(adminApi).project(project.ref)).sites[0]!;
    const d5 = await k.create({ form: "05", project_ref: project.ref, site_ref: site.ref });
    const e5 = await k.update(d5.ref, d5.version, { ...d5.content, fields: { ...d5.content.fields, don_vi_thuc_hien: "TTKN", danh_gia: "Đạt" } });
    return k.act(d5.ref, "request_sign", e5.version);
  }

  it("ký: lần đầu đã ghi nhưng mất phản hồi → bấm lại cùng khóa, máy chủ phát lại trạng thái đã ký, không ghi thêm", async () => {
    const { k, sent, loseNext } = await flaky();
    const w5 = await awaitingSign(k);
    const file = new File(["%PDF-1.4 scan"], "bien-ban.pdf", { type: "application/pdf" });
    const op = new IdempotentOperation();
    loseNext();
    await expect(withIdempotentOperation(op, () => k.sign(w5.ref, w5.version, file))).rejects.toBeTruthy();
    const s5 = await withIdempotentOperation(op, () => k.sign(w5.ref, w5.version, file));
    const signs = sent.filter((s) => s.path.endsWith("/signed"));
    expect(signs).toHaveLength(2);
    expect(signs[0]!.key).toBeTruthy();
    expect(signs[1]!.key).toBe(signs[0]!.key);
    expect(s5.status).toBe("signed");
    expect(s5.signed?.name).toBe("bien-ban.pdf");
    const cur = await k.get(w5.ref);
    expect(cur.version).toBe(s5.version);
    expect(cur.versions).toHaveLength(s5.versions.length); // phát lại, không thêm phiên bản
  });

  it("ký: đổi tệp (hoặc base_version) là thao tác mới → khóa khác", async () => {
    const { k, sent, loseNext } = await flaky();
    const w5 = await awaitingSign(k);
    const op = new IdempotentOperation();
    loseNext();
    await expect(withIdempotentOperation(op, () => k.sign(w5.ref, w5.version, new File(["%PDF-1.4 a"], "a.pdf", { type: "application/pdf" })))).rejects.toBeTruthy();
    loseNext();
    await expect(withIdempotentOperation(op, () => k.sign(w5.ref, w5.version, new File(["%PDF-1.4 bb"], "b.pdf", { type: "application/pdf" })))).rejects.toBeTruthy();
    loseNext();
    await expect(withIdempotentOperation(op, () => k.sign(w5.ref, w5.version + 1, new File(["%PDF-1.4 bb"], "b.pdf", { type: "application/pdf" })))).rejects.toBeTruthy();
    const keys = sent.filter((s) => s.path.endsWith("/signed")).map((s) => s.key);
    expect(keys).toHaveLength(3);
    expect(new Set(keys).size).toBe(3);
  });

  it("tạo (JSON) vẫn giữ khóa như trước khi mất phản hồi", async () => {
    const { k, sent, loseNext } = await flaky();
    const project = (await createCntsModelApi(adminApi).projects()).items.find((p) => p.kind === "knqg")!;
    const op = new IdempotentOperation();
    loseNext();
    await expect(withIdempotentOperation(op, () => k.create({ form: "07", project_ref: project.ref, period: "2028" }))).rejects.toBeTruthy();
    const d7 = await withIdempotentOperation(op, () => k.create({ form: "07", project_ref: project.ref, period: "2028" }));
    const creates = sent.filter((s) => s.path.endsWith("/knqg-docs"));
    expect(creates).toHaveLength(2);
    expect(creates[1]!.key).toBe(creates[0]!.key);
    expect((await k.list({ project_ref: project.ref, form: "07" })).filter((d) => d.ref === d7.ref)).toHaveLength(1);
  });
});

describe("stableMultipart", () => {
  it("cùng phần → cùng từng byte; máy chủ đọc lại đúng trường và tệp", async () => {
    const file = new File(["%PDF-1.4\r\n--x\r\n"], 'bản "ký".pdf', { type: "application/pdf", lastModified: 1 });
    const a = stableMultipart([["meta", '{"base_version":3}'], ["file", file]]);
    const b = stableMultipart([["meta", '{"base_version":3}'], ["file", file]]);
    expect(a.contentType).toBe(b.contentType);
    expect(new Uint8Array(await a.body.arrayBuffer())).toEqual(new Uint8Array(await b.body.arrayBuffer()));
    const fd = await new Response(a.body, { headers: { "Content-Type": a.contentType } }).formData();
    expect(fd.get("meta")).toBe('{"base_version":3}');
    const f = fd.get("file") as File;
    expect(f.name).toBe('bản "ký".pdf'); // %22 do trình phân tích multipart giải lại
    expect(f.type).toBe("application/pdf");
    expect(await f.text()).toBe("%PDF-1.4\r\n--x\r\n");
    const c = stableMultipart([["meta", '{"base_version":4}'], ["file", file]]);
    expect(c.contentType).not.toBe(a.contentType);
  });
});
