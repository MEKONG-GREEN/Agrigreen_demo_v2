// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { IdempotentOperation, withIdempotentOperation } from "@/api/idempotency";
import { createAppraisalApi } from "./appraisal-api";
import { createDocApi, type HrmDocInput } from "./doc-api";
import { createPromotionApi, type DecisionInput } from "./promotion-api";
import { saveDecision } from "./DecisionScreens";
import { HRM_MAX_DOC_BYTES } from "./upload-file";
import { createSurveyApi } from "@/kn/price/surveys/api";

const pdf = (bytes = "%PDF-1.7\nscan", modified = 1) => new File([bytes], "scan.pdf", { type: "application/pdf", lastModified: modified });
const xlsx = (bytes = "PK\u0003\u0004workbook", modified = 1) => new File([bytes], "de-xuat.xlsx", {type:"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",lastModified:modified});
const csv = (bytes = "xa,gia\nA,100\n", modified = 1) => new File([bytes], "nhap.csv", { type: "text/csv", lastModified: modified });
const meta = { folder_id: "folder", title: "Quyết định", doc_type: "nang_bac" } as HrmDocInput;
const decision = { decision_no: "540/QĐ", decided_on: "2026-07-01", issuer: "Sở NN&MT", kind: "nltx", people: [] } as DecisionInput;

/** Chỉ lớp mạng giả; serializer/middleware/client thật. Máy chủ ghi rồi làm mất phản hồi. */
function server() {
  const requests: { path: string; key: string; body: string; type: string; context: string }[] = [];
  const ledger = new Map<string, { body: string; result: object }>();
  let context = "tenant-A:actor-A:scope-HCTH", lose = 1, writes = 0, denied = false, warn = false;
  const api = createApiClient({
    baseUrl: "http://retry",
    getAccessToken: () => "test-token",
    getContextId: () => context,
    fetch: async (req: Request) => {
      const path = new URL(req.url).pathname;
      const key = req.headers.get("Idempotency-Key") ?? "";
      const body = Buffer.from(await req.clone().arrayBuffer()).toString("hex");
      requests.push({ path, key, body, type: req.headers.get("Content-Type") ?? "", context });
      if (denied) return Response.json({ type: "about:blank", title: "Forbidden", status: 403, code: "forbidden" }, { status: 403 });
      if (warn && path.endsWith("/decisions") && !(await req.clone().json() as { acknowledge_warnings?: boolean }).acknowledge_warnings)
        return Response.json({ type: "about:blank", title: "Cảnh báo", status: 409, code: "hrm.decision_warnings" }, { status: 409 });
      const scope = `${context}|${path}|${key}`;
      let saved = ledger.get(scope);
      if (saved && saved.body !== body) return Response.json({ type: "about:blank", title: "Conflict", status: 422, code: "idempotency_key_reused" }, { status: 422 });
      const replay = !!saved;
      if (!saved) {
        writes++;
        saved = { body, result: path.endsWith("/decisions") ? { id: "decision-1", updated: 1, returned: 0, warnings: [] } : { doc_id: "doc-1", version: 1, created: 1, skipped: 0, errors: 0, sha256: "a".repeat(64) } };
        ledger.set(scope, saved);
      }
      if (--lose === 0) throw new TypeError("Mất phản hồi sau commit");
      return Response.json(saved.result, { status: 201, headers: { "Idempotent-Replayed": String(replay) } });
    },
  });
  return { api, requests, get writes() { return writes; }, loseAt: (n: number) => { lose = n; }, scope: (s: string) => { context = s; }, deny: () => { denied = true; }, warn: () => { warn = true; } };
}

const variants = [
  { name: "Đề xuất xếp loại", file: xlsx, send: (s: ReturnType<typeof server>, f: File) => createAppraisalApi(s.api).importExcel("period-281", 1, f) },
  { name: "Kho HCTH", file: pdf, send: (s: ReturnType<typeof server>, f: File) => createDocApi(s.api).upload(meta, f) },
  { name: "Tệp quyết định", file: pdf, send: (s: ReturnType<typeof server>, f: File) => createPromotionApi(s.api).attachFile("decision-1", f) },
  { name: "Xem trước điều tra", file: csv, send: (s: ReturnType<typeof server>, f: File) => createSurveyApi(s.api).preview("round-1", { file: f, mapping: { commune: 0, price: 1 } }) },
  { name: "Ghi nhận điều tra", file: csv, send: (s: ReturnType<typeof server>, f: File) => createSurveyApi(s.api).commit("round-1", { file: f, sha256: "a".repeat(64), mapping: { commune: 0, price: 1 }, saveSynonyms: true }) },
];

