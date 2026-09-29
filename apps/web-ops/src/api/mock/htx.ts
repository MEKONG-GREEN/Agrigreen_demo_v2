// ============================================================================
// BACKEND GIẢ LẬP cho App HTX ngoài pf/cropcycle:
//  - pf/accounts: tài khoản nông dân (tạo, nhập hàng loạt, phát lại liên kết/QR kích hoạt);
//  - pf/extension: nhiệm vụ tự sinh từ yêu cầu hỗ trợ của nông dân (source = htx_app);
//  - Admin: nhập danh mục hàng loạt (HTX US-CAT-02), vô hiệu hoá HTX.
// SĐT chủ hộ là SỐ TỔNG HỢP dạng 09990xxxxx (seed-synthetic), không phải số thật.
// Liên kết kích hoạt là chuỗi ngẫu nhiên — không đăng nhập được vào đâu.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { CatalogItem, CatalogName, ExtTask, FarmerAccount, FarmerActivation, FarmerGrant, HouseholdListItem, Org } from "../types";
import { CATALOGS, COOPERATIVES, syntheticPhone, findCoop, hexId, householdsOf, htxCoop, ORG_EVENTS, orgEvents, ORGS, OFFICER_NAMES, OFFICER_REFS, TASKS, type MockCooperative } from "./data";
import { headerKey, parseCsv } from "./admin-ops";
import { hasRole, inScope, isZip, json, nowIso, paginate, problem, reasonOf, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

function householdByPhone(phone: string): { coop: MockCooperative; h: HouseholdListItem; index: number } | undefined {
  for (const coop of COOPERATIVES) {
    const list = householdsOf(coop);
    const index = list.findIndex((_, i) => syntheticPhone(coop, i) === phone);
    if (index >= 0) return { coop, h: list[index]!, index };
  }
  return undefined;
}
function findHousehold(ref: string): { coop: MockCooperative; h: HouseholdListItem; index: number } | undefined {
  for (const coop of COOPERATIVES) {
    const list = householdsOf(coop);
    const index = list.findIndex((h) => h.household_ref === ref);
    if (index >= 0) return { coop, h: list[index]!, index };
  }
  return undefined;
}

function normalizePhone(raw: string): string | null {
  let s = raw.replace(/[\s.\-()]/g, "");
  if (s.startsWith("+84")) s = `0${s.slice(3)}`;
  else if (/^84\d{9}$/.test(s)) s = `0${s.slice(2)}`;
  return /^0\d{9}$/.test(s) ? s : null;
}

// --------------------------------------------------------------------------
// Tài khoản nông dân + lượt kích hoạt
// --------------------------------------------------------------------------
interface FarmerRec {
  account: FarmerAccount;
  grants: FarmerGrant[];
}
const FARMERS: FarmerRec[] = [];
let seeded = false;

function grant(issuedBy: string, hoursAgo: number, status: FarmerGrant["status"]): FarmerGrant {
  const issued = Date.now() - hoursAgo * 3_600_000;
  return {
    grant_id: uuidv7(),
    status,
    channel: "link_qr",
    issued_by: issuedBy,
    issued_at: new Date(issued).toISOString(),
    expires_at: new Date(issued + 72 * 3_600_000).toISOString(),
    used_at: status === "used" ? new Date(issued + 5 * 3_600_000).toISOString() : null,
    revoked_at: null,
    revoke_reason: null,
    replaced_by: null,
  };
}

function ensure(): void {
  if (seeded) return;
  seeded = true;
  // 2/5 nông hộ của các HTX Đồng Tháp đã có tài khoản; vài lượt hết hạn/chờ kích hoạt.
  for (const coop of COOPERATIVES.filter((c) => c.province_code === "82" || c === htxCoop())) {
    householdsOf(coop).forEach((h, i) => {
      if (i % 5 > 1 || !h.active) return;
      const status: FarmerGrant["status"] = i % 3 === 0 ? "used" : i % 3 === 1 ? "active" : "expired";
      FARMERS.push({
        account: {
          user_ref: hexId(parseInt(coop.api.ref.slice(-4), 16) * 1000 + i, "fa2e"),
          household_ref: h.household_ref,
          cooperative_ref: coop.api.ref,
          commune_code: h.commune_code,
          linked_at: new Date(Date.now() - (40 + i) * 86_400_000).toISOString(),
          enabled: true,
          latest_activation: grant("mock-htx-0001", status === "expired" ? 24 * 20 : 30, status),
        },
        grants: [],
      });
      const last = FARMERS.at(-1)!;
      last.grants.push(last.account.latest_activation!);
    });
  }
}

function activationFor(rec: FarmerRec, issuedBy: string): FarmerActivation {
  const g = grant(issuedBy, 0, "active");
  const active = rec.grants.find((x) => x.status === "active");
  if (active) Object.assign(active, { status: "revoked", revoked_at: g.issued_at, revoke_reason: "reissued", replaced_by: g.grant_id });
  rec.grants.unshift(g);
  rec.account.latest_activation = g;
  const token = `${uuidv7().replace(/-/g, "")}${Math.random().toString(36).slice(2, 10)}`;
  const link = `https://auth.agrigreen.vn/realms/agrigreen/login-actions/action-token?key=mock.${token}`;
  return { ...g, link, qr: { format: "url", data: link, error_correction: "M", min_module_px: 4 }, replayed: false };
}

type FarmerAccess = { kind: "all" } | { kind: "coop"; coop: string } | { kind: "kn" };
function farmerAccess(p: MockPersona): FarmerAccess | null {
  if (hasRole(p, "platform_admin")) return { kind: "all" };
  if (hasRole(p, "coop_manager")) return { kind: "coop", coop: p.me.org?.ref ?? "" };
  if (hasRole(p, "kn_officer", "kn_province_admin", "kn_national_admin")) return { kind: "kn" };
  return null;
}
function coopVisible(p: MockPersona, a: FarmerAccess, coop: MockCooperative | undefined): boolean {
  if (!coop) return false;
  if (a.kind === "all") return true;
  if (a.kind === "coop") return a.coop === coop.api.ref;
  return inScope(p, coop.province_code, coop.api.admin_unit_code);
}

type CreateOut = { ok: true; rec: FarmerRec; activation: FarmerActivation } | { ok: false; res: Response; reason: string };

function createFarmer(p: MockPersona, a: FarmerAccess, input: { phone?: unknown; household_ref?: unknown }): CreateOut {
  const phone = typeof input.phone === "string" ? normalizePhone(input.phone) : null;
  if (!phone) return { ok: false, res: validation([{ field: "phone", reason: "SĐT không đúng định dạng 0xxxxxxxxx" }]), reason: "SĐT không đúng định dạng" };
  const hhRef = typeof input.household_ref === "string" && input.household_ref ? input.household_ref : null;
  const byPhone = householdByPhone(phone);
  const target = hhRef ? findHousehold(hhRef) : byPhone;
  if (!target || !target.h.active || !target.coop.api.active) {
    return { ok: false, res: problem(422, "accounts.household_invalid", "Household invalid"), reason: "Nông hộ không có, đã ngừng, hoặc SĐT không khớp nông hộ nào" };
  }
  if (!coopVisible(p, a, target.coop)) return { ok: false, res: problem(403, "accounts.out_of_scope", "Out of scope"), reason: "Nông hộ ngoài HTX/phạm vi của bạn" };
  if (hhRef && syntheticPhone(target.coop, target.index) !== phone) {
    return { ok: false, res: problem(422, "accounts.phone_household_mismatch", "Phone mismatch"), reason: "SĐT không phải SĐT chủ hộ của nông hộ đã chọn" };
  }
  if (FARMERS.some((f) => f.account.household_ref === target.h.household_ref && f.account.enabled)) {
    return { ok: false, res: problem(409, "accounts.farmer_exists", "Farmer exists"), reason: "Nông hộ đã có tài khoản nông dân" };
  }
  const rec: FarmerRec = {
    account: {
      user_ref: uuidv7(),
      household_ref: target.h.household_ref,
      cooperative_ref: target.coop.api.ref,
      commune_code: target.h.commune_code,
      linked_at: nowIso(),
      enabled: true,
    },
    grants: [],
  };
  FARMERS.push(rec);
  return { ok: true, rec, activation: activationFor(rec, p.me.actor_ref) };
}

// --------------------------------------------------------------------------
// Nhiệm vụ tự sinh từ yêu cầu hỗ trợ (KN US-TASK-01): nguồn htx_app cho HTX của persona Quản lý HTX
// --------------------------------------------------------------------------
const SUPPORT_TEXT: { category: string; text: string; urgent?: boolean }[] = [
  { category: "dich_hai", text: "Rầy nâu mật độ cao ở lô gần kênh, xin cán bộ xuống kiểm tra.", urgent: true },
  { category: "benh_lua", text: "Lá lúa có vết hình thoi màu nâu xám, nghi đạo ôn lá." },
  { category: "thoi_tiet", text: "Mưa lớn kéo dài, ruộng ngập sâu sau sạ 10 ngày." },
  { category: "ky_thuat", text: "Hỏi lịch rút nước lần 2 theo quy trình tưới ướt khô xen kẽ." },
  { category: "may_moc", text: "Cần hỗ trợ đặt lịch máy gặt cho 3 lô liền kề." },
];

function seedSupportTasks(): void {
  const coop = htxCoop();
  SUPPORT_TEXT.forEach((t, i) => {
    const created = Date.now() - (i * 9 + 2) * 3_600_000;
    const status: ExtTask["status"] = (["new", "assigned", "in_progress", "resolved", "accepted"] as const)[i]!;
    const assignee = status === "new" ? null : (OFFICER_REFS[i % OFFICER_REFS.length] ?? null);
    const due = created + 48 * 3_600_000;
    TASKS.push({
      assignee_name: assignee ? (OFFICER_NAMES[assignee] ?? null) : null,
      cooperative_name: coop.api.name,
      commune_name: coop.commune_name,
      task_id: hexId(7000 + i, "7a5c"),
      commune_code: coop.api.admin_unit_code ?? "",
      province_code: coop.province_code,
      cooperative_ref: coop.api.ref,
      household_ref: householdsOf(coop)[i]?.household_ref ?? null,
      plot_ref: null,
      category: t.category,
      description: t.text,
      priority: t.urgent ? "urgent" : "normal",
      source: "htx_app",
      status,
      assignee_ref: assignee,
      created_by: hexId(parseInt(coop.api.ref.slice(-4), 16) * 1000 + i, "fa2e"),
      created_at: new Date(created).toISOString(),
      sla_hours: "48",
      due_at: new Date(due).toISOString(),
      overdue: status !== "resolved" && Date.now() > due,
      escalated_at: null,
      escalated_unit: null,
      escalation_org_ref: null,
      resolved_at: status === "resolved" ? new Date(created + 20 * 3_600_000).toISOString() : null,
      last_note: status === "in_progress" ? "Đã hẹn xuống ruộng sáng mai" : status === "resolved" ? "Đã hướng dẫn phun thuốc đặc trị, theo dõi thêm 5 ngày" : null,
      version: 1 + i,
    });
  });
}
seedSupportTasks();

// --------------------------------------------------------------------------
// Application API
// --------------------------------------------------------------------------
export const htxAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/farmer-accounts$/,
    handler: ({ url, persona }) => {
      ensure();
      const a = farmerAccess(persona);
      if (!a) return problem(403, "forbidden", "Forbidden");
      const ref = url.searchParams.get("cooperative_ref") ?? (a.kind === "coop" ? a.coop : null);
      if (!ref) return validation([{ field: "cooperative_ref", reason: "bắt buộc chọn HTX" }]);
      if (!coopVisible(persona, a, findCoop(ref))) return problem(403, "accounts.out_of_scope", "Out of scope");
      return json(paginate(FARMERS.filter((f) => f.account.cooperative_ref === ref).map((f) => f.account), url));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/farmer-accounts$/,
    handler: ({ body, persona }) => {
      ensure();
      const a = farmerAccess(persona);
      if (!a || a.kind === "kn") return problem(403, "forbidden", "Forbidden");
      const out = createFarmer(persona, a, (body ?? {}) as Record<string, unknown>);
      if (!out.ok) return out.res;
      return json({ farmer: out.rec.account, replayed: false, activation: out.activation, activation_error: null }, 201, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/farmer-accounts\/import$/,
    handler: ({ body, persona, request }) => {
      ensure();
      const a = farmerAccess(persona);
      if (!a || a.kind === "kn") return problem(403, "forbidden", "Forbidden");
      let rows: { phone: string; household_ref: string }[];
      if (isZip(body)) {
        // .xlsx: giả lập không giải nén — minh hoạ bằng 2 nông hộ chưa có tài khoản + 1 dòng sai SĐT.
        const coop = a.kind === "coop" ? findCoop(a.coop) : htxCoop();
        const free = coop ? householdsOf(coop).map((h, i) => ({ h, i })).filter(({ h }) => h.active && !FARMERS.some((f) => f.account.household_ref === h.household_ref)).slice(0, 2) : [];
        rows = [...free.map(({ i }) => ({ phone: syntheticPhone(coop!, i), household_ref: "" })), { phone: "12345", household_ref: "" }];
      } else {
        if (!(request.headers.get("Content-Type") ?? "").startsWith("text/csv")) return problem(422, "accounts.import_invalid", "Expected CSV or XLSX");
        const csv = parseCsv(typeof body === "string" ? body : "");
        const header = (csv[0] ?? []).map(headerKey);
        const iPhone = header.findIndex((h) => ["phone", "so_dien_thoai", "sdt"].includes(h));
        const iHh = header.findIndex((h) => ["household_ref", "ma_nong_ho"].includes(h));
        if (iPhone < 0) return problem(422, "accounts.import_invalid", "Missing phone column");
        rows = csv.slice(1).map((r) => ({ phone: r[iPhone] ?? "", household_ref: iHh >= 0 ? (r[iHh] ?? "") : "" }));
        if (rows.length === 0 || rows.length > 200) return problem(422, "accounts.import_invalid", "Row count out of range");
      }
      const out = rows.map((r, i) => {
        const res = createFarmer(persona, a, r);
        return res.ok
          ? { row: i + 2, status: "created" as const, reason: null, user_ref: res.rec.account.user_ref, household_ref: res.rec.account.household_ref, activation: res.activation }
          : { row: i + 2, status: "rejected" as const, reason: res.reason, user_ref: null, household_ref: null, activation: null };
      });
      return json({ created: out.filter((r) => r.status === "created").length, rejected: out.filter((r) => r.status === "rejected").length, rows: out }, 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/farmer-accounts\/([\w-]+)\/activation$/,
    handler: ({ params, persona }) => {
      ensure();
      const a = farmerAccess(persona);
      const rec = FARMERS.find((f) => f.account.user_ref === params[0]);
      if (!a || !rec || !coopVisible(persona, a, findCoop(rec.account.cooperative_ref))) return problem(404, "accounts.farmer_not_found", "Not found");
      return json({ user_ref: rec.account.user_ref, items: rec.grants });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/farmer-accounts\/([\w-]+)\/activation$/,
    handler: ({ params, persona }) => {
      ensure();
      const a = farmerAccess(persona);
      if (!a || a.kind === "kn") return problem(403, "forbidden", "Forbidden");
      const rec = FARMERS.find((f) => f.account.user_ref === params[0]);
      if (!rec || !coopVisible(persona, a, findCoop(rec.account.cooperative_ref))) return problem(404, "accounts.farmer_not_found", "Not found");
      if (!rec.account.enabled) return problem(409, "accounts.state_conflict", "Account locked");
      return json({ activation: activationFor(rec, persona.me.actor_ref) }, 201, { "Cache-Control": "no-store" });
    },
  },
];

// --------------------------------------------------------------------------
// Admin API
// --------------------------------------------------------------------------
const CATALOG_CODE = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$/;

export const htxAdminRoutes: Route[] = [
  {
    method: "POST",
    pattern: /^\/v1\/masterdata\/catalog-imports$/,
    handler: ({ body, persona, request }) => {
      if (!hasRole(persona, "masterdata_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      let rows: string[][];
      if (isZip(body)) {
        // .xlsx: minh hoạ trang tính đầu (1 tạo mới, 1 bỏ qua vì trùng, 1 lỗi).
        rows = [
          ["catalog", "code", "name", "growth_days", "expected_yield_t_per_ha"],
          ["rice_variety", "OM9582", "OM9582 (nhập từ Excel)", "95", "6.8"],
          ["rice_variety", "OM5451", "OM5451", "95", "6.75"],
          ["rice_variety", "LOI-1", "Giống lỗi", "0", ""],
        ];
      } else {
        if (!(request.headers.get("Content-Type") ?? "").startsWith("text/csv")) return problem(422, "masterdata.catalog_import_invalid", "Expected CSV or XLSX");
        rows = parseCsv(typeof body === "string" ? body : "");
      }
      const header = (rows[0] ?? []).map(headerKey);
      const iCat = header.findIndex((h) => ["catalog", "danh_muc"].includes(h));
      const iCode = header.findIndex((h) => ["code", "ma"].includes(h));
      const iName = header.findIndex((h) => ["name", "ten"].includes(h));
      if (iCat < 0 || iCode < 0 || iName < 0) return problem(422, "masterdata.catalog_import_invalid", "Missing columns");
      const data = rows.slice(1);
      if (data.length === 0 || data.length > 500) return problem(422, "masterdata.catalog_import_invalid", "Row count out of range");
      if (data.some((r) => r.some((c) => c.startsWith("=")))) return problem(422, "masterdata.catalog_import_invalid", "Formula cells");
      const now = nowIso();
      const out = data.map((r, i) => {
        const row = i + 2;
        const catalog = (r[iCat] ?? "").trim();
        const code = (r[iCode] ?? "").trim();
        const name = (r[iName] ?? "").trim();
        const attrs: Record<string, string> = {};
        header.forEach((h, j) => {
          if (j !== iCat && j !== iCode && j !== iName && (r[j] ?? "").trim()) attrs[h] = (r[j] ?? "").trim();
        });
        const list = CATALOGS[catalog as CatalogName];
        if (!list) return { row, status: "rejected" as const, catalog, code, reason: `Danh mục "${catalog}" không tồn tại` };
        if (!CATALOG_CODE.test(code)) return { row, status: "rejected" as const, catalog, code, reason: "Mã không hợp lệ (chữ, số, _ . -; tối đa 40 ký tự)" };
        if (!name) return { row, status: "rejected" as const, catalog, code, reason: "Thiếu tên" };
        if (attrs.growth_days !== undefined && !(Number(attrs.growth_days) > 0)) return { row, status: "rejected" as const, catalog, code, reason: "Thời gian sinh trưởng phải lớn hơn 0." };
        const existing = list.find((x) => x.code === code);
        if (existing) {
          const same = existing.name === name && Object.entries(attrs).every(([k, v]) => existing.attrs[k] === v);
          return same
            ? { row, status: "skipped" as const, catalog, code, reason: "Đã có mục cùng nội dung" }
            : { row, status: "rejected" as const, catalog, code, reason: "Mã đã có với nội dung khác — sửa từng mục trên màn hình danh mục" };
        }
        if (list.some((x) => x.name.trim().toLowerCase() === name.toLowerCase())) return { row, status: "rejected" as const, catalog, code, reason: "Tên đã tồn tại, vui lòng chọn tên khác" };
        const item: CatalogItem = { catalog: catalog as CatalogName, code, name, attrs, active: true, deactivation_reason: null, revision: 1, updated_at: now };
        list.push(item);
        return { row, status: "created" as const, catalog, code };
      });
      return json({
        import_id: uuidv7(),
        total: out.length,
        created: out.filter((r) => r.status === "created").length,
        skipped: out.filter((r) => r.status === "skipped").length,
        rejected: out.filter((r) => r.status === "rejected").length,
        replayed: false,
        rows: out,
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/deactivate$/,
    handler: ({ params, body, persona }) => {
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const coop = findCoop(params[0]);
      const org = ORGS.find((o) => o.ref === params[0]);
      if (!coop || !org) return problem(404, "party.org_not_found", "Not found");
      const reason = reasonOf(body);
      if (reason.length < 20) return validation([{ field: "reason", reason: "Lý do tối thiểu 20 ký tự" }]);
      if (!coop.api.active) return problem(409, "party.org_state_conflict", "Already inactive");
      const now = nowIso();
      coop.api.active = false;
      coop.api.status_reason = reason;
      org.active = false;
      const events = orgEvents(org);
      const event = { ...events[events.length - 1]!, seq: events.length + 1, action: "deactivated" as const, event_time: now, recorded_at: now, actor_ref: persona.me.actor_ref, reason, active: false };
      events.push(event);
      ORG_EVENTS.set(org.ref, events);
      const partyOrg: Org = { ...org };
      return json({ org: partyOrg, event, active_households: householdsOf(coop).filter((h) => h.active).length });
    },
  },
];
