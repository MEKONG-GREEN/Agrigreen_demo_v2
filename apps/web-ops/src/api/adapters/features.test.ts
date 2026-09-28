// Kiểm thử luồng các route mới của đợt hoàn thiện G1 qua backend giả lập (hình dạng theo hợp đồng thật)
// với toàn bộ middleware thật: Bearer, Idempotency-Key, problem+json.
import { describe, expect, it } from "vitest";
import { importHouseholds } from "@/admin/households";
import { deepLinkHref } from "@/lib/deepLink";
import { detectKind, filenameFromDisposition } from "@/lib/files";
import { markAllRead, markRead, unreadCount } from "@/notify/notify";
import { buildPeriod, getExport, requestExport, waitExport } from "@/reports/exports";
import { createAdminApiClient, createApiClient, unwrap } from "../client";
import { newIdempotencyKey } from "../idempotency";
import { ACCOUNTS } from "../mock/admin-ops";
import { COOPERATIVES, ORGS } from "../mock/data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "../mock/handler";
import { DAILY_EXPORT_QUOTA, resetExportQuota } from "../mock/ops";
import { mockToken } from "../mock/personas";
import { ApiProblem, parseRetryAfter } from "../problem";
import { cooperativesPage, householdsPage } from "./cooperatives";

const app = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
const admin = (persona: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });
const key = () => ({ "Idempotency-Key": newIdempotencyKey() });

async function problemOf(p: Promise<unknown>): Promise<ApiProblem> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiProblem) return e;
    throw e;
  }
  throw new Error("mong đợi ApiProblem");
}

describe("deep_link của thông báo → trang web", () => {
  it("ánh xạ đúng 5 loại đường dẫn; đường dẫn lạ → null", () => {
    const id = "01927b3e-d1a0-7000-8000-000000000001";
    expect(deepLinkHref(`/extension/directives/${id}`)).toBe(`/chi-dao?id=${id}`);
    expect(deepLinkHref(`/documents/${id}`)).toBe(`/thu-vien?doc=${id}`);
    expect(deepLinkHref(`/extension/tasks/${id}`)).toBe(`/nhiem-vu?task=${id}`);
    expect(deepLinkHref(`/sync/conflicts/${id}`)).toBe(`/quan-tri/xung-dot?id=${id}`);
    expect(deepLinkHref(`/orgs/${id}`)).toBe(`/quan-tri/to-chuc?org=${id}`);
    expect(deepLinkHref("https://evil.example/x")).toBeNull();
    expect(deepLinkHref("/extension/directives/../../admin")).toBeNull();
    expect(deepLinkHref(null)).toBeNull();
  });
});

describe("tệp và Retry-After", () => {
  it("nhận dạng .xlsx/.zip theo chữ ký, .xls cũ, CSV theo đuôi", () => {
    const pk = new Uint8Array([0x50, 0x4b, 3, 4]);
    expect(detectKind("a.xlsx", pk)).toBe("xlsx");
    expect(detectKind("thua.zip", pk)).toBe("zip");
    expect(detectKind("cu.xls", new Uint8Array([0xd0, 0xcf, 0x11, 0xe0]))).toBe("xls");
    expect(detectKind("a.csv", new Uint8Array([0x68]))).toBe("csv");
    expect(filenameFromDisposition("attachment; filename*=UTF-8''c%C6%A1-c%E1%BA%A5u.xlsx", "x")).toBe("cơ-cấu.xlsx");
    expect(filenameFromDisposition('attachment; filename="a.csv"', "x")).toBe("a.csv");
  });

  it("Retry-After dạng giây hoặc HTTP-date", () => {
    expect(parseRetryAfter("120")).toBe(120);
    const now = Date.parse("2026-09-28T00:00:00Z");
    expect(parseRetryAfter("Mon, 28 Sep 2026 00:01:00 GMT", now)).toBe(60);
    expect(parseRetryAfter("abc")).toBeUndefined();
  });
});

