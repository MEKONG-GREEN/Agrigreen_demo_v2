// @vitest-environment node
// #172 WEB-MULTIPART-IDEMKEY: màn quản trị bài viết — "Lưu nháp" có ảnh bìa (tạo tài liệu → tải ảnh multipart → phiên bản
// gắn ảnh) qua client Admin thật (createAdminApiClient + middleware Idempotency-Key) trên giả lập. Mất phản hồi ở một bước
// → bấm lại cùng nội dung/ảnh: mọi bước đã ghi được phát lại bằng đúng khóa cũ, không thêm tài liệu/ảnh/phiên bản.
import { describe, expect, it } from "vitest";
import { createAdminApiClient, unwrap } from "@/api/client";
import { IdempotentOperation } from "@/api/idempotency";
import { mockAdminFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import { saveArticleDraft, type ArticleInput } from "./articleDraft";

function flaky() {
  const sent: { path: string; key: string | null; replayed: boolean }[] = [];
  let loseSuffix: string | null = null;
  const api = createAdminApiClient({
    baseUrl: "https://admin.test",
    getAccessToken: () => mockToken("setter"),
    fetch: async (req: Request) => {
      const res = await mockAdminFetchNoDelay(req);
      const path = new URL(req.url).pathname;
      if (req.method === "POST") sent.push({ path, key: req.headers.get("Idempotency-Key"), replayed: res.headers.get("Idempotent-Replayed") === "true" });
      if (req.method === "POST" && loseSuffix && path.endsWith(loseSuffix)) {
        loseSuffix = null;
        throw new TypeError("Failed to fetch"); // máy chủ đã ghi, phản hồi không về
      }
      return res;
    },
  });
  return { api, sent, loseNextAt: (suffix: string) => (loseSuffix = suffix) };
}

/** Một lần bấm "Lưu nháp" như màn quan-tri/bai-viet: một lượt của useIdempotentAction, complete khi thành công. */
async function clickSave(api: ReturnType<typeof flaky>["api"], op: IdempotentOperation, ref: string | null, input: ArticleInput, cover: File | null) {
  const turn = op.begin();
  const r = await saveArticleDraft(api, turn, { ref, input, cover });
  turn.complete();
  return r;
}

const input = (title: string): ArticleInput => ({ title, summary: "Tóm tắt", body: "Nội dung **bài**", body_format: "markdown", category: undefined });
const png = (s: string, name = "bia.png") => new File([`\x89PNG\r\n\x1a\n${s}`], name, { type: "image/png", lastModified: 1 });

async function versionStatus(api: ReturnType<typeof flaky>["api"], ref: string, no: number) {
  const r = await api.GET("/v1/documents/{ref}/versions/{no}", { params: { path: { ref, no } } });
  return { status: r.response.status, cover: r.data?.version.attachments.find((a) => a.role === "cover")?.id ?? null };
}

describe("#172 lưu nháp bài viết có ảnh bìa (multipart) giữ Idempotency-Key khi bấm lại", () => {
  it("mất phản hồi khi tải ảnh bìa → bấm lại: tài liệu và ảnh được phát lại, chỉ thêm đúng một phiên bản gắn ảnh", async () => {
    const { api, sent, loseNextAt } = flaky();
    const op = new IdempotentOperation();
    const cover = png("bia-1");
    loseNextAt("/attachments");
    await expect(clickSave(api, op, null, input("Bài 172 A"), cover)).rejects.toBeTruthy();
    const saved = await clickSave(api, op, null, input("Bài 172 A"), new File([cover], cover.name, { type: cover.type, lastModified: cover.lastModified }));
    const docs = sent.filter((s) => s.path === "/v1/documents");
    const atts = sent.filter((s) => s.path.endsWith("/attachments"));
    expect(docs).toHaveLength(2);
    expect(docs[1]!.key).toBe(docs[0]!.key);
    expect(docs[1]!.replayed).toBe(true); // không tạo tài liệu thứ hai
    expect(atts).toHaveLength(2);
    expect(atts[1]!.key).toBe(atts[0]!.key);
    expect(atts[1]!.replayed).toBe(true); // không lưu ảnh thứ hai
    expect(saved.version.version_no).toBe(2);
    expect((await versionStatus(api, saved.ref, 2)).cover).toBeTruthy();
    expect((await versionStatus(api, saved.ref, 3)).status).toBe(404);
  });

  it("mất phản hồi ở bước gắn ảnh → bấm lại phát lại cả ba bước, không thêm phiên bản", async () => {
    const { api, sent, loseNextAt } = flaky();
    const op = new IdempotentOperation();
    const cover = png("bia-2");
    loseNextAt("/versions");
    await expect(clickSave(api, op, null, input("Bài 172 B"), cover)).rejects.toBeTruthy();
    const saved = await clickSave(api, op, null, input("Bài 172 B"), cover);
    const second = sent.slice(3);
    expect(second.map((s) => s.replayed)).toEqual([true, true, true]);
    expect(second.map((s) => s.key)).toEqual(sent.slice(0, 3).map((s) => s.key));
    expect(saved.version.version_no).toBe(2);
    expect((await versionStatus(api, saved.ref, 3)).status).toBe(404);
  });

  it("đổi ảnh bìa là thao tác mới cho bước tải ảnh → khóa khác; thao tác sau khi thành công dùng khóa mới", async () => {
    const { api, sent, loseNextAt } = flaky();
    const op = new IdempotentOperation();
    loseNextAt("/attachments");
    await expect(clickSave(api, op, null, input("Bài 172 C"), png("a", "a.png"))).rejects.toBeTruthy();
    const saved = await clickSave(api, op, null, input("Bài 172 C"), png("bb", "b.png"));
    const atts = sent.filter((s) => s.path.endsWith("/attachments"));
    expect(atts).toHaveLength(2);
    expect(atts[1]!.key).not.toBe(atts[0]!.key);
    expect(atts[1]!.replayed).toBe(false);
    const next = await clickSave(api, op, saved.ref, input("Bài 172 C"), png("bb", "b.png"));
    const atts2 = sent.filter((s) => s.path.endsWith("/attachments"));
    expect(atts2[2]!.key).not.toBe(atts2[1]!.key); // lượt trước đã xong → lần lưu mới
    expect(next.version.version_no).toBeGreaterThan(saved.version.version_no);
    expect(await unwrap(api.GET("/v1/documents/{ref}/versions/{no}", { params: { path: { ref: saved.ref, no: next.version.version_no } } }))).toBeTruthy();
  });
});
