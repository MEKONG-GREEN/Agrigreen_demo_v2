import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { importHouseholds } from "@/admin/households";
import { proposeParam } from "@/admin/params";
import { createAdminApiClient, createApiClient, unwrap } from "../client";
import { newIdempotencyKey } from "../idempotency";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "../mock/handler";
import { COOPERATIVES } from "../mock/data";
import { mockToken } from "../mock/personas";
import { collectPages } from "../pagination";
import { buildPrincipal } from "@/auth/principal";
import { ApiProblem } from "../problem";
import type { Cooperative } from "../types";
import { loadBoundaries } from "./boundaries";
import { assignPlot, bboxOf, cooperativePlotsPage, cooperativePoints, draftPlotsInCommune } from "./cooperatives";
import { loadDashboard, progressPct } from "./dashboard";
import { unitLabel, unitResolver } from "./units";

/** fetch ghi lại request rồi chuyển cho backend giả lập (hình dạng theo hợp đồng thật). */
function recording(inner: (r: Request) => Promise<Response>) {
  const seen: Request[] = [];
  return {
    seen,
    fetch: async (r: Request) => {
      seen.push(r.clone());
      return inner(r);
    },
  };
}

const app = (persona: string, fetch: (r: Request) => Promise<Response> = mockFetchNoDelay) =>
  createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch });
const admin = (persona: string, fetch: (r: Request) => Promise<Response> = mockAdminFetchNoDelay) =>
  createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch });

async function problemOf(p: Promise<unknown>): Promise<ApiProblem> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiProblem) return e;
    throw e;
  }
  throw new Error("mong đợi ApiProblem");
}

describe("hợp đồng: không còn route viết tay", () => {
  it("mọi route web gọi đều có trong kiểu sinh (không còn thư mục pending)", () => {
    const generated = readFileSync(resolve(process.cwd(), "src/api/generated/application.ts"), "utf8");
    for (const p of [
      "/v1/me",
      "/v1/mechanization/coverage",
      "/v1/mechanization/dashboard",
      "/v1/dashboard",
      "/v1/geo/admin-boundaries",
      "/v1/geo/admin-boundaries/{code}",
      "/v1/notifications",
      "/v1/notifications/unread-count",
      "/v1/sync/nudge",
      "/v1/library",
      "/v1/reports/exports/{id}/download",
      "/v1/mechanization/forecasts/{id}",
    ]) {
      expect(generated.includes(`"${p}": {`)).toBe(true);
    }
    const admin = readFileSync(resolve(process.cwd(), "src/api/generated/admin.ts"), "utf8");
    for (const p of ["/v1/accounts/users", "/v1/accounts/grants", "/v1/reports/schedules", "/v1/sync/conflicts/{id}", "/v1/orgs/export.xlsx", "/v1/masterdata/params"]) {
      expect(admin.includes(`"${p}": {`)).toBe(true);
    }
  });
});

describe("GET /v1/me → Principal", () => {
  it("cấp phạm vi, tên đơn vị, MFA; Cục (dcrd) → cổng CGH", async () => {
    const tinh = await unwrap(app("tinh").GET("/v1/me"));
    const p = buildPrincipal({ name: "Cán bộ tỉnh" }, tinh);
    expect(p.level).toBe("tinh");
    expect(p.unitName).toBe("Đồng Tháp");
    expect(p.scopeUnits).toContain("82101");
    expect(p.mfa).toBe(false);
    const cuc = buildPrincipal({}, await unwrap(app("cuc").GET("/v1/me")));
    expect(cuc.level).toBe("cuc");
    expect(cuc.unitName).toBe("Toàn vùng");
    expect(buildPrincipal({}, await unwrap(app("setter").GET("/v1/me"))).mfa).toBe(true);
  });
});

describe("masterdata — tra tên đơn vị", () => {
  it("nhớ kết quả, 404 → bỏ qua, kèm đơn vị cha", async () => {
    const { seen, fetch } = recording(mockFetchNoDelay);
    const api = app("tw", fetch);
    const units = await unitResolver(api).many(["82101", "82101", "ZZ999"]);
    expect(unitLabel(units, "82101")).toMatch(/^Xã .+, Đồng Tháp$/);
    expect(unitLabel(units, "ZZ999")).toBe("ZZ999");
    await unitResolver(api).get("82101");
    expect(seen.filter((r) => r.url.endsWith("/v1/masterdata/admin-units/82101"))).toHaveLength(1);
  });
});