describe("pf/party — G-03/G-04", () => {
  it("lọc HTX theo admin_unit (tỉnh hoặc xã) và trả tên xã/tỉnh, số hộ, số thửa số hoá", async () => {
    const page = await cooperativesPage(app("tw"), { admin_unit: "91", limit: 200 });
    expect(page.items.length).toBeGreaterThan(0);
    expect(page.items.every((c) => c.province_code === "91" && c.province_name === "An Giang")).toBe(true);
    expect(page.items.every((c) => typeof c.member_households === "number" && (c.commune_name ?? "").startsWith("Xã "))).toBe(true);
    // Ngoài phạm vi → danh sách rỗng, không lỗi.
    expect((await cooperativesPage(app("tinh"), { admin_unit: "91" })).items).toEqual([]);
  });

  it("nông hộ kèm trạng thái đồng ý theo mục đích", async () => {
    const coop = COOPERATIVES.find((c) => c.member_households > 10)!;
    const page = await householdsPage(app("tw"), coop.api.ref, undefined, 30);
    const withConsent = page.items.filter((h) => (h.consents ?? []).length > 0);
    expect(withConsent.length).toBeGreaterThan(0);
    expect(withConsent.flatMap((h) => h.consents ?? []).every((c) => ["granted", "withdrawn"].includes(c.status))).toBe(true);
  });
});

describe("pf/notify — hộp thư, đồng bộ ngay", () => {
  it("đếm chưa đọc, đọc một, đọc tất cả", async () => {
    const api = app("xa");
    const before = await unreadCount(api);
    expect(before).toBeGreaterThan(0);
    const list = await unwrap(api.GET("/v1/notifications", { params: { query: { unread: true } } }));
    const read = await markRead(api, list.items[0]!.id);
    expect(read.read_at).not.toBeNull();
    expect(await unreadCount(api)).toBe(before - 1);
    expect(await markAllRead(api)).toBe(before - 1);
    expect(await unreadCount(api)).toBe(0);
  });

  it("đồng bộ ngay: 202, gọi lại trong 5 phút → 429 kèm Retry-After; ngoài phạm vi → 403", async () => {
    const api = app("tinh");
    const ok = await unwrap(api.POST("/v1/sync/nudge", { params: { header: key() }, body: { unit: "82102" } }));
    expect(ok.target_kind).toBe("unit");
    const limited = await problemOf(unwrap(api.POST("/v1/sync/nudge", { params: { header: key() }, body: { unit: "82102" } })));
    expect(limited.status).toBe(429);
    expect(limited.code).toBe("notify.nudge_rate_limited");
    expect(limited.retryAfterS).toBeGreaterThan(250);
    const out = await problemOf(unwrap(api.POST("/v1/sync/nudge", { params: { header: key() }, body: { unit: "91101" } })));
    expect(out.code).toBe("notify.out_of_scope");
    const xa = await problemOf(unwrap(app("xa").POST("/v1/sync/nudge", { params: { header: key() }, body: { unit: "82101" } })));
    expect(xa.code).toBe("forbidden");
  });
});

describe("pf/reports — xuất bất đồng bộ", () => {
  it("202 → queued → completed (tạm tính khi còn xã chưa báo cáo) → URL tải", async () => {
    const api = app("tinh");
    const job = await requestExport(api, { template_key: "kn.sowing_progress", format: "xlsx", params: { period: buildPeriod("month", "2026", "9", "3") } }, newIdempotencyKey());
    expect(job.status).toBe("queued");
    expect(job.params.period).toBe("2026-09");
    const notReady = await problemOf(unwrap(api.GET("/v1/reports/exports/{id}/download", { params: { path: { id: job.id } } })));
    expect(notReady.code).toBe("reports.export_not_ready");
    const done = await waitExport(api, job.id, { intervalMs: 100, timeoutMs: 5000 });
    expect(done.status).toBe("completed");
    expect(done.provisional).toBe(true);
    const dl = await unwrap(api.GET("/v1/reports/exports/{id}/download", { params: { path: { id: job.id } } }));
    expect(dl.file_name).toMatch(/kn\.sowing_progress/);
    // Người khác (không bao trọn phạm vi) không xem được việc xuất.
    expect((await problemOf(getExport(app("xa"), job.id))).code).toBe("reports.export_not_found");
  });

  it("định dạng mẫu không hỗ trợ → 422; vượt hạn mức ngày → 429 reports.export_quota_exceeded", async () => {
    const api = app("tw");
    resetExportQuota("tw");
    const unsupported = await problemOf(requestExport(api, { template_key: "kn.tasks_sla", format: "pdf", params: {} }, newIdempotencyKey()));
    expect(unsupported.code).toBe("reports.format_unsupported");
    for (let i = 0; i < DAILY_EXPORT_QUOTA; i++) await requestExport(api, { template_key: "kn.tasks_sla", format: "xlsx", params: {} }, newIdempotencyKey());
    const quota = await problemOf(requestExport(api, { template_key: "kn.tasks_sla", format: "xlsx", params: {} }, newIdempotencyKey()));
    expect(quota.status).toBe(429);
    expect(quota.message).toMatch(/hạn mức xuất báo cáo/);
    resetExportQuota("tw");
  });
});

