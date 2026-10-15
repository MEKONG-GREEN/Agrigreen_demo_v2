import { uuidv7 } from "../idempotency";
import { COOPERATIVES, findCoop, householdsOf, orgEvents, ORG_EVENTS, ORGS } from "./data";
import { headerKey, parseCsv } from "./admin-ops";
import { hasRole, inScope, isZip, json, norm, paginate, problem, scopeUnits, type Route } from "./http";

const importedPhones = new Set<string>();

export const appRoutes1: Route[] = [{ method: "GET", pattern: /^\/v1\/me$/, handler: ({ persona }) => json(persona.me) }];

export const appRoutes34: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/orgs$/,
    handler: ({ url, persona }) => {
      const kind = url.searchParams.get("kind");
      const inactive = url.searchParams.get("include_inactive") === "true";
      const s = scopeUnits(persona);
      const list = ORGS.filter(
        (o) => (!s || (o.admin_unit_code !== null && s.has(o.admin_unit_code))) && (!kind || o.kind === kind) && (inactive || o.active),
      );
      return json(paginate(list, url));
    },
  }];

export const appRoutes35: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/cooperatives$/,
    handler: ({ url, persona }) => {
      const q = norm(url.searchParams.get("q") ?? "");
      const inactive = url.searchParams.get("include_inactive") === "true";
      // G-03: admin_unit = mã tỉnh hoặc xã; ngoài phạm vi → danh sách rỗng (không lỗi).
      const unit = url.searchParams.get("admin_unit");
      const list = COOPERATIVES.filter(
        (c) =>
          inScope(persona, c.province_code, c.api.admin_unit_code) &&
          (!unit || c.province_code === unit || c.api.admin_unit_code === unit) &&
          (inactive || c.api.active) &&
          (!q || norm(`${c.api.name} ${c.api.code} ${c.api.tax_code ?? ""}`).includes(q)),
      ).map((c) => c.api);
      return json(paginate(list, url));
    },
  }];

export const appRoutes36: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)$/,
    handler: ({ params, persona }) => {
      const c = findCoop(params[0]);
      // Ngoài phạm vi trả 404 như backend (không lộ sự tồn tại).
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(404, "party.org_not_found", "Not found");
      return json(c.api);
    },
  }];

export const appRoutes37: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([\w-]+)\/households$/,
    handler: ({ params, url, persona }) => {
      const c = findCoop(params[0]);
      if (!c || !inScope(persona, c.province_code, c.api.admin_unit_code)) return problem(404, "party.org_not_found", "Not found");
      return json(paginate(householdsOf(c), url));
    },
  }];

export const adminRoutes10: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/orgs\/([\w-]+)\/events$/,
    handler: ({ params }) => {
      const org = ORGS.find((o) => o.ref === params[0]);
      return org ? json({ items: orgEvents(org) }) : problem(404, "party.org_not_found", "Not found");
    },
  }];

export const adminRoutes11: Route[] = [{
    method: "POST",
    pattern: /^\/v1\/orgs\/([\w-]+)\/rename$/,
    handler: ({ params, body, persona }) => {
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const org = ORGS.find((o) => o.ref === params[0]);
      if (!org) return problem(404, "party.org_not_found", "Not found");
      const input = (body ?? {}) as { name?: unknown; reason?: unknown };
      const name = typeof input.name === "string" ? input.name.trim() : "";
      if (!name || name.length > 200) return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "name", reason: "Tên từ 1 đến 200 ký tự" }] });
      if (name === org.name) return problem(409, "party.org_state_conflict", "No change");
      const events = orgEvents(org);
      org.name = name;
      const now = new Date().toISOString();
      const event = {
        ...events[events.length - 1]!,
        seq: events.length + 1,
        action: "renamed" as const,
        event_time: now,
        recorded_at: now,
        actor_ref: persona.me.actor_ref,
        reason: typeof input.reason === "string" && input.reason ? input.reason : null,
        name,
      };
      events.push(event);
      ORG_EVENTS.set(org.ref, events);
      return json({ org, event, active_households: 0 });
    },
  }];

export const adminRoutes12: Route[] = [{
    method: "POST",
    pattern: /^\/v1\/households\/import$/,
    handler: ({ body, persona, request }) => {
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      if (isZip(body)) {
        // .xlsx (chữ ký ZIP): giả lập không giải nén — trả kết quả minh hoạ cho trang tính đầu.
        return json({
          accepted: 2,
          duplicate: 0,
          rejected: 1,
          rows: [
            { row: 2, status: "accepted", reason: null, household_ref: uuidv7() },
            { row: 3, status: "accepted", reason: null, household_ref: uuidv7() },
            { row: 4, status: "rejected", reason: "Số điện thoại không đúng định dạng", household_ref: null },
          ],
        });
      }
      if (!(request.headers.get("Content-Type") ?? "").startsWith("text/csv")) return problem(422, "party.import_invalid", "Expected text/csv or .xlsx");
      const rows = parseCsv(typeof body === "string" ? body : "");
      const header = (rows[0] ?? []).map(headerKey);
      const col = (...names: string[]) => header.findIndex((h) => names.includes(h));
      const iName = col("full_name", "ho_ten");
      const iPhone = col("phone", "so_dien_thoai");
      const iCoop = col("cooperative_code", "ma_htx");
      if (iName < 0 || iPhone < 0 || iCoop < 0) return problem(422, "party.import_invalid", "Missing required columns");
      const data = rows.slice(1);
      if (data.length === 0 || data.length > 200) return problem(422, "party.import_invalid", "Row count out of range");
      const out = data.map((r, i) => {
        const row = i + 2;
        const phone = (r[iPhone] ?? "").replace(/\s+/g, "");
        if (!(r[iName] ?? "").trim()) return { row, status: "rejected" as const, reason: "Thiếu họ tên chủ hộ", household_ref: null };
        if (!/^0\d{9}$/.test(phone)) return { row, status: "rejected" as const, reason: "Số điện thoại không đúng định dạng", household_ref: null };
        if (!COOPERATIVES.some((c) => c.api.code === (r[iCoop] ?? "").trim())) return { row, status: "rejected" as const, reason: "Không tìm thấy HTX theo mã đã nhập", household_ref: null };
        if (importedPhones.has(phone)) return { row, status: "duplicate" as const, reason: "Trùng chủ hộ đã có — dùng lại mã cũ", household_ref: uuidv7() };
        importedPhones.add(phone);
        return { row, status: "accepted" as const, reason: null, household_ref: uuidv7() };
      });
      return json({
        accepted: out.filter((r) => r.status === "accepted").length,
        duplicate: out.filter((r) => r.status === "duplicate").length,
        rejected: out.filter((r) => r.status === "rejected").length,
        rows: out,
      });
    },
  }];
