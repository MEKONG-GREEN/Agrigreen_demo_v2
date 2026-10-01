import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { CLIENT_IDS, g5MockSnapshot, resetG5Mock, statementCsv } from "@/api/mock/g5";
import { mockAdminFetchNoDelay } from "@/api/mock/handler";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { ApiProblem, PROBLEM_MESSAGES, problemMessage } from "@/api/problem";
import type { ApiKeyRecord, MeteringStatement } from "@/api/types";
import { buildPrincipal, type Principal } from "@/auth/principal";
import { canSee, findNavItem } from "@/nav/nav";
import { formatCount, formatUnitPrice, formatVnd, lineAmount, parseDecimal, sumMoney } from "@/metering/money";
import { describeParam, paramValue } from "@/metering/ParamsTable";
import { filterByCounterparty, periodRange, recentPeriods, summarizeUsage } from "@/metering/usage";
import {
  approveBlock,
  approveStatementBlock,
  effectivePlan,
  isKeyEffective,
  issueKeyBlock,
  lastEndedPeriod,
  periodEnded,
  reasonError,
  validateClientProposal,
  validatePlan,
  validateSubscription,
} from "./rules";
import { SecretOnceDialog } from "./SecretOnceDialog";

function persona(id: string): Principal {
  const p = MOCK_PERSONAS.find((x) => x.id === id);
  if (!p) throw new Error(id);
  return buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
}
const admin = (p: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(p), fetch: mockAdminFetchNoDelay });
const hdr = () => ({ "Idempotency-Key": newIdempotencyKey() });

async function problemOf(p: Promise<unknown>): Promise<ApiProblem> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiProblem) return e;
    throw e;
  }
  throw new Error("expected problem");
}