describe("pf/accounts — Admin API", () => {
  it("quản trị tỉnh chỉ cấp kn_officer/coop_manager; lỗi backend trả về nguyên mã", async () => {
    const api = admin("account-tinh");
    const grants = await unwrap(api.GET("/v1/accounts/grants"));
    expect(grants.grantable).toEqual(["kn_officer", "coop_manager"]);
    const org = ORGS.find((o) => o.kind === "extension_team" && o.admin_unit_code === "82102")!;
    const denied = await problemOf(
      unwrap(api.POST("/v1/accounts/users", { params: { header: key() }, body: { email: "moi@example.test", org_ref: org.ref, roles: ["kn_province_admin"] } })),
    );
    expect(denied.code).toBe("accounts.role_not_grantable");
    const coopRole = await problemOf(
      unwrap(api.POST("/v1/accounts/users", { params: { header: key() }, body: { email: "moi2@example.test", org_ref: org.ref, roles: ["coop_manager"] } })),
    );
    expect(coopRole.code).toBe("validation_failed");
    expect(coopRole.fieldErrors[0]?.reason).toBe("Vui lòng chọn HTX liên kết.");
    const created = await unwrap(
      api.POST("/v1/accounts/users", { params: { header: key() }, body: { email: "knv.moi@example.test", full_name: "Nguyễn Văn Mới", org_ref: org.ref, roles: ["kn_officer"] } }),
    );
    expect(created.user.required_actions).toContain("UPDATE_PASSWORD");
    const outOfScope = ORGS.find((o) => o.kind === "extension_team" && o.admin_unit_code?.startsWith("91"))!;
    const scope = await problemOf(
      unwrap(api.POST("/v1/accounts/users", { params: { header: key() }, body: { email: "x91@example.test", org_ref: outOfScope.ref, roles: ["kn_officer"] } })),
    );
    expect(scope.code).toBe("accounts.out_of_scope");
  });

  it("khóa cần lý do ≥ 20 ký tự; khóa lần hai → 409; chưa MFA → mfa_required", async () => {
    const api = admin("setter");
    const target = ACCOUNTS.find((a) => a.user.enabled)!.user;
    const short = await problemOf(unwrap(api.POST("/v1/accounts/users/{id}/lock", { params: { path: { id: target.id }, header: key() }, body: { reason: "ngắn quá" } })));
    expect(short.code).toBe("validation_failed");
    expect(short.fieldErrors[0]?.field).toBe("reason");
    const ok = await unwrap(api.POST("/v1/accounts/users/{id}/lock", { params: { path: { id: target.id }, header: key() }, body: { reason: "Cán bộ chuyển công tác sang đơn vị khác" } }));
    expect(ok.account.enabled).toBe(false);
    const again = await problemOf(unwrap(api.POST("/v1/accounts/users/{id}/lock", { params: { path: { id: target.id }, header: key() }, body: { reason: "Cán bộ chuyển công tác sang đơn vị khác" } })));
    expect(again.code).toBe("accounts.state_conflict");
    const events = await unwrap(api.GET("/v1/accounts/users/{id}/events", { params: { path: { id: target.id } } }));
    expect(events.items.at(-1)?.action).toBe("locked");
    expect((await problemOf(unwrap(admin("admin-no-mfa").GET("/v1/accounts/users", { params: { query: {} } })))).code).toBe("mfa_required");
  });
});

describe("pf/sync — đối soát xung đột (Admin)", () => {
  it("lọc theo SLA; giữ phiên bản phải chọn bản ghi của xung đột; đóng lần hai → 409", async () => {
    const api = admin("setter");
    const overdue = await unwrap(api.GET("/v1/sync/conflicts", { params: { query: { sla: "overdue" } } }));
    expect(overdue.items.length).toBeGreaterThan(0);
    expect(overdue.items.every((c) => c.sla_status === "overdue")).toBe(true);
    const id = overdue.items[0]!.conflict_id;
    const detail = await unwrap(api.GET("/v1/sync/conflicts/{id}", { params: { path: { id } } }));
    expect(detail.versions.length).toBe(2);
    const bad = await problemOf(
      unwrap(api.POST("/v1/sync/conflicts/{id}/resolve", { params: { path: { id }, header: key() }, body: { note: "Giữ bản hiện hành", resolution: "keep_version", resolved_record_id: newIdempotencyKey() } })),
    );
    expect(bad.code).toBe("sync.conflict_resolution_invalid");
    const keep = detail.records[1]!.record_id!;
    const done = await unwrap(
      api.POST("/v1/sync/conflicts/{id}/resolve", { params: { path: { id }, header: key() }, body: { note: "Giữ bản hiện hành sau khi đối chiếu", resolution: "keep_version", resolved_record_id: keep } }),
    );
    expect(done.status).toBe("resolved");
    const twice = await problemOf(unwrap(api.POST("/v1/sync/conflicts/{id}/resolve", { params: { path: { id }, header: key() }, body: { note: "Đóng lại lần nữa để thử" } })));
    expect(twice.code).toBe("sync.conflict_state");
  });
});

