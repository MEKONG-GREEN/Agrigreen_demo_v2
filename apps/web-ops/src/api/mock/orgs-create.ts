// GIẢ LẬP Admin POST /v1/orgs (partyCreateOrg, contracts/openapi/admin.yaml) — gói ORGUI.
// Mô phỏng quy tắc backend internal/platform/party/{domain/org.go, app/org.go}: vai (registry_admin,
// kn_province_admin, platform_admin; Nút không phải HTX chỉ platform_admin/registry_admin), phạm vi ghi, mã 1–40 ký tự, cấp ĐVHC theo loại và còn hiệu lực,
// cấp trên hợp lệ (AllowedParent) và cùng địa bàn (unitWithin) → 201 / 409 party.org_code_taken / 422.
// Idempotency-Key và phát lại do mockFetch (handler.ts) lo.
import { KIND_RULES, unitValidOn, vnToday } from "../../admin/orgCreate";
import { uuidv7 } from "../idempotency";
import type { Org, OrgKind } from "../types";
import { ADMIN_UNITS, ORG_EVENTS, ORGS } from "./data";
import { hasRole, json, problem, scopeUnits, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

const KINDS = Object.keys(KIND_RULES) as OrgKind[];
const CODE_RE = /^[A-Za-z0-9._-]{1,40}$/;

function writeAllowed(p: MockPersona, unit: string): boolean {
  const s = scopeUnits(p);
  if (!s) return true;
  if (!unit) return false; // tổ chức cấp quốc gia: chỉ quản trị toàn quốc
  const u = ADMIN_UNITS.find((x) => x.code === unit);
  return s.has(unit) || (!!u?.parent_code && s.has(u.parent_code));
}

const hierarchy = (detail: string) => problem(422, "party.invalid_hierarchy", "Invalid hierarchy", { detail });

export const orgCreateRoutes: Route[] = [
  {
    method: "POST",
    pattern: /^\/v1\/orgs$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, "registry_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden", { detail: "chỉ Admin nền tảng (platform_admin, registry_admin) được tạo tổ chức" });
      const b = (body ?? {}) as Record<string, unknown>;
      const str = (k: string) => (typeof b[k] === "string" ? (b[k] as string).trim() : "");
      const kind = str("kind") as OrgKind;
      if (!KINDS.includes(kind)) return validation([{ field: "kind", reason: "kind không hợp lệ" }]);
      const isCoop = kind === "cooperative";
      if (isCoop) return problem(422, "party.cooperative_requires_anchor", "Tạo HTX qua Neo HTX", { redirect: "/quan-tri/neo-htx" });
      // Quyết định PO 14/10/2026 (ORGUI): Nút không phải HTX chỉ Admin nền tảng tạo (app.requireNodeCreator).
      if (!isCoop && !hasRole(persona, "registry_admin", "platform_admin")) {
        return problem(403, "forbidden", "Forbidden", { detail: "chỉ Admin nền tảng (platform_admin, registry_admin) được tạo Nút hành chính / TTKN" });
      }
      const code = str("code");
      const name = str("name");
      const unit = str("admin_unit_code");
      const parentRef = str("parent_ref");
      if (isCoop && code)
        return validation([
          {
            field: "code",
            reason: "Mã HTX do hệ thống tự sinh, không nhập tay (CGH BR-01)",
          },
        ]);
      if (!isCoop && !CODE_RE.test(code)) return validation([{ field: "code", reason: "code bắt buộc, 1–40 ký tự [A-Za-z0-9._-]" }]);
      const n = [...name].length;
      if (n === 0 || n > 200) return validation([{ field: "name", reason: "name bắt buộc, tối đa 200 ký tự" }]);
      const level = KIND_RULES[kind].unit;
      if (level === "none" && unit)
        return validation([
          {
            field: "admin_unit_code",
            reason: "tổ chức cấp quốc gia không gắn đơn vị hành chính",
          },
        ]);
      if ((level === "province" || level === "commune") && !unit)
        return validation([
          {
            field: "admin_unit_code",
            reason: "admin_unit_code bắt buộc với loại tổ chức này",
          },
        ]);
      const day = vnToday();
      const u = unit ? ADMIN_UNITS.find((x) => x.code === unit && unitValidOn(x, day)) : undefined;
      if (unit && !u)
        return validation([
          {
            field: "admin_unit_code",
            reason: "không có đơn vị hành chính còn hiệu lực với mã này",
          },
        ]);
      if (u && (level === "province" || level === "commune") && u.level !== level) {
        return validation([
          {
            field: "admin_unit_code",
            reason: `loại tổ chức này phải gắn đơn vị cấp ${level}`,
          },
        ]);
      }
      if (!writeAllowed(persona, unit)) return problem(403, "party.out_of_scope", "Out of scope");

      const parent = parentRef ? ORGS.find((o) => o.ref === parentRef) : undefined;
      if (parentRef && (!parent || !parent.active)) return hierarchy("tổ chức cấp trên không tồn tại hoặc đã ngừng hoạt động");
      const rule = KIND_RULES[kind];
      if (parent ? !rule.parents.includes(parent.kind) : !rule.root) return hierarchy("tổ chức cấp trên không hợp lệ cho loại tổ chức này");
      if (parent?.admin_unit_code && u && u.code !== parent.admin_unit_code && u.parent_code !== parent.admin_unit_code) {
        return hierarchy("đơn vị hành chính không thuộc địa bàn của tổ chức cấp trên");
      }
      let finalCode = code;
      if (isCoop) finalCode = `HTX-${String(ORGS.filter((o) => o.kind === "cooperative").length + 1).padStart(5, "0")}`;
      else if (ORGS.some((o) => o.code === code)) return problem(409, "party.org_code_taken", "Org code taken");

      const org: Org = {
        ref: uuidv7(),
        kind,
        code: finalCode,
        name,
        admin_unit_code: unit || null,
        parent_ref: parent?.ref ?? null,
        representative_ref: null,
        active: true,
      };
      ORGS.push(org);
      const now = new Date().toISOString();
      ORG_EVENTS.set(org.ref, [
        {
          seq: 1,
          action: "created",
          event_time: now,
          recorded_at: now,
          actor_ref: persona.me.actor_ref,
          reason: null,
          name,
          admin_unit_code: org.admin_unit_code,
          parent_ref: org.parent_ref,
          prev_parent_ref: null,
          prev_admin_unit_code: null,
          active: true,
        },
      ]);
      return json({ org, seq: 1, representative_duplicate: false }, 201, {
        Location: `/v1/orgs/${org.ref}`,
      });
    },
  },
];
