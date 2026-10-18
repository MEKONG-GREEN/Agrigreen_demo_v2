import type { AppGet, ParamVersion } from "../types";
import { uuidv7 } from "../idempotency";
import { ADMIN_UNITS, CATALOGS, PARAM_VERSIONS, ACTOR_NAMES } from "./data";
import { hasRole, json, paginate, problem, type Route } from "./http";
import { checkDisplayProposal, DISPLAY_DEFAULTS, displayConfig, isColorKey, isPercentKey, PERCENT_TITLE, RESTORE_REASON } from "./display";

const DECIMAL = /^-?\d+(\.\d+)?$/;

const PARAM_KEY = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+(:[A-Za-z0-9_.-]+)*$/;

function normEffectiveFrom(v: string): string | null {
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) return `${v}T00:00:00+07:00`;
  return Number.isFinite(Date.parse(v)) ? v : null;
}

function findVersion(id: string | undefined): ParamVersion | undefined {
  return PARAM_VERSIONS.find((v) => v.id === id);
}

export const appRoutes30: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/masterdata\/admin-units$/,
    handler: ({ url }) => {
      const parent = url.searchParams.get("parent");
      if (parent && !ADMIN_UNITS.some((u) => u.code === parent)) return problem(404, "masterdata.admin_unit_not_found", "Not found");
      return json(paginate(ADMIN_UNITS.filter((u) => (parent ? u.parent_code === parent : u.parent_code === null)), url));
    },
  }];

export const appRoutes31: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/masterdata\/admin-units\/([0-9A-Z.-]+)$/,
    handler: ({ params }) => {
      const u = ADMIN_UNITS.find((x) => x.code === params[0]);
      return u ? json(u) : problem(404, "masterdata.admin_unit_not_found", "Not found");
    },
  }];

export const appRoutes32: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/([a-z_]+)$/,
    handler: ({ params, url }) => {
      const items = CATALOGS[params[0] as keyof typeof CATALOGS];
      if (!items) return problem(404, "masterdata.catalog_not_found", "Not found");
      const all = url.searchParams.get("include_inactive") === "true";
      return json(paginate(items.filter((i) => all || i.active), url));
    },
  }];

export const appRoutes33: Route[] = [{ method: "GET", pattern: /^\/v1\/masterdata\/display-config$/, handler: () => json(displayConfig()) }];

export const adminRoutes4: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs\/([a-z_]+)\/items$/,
    handler: ({ params, url, persona }) => {
      if (!hasRole(persona, "masterdata_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const items = CATALOGS[params[0] as keyof typeof CATALOGS];
      if (!items) return problem(404, "masterdata.catalog_not_found", "Not found");
      const all = url.searchParams.get("include_inactive") !== "false";
      return json(paginate(items.filter((i) => all || i.active), url));
    },
  }];

export const adminRoutes5: Route[] = [{
    method: "GET",
    pattern: /^\/v1\/masterdata\/params\/versions$/,
    handler: ({ url, persona }) => {
      if (!hasRole(persona, "param_setter", "param_approver", "platform_admin")) return problem(403, "forbidden", "Forbidden");
      const key = url.searchParams.get("key");
      const status = url.searchParams.get("status");
      return json(paginate(PARAM_VERSIONS.filter((v) => (!key || v.key === key) && (!status || v.status === status)), url));
    },
  }];

