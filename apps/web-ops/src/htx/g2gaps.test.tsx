import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { householdRefOf, htxCoop, syntheticPhone } from "@/api/mock/data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { sanitizeMock } from "@/api/mock/htx-gaps";
import { mockToken } from "@/api/mock/personas";
import type { PlotFeature } from "@/api/types";
import { Markdown, parseBlocks, safeHref } from "@/lib/markdown";
import { describeEntities, entitiesOf, KNOWN_ENTITIES } from "@/lib/syncEntities";
import { CANNOT_CALL, copyDraftErrors, copyPayload, householdQuery, likelyCanCall } from "./households";
import { DRAFT_LABEL, plotAreas, plotRows, plotStatus } from "./plotMap";
import { deliveryWarning, DELIVERY_FAILED, scheduleWhen, validateSchedule, type ScheduleDraft } from "./schedules";

const app = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
const admin = (persona: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });

describe("HTX US-HH-02: sao chép hàng loạt — kiểm phía client", () => {
  it("báo Thiếu họ tên / Thiếu SĐT / SĐT sai dạng / trùng trong lần sao chép", () => {
    const rows = [
      { sourceRef: "a", sourceLabel: "A", fullName: "Nguyễn Văn Một", phone: "0912 345 678" },
      { sourceRef: "a", sourceLabel: "A", fullName: " ", phone: "0912345679" },
      { sourceRef: "b", sourceLabel: "B", fullName: "Lê Thị Hai", phone: "" },
      { sourceRef: "b", sourceLabel: "B", fullName: "Lê Thị Ba", phone: "123" },
      { sourceRef: "c", sourceLabel: "C", fullName: "Trần Bốn", phone: "+84912345678" },
    ];
    expect(copyDraftErrors(rows)).toEqual([null, "Thiếu họ tên", "Thiếu SĐT", expect.stringContaining("SĐT phải có dạng"), "SĐT trùng với hồ sơ 1 trong lần sao chép này"]);
    expect(copyPayload(rows.slice(0, 1))).toEqual([{ source_ref: "a", full_name: "Nguyễn Văn Một", phone: "0912345678" }]);
  });
});

describe("HTX US-HH-01 AC-2: ô tìm nông hộ", () => {
  it("chỉ SĐT được gửi lên; họ tên → giải thích vì sao không tìm được", () => {
    expect(householdQuery("")).toBeNull();
    expect(householdQuery("+84 912 345 678")).toEqual({ phone: "0912345678" });
    expect(householdQuery("0912")).toEqual({ error: expect.stringContaining("SĐT phải có dạng") });
    expect(householdQuery("Nguyễn Văn A")).toEqual({ error: expect.stringContaining("họ tên chủ hộ được lưu mã hoá") });
  });

  it("mock tìm đúng một hộ theo SĐT, SĐT lạ → rỗng, không phải SĐT → 422", async () => {
    const c = htxCoop();
    const api = app("htx");
    const hit = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: c.api.ref }, query: { phone: syntheticPhone(c, 2) } } }));
    expect(hit.items.map((h) => h.household_ref)).toEqual([householdRefOf(c, 2)]);
    const miss = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: c.api.ref }, query: { phone: "0988000000" } } }));
    expect(miss.items).toEqual([]);
    await expect(unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: c.api.ref }, query: { phone: "abc" } } }))).rejects.toMatchObject({ status: 422 });
  });
});

describe("HTX US-HH-03: gọi nhanh", () => {
  it("SĐT đọc lúc bấm, no-store; thiết bị không gọi được → thông báo AC-2", async () => {
    const c = htxCoop();
    const res = await app("htx").GET("/v1/households/{ref}/contact", { params: { path: { ref: householdRefOf(c, 0) } } });
    expect(res.data?.phone).toBe(syntheticPhone(c, 0));
    expect(res.response.headers.get("Cache-Control")).toBe("no-store");
    expect(likelyCanCall({ maxTouchPoints: 0, userAgent: "Mozilla/5.0 (Windows NT 10.0)" })).toBe(false);
    expect(likelyCanCall({ maxTouchPoints: 5, userAgent: "Android" })).toBe(true);
    expect(CANNOT_CALL).toBe("Không thể khởi tạo cuộc gọi trên thiết bị này.");
  });
});

