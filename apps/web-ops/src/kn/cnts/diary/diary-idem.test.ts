// @vitest-environment node
// #172 WEB-MULTIPART-IDEMKEY: tải ảnh nhật ký nuôi qua client thật (createApiClient + middleware Idempotency-Key) trên giả
// lập. Như màn nhật ký: lượt thao tác của useAction, nén ảnh (await) rồi mới gọi tải trong `idem` của lượt. Mất phản hồi
// sau khi máy chủ đã lưu → chọn lại đúng ảnh → cùng id/captured_at + thân tất định → cùng khóa → máy chủ phát lại.
import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { IdempotentOperation, withIdempotentOperation } from "@/api/idempotency";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsDiary } from "@/api/mock/kn-cnts-diary";
import { resetCntsModel } from "@/api/mock/kn-cnts-model";
import { createCntsDiaryApi, createPhotoDrafts, type Photo } from "./api";

const HH = "0190f1c4-7d3e-7000-8000-0000000c7301";

function flaky() {
  const sent: { path: string; key: string | null; replayed: boolean }[] = [];
  let lose = false;
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => "mock.setter",
    getContextId: () => null,
    fetch: async (req: Request) => {
      const res = await mockFetchNoDelay(req);
      if (req.method === "POST") sent.push({ path: new URL(req.url).pathname, key: req.headers.get("Idempotency-Key"), replayed: res.headers.get("Idempotent-Replayed") === "true" });
      if (lose) {
        lose = false;
        throw new TypeError("Failed to fetch"); // máy chủ đã lưu, phản hồi không về
      }
      return res;
    },
  });
  return { dapi: createCntsDiaryApi(api), sent, loseNext: () => (lose = true) };
}

/** Một lần chọn ảnh trên màn nhật ký (DiaryScreen EntryForm): lượt của useAction.run, nén (await) rồi tải trong `idem`. */
function pick(dapi: ReturnType<typeof createCntsDiaryApi>, op: IdempotentOperation, drafts: ReturnType<typeof createPhotoDrafts>, kind: Photo["kind"], file: File) {
  const draft = drafts.draft(HH, kind, file);
  const turn = op.begin();
  return withIdempotentOperation(turn, async () => {
    const blob: Blob = await Promise.resolve(file); // compressImage: ảnh nhỏ giữ nguyên
    return withIdempotentOperation(turn, () => dapi.uploadPhoto(HH, kind, blob, draft));
  }).then((p) => {
    turn.complete();
    drafts.done(draft);
    return p;
  });
}

const jpg = (s: string, name = "IMG_0001.jpg") => new File([`\xff\xd8\xff\xe0${s}`], name, { type: "image/jpeg", lastModified: 1_700_000_000_000 });

beforeEach(() => (resetCntsModel(), resetCntsDiary()));

describe("#172 tải ảnh nhật ký (multipart) giữ Idempotency-Key khi bấm lại", () => {
  it("lần đầu đã lưu nhưng mất phản hồi → chọn lại cùng ảnh: cùng khóa, máy chủ phát lại đúng ảnh cũ", async () => {
    const { dapi, sent, loseNext } = flaky();
    const op = new IdempotentOperation();
    const drafts = createPhotoDrafts();
    const file = jpg("dan-ga");
    loseNext();
    await expect(pick(dapi, op, drafts, "flock", file)).rejects.toBeTruthy();
    // Trình duyệt trả lại File mới cho cùng ảnh khi chọn lại.
    const again = new File([file], file.name, { type: file.type, lastModified: file.lastModified });
    const p = await pick(dapi, op, drafts, "flock", again);
    const ups = sent.filter((s) => s.path.endsWith("/diary-photos"));
    expect(ups).toHaveLength(2);
    expect(ups[0]!.key).toBeTruthy();
    expect(ups[1]!.key).toBe(ups[0]!.key);
    expect(ups[0]!.replayed).toBe(false);
    expect(ups[1]!.replayed).toBe(true); // phát lại, không lưu ảnh thứ hai
    expect(p.household_ref).toBe(HH);
    expect(p.kind).toBe("flock");
  });

  it("ảnh khác hoặc loại ảnh khác là lần tải mới → khóa khác, id khác", async () => {
    const { dapi, sent, loseNext } = flaky();
    const op = new IdempotentOperation();
    const drafts = createPhotoDrafts();
    loseNext();
    await expect(pick(dapi, op, drafts, "flock", jpg("a", "a.jpg"))).rejects.toBeTruthy();
    loseNext();
    await expect(pick(dapi, op, drafts, "flock", jpg("bb", "b.jpg"))).rejects.toBeTruthy();
    const c = await pick(dapi, op, drafts, "other", jpg("bb", "b.jpg"));
    const keys = sent.filter((s) => s.path.endsWith("/diary-photos")).map((s) => s.key);
    expect(keys).toHaveLength(3);
    expect(new Set(keys).size).toBe(3);
    expect(c.kind).toBe("other");
  });

  it("tải xong rồi chọn lại cùng ảnh là lần tải mới (id mới)", () => {
    const drafts = createPhotoDrafts(() => new Date("2027-10-01T01:00:00Z"));
    const f = jpg("x");
    const a = drafts.draft(HH, "flock", f);
    expect(drafts.draft(HH, "flock", new File([f], f.name, { type: f.type, lastModified: f.lastModified }))).toBe(a);
    expect(a.captured_at).toBe("2027-10-01T01:00:00.000Z");
    drafts.done(a);
    expect(drafts.draft(HH, "flock", f).id).not.toBe(a.id);
  });
});
