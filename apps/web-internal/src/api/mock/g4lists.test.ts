// @vitest-environment node
// Môi trường node: Request / FormData / File của Node (undici) — jsdom thay FormData nên multipart không đi qua được.
import { describe, expect, it } from "vitest";
import { createApiClient, idem, unwrap } from "../client";
import { sha256Hex } from "@/lib/sha256";
import { buildUploadForm, detectFileType, fetchMkgFileContent, sha256HexAsync, uploadMkgFile } from "@/lib/upload";
import { AB } from "./acctbridge";
import { CO } from "./costing";
import { F } from "./db";
import { tinyPdf } from "./files";
import { mockFetchNoDelay } from "./handler";
import { LG } from "./logistics";
import { OR } from "./orders";
import { mockToken } from "./personas";

const app = (persona: string) => createApiClient({ baseUrl: "http://mock", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
const h = () => ({ header: idem() });
const pdf = (label: string) => new File([tinyPdf(label) as BlobPart], `${label}.pdf`, { type: "application/pdf" });
const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13]);

/** Đi hết danh sách theo next_cursor. */
async function all<T>(page: (cursor: string | null) => Promise<{ items?: T[] | null; next_cursor?: string | null }>): Promise<{ items: T[]; pages: number }> {
  const items: T[] = [];
  let cursor: string | null = null;
  let pages = 0;
  do {
    const p = await page(cursor);
    items.push(...(p.items ?? []));
    cursor = p.next_cursor ?? null;
    pages += 1;
  } while (cursor && pages < 50);
  return { items, pages };
}

describe("tải tệp: SHA-256 ở trình duyệt + nhận diện loại theo nội dung", () => {
  it("sha256HexAsync (Web Crypto) = sha256Hex đồng bộ; vector chuẩn 'abc'", async () => {
    const abc = new TextEncoder().encode("abc");
    expect(await sha256HexAsync(abc)).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    const bytes = tinyPdf("x");
    expect(await sha256HexAsync(bytes)).toBe(sha256Hex(bytes));
  });
  it("magic bytes: PDF / JPEG / PNG / WebP; còn lại null", () => {
    expect(detectFileType(tinyPdf("a"))).toBe("application/pdf");
    expect(detectFileType(new Uint8Array([0xff, 0xd8, 0xff, 0xe0]))).toBe("image/jpeg");
    expect(detectFileType(PNG)).toBe("image/png");
    expect(detectFileType(new Uint8Array([0x52, 0x49, 0x46, 0x46, 1, 2, 3, 4, 0x57, 0x45, 0x42, 0x50]))).toBe("image/webp");
    expect(detectFileType(new TextEncoder().encode("hello"))).toBeNull();
  });
  it("multipart gồm file, sha256 (hex nội dung), purpose", async () => {
    const f = pdf("bb");
    const { form, sha256 } = await buildUploadForm(f, "delivery_evidence");
    expect(sha256).toBe(sha256Hex(tinyPdf("bb")));
    expect(form.get("sha256")).toBe(sha256);
    expect(form.get("purpose")).toBe("delivery_evidence");
    expect((form.get("file") as File).name).toBe("bb.pdf");
  });
});