describe.each(variants)("$name — multipart retry", ({ file, send }) => {
  it("mất phản hồi → chọn lại cùng nội dung/tên, lastModified khác vẫn cùng khóa/thân/biên; thành công mới nhả khóa", async () => {
    const s = server(), op = new IdempotentOperation();
    await expect(withIdempotentOperation(op, () => send(s, file(undefined, 1)))).rejects.toBeDefined();
    await withIdempotentOperation(op, () => send(s, file(undefined, 2)));
    expect(s.requests).toHaveLength(2);
    expect(s.requests[0]!.type).toContain("multipart/form-data; boundary=----AgFormBoundary");
    expect(s.requests[1]).toEqual(s.requests[0]);
    expect(s.writes).toBe(1);
    await withIdempotentOperation(op, () => send(s, file()));
    expect(s.requests[2]!.key).not.toBe(s.requests[0]!.key);
    expect(s.writes).toBe(2);
  });

  it("cùng metadata nhưng khác byte → khóa mới; đổi context và quyền không phát lại kết quả cũ", async () => {
    const s = server(), op = new IdempotentOperation();
    const a = file(), raw = await a.text();
    const b = file(raw.slice(0, -1) + "X");
    expect(b.size).toBe(a.size);
    await expect(withIdempotentOperation(op, () => send(s, a))).rejects.toBeDefined();
    s.loseAt(1);
    await expect(withIdempotentOperation(op, () => send(s, b))).rejects.toBeDefined();
    expect(s.requests[0]!.key).not.toBe(s.requests[1]!.key);
    expect(s.requests[0]!.body).not.toBe(s.requests[1]!.body);
    s.scope("tenant-B:actor-B:read-only");
    s.deny();
    await expect(withIdempotentOperation(op, () => send(s, b))).rejects.toMatchObject({ code: "forbidden" });
    expect(s.requests[2]!.key).not.toBe(s.requests[1]!.key);
    expect(s.writes).toBe(2);
  });

  it("tệp rỗng/quá lớn/không đọc được bị chặn trước request", async () => {
    const s = server();
    for (const broken of [new File([], "scan.pdf"), file()]) {
      if (broken.size) vi.spyOn(broken, "arrayBuffer").mockRejectedValueOnce(new Error("read failed"));
      await expect(send(s, broken)).rejects.toBeDefined();
    }
    const huge = file();
    Object.defineProperty(huge, "size", { value: HRM_MAX_DOC_BYTES + 1 });
    const read = vi.spyOn(huge, "arrayBuffer");
    await expect(send(s, huge)).rejects.toBeDefined();
    expect(read).not.toHaveBeenCalled();
    expect(s.requests).toHaveLength(0);
  });
});

describe("Quyết định + tệp, một lượt qua await", () => {
  it.each([1, 2])("mất phản hồi bước %i, retry không tạo thêm quyết định/Attach; lần mới đổi khóa", async (step) => {
    const s = server(), op = new IdempotentOperation(), pa = createPromotionApi(s.api);
    s.loseAt(step);
    await expect(withIdempotentOperation(op, () => saveDecision(pa, decision, pdf()))).rejects.toBeDefined();
    await withIdempotentOperation(op, () => saveDecision(pa, decision, pdf(undefined, 10)));
    expect(s.writes).toBe(2);
    for (const path of ["/v1/hrm/promotion/decisions", "/v1/hrm/promotion/decisions/decision-1/file"]) {
      const attempts = s.requests.filter((r) => r.path === path);
      expect(new Set(attempts.map((r) => r.key)).size).toBe(1);
      expect(new Set(attempts.map((r) => r.body)).size).toBe(1);
    }
    await withIdempotentOperation(op, () => saveDecision(pa, decision, pdf()));
    expect(s.writes).toBe(4);
  });

  it("tệp sai không ghi metadata quyết định trước", async () => {
    const s = server();
    await expect(saveDecision(createPromotionApi(s.api), decision, new File(["bad"], "scan.pdf"))).rejects.toBeDefined();
    expect(s.requests).toHaveLength(0);
  });

  it("nhánh xác nhận cảnh báo cũng giữ khóa của metadata đã commit và bước attach", async () => {
    const s = server(), op = new IdempotentOperation(), pa = createPromotionApi(s.api);
    s.warn();
    vi.stubGlobal("window", { confirm: vi.fn(() => true) });
    try {
      await expect(withIdempotentOperation(op, () => saveDecision(pa, decision, pdf()))).rejects.toBeDefined();
      await withIdempotentOperation(op, () => saveDecision(pa, decision, pdf()));
      const writes = s.requests.filter((r) => r.path.endsWith("/decisions") && Buffer.from(r.body, "hex").toString().includes("acknowledge_warnings"));
      expect(writes).toHaveLength(2);
      expect(writes[1]).toEqual(writes[0]);
      expect(s.writes).toBe(2);
    } finally { vi.unstubAllGlobals(); }
  });
});

it("HC2: nhập đề xuất kiểm đuôi, cỡ tệp và phiên bản trước gửi",async()=>{
 const s=server(),api=createAppraisalApi(s.api);
 for(const version of [0,-1,1.1,Number.POSITIVE_INFINITY,Number.NaN])await expect(api.importExcel("period-281",version,xlsx())).rejects.toBeDefined();
 await expect(api.importExcel("period-281",1,new File(["PK"],"de-xuat.csv"))).rejects.toBeDefined();
 await expect(api.importExcel("period-281",1,new File([new Uint8Array(5*1024*1024+1)],"de-xuat.xlsx"))).rejects.toBeDefined();
 expect(s.requests).toHaveLength(0);
});