describe("HTX US-PLOT-01: bản đồ lô ruộng", () => {
  const plot = (ref: string, status: "active" | "draft" | "retired", coop: string | null, review = false): PlotFeature =>
    ({
      type: "Feature",
      id: ref,
      geometry: { type: "Polygon", coordinates: [[[105, 10], [105.001, 10], [105.001, 10.001], [105, 10]]] },
      properties: { ref, code: `L-${ref}`, name: null, commune_code: "82101", cooperative_ref: coop, household_ref: null, status, status_reason: null, version: 1, boundary_id: ref, area_ha: "1.0000", source: "kn_field_app", needs_review: review, revision: 1, created_at: "", updated_at: "" },
    }) as unknown as PlotFeature;

  it("lô KN vẽ trước chưa gán → 'Nháp - KN vẽ trước'; gộp một bản ghi (không trùng)", () => {
    expect(plotStatus(plot("d", "draft", null), "C").text).toBe(DRAFT_LABEL);
    expect(plotStatus(plot("a", "active", "C"), "C").text).toBe("Đang dùng");
    expect(plotStatus(plot("r", "active", "C", true), "C").text).toBe("Có chồng lấn chờ xem xét");
    expect(plotStatus(plot("x", "retired", "C"), "C").text).toBe("Ngừng dùng");
    const own = [plot("a", "active", "C")];
    const drafts = [plot("d", "draft", null), plot("a", "active", "C")];
    expect(plotRows(own, drafts, "C").map((r) => r.plot.properties.ref)).toEqual(["a", "d"]);
    const fc = plotAreas(own, drafts, "C");
    expect(fc.features.map((f) => [f.properties.code, f.properties.status_label])).toEqual([
      ["a", "Đang dùng"],
      ["d", DRAFT_LABEL],
    ]);
  });
});

describe("HTX US-DASH-03: lịch gửi báo cáo của quản lý HTX", () => {
  const base: ScheduleDraft = { format: "xlsx", frequency: "monthly", weekday: "1", day: "1", hour: "6", seasonCode: "", seasonYear: "", recipients: ["r1"] };
  it("kiểm biểu mẫu và dựng thân mẫu htx.production", () => {
    const v = validateSchedule(base);
    expect(v.ok && v.body).toMatchObject({ template_key: "htx.production", frequency: "monthly", day_of_month: 1, hour: 6, recipients: ["r1"] });
    const bad = validateSchedule({ ...base, hour: "25", day: "31", recipients: [], seasonCode: "HT", seasonYear: "" });
    expect(bad.ok).toBe(false);
    if (!bad.ok) expect(bad.errors).toHaveLength(4);
    expect(scheduleWhen({ frequency: "weekly", weekday: 1, day_of_month: null, hour: 7 })).toBe("Hằng tuần, thứ Hai lúc 7 giờ");
    expect(deliveryWarning([{ delivery_status: "sent", delivery_note: null }])).toBeNull();
    expect(deliveryWarning([{ delivery_status: "skipped", delivery_note: "1 người nhận chưa có email" }])).toContain(DELIVERY_FAILED);
  });

  it("mock: chỉ người nhận của HTX; người ngoài → reports.recipient_not_in_cooperative", async () => {
    const api = app("htx");
    const rc = await unwrap(api.GET("/v1/reports/recipients"));
    expect(rc.items.length).toBeGreaterThan(0);
    const ok = validateSchedule({ ...base, recipients: [rc.items[0]!.actor_ref] });
    if (!ok.ok) throw new Error("form");
    const s = await unwrap(api.POST("/v1/reports/schedules", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: ok.body }));
    expect((s.params as Record<string, unknown>).cooperative_ref).toBe(htxCoop().api.ref);
    await expect(
      unwrap(api.POST("/v1/reports/schedules", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { ...ok.body, recipients: ["01927b3e-0ff1-7000-8000-00000000ffff"] } })),
    ).rejects.toMatchObject({ code: "reports.recipient_not_in_cooperative" });
    await expect(unwrap(app("xa").GET("/v1/reports/schedules"))).rejects.toMatchObject({ status: 403 });
  });
});