describe("giả lập tệp chứng từ orders / logistics", () => {
  it("tải biên bản chênh lệch → 201 + đọc lại nội dung có X-Content-SHA256; ghi biên bản với file.id", async () => {
    const api = app("kinh-doanh");
    const f = await uploadMkgFile(api, "orders", pdf("bien-ban"), "receipt_variance_report");
    expect(f).toMatchObject({ purpose: "receipt_variance_report", content_type: "application/pdf", file_name: "bien-ban.pdf", sha256: sha256Hex(tinyPdf("bien-ban")) });
    expect(f.size).toBe(tinyPdf("bien-ban").length);
    const meta = await unwrap(api.GET("/v1/orders/files/{id}", { params: { path: { id: f.id } } }));
    expect(meta.file.id).toBe(f.id);
    const c = await fetchMkgFileContent(api, "orders", f.id);
    expect(c.sha256).toBe(f.sha256);
    expect(sha256Hex(new Uint8Array(await c.blob.arrayBuffer()))).toBe(f.sha256);
    const r = await unwrap(api.POST("/v1/orders/receipts/{id}/variance-report", { params: { path: { id: OR.receiptVariance }, ...h() }, body: { note: "Ghe chở dư do HTX gom thêm thửa", file_ref: f.id } }));
    expect(r.receipt?.report_file_ref).toBe(f.id);
  });
  it("tệp tải cho mục đích khác → 422 orders.file_purpose; mã chưa đăng ký vẫn nhận", async () => {
    const api = app("kinh-doanh");
    const ev = await uploadMkgFile(api, "orders", pdf("epod"), "delivery_evidence");
    await expect(unwrap(api.POST("/v1/orders/receipts/{id}/variance-report", { params: { path: { id: OR.receiptVariance }, ...h() }, body: { note: "Biên bản gắn nhầm tệp ePOD", file_ref: ev.id } }))).rejects.toMatchObject({ code: "orders.file_purpose", status: 422 });
    const r = await unwrap(api.POST("/v1/orders/receipts/{id}/variance-report", { params: { path: { id: OR.receiptVariance }, ...h() }, body: { note: "Tệp ở hệ thống cũ trước G4", file_ref: "0192f0ff-0000-7000-8000-0000000000aa" } }));
    expect(r.receipt?.report_file_ref).toBe("0192f0ff-0000-7000-8000-0000000000aa");
  });
  it("băm lệch → 422 file_checksum; không phải PDF/ảnh → 415 file_type; > 15 MB → 413 file_too_large", async () => {
    const api = app("kinh-doanh");
    const post = (form: FormData) => unwrap(api.POST("/v1/orders/files", { params: h(), body: form as never, bodySerializer: (b: unknown) => b as FormData }));
    const bad = new FormData();
    bad.append("file", pdf("a"));
    bad.append("sha256", "0".repeat(64));
    bad.append("purpose", "delivery_evidence");
    await expect(post(bad)).rejects.toMatchObject({ code: "orders.file_checksum", status: 422 });
    const txt = new TextEncoder().encode("không phải pdf");
    const t = new FormData();
    t.append("file", new File([txt as BlobPart], "a.pdf", { type: "application/pdf" }));
    t.append("sha256", sha256Hex(txt));
    t.append("purpose", "delivery_evidence");
    await expect(post(t)).rejects.toMatchObject({ code: "orders.file_type", status: 415 });
    const big = new Uint8Array(15 * 1024 * 1024 + 1);
    big.set(tinyPdf("big"));
    const b = new FormData();
    b.append("file", new File([big as BlobPart], "big.pdf"));
    b.append("sha256", "1".repeat(64));
    b.append("purpose", "delivery_evidence");
    await expect(post(b)).rejects.toMatchObject({ code: "orders.file_too_large", status: 413 });
  });
  it("gọi lặp cùng Idempotency-Key → 200 bản cũ (Idempotent-Replayed)", async () => {
    const api = app("kinh-doanh");
    const f1 = await uploadMkgFile(api, "orders", pdf("lap"), "delivery_evidence", "upload-key-0001");
    const f2 = await uploadMkgFile(api, "orders", pdf("lap"), "delivery_evidence", "upload-key-0001");
    expect(f2.id).toBe(f1.id);
  });
  it("chứng từ chuyến: tải tệp (logistics) → ghi ePOD có người ký; tệp chưa tải → 404; tài xế không được giao → 403", async () => {
    const api = app("van-tai");
    const f = await uploadMkgFile(api, "logistics", new File([PNG as BlobPart], "ebill.png"));
    expect(f).toMatchObject({ purpose: "trip_document", content_type: "image/png" });
    const d = await unwrap(api.POST("/v1/logistics/trips/{id}/documents", { params: { path: { id: LG.tripDeparted }, ...h() }, body: { kind: "ebill", file_ref: f.id, signer_role: "driver", signed_at: new Date(Date.now() - 60_000).toISOString(), note: "e-bill xuống ghe" } }));
    expect(d.document).toMatchObject({ kind: "ebill", file_ref: f.id, file_sha256: f.sha256, signer_role: "driver" });
    const list = await unwrap(api.GET("/v1/logistics/trips/{id}/documents", { params: { path: { id: LG.tripDeparted } } }));
    expect(list.items.map((x) => x.id)).toContain(d.document.id);
    await expect(unwrap(api.POST("/v1/logistics/trips/{id}/documents", { params: { path: { id: LG.tripDeparted }, ...h() }, body: { kind: "epod", file_ref: "0192f0ff-0000-7000-8000-0000000000bb", signer_role: "consignee", signed_at: new Date().toISOString() } }))).rejects.toMatchObject({ code: "logistics.file_not_found" });
    await expect(unwrap(app("tai-xe").POST("/v1/logistics/trips/{id}/documents", { params: { path: { id: LG.tripArrived }, ...h() }, body: { kind: "epod", file_ref: f.id, signer_role: "driver", signed_at: new Date().toISOString() } }))).rejects.toMatchObject({ code: "logistics.not_assigned_driver" });
    // Tài xế được giao ghi được; bản bù phải có lý do.
    const own = await unwrap(app("tai-xe").POST("/v1/logistics/trips/{id}/documents", { params: { path: { id: LG.tripDeparted }, ...h() }, body: { kind: "epod", file_ref: f.id, signer_role: "driver", signed_at: new Date().toISOString(), supersedes_id: d.document.id, correction_reason: "Ghi nhầm loại chứng từ, đây là ePOD" } }));
    expect(own.document.supersedes_id).toBe(d.document.id);
  });
});

