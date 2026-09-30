import { describe, expect, it } from "vitest";
import { createApiClient, idem, unwrap, unwrapAs } from "../client";
import type { CollectionOrderResult, Dispute, DisputePage, LotTickets, Reconciliation } from "../types";
import { F, L, O } from "./db";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { mockToken } from "./personas";

const app = (persona: string) => createApiClient({ baseUrl: "http://mock", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
const admin = (persona: string) => createApiClient({ baseUrl: "http://mock-admin", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });

describe("backend giả lập api-internal", () => {
  it("bề mặt quản trị từ chối phiên chưa qua OTP (mfa_required)", async () => {
    await expect(unwrap(admin("hub").GET("/v1/hub/params/{set}", { params: { path: { set: "risk" } } }))).rejects.toMatchObject({ code: "mfa_required", status: 403 });
    const ok = await unwrap(admin("dieu-phoi").GET("/v1/hub/params/{set}", { params: { path: { set: "risk" } } }));
    expect(ok.active_version).toBe("hub.risk.v2#1");
  });

  it("route quản trị không có trên bề mặt ứng dụng (404)", async () => {
    await expect(unwrap(app("dieu-phoi").GET("/v1/hub/facilities/{id}/versions", { params: { path: { id: F.tb } } }))).rejects.toMatchObject({ status: 404 });
  });

  it("R-56: người đề xuất không tự duyệt phiên bản cơ sở; người khác duyệt được và xuất van", async () => {
    const decide = (p: string) =>
      unwrap(admin(p).POST("/v1/hub/facilities/{id}/versions/{version}/decision", { params: { path: { id: F.tb, version: 2 }, header: idem() }, body: { decision: "approved" } }));
    await expect(decide("dieu-phoi")).rejects.toMatchObject({ code: "self_approval_forbidden" });
    const r = await decide("phe-duyet");
    expect(r.facility?.capacity_kg).toBe("600000.000");
    expect(r.publications?.map((x) => x.contract)).toEqual(["hub_facility", "hub_capacity"]);
  });

  it("phát lại cùng Idempotency-Key → cùng kết quả, không lập lệnh thứ hai", async () => {
    const key = "test-key-000001";
    const body = { htx_ref: "0192f007-0000-7000-8000-000000000002", window_from: "2026-10-01", window_to: "2026-10-05", plot_refs: ["0192f008-0000-7000-8000-000000000003"] };
    const a = await unwrap(app("dieu-phoi").POST("/v1/fieldops/orders", { params: { header: idem(key) }, body }));
    const b = await unwrap(app("dieu-phoi").POST("/v1/fieldops/orders", { params: { header: idem(key) }, body }));
    expect((b as CollectionOrderResult).order?.order_id).toBe(a.order?.order_id);
  });

  it("lệnh gom: thửa của HTX khác bị từ chối (fieldops.plot_other_htx)", async () => {
    await expect(
      unwrap(app("dieu-phoi").POST("/v1/fieldops/orders", { params: { header: idem() }, body: { htx_ref: "0192f007-0000-7000-8000-000000000001", window_from: "2026-10-01", window_to: "2026-10-02", plot_refs: ["0192f008-0000-7000-8000-000000000003"] } })),
    ).rejects.toMatchObject({ code: "fieldops.plot_other_htx" });
  });

  it("lệnh gom chờ duyệt: người lập tự duyệt → 403; tổ gom không lập được", async () => {
    await expect(unwrap(app("dieu-phoi").POST("/v1/fieldops/orders/{id}/decision", { params: { path: { id: O.planned }, header: idem() }, body: { decision: "approved" } }))).rejects.toMatchObject({
      code: "self_approval_forbidden",
    });
    await expect(
      unwrap(app("hien-truong").POST("/v1/fieldops/orders", { params: { header: idem() }, body: { htx_ref: O.planned, window_from: "2026-10-01", window_to: "2026-10-02", plot_refs: [O.planned] } })),
    ).rejects.toMatchObject({ code: "forbidden" });
  });

  it("dữ liệu mẫu prototype: lô 96,8 t cờ +10 % cuộn ↔ khối lượng; lô lệch 5,9 % mở tranh chấp", async () => {
    const h = await unwrapAs<{ latest: Reconciliation }>(app("can").GET("/v1/measurement/lots/{id}/reconciliations", { params: { path: { id: L.proto } } }));
    expect(h.latest.roll_check).toMatchObject({ status: "flag", variance_pct: "10.00" });
    const t = await unwrapAs<LotTickets>(app("can").GET("/v1/measurement/lots/{id}/tickets", { params: { path: { id: L.proto } } }));
    expect(t.mass.hub_in?.mass_kg).toBe("96800.000");
    const d = await unwrapAs<DisputePage>(app("can").GET("/v1/measurement/disputes", { params: { query: { status: "open" } } }));
    expect(d.items.find((x) => x.lot_ref === L.dispute)).toMatchObject({ severity: "flag", diff_pct: "-5.90" });
  });

  it("tranh chấp: open → under_review → resolved, không nhảy cóc", async () => {
    const d = (await unwrapAs<DisputePage>(app("can").GET("/v1/measurement/disputes", { params: { query: { status: "open" } } }))).items[0]!;
    const act = (body: Record<string, unknown>) => unwrapAs<Dispute>(app("can").POST("/v1/measurement/disputes/{id}/actions", { params: { path: { id: d.id }, header: idem() }, body: body as { action: "resolve" } }));
    await expect(act({ action: "resolve", resolution: "mkg_weight", note: "Theo số cân MKG sau khi kiểm camera" })).rejects.toMatchObject({ code: "measurement.dispute_transition" });
    expect((await act({ action: "start_review" })).status).toBe("under_review");
    expect((await act({ action: "resolve", resolution: "mkg_weight", note: "Theo số cân MKG sau khi kiểm camera" })).status).toBe("resolved");
  });
});
