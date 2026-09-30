import { describe, expect, it } from "vitest";
import { createApiClient, idem, unwrap } from "../client";
import { sha256Hex, base64ToBytes } from "@/lib/sha256";
import { journalTotals } from "@/lib/money";
import { AB } from "./acctbridge";
import { CO } from "./costing";
import { HTX, L, O } from "./db";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { LG } from "./logistics";
import { OR } from "./orders";
import { mockToken } from "./personas";

const app = (persona: string) => createApiClient({ baseUrl: "http://mock", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
const admin = (persona: string) => createApiClient({ baseUrl: "http://mock-admin", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });
const h = () => ({ header: idem() });

describe("giả lập mkg/logistics", () => {
  it("chuyến FM-05 chưa gán ghe; gán ghe sắp hết hạn → cảnh báo không chặn; chặng đường thuỷ lưu nguyên văn + SHA-256", async () => {
    const t = await unwrap(app("van-tai").GET("/v1/logistics/trips/{id}", { params: { path: { id: LG.tripFm05 } } }));
    expect(t.trip.flags).toContain("no_vehicle");
    const a = await unwrap(app("van-tai").POST("/v1/logistics/trips/{id}/vehicle", { params: { path: { id: LG.tripFm05 }, ...h() }, body: { vehicle_ref: LG.vesselExpiring } }));
    expect(a.warnings).toContain("registration_expiring");
    const legs = await unwrap(
      app("van-tai").POST("/v1/logistics/trips/{id}/legs", {
        params: { path: { id: LG.tripFm05 }, ...h() },
        body: { legs: [{ mode: "waterway", from: { kind: "collection_point", lat: "10.0452", lng: "105.2134" }, to: { kind: "facility", lat: "10.0341", lng: "105.7880" } }] },
      }),
    );
    const leg = legs.trip.legs![0]!;
    expect(leg.route_source).toBe("routed");
    const snap = await unwrap(app("van-tai").GET("/v1/logistics/trips/{id}/legs/{leg}/route-snapshot", { params: { path: { id: LG.tripFm05, leg: leg.leg_id } } }));
    expect(sha256Hex(base64ToBytes(snap.raw_base64))).toBe(snap.raw_sha256);
  });
  it("km tay thiếu lý do → logistics.manual_km_reason_required; mốc nhảy bậc → trip_transition", async () => {
    await expect(
      unwrap(app("van-tai").POST("/v1/logistics/trips/{id}/legs", { params: { path: { id: LG.tripDeparted }, ...h() }, body: { legs: [{ mode: "road", from: { kind: "point", lat: "10.1", lng: "105.1" }, to: { kind: "point", lat: "10.2", lng: "105.2" }, manual_km: "12.5" }] } })),
    ).rejects.toMatchObject({ code: "logistics.manual_km_reason_required" });
    await expect(unwrap(app("tai-xe").POST("/v1/logistics/trips/{id}/milestones", { params: { path: { id: LG.tripFm05 }, ...h() }, body: { kind: "arrived", event_time: new Date().toISOString(), lat: "10.1", lng: "105.2" } }))).rejects.toMatchObject({
      code: "logistics.trip_transition",
    });
  });
  it("tham số cước: điều phối đề xuất, kế toán trưởng KHÁC duyệt; điều phối không duyệt được", async () => {
    const p = await unwrap(app("van-tai").POST("/v1/logistics/params/{key}/versions", { params: { path: { key: "logistics.rate.road_vnd_per_t_km" }, ...h() }, body: { value: "2400", reason: "Giá dầu tăng quý IV theo báo giá nhà xe" } }));
    await expect(unwrap(app("van-tai").POST("/v1/logistics/params/{key}/versions/{version}/decision", { params: { path: { key: "logistics.rate.road_vnd_per_t_km", version: p.param!.version_id! }, ...h() }, body: { decision: "approved" } }))).rejects.toMatchObject({ code: "forbidden" });
    await unwrap(app("ke-toan-truong").POST("/v1/logistics/params/{key}/versions/{version}/decision", { params: { path: { key: "logistics.rate.road_vnd_per_t_km", version: p.param!.version_id! }, ...h() }, body: { decision: "approved" } }));
    const v = await unwrap(app("van-tai").GET("/v1/logistics/params/{key}", { params: { path: { key: "logistics.rate.road_vnd_per_t_km" } } }));
    expect(v.effective?.value).toBe("2400");
  });
});

describe("giả lập mkg/orders", () => {
  it("CT-01: hợp đồng chồng lấn → 409; người lập không tự duyệt; người khác duyệt", async () => {
    await expect(unwrap(app("kinh-doanh").POST("/v1/orders/contracts", { params: h(), body: { htx_ref: HTX.tanHiep, basis: "per_tonne", unit_price: "600000", committed_kg: "1000", valid_from: "2020-01-01", valid_to: "2099-01-01" } }))).rejects.toMatchObject({ code: "orders.contract_overlap" });
    const decide = (p: string) => unwrap(app(p).POST("/v1/orders/contracts/{id}/versions/{version}/decision", { params: { path: { id: OR.contractPending, version: 1 }, ...h() }, body: { decision: "approved" } }));
    await expect(decide("kinh-doanh")).rejects.toMatchObject({ code: "self_approval_forbidden" });
    expect((await decide("truong-kinh-doanh")).versions![0]!.status).toBe("active");
  });
  it("phiếu nhận lệch > 3 % → cần biên bản, KHÔNG chặn duyệt; rơm không sinh phải trả", async () => {
    const r = await unwrap(app("kinh-doanh").GET("/v1/orders/receipts/{id}", { params: { path: { id: OR.receiptVariance } } }));
    expect(r.receipt).toMatchObject({ needs_variance_report: true, variance_pct: "7.56", received_qty: "96800.000", qty_source: "ticket" });
    const d = await unwrap(app("truong-kinh-doanh").POST("/v1/orders/receipts/{id}/decision", { params: { path: { id: OR.receiptVariance }, ...h() }, body: { decision: "approved" } }));
    expect(d.receipt?.status).toBe("approved");
    expect(d.receipt?.payable).toBeUndefined();
  });
  it("đơn bán vượt ngưỡng chiết khấu: không có vai duyệt → price_approval_required; người lập tự duyệt → 403", async () => {
    await expect(unwrap(app("ban-hang").POST("/v1/orders/sales-orders/{id}/confirm", { params: { path: { id: OR.soDraftDiscount }, ...h() } }))).rejects.toMatchObject({ code: "orders.price_approval_required" });
    await expect(unwrap(app("kinh-doanh").POST("/v1/orders/sales-orders/{id}/confirm", { params: { path: { id: OR.soDraftSelf }, ...h() } }))).rejects.toMatchObject({ code: "self_approval_forbidden" });
    const ok = await unwrap(app("kinh-doanh").POST("/v1/orders/sales-orders/{id}/confirm", { params: { path: { id: OR.soDraftDiscount }, ...h() } }));
    expect(ok.sales_order).toMatchObject({ status: "confirmed", net_price: "1425000.0000", amount: "85500000.00" });
  });
  it("giao nhận: khối lượng = phiếu CUSTOMER_IN; thiếu phiếu → orders.customer_in_missing", async () => {
    const ev = { evidence: { file_ref: "0192f0ff-0000-7000-8000-000000000001", kind: "minutes" as const } };
    const ok = await unwrap(app("kinh-doanh").POST("/v1/orders/deliveries/{id}/accept", { params: { path: { id: OR.deliveryWithTicket }, ...h() }, body: ev }));
    expect(ok.delivery).toMatchObject({ status: "accepted", accepted_kg: "14800.000", amount: "26640000.00" });
    await unwrap(app("kinh-doanh").POST("/v1/orders/deliveries/{id}/dispatch", { params: { path: { id: OR.deliveryPlanned }, ...h() }, body: {} }));
    await expect(unwrap(app("kinh-doanh").POST("/v1/orders/deliveries/{id}/accept", { params: { path: { id: OR.deliveryPlanned }, ...h() }, body: ev }))).rejects.toMatchObject({ code: "orders.customer_in_missing" });
  });
});

describe("giả lập mkg/costing + mkg/acctbridge", () => {
  it("quyết toán theo cuộn: người lập không tự duyệt (bề mặt quản trị); người khác duyệt → hạn 15 ngày; đã duyệt không huỷ", async () => {
    const approve = (p: string) => unwrap(admin(p).POST("/v1/costing/settlements/{id}/approve", { params: { path: { id: CO.settlementPerBale }, ...h() } }));
    await expect(approve("ke-toan")).rejects.toMatchObject({ code: "self_approval_forbidden" });
    const s = await approve("ke-toan-truong");
    expect(s).toMatchObject({ status: "approved", amount: "66000000.00" });
    expect(s.due_on).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    await expect(unwrap(app("ke-toan").POST("/v1/costing/settlements/{id}/cancel", { params: { path: { id: CO.settlementPerBale }, ...h() }, body: { reason: "Thử huỷ sau khi đã duyệt" } }))).rejects.toMatchObject({ code: "costing.settlement_frozen" });
  });
  it("không hợp đồng → chưa có số tiền; nhập giá thoả thuận → có số tiền; theo tấn chờ cân → chưa duyệt được", async () => {
    const s0 = await unwrap(app("ke-toan").GET("/v1/costing/settlements/{id}", { params: { path: { id: CO.settlementNoContract } } }));
    expect(s0).toMatchObject({ blocked_reason: "no_contract", amount: null });
    const s1 = await unwrap(app("ke-toan").POST("/v1/costing/settlements/{id}/price", { params: { path: { id: CO.settlementNoContract }, ...h() }, body: { basis: "per_tonne", unit_price: "600000", reason: "Giá thoả thuận biên bản họp HTX 20/09" } }));
    expect(s1).toMatchObject({ blocked_reason: null, amount: "54900000.00", price_source: "manual" });
    await expect(unwrap(admin("ke-toan-truong").POST("/v1/costing/settlements/{id}/approve", { params: { path: { id: CO.settlementPerTonne }, ...h() } }))).rejects.toMatchObject({ code: "costing.settlement_not_ready" });
  });
  it("đóng kỳ: người lập điều chỉnh không đóng được; người khác → 202 + job → closed; bút toán dựng cân, người đóng không tự duyệt; duyệt → đẩy → posted", async () => {
    await expect(unwrap(app("ke-toan").POST("/v1/costing/periods/{id}/close", { params: { path: { id: CO.periodPrev1 }, ...h() } }))).rejects.toMatchObject({ code: "self_approval_forbidden" });
    await expect(unwrap(app("ke-toan-truong").POST("/v1/costing/periods/{id}/close", { params: { path: { id: CO.periodCurrent }, ...h() } }))).rejects.toMatchObject({ code: "costing.period_not_ended" });
    const job = await unwrap(app("ke-toan-truong").POST("/v1/costing/periods/{id}/close", { params: { path: { id: CO.periodPrev1 }, ...h() } }));
    expect(job.status).toBe("queued");
    expect((await unwrap(app("ke-toan-truong").GET("/v1/costing/period-close-jobs/{id}", { params: { path: { id: job.job_id! } } }))).status).toBe("queued");
    expect((await unwrap(app("ke-toan-truong").GET("/v1/costing/period-close-jobs/{id}", { params: { path: { id: job.job_id! } } }))).status).toBe("completed");
    const per = await unwrap(app("ke-toan").GET("/v1/costing/periods/{id}", { params: { path: { id: CO.periodPrev1 } } }));
    expect(per.status).toBe("closed");
    expect(per.summary?.result_set_sha256).toMatch(/^[0-9a-f]{64}$/);
    const list = await unwrap(app("ke-toan").GET("/v1/acctbridge/journals", { params: { query: { period_id: CO.periodPrev1 } } }));
    const jid = list.items![0]!.journal_id!;
    const jd = await unwrap(app("ke-toan").GET("/v1/acctbridge/journals/{id}", { params: { path: { id: jid } } }));
    expect(jd.journal?.source_hash).toBe(per.summary?.result_set_sha256);
    expect(journalTotals(jd.journal!.lines!).balanced).toBe(true);
    await expect(unwrap(admin("ke-toan-truong").POST("/v1/acctbridge/journals/{id}/approve", { params: { path: { id: jid }, ...h() } }))).rejects.toMatchObject({ code: "self_approval_forbidden" });
    const pj = await unwrap(admin("ke-toan").POST("/v1/acctbridge/journals/{id}/approve", { params: { path: { id: jid }, ...h() } }));
    await unwrap(app("ke-toan").GET("/v1/acctbridge/jobs/{id}", { params: { path: { id: pj.job!.job_id! } } }));
    const done = await unwrap(app("ke-toan").GET("/v1/acctbridge/jobs/{id}", { params: { path: { id: pj.job!.job_id! } } }));
    expect(done.job?.status).toBe("done");
    expect((await unwrap(app("ke-toan").GET("/v1/costing/periods/{id}", { params: { path: { id: CO.periodPrev1 } } }))).status).toBe("posted");
  });
  it("đẩy lỗi (DLQ) → đẩy lại; tệp xuất CSV CRLF có X-Content-SHA256 đúng", async () => {
    const r = await unwrap(app("ke-toan-truong").POST("/v1/acctbridge/journals/{id}/retry-push", { params: { path: { id: AB.journalFailed }, ...h() } }));
    expect(r.job?.attempt).toBe(4);
    const res = await app("ke-toan").GET("/v1/acctbridge/journals/{id}/export", { params: { path: { id: AB.journalPushed }, query: { format: "csv" } }, parseAs: "text" });
    const text = res.data as unknown as string;
    expect(text.startsWith("schema;journal_id;")).toBe(true);
    expect(text).toContain("\r\n");
    const { sha256Text } = await import("@/lib/sha256");
    expect(res.response.headers.get("X-Content-SHA256")).toBe(sha256Text(text));
  });
  it("giá thành lô: đ/t = tổng / tấn giao; lô chưa giao → không xác định", async () => {
    const c = await unwrap(app("ke-toan").GET("/v1/costing/lots/{id}/cost", { params: { path: { id: L.proto } } }));
    expect(c.total_cost).toBe("29752250.00");
    expect(c.cost_per_tonne).toBe("307357.95");
    const u = await unwrap(app("ke-toan").GET("/v1/costing/lots/{id}/cost", { params: { path: { id: L.ct } } }));
    expect(u.cost_per_tonne_undetermined).toBe(true);
    void O;
  });
});
