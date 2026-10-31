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
import { createCntsDiaryApi, createPhotoDrafts, type Photo, type PhotoDraft } from "./api";

const HH = "0190f1c4-7d3e-7000-8000-0000000c7301";

function flaky() {
  const sent: { path: string; key: string | null; replayed: boolean; status: number; id: string | null }[] = [];
  let lose = false;
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => "mock.setter",
    getContextId: () => null,
    fetch: async (req: Request) => {
      const path = new URL(req.url).pathname;
      const id = req.method === "POST" && path.endsWith("/diary-photos") ? String((await req.clone().formData()).get("id")) : null;
      const res = await mockFetchNoDelay(req);
      if (req.method === "POST") sent.push({ path, key: req.headers.get("Idempotency-Key"), replayed: res.headers.get("Idempotent-Replayed") === "true", status: res.status, id });
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
  let draft: PhotoDraft | undefined;
  const turn = op.begin();
  return withIdempotentOperation(turn, async () => {
    draft = await drafts.draft(HH, kind, file); // băm nội dung (await) trong lượt, như DiaryScreen
    const d = draft;
    const blob: Blob = await Promise.resolve(file); // compressImage: ảnh nhỏ giữ nguyên
    return withIdempotentOperation(turn, () => dapi.uploadPhoto(HH, kind, blob, d));
  }).then((p) => {
    turn.complete();
    drafts.done(draft!);
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

  it("tải xong rồi chọn lại cùng ảnh là lần tải mới (id mới)", async () => {
    const drafts = createPhotoDrafts(() => new Date("2027-10-01T01:00:00Z"));
    const f = jpg("x");
    const a = await drafts.draft(HH, "flock", f);
    expect(await drafts.draft(HH, "flock", new File([f], f.name, { type: f.type, lastModified: f.lastModified }))).toBe(a);
    expect(a.captured_at).toBe("2027-10-01T01:00:00.000Z");
    drafts.done(a);
    expect((await drafts.draft(HH, "flock", f)).id).not.toBe(a.id);
  });

  // Review #172 (Codex): hai ảnh khác byte nhưng trùng tên/cỡ/kiểu/thời điểm sửa. Ảnh A đã lưu, mất phản hồi; chọn ảnh B
  // → phải là lần tải mới (id mới, khóa mới) và được lưu, không dùng lại id của A (máy chủ sẽ báo "mã ảnh đã dùng").
  it("khác nội dung, trùng metadata tệp → id mới, khóa mới, ảnh B được lưu", async () => {
    const same = (tail: number) =>
      new File([new Uint8Array([0xff, 0xd8, 0xff, 0xe0, 0, 1, 2, tail])], "IMG_0001.jpg", { type: "image/jpeg", lastModified: 1_700_000_000_000 });
    const a = same(65);
    const b = same(66);
    expect([a.name, a.size, a.type, a.lastModified]).toEqual([b.name, b.size, b.type, b.lastModified]);
    const drafts = createPhotoDrafts();
    expect((await drafts.draft(HH, "flock", b)).id).not.toBe((await drafts.draft(HH, "flock", a)).id);

    const { dapi, sent, loseNext } = flaky();
    const op = new IdempotentOperation();
    const real = createPhotoDrafts();
    loseNext();
    await expect(pick(dapi, op, real, "flock", a)).rejects.toBeTruthy(); // A đã lưu, phản hồi mất
    const pb = await pick(dapi, op, real, "flock", b);
    const ups = sent.filter((s) => s.path.endsWith("/diary-photos"));
    expect(ups).toHaveLength(2);
    expect(ups[0]!.status).toBe(201);
    expect(ups[1]!.status).toBe(201);
    expect(ups[1]!.replayed).toBe(false);
    expect(ups[1]!.key).not.toBe(ups[0]!.key);
    expect(ups[1]!.id).not.toBe(ups[0]!.id);
    expect(pb.file_ref).toBe(ups[1]!.id);
    // A vẫn còn chờ: chọn lại đúng A → cùng id/khóa → phát lại, không lưu bản thứ hai.
    await pick(dapi, op, real, "flock", new File([a], a.name, { type: a.type, lastModified: a.lastModified }));
    const last = sent.filter((s) => s.path.endsWith("/diary-photos")).at(-1)!;
    expect(last.id).toBe(ups[0]!.id);
    expect(last.key).toBe(ups[0]!.key);
    expect(last.replayed).toBe(true);
  });

  it("giả lập như máy chủ: khóa mới nhưng id ảnh đã lưu → 422 mã ảnh đã dùng", async () => {
    const { dapi } = flaky();
    const d = { id: "0190f1c4-7d3e-7000-8000-00000172a001", captured_at: "2027-10-01T01:00:00.000Z" };
    await dapi.uploadPhoto(HH, "flock", jpg("a"), d);
    await expect(dapi.uploadPhoto(HH, "flock", jpg("b"), d)).rejects.toMatchObject({ status: 422 });
  });
});
