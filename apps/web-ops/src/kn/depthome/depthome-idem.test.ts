// @vitest-environment node
// #172 WEB-MULTIPART-IDEMKEY: kho văn bản phòng — tải tệp (thêm văn bản, tải tệp mới) qua client thật (createApiClient +
// middleware Idempotency-Key) trên giả lập. Mất phản hồi sau khi máy chủ đã ghi → bấm lại trong cùng phiên thao tác gửi
// đúng thân multipart cũ với cùng khóa → máy chủ phát lại, không thêm văn bản/phiên bản; đổi tệp → khóa mới.
// Môi trường node: File/Request của undici (như knqg-api.test.ts).
import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { IdempotentOperation, withIdempotentOperation } from "@/api/idempotency";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetDeptHome } from "@/api/mock/kn-dept-home";
import { createDeptHomeApi, docPayload, emptyDocForm } from "./api";

const DEPT = "chan_nuoi_thuy_san";

function flaky() {
  const sent: { path: string; key: string | null }[] = [];
  let lose = false;
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => "mock.setter",
    fetch: async (req: Request) => {
      if (req.method === "POST") sent.push({ path: new URL(req.url).pathname, key: req.headers.get("Idempotency-Key") });
      const res = await mockFetchNoDelay(req);
      if (lose) {
        lose = false;
        throw new TypeError("Failed to fetch"); // máy chủ đã ghi, phản hồi không về
      }
      return res;
    },
  });
  return { h: createDeptHomeApi(api, DEPT), sent, loseNext: () => (lose = true) };
}

const pdf = (s: string, name = "qt.pdf") => new File([`%PDF-1.7\n${s}\n%%EOF`], name, { type: "application/pdf", lastModified: 1 });
const meta = (title: string) => docPayload({ ...emptyDocForm(), title, kind: "quy_trinh_ky_thuat" });

beforeEach(() => resetDeptHome());

describe("#172 thêm văn bản (multipart) giữ Idempotency-Key khi bấm lại", () => {
  it("lần đầu đã ghi nhưng mất phản hồi → bấm lại cùng khóa, máy chủ phát lại, không có văn bản thứ hai", async () => {
    const { h, sent, loseNext } = flaky();
    const before = (await h.list({ q: "Quy trình nuôi tôm 172" })).items.length;
    const op = new IdempotentOperation();
    const file = pdf("tom");
    loseNext();
    await expect(withIdempotentOperation(op, () => h.create(meta("Quy trình nuôi tôm 172"), file))).rejects.toBeTruthy();
    const d = await withIdempotentOperation(op, () => h.create(meta("Quy trình nuôi tôm 172"), file));
    const creates = sent.filter((s) => s.path === "/v1/extension/dept-docs");
    expect(creates).toHaveLength(2);
    expect(creates[0]!.key).toBeTruthy();
    expect(creates[1]!.key).toBe(creates[0]!.key);
    expect(d.title).toBe("Quy trình nuôi tôm 172");
    const after = (await h.list({ q: "Quy trình nuôi tôm 172" })).items;
    expect(after.length).toBe(before + 1);
    expect(after.filter((x) => x.ref === d.ref)).toHaveLength(1);
  });

  it("đổi tệp là thao tác mới → khóa khác", async () => {
    const { h, sent, loseNext } = flaky();
    const op = new IdempotentOperation();
    loseNext();
    await expect(withIdempotentOperation(op, () => h.create(meta("Kế hoạch 172"), pdf("a", "a.pdf")))).rejects.toBeTruthy();
    loseNext();
    await expect(withIdempotentOperation(op, () => h.create(meta("Kế hoạch 172"), pdf("bb", "b.pdf")))).rejects.toBeTruthy();
    const keys = sent.filter((s) => s.path === "/v1/extension/dept-docs").map((s) => s.key);
    expect(keys).toHaveLength(2);
    expect(new Set(keys).size).toBe(2);
  });
});

describe("#172 tải tệp mới (multipart) giữ Idempotency-Key khi bấm lại", () => {
  it("mất phản hồi → bấm lại phát lại đúng kết quả, không thêm phiên bản tệp; đổi tệp → khóa mới", async () => {
    const { h, sent, loseNext } = flaky();
    const d = await h.create(meta("Hướng dẫn 172"), pdf("v1"));
    const op = new IdempotentOperation();
    const v2 = pdf("v2", "v2.pdf");
    loseNext();
    await expect(withIdempotentOperation(op, () => h.replaceFile(d.ref, v2, d.version))).rejects.toBeTruthy();
    const r = await withIdempotentOperation(op, () => h.replaceFile(d.ref, v2, d.version));
    const files = sent.filter((s) => s.path.endsWith("/file"));
    expect(files).toHaveLength(2);
    expect(files[1]!.key).toBe(files[0]!.key);
    expect(r.file.version).toBe(2);
    const cur = await h.get(d.ref);
    expect(cur.version).toBe(r.version);
    expect(cur.file.version).toBe(2);
    expect(cur.versions).toHaveLength(2); // phát lại, không ghi phiên bản thứ ba
    loseNext();
    await expect(withIdempotentOperation(op, () => h.replaceFile(d.ref, pdf("v3", "v3.pdf"), cur.version))).rejects.toBeTruthy();
    expect(new Set(sent.filter((s) => s.path.endsWith("/file")).map((s) => s.key)).size).toBe(2);
  });
});