describe("geo — ranh giới hành chính theo cấp (G-05)", () => {
  it("MỘT lời gọi cho cả tỉnh; đơn vị chưa nhập ranh giới vào missing; diện tích giữ chuỗi", async () => {
    const { seen, fetch } = recording(mockFetchNoDelay);
    const res = await loadBoundaries(app("tw", fetch), { level: "commune", parent: "96" });
    expect(seen).toHaveLength(1);
    expect(new URL(seen[0]!.url).search).toBe("?level=commune&parent=96");
    expect(res.collection.features.every((f) => f.properties.parent_code === "96")).toBe(true);
    expect(res.missing).toEqual(["96104"]);
    expect(typeof res.collection.features[0]?.properties.area_ha).toBe("string");
    const provinces = await loadBoundaries(app("tw"), { level: "province" });
    expect(provinces.collection.features.length).toBeGreaterThanOrEqual(6);
  });

  it("khung bao của hình", () => {
    expect(bboxOf([[[105, 10], [105.5, 10.2], [105.2, 9.9], [105, 10]]])).toBe("105.000000,9.900000,105.500000,10.200000");
    expect(bboxOf([])).toBeNull();
  });
});

describe("bảng điều hành gộp — GET /v1/dashboard (G-02)", () => {
  it("tên đơn vị con do backend trả; tổng = tổng các con; decimal là chuỗi; thiếu báo cáo → tạm tính", async () => {
    const d = await loadDashboard(app("tinh"));
    expect(d.level).toBe("province");
    expect(d.unit).toBe("82");
    expect(d.children.length).toBeGreaterThan(0);
    expect(d.children.every((c) => (c.name ?? "").startsWith("Xã "))).toBe(true);
    expect(d.total.sown_area_ha).toMatch(/^\d+\.\d{4}$/);
    expect(d.total.digitized_plots).toBe(d.children.reduce((s, c) => s + c.digitized_plots, 0));
    expect(d.total.cooperatives).toBe(d.children.reduce((s, c) => s + c.cooperatives, 0));
    expect(d.total.provisional).toBe(true);
    expect(d.total.provisional_label).toBe("Số liệu tạm tính");
    expect(d.sources).toContain("geo");
  });

  it("tiến độ so với mục tiêu tính trên số nguyên đã nhân tỉ lệ (không float trên chuỗi)", () => {
    expect(progressPct({ sown_area_ha: "750.0000", target_area_ha: "1500.0000" })).toBe(50);
    expect(progressPct({ sown_area_ha: "1.0000", target_area_ha: "3.0000" })).toBe(33.33);
    expect(progressPct({ sown_area_ha: "10", target_area_ha: null })).toBeNull();
    expect(progressPct({ sown_area_ha: "10", target_area_ha: "0" })).toBeNull();
  });

  it("đơn vị ngoài phạm vi → 403 extension.out_of_scope", async () => {
    const p = await problemOf(loadDashboard(app("tinh"), { unit: "91" }));
    expect(p.code).toBe("extension.out_of_scope");
  });
});

describe("party/geo — HTX, thửa", () => {
  it("điểm HTX: toạ độ chuỗi → số chỉ để vẽ; HTX vị trí tạm không có điểm", () => {
    const base = COOPERATIVES[0]!.api;
    const coops: Cooperative[] = [
      { ...base, ref: "a", location: { lat: "10.450001", lon: "105.900002" }, location_provisional: false },
      { ...base, ref: "b", location: null, location_provisional: true },
    ];
    const fc = cooperativePoints(coops);
    expect(fc.features).toHaveLength(1);
    expect(fc.features[0]?.geometry.coordinates).toEqual([105.900002, 10.450001]);
  });

  it("thửa của HTX: FeatureCollection + next_cursor → Page; gom trang bằng cursor", async () => {
    const api = app("tw");
    const coop = COOPERATIVES.find((c) => c.plots > 20)!;
    const first = await cooperativePlotsPage(api, coop.api.ref, undefined, 5);
    expect(first.items).toHaveLength(5);
    expect(first.next_cursor).not.toBeNull();
    const all = await collectPages((cursor) => cooperativePlotsPage(api, coop.api.ref, cursor, 7));
    expect(all.every((p) => p.properties.cooperative_ref === coop.api.ref)).toBe(true);
    expect(new Set(all.map((p) => p.properties.ref)).size).toBe(all.length);
  });

  it("gán thửa nháp: needs_confirm → xác nhận với khóa mới; mọi POST có Idempotency-Key", async () => {
    const { seen, fetch } = recording(mockFetchNoDelay);
    const api = app("tw", fetch);
    const coop = COOPERATIVES.find((c) => c.api.active && c.plots >= 14)!;
    const drafts = await draftPlotsInCommune(api, coop.api.admin_unit_code!);
    expect(drafts.items.length).toBeGreaterThan(0);
    const results = await Promise.all(
      drafts.items.map(async (plot) => {
        const key = newIdempotencyKey();
        try {
          await assignPlot(api, plot.properties.ref, { cooperative_ref: coop.api.ref }, key);
          return "ok";
        } catch (e) {
          const p = e as ApiProblem;
          if (p.code !== "geo.overlap_needs_confirm") return p.code;
          expect(p.traceId).toBeTruthy();
          const done = await assignPlot(api, plot.properties.ref, { cooperative_ref: coop.api.ref, confirm: true }, newIdempotencyKey());
          expect(done.plot.properties.cooperative_ref).toBe(coop.api.ref);
          return "confirmed";
        }
      }),
    );
    expect(results).toContain("confirmed");
    const posts = seen.filter((r) => r.method === "POST");
    expect(posts.length).toBeGreaterThan(0);
    expect(posts.every((r) => !!r.headers.get("Idempotency-Key"))).toBe(true);
    // Lần xác nhận gửi confirm_overlap: true.
    const bodies = await Promise.all(posts.map((r) => r.clone().json() as Promise<{ confirm_overlap?: boolean }>));
    expect(bodies.some((b) => b.confirm_overlap === true)).toBe(true);
  });
});