// useAuth của các panel trỏ tới persona hiện tại của kiểm thử.
const auth = vi.hoisted(() => ({ persona: "api-admin" }));
vi.mock("@/auth/AuthProvider", async () => {
  const { createAdminApiClient: mk } = await import("@/api/client");
  const { mockAdminFetchNoDelay: f } = await import("@/api/mock/handler");
  const { MOCK_PERSONAS: ps, mockToken: tok } = await import("@/api/mock/personas");
  const { buildPrincipal: bp } = await import("@/auth/principal");
  // Giá trị ổn định theo persona (như context thật) để deps của useAsync không đổi mỗi lần render.
  const cache = new Map<string, unknown>();
  return {
    useAuth: () => {
      const p = ps.find((x) => x.id === auth.persona)!;
      if (!cache.has(p.id))
        cache.set(p.id, {
          adminApi: mk({ baseUrl: "https://admin.test", getAccessToken: () => tok(p.id), fetch: f }),
          principal: bp({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me),
        });
      return cache.get(p.id);
    },
  };
});

beforeEach(() => {
  resetG5Mock();
  auth.persona = "api-admin";
});

describe("tiền VND dạng chuỗi thập phân — không float (ADR-0023, R-10)", () => {
  it("định dạng vi-VN: nhóm nghìn bằng dấu chấm, thập phân bằng dấu phẩy, bỏ số 0 thừa", () => {
    expect(formatVnd("1055518.50")).toBe("1.055.518,5 ₫");
    expect(formatVnd("1000000.00")).toBe("1.000.000 ₫");
    expect(formatVnd("0.00")).toBe("0 ₫");
    expect(formatVnd("999")).toBe("999 ₫");
    expect(formatVnd("1234567.891", { currency: false })).toBe("1.234.567,89");
    expect(formatVnd("-2500.5")).toBe("-2.500,5 ₫");
    expect(formatVnd("-0.001")).toBe("0 ₫");
  });

  it("giá trị lớn hơn 2^53 vẫn chính xác (BigInt, không qua Number)", () => {
    expect(formatVnd("123456789012345678.99")).toBe("123.456.789.012.345.678,99 ₫");
    expect(formatVnd("9007199254740993.01", { currency: false })).toBe("9.007.199.254.740.993,01");
  });

  it("làm tròn nửa lên khi cắt chữ số; đơn giá 4 chữ số", () => {
    expect(formatVnd("10.005")).toBe("10,01 ₫");
    expect(formatVnd("10.0049")).toBe("10 ₫");
    expect(formatVnd("99.995")).toBe("100 ₫");
    expect(formatUnitPrice("1500.5000")).toBe("1.500,5 ₫");
    expect(formatUnitPrice("0.1234")).toBe("0,1234 ₫");
  });

  it("chuỗi trống, null, sai dạng (kể cả ký hiệu mũ, dấu phẩy) → “—”", () => {
    for (const v of [null, undefined, "", "  ", "1e5", "1,5", "abc", "1.2.3", "NaN"]) expect(formatVnd(v)).toBe("—");
    expect(parseDecimal("007.50")).toEqual({ negative: false, int: "7", frac: "50" });
  });

  it("thành tiền = đơn giá numeric(18,4) × đơn vị, làm tròn về numeric(18,2); tổng cộng chính xác", () => {
    expect(lineAmount("1500.5000", 37)).toBe("55518.50");
    expect(lineAmount("0.0050", 1)).toBe("0.01");
    expect(lineAmount("0.0049", 1)).toBe("0.00");
    expect(lineAmount("1500.5000", 0)).toBe("0.00");
    expect(sumMoney(["55518.50", "0.00", "1000000.00"])).toBe("1055518.50");
    expect(sumMoney(["0.10", "0.20"])).toBe("0.30");
    expect(formatCount(1234567)).toBe("1.234.567");
  });

  it("tham số đo lường: giá hiển thị là tiền, ngưỡng là số; chưa đặt → “Chưa đặt”", () => {
    expect(paramValue("metering.price.default.collection_volume.per_unit_vnd", "1500.5")).toBe("1.500,5 ₫");
    expect(paramValue("metering.egress.valve.anomaly_records", "20000")).toBe("20.000");
    expect(paramValue("metering.price.default.subscription_vnd_per_month", null)).toBe("Chưa đặt");
    expect(describeParam("metering.egress.public_api.anomaly_factor")).toMatch(/Public API — Hệ số bất thường/);
    expect(describeParam("metering.price.default.hub_capacity.per_unit_vnd")).toMatch(/Đơn giá.*hub_capacity/);
  });
});

describe("quy tắc hiển thị khách hàng Public API (ADR-0022, R-56)", () => {
  const A = persona("api-admin");
  const B = persona("api-approver");
  const viewer = persona("metering-viewer");

  it("tự duyệt bị khóa kèm gợi ý; người khác có vai duyệt thì được; không có vai/không MFA thì nêu lý do", () => {
    const rec = { status: "proposed", proposed_by: A.subject };
    expect(approveBlock(rec, A)).toMatch(/khác người đề xuất \(R-56\)/);
    expect(approveBlock(rec, B)).toBeNull();
    expect(approveBlock(rec, viewer)).toMatch(/api_client_approver/);
    expect(approveBlock({ ...rec, status: "active" }, B)).toMatch(/không còn chờ duyệt/);
    expect(approveBlock(rec, { ...B, mfa: false })).toMatch(/hai yếu tố/);
  });

  it("phát hành khóa: chỉ khách hàng active, tối đa 2 khóa hiệu lực (khóa đã xoay còn chồng lấn vẫn tính)", () => {
    const now = Date.parse("2026-10-02T00:00:00Z");
    const k = (status: ApiKeyRecord["status"], grace: string | null = null): ApiKeyRecord => ({
      id: status + (grace ?? ""),
      key_prefix: "abcdefghijkl",
      status,
      issued_at: "2026-01-01T00:00:00Z",
      expires_at: "2027-01-01T00:00:00Z",
      grace_until: grace,
    });
    expect(isKeyEffective(k("rotated", "2026-10-05T00:00:00Z"), now)).toBe(true);
    expect(isKeyEffective(k("rotated", "2026-09-30T00:00:00Z"), now)).toBe(false);
    expect(isKeyEffective({ ...k("active"), expires_at: "2026-10-01T00:00:00Z" }, now)).toBe(false);
    expect(issueKeyBlock({ status: "active" }, [k("active")], A, now)).toBeNull();
    expect(issueKeyBlock({ status: "active" }, [k("active"), k("rotated", "2026-10-05T00:00:00Z")], A, now)).toMatch(/tối đa 2 khóa/);
    expect(issueKeyBlock({ status: "suspended" }, [], A, now)).toMatch(/đang hoạt động/);
    expect(issueKeyBlock({ status: "active" }, [], B, now)).toMatch(/api_client_admin/);
  });

  it("kiểm đề xuất như hợp đồng: UUID tenant/đầu mối, độ dài, OAuth cần mã client; scope theo hợp đồng sản phẩm; gói hạn mức", () => {
    const ok = { client_tenant_id: CLIENT_IDS.factory, display_name: "Tổ chức X", purpose: "Mục đích đủ mười ký tự", contact_ref: CLIENT_IDS.buyer, auth_methods: ["api_key" as const] };
    expect(validateClientProposal(ok)).toEqual([]);
    expect(validateClientProposal({ ...ok, contact_ref: "Nguyễn Văn A 0901234567" }).map((e) => e.field)).toEqual(["contact_ref"]);
    expect(validateClientProposal({ ...ok, auth_methods: ["oauth_client_credentials"] }).map((e) => e.field)).toEqual(["oauth_client_id"]);
    expect(validateSubscription("mrv_package_status", ["dp:mrv_package_status:export"])).toHaveLength(1);
    expect(validateSubscription("collection_volume", ["dp:collection_volume:read", "dp:collection_volume:export"])).toEqual([]);
    expect(validatePlan({ requests_per_minute: 60, daily_units: 10_000, monthly_units: 5_000, max_export_rows: 1 }).map((e) => e.field)).toEqual(["monthly_units"]);
    expect(validatePlan({ requests_per_minute: 0.5, daily_units: 1, monthly_units: 1, max_export_rows: 1 }).map((e) => e.field)).toEqual(["requests_per_minute"]);
    expect(reasonError("ngắn")).toMatch(/10 ký tự/);
    expect(reasonError("Lý do đủ dài để ghi sổ")).toBeNull();
  });

  it("gói đang áp dụng = bản đã duyệt mới nhất đã tới ngày hiệu lực", () => {
    const p = (version: number, status: "approved" | "proposed", eff: string) =>
      ({ version, status, effective_from: eff, requests_per_minute: 1, daily_units: 1, monthly_units: 1, max_export_rows: 1, proposed_by: "x", proposed_at: eff }) as const;
    const now = Date.parse("2026-10-02T00:00:00Z");
    expect(effectivePlan([p(1, "approved", "2026-01-01T00:00:00Z"), p(2, "approved", "2026-12-01T00:00:00Z"), p(3, "proposed", "2026-02-01T00:00:00Z")], now)?.version).toBe(1);
  });

  it("bảng kê: kỳ đã kết thúc theo giờ VN; duyệt ≠ người yêu cầu; đã duyệt/đã bị thay thì đóng băng", () => {
    expect(lastEndedPeriod(new Date("2026-10-02T03:00:00Z"))).toBe("2026-09");
    expect(lastEndedPeriod(new Date("2026-01-01T01:00:00Z"))).toBe("2025-12");
    expect(periodEnded("2026-09", new Date("2026-09-30T16:59:59Z"))).toBe(false);
    expect(periodEnded("2026-09", new Date("2026-09-30T17:00:00Z"))).toBe(true);
    const st = { status: "draft" as const, requested_by: A.subject, superseded_by: null };
    expect(approveStatementBlock(st, A)).toMatch(/khác người yêu cầu/);
    expect(approveStatementBlock(st, B)).toBeNull();
    expect(approveStatementBlock({ ...st, status: "approved" }, B)).toMatch(/đóng băng/);
    expect(approveStatementBlock({ ...st, superseded_by: "x" }, B)).toMatch(/bảng kê bù/);
  });

  it("tổng sử dụng theo sản phẩm, khoảng ngày của kỳ, lọc bên nhận", () => {
    const s = summarizeUsage([
      { date: "2026-09-01", product: "b", calls: 3, units: 2 },
      { date: "2026-09-02", product: "a", calls: 5, units: 5 },
      { date: "2026-09-03", product: "b", calls: 1, units: 1 },
    ]);
    expect(s.products.map((p) => [p.product, p.calls, p.units, p.days])).toEqual([
      ["a", 5, 5, 1],
      ["b", 4, 3, 2],
    ]);
    expect([s.calls, s.units]).toEqual([9, 8]);
    expect(periodRange("2026-02", "2026-10-02")).toEqual({ from: "2026-02-01", to: "2026-02-28" });
    expect(periodRange("2026-10", "2026-10-02")).toEqual({ from: "2026-10-01", to: "2026-10-02" });
    expect(recentPeriods("2026-01-15", 3)).toEqual(["2026-01", "2025-12", "2025-11"]);
    const ev = [{ counterparty_ref: "0192a5c0-aaaa" }, { counterparty_ref: "ffff" }] as never[];
    expect(filterByCounterparty(ev, "0192A5").length).toBe(1);
    expect(filterByCounterparty(ev, "nhà máy", (e: { counterparty_ref: string }) => (e.counterparty_ref === "ffff" ? "Nhà máy X" : "")).length).toBe(1);
  });

  it("menu theo vai G5: khách hàng API cho admin/approver; đo lường cho viewer/billing; nhật ký xuất cho viewer/platform_admin", () => {
    const see = (href: string, id: string) => canSee(findNavItem(href)!, persona(id));
    expect(see("/quan-tri/khach-hang-api", "api-admin")).toBe(true);
    expect(see("/quan-tri/khach-hang-api", "api-approver")).toBe(true);
    expect(see("/quan-tri/khach-hang-api", "metering-viewer")).toBe(false);
    expect(see("/quan-tri/khach-hang-api", "setter")).toBe(false);
    expect(see("/quan-tri/bang-ke", "metering-viewer")).toBe(true);
    expect(see("/quan-tri/su-dung-api", "tinh")).toBe(false);
    expect(see("/quan-tri/nhat-ky-xuat", "setter")).toBe(true);
    expect(see("/quan-tri/nhat-ky-xuat", "api-approver")).toBe(true);
    expect(see("/quan-tri/nhat-ky-xuat", "approver")).toBe(false);
  });
});

describe("mã lỗi G5 có thông báo tiếng Việt (docs/errors.md)", () => {
  it("mọi mã apiclient.* và metering.* trong docs/errors.md đều có nhãn", () => {
    const doc = readFileSync(resolve(process.cwd(), "../../docs/errors.md"), "utf-8");
    const codes = new Set([...doc.matchAll(/`((?:apiclient|metering)\.[a-z_]+)`/g)].map((m) => m[1]!));
    expect(codes.size).toBeGreaterThanOrEqual(25);
    const missing = [...codes].filter((c) => !PROBLEM_MESSAGES[c]);
    expect(missing).toEqual([]);
    for (const c of codes) expect(PROBLEM_MESSAGES[c]).toMatch(/[ạảãáàâấầẩẫậăắằẳẵặđêếềểễệôốồổỗộơớờởỡợưứừửữựíìỉĩịúùủũụóòỏõọéèẻẽẹýỳỷỹỵ]/);
    expect(problemMessage({ code: "apiclient.self_approval", status: 403, title: "Self approval" })).toMatch(/R-56/);
  });
});

describe("backend giả lập G5 — quy tắc như app/", () => {
  it("R-56: người đề xuất không tự duyệt khách hàng (403 apiclient.self_approval); người khác duyệt được", async () => {
    const c = await unwrap(
      admin("api-admin").POST("/v1/api-clients", {
        params: { header: hdr() },
        body: { client_tenant_id: "0192a5b0-0000-7000-8000-0000000000b9", display_name: "Tổ chức thử", purpose: "Kiểm thử quy tắc không tự duyệt", contact_ref: CLIENT_IDS.buyer },
      }),
    );
    expect(c.status).toBe("proposed");
    const p = await problemOf(unwrap(admin("api-admin").POST("/v1/api-clients/{id}/approve", { params: { path: { id: c.id }, header: hdr() } })));
    expect([p.status, p.code]).toEqual([403, "apiclient.self_approval"]);
    expect(p.message).toBe(PROBLEM_MESSAGES["apiclient.self_approval"]);
    const ok = await unwrap(admin("api-approver").POST("/v1/api-clients/{id}/approve", { params: { path: { id: c.id }, header: hdr() } }));
    expect(ok.status).toBe("active");
    const viewer = await problemOf(unwrap(admin("metering-viewer").GET("/v1/api-clients")));
    expect(viewer.code).toBe("forbidden");
  });

  it("khóa API: bí mật chỉ ở phản hồi 201 đầu, gọi lặp cùng Idempotency-Key không có bí mật, mock không giữ bí mật", async () => {
    const key = newIdempotencyKey();
    const first = await unwrap(admin("api-admin").POST("/v1/api-clients/{id}/keys", { params: { path: { id: CLIENT_IDS.factory }, header: { "Idempotency-Key": key } } }));
    expect(first.api_key).toMatch(/^agk_[a-z2-7]{12}_[A-Za-z0-9_-]{43}$/);
    expect(first.api_key?.startsWith(`agk_${first.key_prefix}_`)).toBe(true);
    const replay = await unwrap(admin("api-admin").POST("/v1/api-clients/{id}/keys", { params: { path: { id: CLIENT_IDS.factory }, header: { "Idempotency-Key": key } } }));
    expect(replay.key_id).toBe(first.key_id);
    expect(replay.api_key).toBeUndefined();
    const secretPart = first.api_key!.split("_").pop()!;
    expect(g5MockSnapshot()).not.toContain(secretPart);
    expect(JSON.stringify(window.sessionStorage)).not.toContain(secretPart);
    const list = await unwrap(admin("api-admin").GET("/v1/api-clients/{id}/keys", { params: { path: { id: CLIENT_IDS.factory } } }));
    expect(JSON.stringify(list)).not.toContain(secretPart);
    // Đã có 2 khóa hiệu lực → 409 apiclient.too_many_keys.
    const tooMany = await problemOf(unwrap(admin("api-admin").POST("/v1/api-clients/{id}/keys", { params: { path: { id: CLIENT_IDS.factory }, header: hdr() } })));
    expect(tooMany.code).toBe("apiclient.too_many_keys");
  });

  it("bảng kê: kỳ chưa kết thúc → 409; 202 + job → hoàn tất; người yêu cầu không tự duyệt; CSV tất định như domain.StatementCSV", async () => {
    const today = new Date();
    const thisMonth = `${today.getUTCFullYear()}-${String(today.getUTCMonth() + 1).padStart(2, "0")}`;
    const notEnded = await problemOf(unwrap(admin("api-admin").POST("/v1/metering/statements", { params: { header: hdr() }, body: { period: thisMonth } })));
    expect(notEnded.code).toBe("metering.period_not_ended");
    const period = lastEndedPeriod();
    const job = await unwrap(admin("api-admin").POST("/v1/metering/statements", { params: { header: hdr() }, body: { period, client_id: CLIENT_IDS.verifier } }));
    expect(job.status).toBe("queued");
    let j = job;
    for (let i = 0; i < 3 && j.status === "queued"; i++) j = await unwrap(admin("api-admin").GET("/v1/metering/statement-jobs/{id}", { params: { path: { id: job.id } } }));
    expect(j.status).toBe("completed");
    const sid = j.statement_ids[0]!;
    const self = await problemOf(unwrap(admin("api-admin").POST("/v1/metering/statements/{id}/approve", { params: { path: { id: sid }, header: hdr() } })));
    expect(self.code).toBe("metering.self_approval");
    const ok = await unwrap(admin("api-approver").POST("/v1/metering/statements/{id}/approve", { params: { path: { id: sid }, header: hdr() } }));
    expect(ok.status).toBe("approved");
    const frozen = await problemOf(unwrap(admin("api-admin").POST("/v1/metering/statements/{id}/approve", { params: { path: { id: sid }, header: hdr() } })));
    expect(frozen.code).toBe("metering.statement_frozen");
    const res = await admin("api-approver").GET("/v1/metering/statements/{id}/export.csv", { params: { path: { id: sid } }, parseAs: "text" });
    expect(res.response.headers.get("AG-Snapshot-SHA256")).toBe(ok.snapshot_sha256);
    const text = res.data as string;
    expect(text.split("\n")[0]).toBe(
      "statement_id,client_id,client_tenant_id,period,status,product,status_class,calls,units,unit_price_vnd,amount_vnd,price_version,snapshot_sha256,chain_head_hash,first_seq,last_seq,rule_version,supersedes_id",
    );
    expect(text).toContain(",_total,");
    expect(text.endsWith("\n")).toBe(true);
    expect(statementCsv(ok)).toBe(text);
    const verify = await unwrap(admin("api-approver").GET("/v1/metering/statements/{id}/verify", { params: { path: { id: sid } } }));
    expect(verify.match).toBe(true);
    // Lần tải CSV ghi một dòng sổ xuất kênh report_export.
    const eg = await unwrap(admin("metering-viewer").GET("/v1/metering/egress", { params: { query: { channel: "report_export", limit: 5 } } }));
    expect(eg.items[0]?.product).toBe("metering_statement");
  });

  it("CSV có giá: thành tiền và tổng ở dạng thập phân cố định", () => {
    const st = {
      id: "s",
      client_id: "c",
      client_tenant_id: "t",
      period: "2026-08",
      status: "approved",
      lines: [
        { product: "hub_capacity", status_class: "2xx", calls: 3, units: 3, unit_price_vnd: null, amount_vnd: null, price_version: null },
        { product: "collection_volume", status_class: "2xx", calls: 37, units: 37, unit_price_vnd: "1500.5000", amount_vnd: lineAmount("1500.5000", 37), price_version: "p@v1" },
      ],
      subscription_vnd: null,
      total_vnd: "55518.50",
      first_seq: 1,
      last_seq: 40,
      chain_head_hash: "h",
      snapshot_sha256: "x",
      rule_version: "metering.statement.v1",
      supersedes_id: null,
    } as unknown as MeteringStatement;
    const rows = statementCsv(st).trimEnd().split("\n");
    expect(rows[1]).toContain(",collection_volume,2xx,37,37,1500.5000,55518.50,p@v1,");
    expect(rows[2]).toContain(",hub_capacity,2xx,3,3,,,,");
    expect(rows[3]).toContain(",_total,,40,40,,55518.50,,");
  });

  it("sổ xuất: lọc kênh hợp lệ, kênh lạ → 422; cảnh báo bất thường theo kênh; vai không đọc → 403", async () => {
    const bad = await problemOf(unwrap(admin("metering-viewer").GET("/v1/metering/egress", { params: { query: { channel: "email" as never } } })));
    expect(bad.code).toBe("validation_failed");
    const valve = await unwrap(admin("metering-viewer").GET("/v1/metering/egress", { params: { query: { channel: "valve", limit: 200 } } }));
    expect(valve.items.length).toBeGreaterThan(0);
    expect(valve.items.every((e) => e.channel === "valve")).toBe(true);
    const an = await unwrap(admin("metering-viewer").GET("/v1/metering/egress/anomalies", { params: { query: { channel: "public_api" } } }));
    expect(an.items[0]?.basis).toBe("median");
    expect((await problemOf(unwrap(admin("approver").GET("/v1/metering/egress")))).code).toBe("forbidden");
  });
});

describe("<SecretOnceDialog> và bảng khóa — bí mật hiện MỘT lần", () => {
  const SECRET = "agk_abcdefghijkl_Zm9vYmFyYmF6cXV4cXV1eHh4eHh4eHh4eHh4eHh4eHh4eHg";

  it("hiện bí mật, sao chép, Esc không đóng; chỉ “Tôi đã lưu khóa” mới đóng", async () => {
    const onConfirm = vi.fn();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<SecretOnceDialog secret={{ apiKey: SECRET, keyPrefix: "abcdefghijkl", expiresAt: "2027-10-02T00:00:00Z", clientName: "Tổ chức X" }} onConfirm={onConfirm} />);
    const dlg = screen.getByRole("dialog", { name: /chỉ hiển thị một lần/ });
    expect(within(dlg).getByLabelText(/Khóa API/)).toHaveValue(SECRET);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onConfirm).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(within(dlg).getByRole("button", { name: "Sao chép khóa" }));
    });
    expect(writeText).toHaveBeenCalledWith(SECRET);
    expect(within(dlg).getByText(/Đã sao chép/)).toBeInTheDocument();
    fireEvent.click(within(dlg).getByRole("button", { name: "Tôi đã lưu khóa" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("phát hành khóa trong bảng khóa: bí mật hiện trong hộp thoại, đóng là mất khỏi DOM, không ghi log, không vào storage", async () => {
    const { KeysPanel } = await import("./panels");
    const log = vi.spyOn(console, "log");
    const info = vi.spyOn(console, "info");
    const client = (await unwrap(admin("api-admin").GET("/v1/api-clients/{id}", { params: { path: { id: CLIENT_IDS.verifier } } })))!;
    render(<KeysPanel client={client} />);
    const issue = await screen.findByRole("button", { name: "Phát hành khóa mới" });
    await waitFor(() => expect(issue).toBeEnabled());
    fireEvent.click(issue);
    const dlg = await screen.findByRole("dialog", { name: /chỉ hiển thị một lần/ });
    const secret = (within(dlg).getByLabelText(/Khóa API/) as HTMLInputElement).value;
    expect(secret).toMatch(/^agk_[a-z2-7]{12}_/);
    const prefix = secret.split("_")[1]!;
    fireEvent.click(within(dlg).getByRole("button", { name: "Tôi đã lưu khóa" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.body.innerHTML).not.toContain(secret);
    expect(await screen.findByText(`agk_${prefix}_••••`)).toBeInTheDocument();
    expect(JSON.stringify({ ...window.sessionStorage })).not.toContain(secret);
    expect(JSON.stringify({ ...window.localStorage })).not.toContain(secret);
    for (const spy of [log, info]) for (const call of spy.mock.calls) expect(JSON.stringify(call)).not.toContain(secret);
    log.mockRestore();
    info.mockRestore();
  });

  it("vai duyệt (không phải admin) không phát hành được khóa — nút khóa kèm lý do", async () => {
    auth.persona = "api-approver";
    const { KeysPanel } = await import("./panels");
    const client = (await unwrap(admin("api-approver").GET("/v1/api-clients/{id}", { params: { path: { id: CLIENT_IDS.verifier } } })))!;
    render(<KeysPanel client={client} />);
    const issue = await screen.findByRole("button", { name: "Phát hành khóa mới" });
    expect(issue).toBeDisabled();
    expect(screen.getAllByTestId("sod-hint")[0]).toHaveTextContent(/api_client_admin/);
  });
});