describe("thư viện cá nhân, chỉ đạo, dự báo, nhập .xlsx", () => {
  it("lưu/bỏ lưu tài liệu; bỏ lưu tài liệu chưa lưu → 404 content.library_item_not_found", async () => {
    const api = app("xa");
    const docs = await unwrap(api.GET("/v1/documents", { params: { query: { limit: 3 } } }));
    const ref = docs.items[2]!.ref;
    await unwrap(api.POST("/v1/documents/{ref}/save", { params: { path: { ref }, header: key() }, body: { note: "đọc sau" } }));
    expect((await unwrap(api.GET("/v1/library"))).items.some((i) => i.document_ref === ref)).toBe(true);
    await unwrap(api.POST("/v1/documents/{ref}/unsave", { params: { path: { ref }, header: key() } }));
    const again = await problemOf(unwrap(api.POST("/v1/documents/{ref}/unsave", { params: { path: { ref }, header: key() } })));
    expect(again.code).toBe("content.library_item_not_found");
  });

  it("chỉ đạo: người nhận xác nhận, tỷ lệ tăng; vùng không có cán bộ → 422; KNV xã không ban hành được", async () => {
    const xa = app("xa");
    const mine = await unwrap(xa.GET("/v1/extension/directives", { params: { query: { mine: true } } }));
    const d = mine.items.find((x) => x.is_target && !x.acked_at)!;
    const acked = await unwrap(xa.POST("/v1/extension/directives/{id}/ack", { params: { path: { id: d.directive_id }, header: key() } }));
    expect(acked.ack_count).toBe(d.ack_count + 1);
    expect(acked.acked_at).not.toBeNull();
    const tw = app("tw");
    const empty = await problemOf(unwrap(tw.POST("/v1/extension/directives", { params: { header: key() }, body: { title: "Thử vùng trống", body: "Nội dung đủ mười ký tự", audience_units: ["80"] } })));
    expect(empty.code).toBe("extension.no_directive_targets");
    const forbidden = await problemOf(unwrap(xa.POST("/v1/extension/directives", { params: { header: key() }, body: { title: "Không được", body: "Nội dung đủ mười ký tự", audience_units: ["82101"] } })));
    expect(forbidden.code).toBe("forbidden");
  });

  it("dự báo CGH: cgh_admin chạy (Admin), Cục đọc (Application), phương pháp tạm", async () => {
    const res = await unwrap(admin("setter").POST("/v1/mechanization/forecasts", { params: { header: key() }, body: { season_code: "HT", season_year: 2027 } }));
    const id = (res.run as { id: string }).id;
    const f = await unwrap(app("cuc").GET("/v1/mechanization/forecasts/{id}", { params: { path: { id }, query: { farm_stage: "thu_hoach" } } }));
    expect(f.run.method_provisional).toBe(true);
    expect(f.run.rule_version).toBe("cgh.forecast/v1");
    expect(f.results.every((r) => r.farm_stage === "thu_hoach")).toBe(true);
    const denied = await problemOf(unwrap(admin("approver").POST("/v1/mechanization/forecasts", { params: { header: key() }, body: { season_code: "HT", season_year: 2027 } })));
    expect(denied.code).toBe("forbidden");
  });

  it("nhập nông hộ từ .xlsx gửi byte nguyên văn với Content-Type xlsx", async () => {
    const seen: Request[] = [];
    const api = createAdminApiClient({
      baseUrl: "https://admin.test",
      getAccessToken: () => mockToken("setter"),
      fetch: async (r) => {
        seen.push(r.clone());
        return mockAdminFetchNoDelay(r);
      },
    });
    const bytes = new Uint8Array([0x50, 0x4b, 3, 4, 1, 2, 3]);
    const res = await importHouseholds(api, { kind: "xlsx", name: "ho.xlsx", size: bytes.length, body: bytes, contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" }, newIdempotencyKey());
    expect(seen[0]?.headers.get("Content-Type")).toMatch(/spreadsheetml/);
    expect(new Uint8Array(await seen[0]!.arrayBuffer())).toEqual(bytes);
    expect(res.rows.length).toBeGreaterThan(0);
  });
});