describe("KN US-SYNC-02: nhóm dữ liệu đồng bộ ngay", () => {
  it("gộp entity của các nhóm, không trùng; rỗng = tất cả", () => {
    expect(entitiesOf([])).toEqual([]);
    expect(entitiesOf(["plots", "seasons"])).toEqual(["plot", "season", "farm_log", "sop_version"]);
    expect(describeEntities([])).toBe("tất cả dữ liệu");
    expect(describeEntities(["plot"])).toBe("Thửa ruộng & ranh giới");
    expect(KNOWN_ENTITIES.has("notification")).toBe(true);
  });
});

describe("HTX US-CMS-01: xem trước mobile", () => {
  it("Markdown ở chế độ tắt HTML thô; liên kết chỉ http/https/mailto", () => {
    expect(parseBlocks("# Tiêu đề\n\nĐoạn một\ndòng hai\n\n- a\n- b\n1. c")).toEqual([
      { kind: "h", level: 1, text: "Tiêu đề" },
      { kind: "p", text: "Đoạn một dòng hai" },
      { kind: "ul", items: ["a", "b"] },
      { kind: "ol", items: ["c"] },
    ]);
    expect(safeHref("javascript:alert(1)")).toBeNull();
    expect(safeHref("https://mekonggreen.vn")).toBe("https://mekonggreen.vn");
    render(<Markdown source={'**Rầy nâu** <script>alert(1)</script> [xem](javascript:x) [nguồn](https://a.vn)'} />);
    expect(screen.getByText("Rầy nâu").tagName).toBe("STRONG");
    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText("xem").tagName).toBe("SPAN");
    expect(screen.getByRole("link", { name: "nguồn" })).toHaveAttribute("href", "https://a.vn");
    expect(sanitizeMock('<script>x</script><b onclick="y">z</b>')).toBe("<b>z</b>");
  });

  it("mock soạn bài: gửi duyệt thiếu chuyên mục → thông báo AC-3", async () => {
    const api = admin("setter");
    const r = await unwrap(
      api.POST("/v1/documents", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { title: "Tin thử", body: "Nội dung", body_format: "markdown", kind: "news", audience: { level: "national" } } }),
    );
    const v = await unwrap(api.GET("/v1/documents/{ref}/versions/{no}", { params: { path: { ref: r.document.ref, no: 1 } } }));
    expect(v.version.title).toBe("Tin thử");
    await expect(unwrap(api.POST("/v1/documents/{ref}/submit", { params: { path: { ref: r.document.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: {} }))).rejects.toMatchObject({
      fieldErrors: [{ field: "category", reason: "Vui lòng nhập tiêu đề và chọn chuyên mục trước khi xuất bản." }],
    });
  });
});

describe("HTX US-HTX-02/03: nhập và sao chép hồ sơ HTX (mock)", () => {
  it("nhập CSV: thiếu cột bắt buộc theo dòng; sao chép tên trùng → 409", async () => {
    const api = admin("setter");
    const c = htxCoop();
    const csv = `Tên HTX,Địa chỉ,SĐT,Mã xã\nHTX Mới Một,Ấp 1,0901000001,${c.api.admin_unit_code}\n`;
    const r = await unwrap(
      api.POST("/v1/cooperatives/import", {
        params: { header: { "Idempotency-Key": newIdempotencyKey() } },
        body: csv as unknown as string,
        bodySerializer: (b: unknown) => b as BodyInit,
        headers: { "Content-Type": "text/csv; charset=utf-8" },
      }),
    );
    expect(r.rows[0]).toMatchObject({ status: "rejected", reason: "Thiếu cột bắt buộc: Người đại diện" });
    await expect(
      unwrap(api.POST("/v1/cooperatives/{ref}/copy", { params: { path: { ref: c.api.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { name: c.api.name } })),
    ).rejects.toMatchObject({ code: "party.cooperative_name_taken", message: "Tên HTX này đã tồn tại, vui lòng chọn tên khác." });
    const cp = await unwrap(api.POST("/v1/cooperatives/{ref}/copy", { params: { path: { ref: c.api.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { name: `${c.api.name} 2` } }));
    expect(cp.org.representative_ref).toBeNull();
    expect(cp.org.admin_unit_code).toBe(c.api.admin_unit_code);
  });
});