describe("giả lập route liệt kê G4 có con trỏ", () => {
  it("lô có giá thành: mới nhất trước, đi hết trang = đủ lô, không trùng", async () => {
    const api = app("ke-toan");
    const { items, pages } = await all((cursor) => unwrap(api.GET("/v1/costing/lots", { params: { query: { limit: 2, ...(cursor ? { cursor } : {}) } } })));
    expect(pages).toBeGreaterThan(2);
    const ids = items.map((x) => x.lot_ref);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids.length).toBeGreaterThanOrEqual(6);
    expect(items.every((x) => x.total_cost)).toBe(true);
  });
  it("doanh thu carbon: lọc theo kỳ; ghi mới xuất hiện đầu danh sách", async () => {
    const api = app("ke-toan");
    const r = await unwrap(api.POST("/v1/costing/carbon-revenue", { params: h(), body: { amount: "1000000", reference: "HĐ-TEST" } }));
    const first = await unwrap(api.GET("/v1/costing/carbon-revenue", { params: { query: { limit: 3 } } }));
    expect(first.items[0]!.id).toBe(r.id);
    expect(first.next_cursor).toBeTruthy();
    const m2 = await unwrap(api.GET("/v1/costing/carbon-revenue", { params: { query: { period_ref: CO.periodPrev2 } } }));
    expect(m2.items.length).toBe(2);
    expect(m2.items.every((x) => x.period_ref === CO.periodPrev2)).toBe(true);
  });
  it("job đóng kỳ và job cầu nối: lọc trạng thái / loại", async () => {
    const api = app("ke-toan-truong");
    const jobs = await unwrap(api.GET("/v1/costing/period-close-jobs", { params: { query: { status: "completed" } } }));
    expect(jobs.items.length).toBeGreaterThanOrEqual(2);
    const push = await all((cursor) => unwrap(api.GET("/v1/acctbridge/jobs", { params: { query: { kind: "push", limit: 2, ...(cursor ? { cursor } : {}) } } })));
    expect(push.items.every((j) => j.kind === "push")).toBe(true);
    expect(push.items.filter((j) => j.journal_id === AB.journalFailed && j.status === "failed").length).toBeGreaterThanOrEqual(3);
    const builds = await unwrap(api.GET("/v1/acctbridge/jobs", { params: { query: { kind: "build" } } }));
    expect(builds.items.length).toBeGreaterThanOrEqual(2);
  });
  it("lịch sử phiên bản phương tiện: tăng dần, con trỏ = version cuối trang", async () => {
    const api = app("van-tai");
    const p1 = await unwrap(api.GET("/v1/logistics/vessels/{id}/versions", { params: { path: { id: LG.vesselExpiring }, query: { limit: 5 } } }));
    expect(p1.items.map((v) => v.version)).toEqual([1, 2, 3, 4, 5]);
    expect(p1.next_cursor).toBe("5");
    const p2 = await unwrap(api.GET("/v1/logistics/vessels/{id}/versions", { params: { path: { id: LG.vesselExpiring }, query: { limit: 5, cursor: p1.next_cursor! } } }));
    expect(p2.items.map((v) => v.version)).toEqual([6, 7]);
    expect(p2.next_cursor).toBeNull();
    expect(p1.items[0]!.kind).toBe("created");
  });
  it("hàng sắp đến: chuyến xuất phát tới cơ sở → expected mang mã chuyến; đến nơi → arrived", async () => {
    const hub = app("dieu-phoi");
    const exp = await unwrap(hub.GET("/v1/hub/arrivals", { params: { query: { facility_id: F.ct, status: "expected" } } }));
    const dep = exp.items.find((a) => a.trip_id === LG.tripDeparted)!;
    expect(dep).toMatchObject({ status: "expected", proto_status: "cho_den", facility_ref: F.ct, vessel_code: "TG-12345" });
    expect(dep.trip_code).toMatch(/^CH\d{6}$/);
    await unwrap(app("tai-xe").POST("/v1/logistics/trips/{id}/milestones", { params: { path: { id: LG.tripDeparted }, ...h() }, body: { kind: "arrived", event_time: new Date().toISOString(), lat: "10.03", lng: "105.78" } }));
    const arr = await unwrap(hub.GET("/v1/hub/arrivals", { params: { query: { status: "arrived" } } }));
    expect(arr.items[0]).toMatchObject({ trip_id: LG.tripDeparted, status: "arrived", proto_status: "da_den" });
    await expect(unwrap(app("ke-toan").GET("/v1/hub/arrivals", {}))).rejects.toMatchObject({ code: "forbidden" });
  });
});
