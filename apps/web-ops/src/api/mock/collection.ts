// ============================================================================
// BACKEND GIẢ LẬP pf/cropcycle G3-2 — thu gom rơm MKG phía HTX:
//  - lịch gom (van collection_schedule) và bảng kê số cuộn theo thửa (van htx_collection_statement);
//  - quản lý HTX xác nhận / tranh chấp theo revision hiện hành (409 nếu cũ).
// Dữ liệu TỔNG HỢP (R-95): số cuộn, ngày gom sinh tất định — không phải số liệu thật của MKG.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { HtxCollectionResponse, HtxCollectionSchedule, HtxCollectionStatement } from "../types";
import { COOPERATIVES, htxCoop, plotsOf, type MockCooperative } from "./data";
import { hasRole, inScope, json, nowIso, paginate, problem, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

const day = (offset: number) => new Date(Date.now() + offset * 86_400_000).toISOString().slice(0, 10);

const SCHEDULES: HtxCollectionSchedule[] = [];
const STATEMENTS: HtxCollectionStatement[] = [];
const HISTORY = new Map<string, HtxCollectionResponse[]>();
let seeded = false;

function seedCoop(coop: MockCooperative, base: number) {
  const commune = coop.api.admin_unit_code ?? "00000";
  SCHEDULES.push(
    { schedule_ref: uuidv7(Date.now() - base), cooperative_ref: coop.api.ref, commune_code: commune, window_from: day(-3), window_to: day(1), status: "in_progress", revision: 3, published_at: nowIso(), updated_at: nowIso() },
    { schedule_ref: uuidv7(Date.now() - base + 1), cooperative_ref: coop.api.ref, commune_code: commune, window_from: day(6), window_to: day(10), status: "planned", revision: 1, published_at: nowIso(), updated_at: nowIso() },
  );
  const plots = plotsOf(coop).filter((p) => p.properties.status === "active").slice(0, 4);
  const rolls = [118, 96, 140, 75];
  plots.forEach((p, i) => {
    const ref = uuidv7(Date.now() - base + 10 + i);
    const s: HtxCollectionStatement = {
      statement_ref: ref,
      cooperative_ref: coop.api.ref,
      plot_ref: p.properties.ref,
      plot_code: p.properties.code,
      season_ref: null,
      collected_on: day(-2 + (i % 2)),
      rolls: rolls[i] ?? 60,
      revision: i === 2 ? 2 : 1,
      pending: true,
      response: null,
      published_at: nowIso(),
      updated_at: nowIso(),
    };
    // Bảng kê thứ 2: HTX đã xác nhận; thứ 3: MKG đã sửa sau khi HTX xác nhận bản 1.
    if (i === 1 || i === 2) {
      const r: HtxCollectionResponse = { id: uuidv7(), revision: 1, outcome: "confirmed", htx_rolls: i === 2 ? 142 : s.rolls, reason: null, responded_at: nowIso() };
      s.response = r;
      s.pending = s.revision !== 1;
      HISTORY.set(ref, [r]);
    }
    STATEMENTS.push(s);
  });
}

function ensure() {
  if (seeded) return;
  seeded = true;
  const own = htxCoop();
  seedCoop(own, 90_000_000);
  const other = COOPERATIVES.find((c) => c.api.active && c.api.ref !== own.api.ref);
  if (other) seedCoop(other, 80_000_000);
}

function access(p: MockPersona): { kind: "coop"; coop: string } | { kind: "all" } | { kind: "kn" } | null {
  if (hasRole(p, "platform_admin")) return { kind: "all" };
  if (hasRole(p, "coop_manager")) return { kind: "coop", coop: p.me.org?.ref ?? "" };
  if (hasRole(p, "kn_officer", "kn_province_admin", "kn_national_admin")) return { kind: "kn" };
  return null;
}

const provinceOf = (coopRef: string) => COOPERATIVES.find((c) => c.api.ref === coopRef)?.province_code ?? null;

export const collectionRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/htx-collection\/schedules$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = access(persona);
      if (!a) return problem(403, "forbidden", "Forbidden");
      const coop = url.searchParams.get("cooperative_ref") ?? "";
      const list = SCHEDULES.filter(
        (s) =>
          (a.kind === "all" || (a.kind === "coop" && s.cooperative_ref === a.coop) || (a.kind === "kn" && inScope(persona, provinceOf(s.cooperative_ref), s.commune_code))) &&
          (!coop || s.cooperative_ref === coop),
      ).sort((x, y) => (x.schedule_ref < y.schedule_ref ? 1 : -1));
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/htx-collection\/statements$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = access(persona);
      if (!a || a.kind === "kn") return problem(403, "forbidden", "Forbidden");
      const status = url.searchParams.get("status") ?? "";
      if (status && !["pending", "confirmed", "disputed"].includes(status)) return validation([{ field: "status", reason: "pending | confirmed | disputed" }]);
      const coop = url.searchParams.get("cooperative_ref") ?? "";
      const list = STATEMENTS.filter(
        (s) =>
          (a.kind === "all" || s.cooperative_ref === a.coop) &&
          (!coop || s.cooperative_ref === coop) &&
          (!status || (status === "pending" ? s.pending : !s.pending && s.response?.outcome === status)),
      ).sort((x, y) => (x.statement_ref < y.statement_ref ? 1 : -1));
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/cropcycle\/htx-collection\/statements\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      ensure();
      const a = access(persona);
      const s = STATEMENTS.find((x) => x.statement_ref === params[0]);
      if (!a || a.kind === "kn") return problem(403, "forbidden", "Forbidden");
      if (!s || (a.kind === "coop" && s.cooperative_ref !== a.coop)) return problem(404, "cropcycle.collection_statement_not_found", "Không tìm thấy bảng kê thu gom");
      return json({ statement: s, responses: HISTORY.get(s.statement_ref) ?? [] });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cropcycle\/htx-collection\/statements\/([\w-]+)\/responses$/,
    handler: ({ params, persona, body }) => {
      ensure();
      if (!hasRole(persona, "coop_manager")) return problem(403, "cropcycle.collection_manager_only", "Chỉ quản lý HTX của đúng HTX được xác nhận hoặc tranh chấp bảng kê thu gom");
      const s = STATEMENTS.find((x) => x.statement_ref === params[0]);
      if (!s || s.cooperative_ref !== persona.me.org?.ref) return problem(404, "cropcycle.collection_statement_not_found", "Không tìm thấy bảng kê thu gom");
      const b = (body ?? {}) as { revision?: number; outcome?: string; htx_rolls?: number | null; reason?: string };
      if (b.revision !== s.revision)
        return problem(409, "cropcycle.collection_statement_stale", "MKG đã gửi bản sửa của bảng kê này — tải lại và phản hồi theo bản mới nhất", { current_revision: s.revision });
      const reason = (b.reason ?? "").trim();
      const superseding = !!s.response && s.response.revision === s.revision;
      if (reason && (reason.length < 10 || reason.length > 500)) return validation([{ field: "reason", reason: "Lý do cần từ 10 đến 500 ký tự." }]);
      let htx = s.rolls;
      if (b.outcome === "disputed") {
        if (b.htx_rolls == null || b.htx_rolls < 0) return validation([{ field: "htx_rolls", reason: "Tranh chấp cần nhập số cuộn HTX đếm được (≥ 0)." }]);
        if (b.htx_rolls === s.rolls) return validation([{ field: "htx_rolls", reason: "Số cuộn HTX bằng số MKG — hãy chọn Xác nhận." }]);
        if (!reason) return validation([{ field: "reason", reason: "Lý do cần từ 10 đến 500 ký tự." }]);
        htx = b.htx_rolls;
      } else if (b.outcome !== "confirmed") return validation([{ field: "outcome", reason: "phải là confirmed hoặc disputed" }]);
      if (superseding && !reason) return validation([{ field: "reason", reason: "Đã có phản hồi cho bảng kê này — ghi lý do thay đổi (10–500 ký tự)." }]);
      const r: HtxCollectionResponse = {
        id: uuidv7(),
        revision: s.revision,
        outcome: b.outcome,
        htx_rolls: htx,
        reason: reason || null,
        responded_at: nowIso(),
        supersedes_id: superseding ? (s.response?.id ?? null) : null,
      };
      HISTORY.set(s.statement_ref, [...(HISTORY.get(s.statement_ref) ?? []), r]);
      s.response = r;
      s.pending = false;
      s.updated_at = nowIso();
      return json({ response: r, statement: s }, 201);
    },
  },
];