describe("Admin API (host riêng, MFA bắt buộc)", () => {
  it("chưa qua MFA → problem mfa_required có trace_id và thông báo tiếng Việt", async () => {
    const p = await problemOf(unwrap(admin("admin-no-mfa").GET("/v1/masterdata/params/versions", { params: { query: {} } })));
    expect(p.status).toBe(403);
    expect(p.code).toBe("mfa_required");
    expect(p.traceId).toBeTruthy();
    expect(p.message).toMatch(/xác thực hai yếu tố/);
  });

  it("đề xuất = tạo nháp rồi gửi duyệt; người đặt không tự duyệt được (R-56)", async () => {
    const api = admin("setter");
    const res = await proposeParam(
      api,
      { key: "extension.task_sla_hours", value: "30.5", unit: "giờ", effective_from: "2027-02-01", source_doc: "VB thử 01" },
      newIdempotencyKey(),
    );
    expect(res.draft.status).toBe("draft");
    expect(res.submitted?.status).toBe("submitted");
    expect(res.submitted?.value).toBe("30.5");
    const self = await problemOf(
      unwrap(
        api.POST("/v1/masterdata/params/versions/{id}/approve", {
          params: { path: { id: res.draft.id }, header: { "Idempotency-Key": newIdempotencyKey() } },
          body: {},
        }),
      ),
    );
    expect(self.code).toBe("self_approval_forbidden");
    const ok = await unwrap(
      admin("approver").POST("/v1/masterdata/params/versions/{id}/approve", {
        params: { path: { id: res.draft.id }, header: { "Idempotency-Key": newIdempotencyKey() } },
        body: {},
      }),
    );
    expect(ok.version.status).toBe("approved");
    expect(ok.approved.value).toBe("30.5");
  });

  it("cùng khóa gửi lại → phát lại, không tạo nháp thứ hai", async () => {
    const api = admin("setter");
    const key = newIdempotencyKey();
    const input = { key: "geo.overlap_tolerance_pct", value: "1.5", unit: "%", effective_from: "2027-03-01", source_doc: "VB thử 02" };
    const a = await proposeParam(api, input, key);
    const b = await proposeParam(api, input, key);
    expect(b.draft.id).toBe(a.draft.id);
  });

  it("nhập nông hộ gửi text/csv nguyên văn", async () => {
    const { seen, fetch } = recording(mockAdminFetchNoDelay);
    const code = COOPERATIVES[0]!.api.code;
    const csv = `ho_ten,so_dien_thoai,ma_htx\nNguyen Van A,0912345678,${code}\nB,12,${code}\n`;
    const res = await importHouseholds(admin("setter", fetch), csv, newIdempotencyKey());
    expect(seen[0]?.headers.get("Content-Type")).toMatch(/^text\/csv/);
    expect(await seen[0]?.text()).toBe(csv);
    expect(res.rows.map((r) => r.status)).toEqual(["accepted", "rejected"]);
    expect(res.rows[1]?.reason).not.toMatch(/Nguyen|0912/);
  });
});