export const adminRoutes6: Route[] = [{
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/versions$/,
    handler: ({ body, persona }) => {
      if (!hasRole(persona, "param_setter")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      const errors: { field: string; reason: string }[] = [];
      const key = typeof input.key === "string" ? input.key : "";
      const value = typeof input.value === "string" ? input.value.trim() : typeof input.value === "number" ? String(input.value) : "";
      const text = typeof input.text === "string" ? input.text.trim() : "";
      const eff = typeof input.effective_from === "string" ? normEffectiveFrom(input.effective_from) : null;
      const sourceDoc = typeof input.source_doc === "string" ? input.source_doc.trim() : "";
      const correction = typeof input.correction_reason === "string" ? input.correction_reason.trim() : "";
      // Tham số màu (GIS US-CFG-01): giá trị nằm ở `text` (HEX), không có `value`.
      const colorKey = isColorKey(key);
      if (!PARAM_KEY.test(key)) errors.push({ field: "key", reason: "Khóa tham số không đúng mẫu" });
      if (colorKey) {
        if (value) errors.push({ field: "value", reason: "Tham số màu chỉ nhận text (mã HEX)" });
      } else if (!DECIMAL.test(value)) errors.push({ field: "value", reason: "Giá trị phải là số thập phân (dạng chuỗi)" });
      const display = checkDisplayProposal(key, value, text);
      if (display?.errors) errors.push(...display.errors);
      if (!eff) errors.push({ field: "effective_from", reason: "Ngày hiệu lực không hợp lệ" });
      if (!sourceDoc) errors.push({ field: "source_doc", reason: "Cần số/ngày văn bản ban hành" });
      if (correction && correction.length < 10) errors.push({ field: "correction_reason", reason: "Lý do sửa tối thiểu 10 ký tự" });
      if (errors.length) return problem(422, "validation_failed", "Validation failed", { errors });
      if (display?.percent) return problem(422, "masterdata.param_percent_thresholds", PERCENT_TITLE);
      const sameKey = PARAM_VERSIONS.filter((v) => v.key === key);
      if (!correction && sameKey.some((v) => v.status === "approved" && Date.parse(v.effective_from) === Date.parse(eff!))) {
        return problem(422, "masterdata.param_correction_required", "Correction reason required");
      }
      const last = sameKey.reduce((m, v) => Math.max(m, v.version), 0);
      const v: ParamVersion = {
        id: uuidv7(),
        key,
        version: last + 1,
        value: colorKey ? null : value,
        text: colorKey ? text.toUpperCase() : null,
        unit: typeof input.unit === "string" && input.unit ? input.unit : (sameKey[0]?.unit ?? ""),
        effective_from: eff!,
        source_doc: sourceDoc,
        correction_reason: correction || null,
        status: "draft",
        setter_ref: persona.me.actor_ref,
        setter_name: ACTOR_NAMES[persona.me.actor_ref] ?? null,
        submitted_at: null,
        approver_ref: null,
        approver_name: null,
        decided_at: null,
        decision_note: null,
        created_at: new Date().toISOString(),
      };
      PARAM_VERSIONS.push(v);
      return json(v, 201, { Location: `/v1/masterdata/params/versions/${v.id}` });
    },
  }];

export const adminRoutes7: Route[] = [{
    // GIS US-CFG-02 AC-2: "Xóa cấu hình" = phiên bản NHÁP mới mang giá trị mặc định nhúng (không xóa gì — RB-1).
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/restore-default$/,
    handler: ({ body, persona }) => {
      if (!hasRole(persona, "param_setter")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      const key = typeof input.key === "string" ? input.key : "";
      const reason = typeof input.reason === "string" ? input.reason.trim() : "";
      const def = DISPLAY_DEFAULTS[key];
      if (!def) return problem(404, "masterdata.param_no_default", "Param has no embedded default");
      if (reason && reason.length < 10) return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "reason", reason: "Lý do tối thiểu 10 ký tự" }] });
      const last = PARAM_VERSIONS.filter((v) => v.key === key).reduce((m, v) => Math.max(m, v.version), 0);
      const now = new Date().toISOString();
      const v: ParamVersion = {
        id: uuidv7(),
        key,
        version: last + 1,
        value: def.kind === "color" ? null : def.value,
        text: def.kind === "color" ? def.value : null,
        unit: def.unit,
        effective_from: now,
        source_doc: reason || RESTORE_REASON,
        correction_reason: null,
        status: "draft",
        setter_ref: persona.me.actor_ref,
        setter_name: ACTOR_NAMES[persona.me.actor_ref] ?? null,
        submitted_at: null,
        approver_ref: null,
        approver_name: null,
        decided_at: null,
        decision_note: null,
        created_at: now,
      };
      PARAM_VERSIONS.push(v);
      return json(v, 201, { Location: `/v1/masterdata/params/versions/${v.id}` });
    },
  }];

export const adminRoutes8: Route[] = [{
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/versions\/([\w-]+)\/submit$/,
    handler: ({ params, persona }) => {
      const v = findVersion(params[0]);
      if (!v) return problem(404, "masterdata.param_version_not_found", "Not found");
      if (v.setter_ref !== persona.me.actor_ref) return problem(403, "masterdata.param_not_setter", "Not setter");
      if (v.status !== "draft") return problem(409, "masterdata.param_invalid_transition", "Invalid transition");
      v.status = "submitted";
      v.submitted_at = new Date().toISOString();
      return json(v);
    },
  }];

export const adminRoutes9: Route[] = [{
    method: "POST",
    pattern: /^\/v1\/masterdata\/params\/versions\/([\w-]+)\/(approve|reject)$/,
    handler: ({ params, persona, body }) => {
      if (!hasRole(persona, "param_approver")) return problem(403, "forbidden", "Forbidden");
      const v = findVersion(params[0]);
      if (!v) return problem(404, "masterdata.param_version_not_found", "Not found");
      if (v.setter_ref === persona.me.actor_ref) return problem(403, "self_approval_forbidden", "Self approval forbidden");
      if (v.status !== "submitted") return problem(409, "masterdata.param_invalid_transition", "Invalid transition");
      const note = typeof (body as { note?: unknown } | undefined)?.note === "string" ? (body as { note: string }).note.trim() : "";
      if (params[1] === "reject" && note.length < 10) {
        return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "note", reason: "Lý do từ chối tối thiểu 10 ký tự" }] });
      }
      // Bộ ngưỡng % kiểm lại lúc duyệt (mốc còn lại có thể đã đổi trong lúc chờ — GIS US-CFG-01 AC-2).
      if (params[1] === "approve" && isPercentKey(v.key) && checkDisplayProposal(v.key, v.value ?? "", "")) {
        return problem(422, "masterdata.param_percent_thresholds", PERCENT_TITLE);
      }
      v.status = params[1] === "approve" ? "approved" : "rejected";
      v.approver_ref = persona.me.actor_ref;
      v.approver_name = ACTOR_NAMES[persona.me.actor_ref] ?? null;
      v.decided_at = new Date().toISOString();
      v.decision_note = note || null;
      if (v.status === "rejected") return json(v);
      const approved: AppGet<"/v1/masterdata/params/value"> = {
        key: v.key,
        value: v.value,
        text: v.text,
        unit: v.unit,
        version: v.version,
        effective_from: v.effective_from,
        source_doc: v.source_doc,
        approved_at: v.decided_at,
        param_version_id: v.id,
        supersedes_id: null,
        seq: PARAM_VERSIONS.filter((x) => x.status === "approved").length,
      };
      return json({ version: v, approved });
    },
  }];
