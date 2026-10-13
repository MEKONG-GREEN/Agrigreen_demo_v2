// GIẢ LẬP neo HTX theo mã số thuế (D2 — ADR-0052) — Admin API pf/cooperative /v1/htx-anchors*.
// Hành vi giữ đúng máy chủ ở những điểm màn hình dựa vào: MST trùng → 409 cooperative.htx_anchor_mst_taken,
// MST sai dạng → 422, nhận quyền không MST → 422, chạy thử không ghi, nhận quyền lần hai → phát lại / 409.
import {
  normalizeMST,
  type HTXAnchor,
  type HTXClaimReport,
} from "@/admin/htxAnchors";
import { COOPERATIVES, hexId } from "./data";
import { hasRole, json, nowIso, pageSlice, problem, type Route } from "./http";

type Body = Record<string, unknown>;
type Event = {
  id: string;
  seq: number;
  action: string;
  anchor_key: string;
  mst: string | null;
  org_ref: string;
  verification_ref: string | null;
  details: Record<string, unknown> | null;
  actor_ref: string;
  event_time: string;
  recorded_at: string;
};

const ANCHORS: HTXAnchor[] = [];
const EVENTS = new Map<string, Event[]>();
let seq = 0;

const tempCode = (id: string) =>
  "TMP-" + id.replace(/-/g, "").slice(-12).toUpperCase();

function seed() {
  if (ANCHORS.length > 0) return;
  COOPERATIVES.slice(0, 6).forEach((c, i) => {
    const id = hexId(90000 + i, "a2c0");
    const mst = i === 2 ? null : `03${String(10000000 + i * 7919).slice(0, 8)}`;
    const claimed = i % 3 === 0;
    ANCHORS.push({
      anchor_id: id,
      anchor_key: mst ?? tempCode(id),
      mst,
      temporary: mst === null,
      status: claimed ? "claimed" : "provisional",
      created_by: i % 2 === 0 ? "kn" : "mkg",
      provisional_org_ref: c.api.ref,
      org_ref: c.api.ref,
      aliases: [c.api.ref],
      htx_account_ref: claimed ? hexId(91000 + i, "acc0") : null,
      agreement_ref: i === 1 ? "TT-HTX/2026/017" : null,
      co_owners: ["mkg", "htx"],
      confirmation_status: claimed ? "confirmed" : "unconfirmed",
      confirmation_label: claimed ? "Đã xác nhận" : "Chưa xác nhận",
      claimed_at: claimed ? nowIso() : null,
      created_at: nowIso(),
    });
    addEvent(
      id,
      claimed ? "claimed" : "provisional_created",
      mst ?? tempCode(id),
      mst,
      c.api.ref,
      null,
    );
  });
}

function addEvent(
  id: string,
  action: string,
  key: string,
  mst: string | null,
  org: string,
  details: Record<string, unknown> | null,
  vref: string | null = null,
) {
  const list = EVENTS.get(id) ?? [];
  seq++;
  list.push({
    id: hexId(92000 + seq, "e0e0"),
    seq: list.length + 1,
    action,
    anchor_key: key,
    mst,
    org_ref: org,
    verification_ref: vref,
    details,
    actor_ref: hexId(1, "ac70"),
    event_time: nowIso(),
    recorded_at: nowIso(),
  });
  EVENTS.set(id, list);
}

const reader = (p: Parameters<typeof hasRole>[0]) =>
  hasRole(
    p,
    "platform_admin",
    "registry_admin",
    "kn_national_admin",
    "kn_province_admin",
  );
const admin = (p: Parameters<typeof hasRole>[0]) =>
  hasRole(p, "platform_admin", "registry_admin");
const malformed = () =>
  problem(
    422,
    "cooperative.htx_anchor_mst_malformed",
    "Mã số thuế không đúng dạng",
  );

export const htxAnchorRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/htx-anchors$/,
    handler: ({ persona, url }) => {
      if (!reader(persona)) return problem(403, "forbidden", "Forbidden");
      seed();
      const status = url.searchParams.get("status");
      const raw = url.searchParams.get("q") ?? "";
      const q = (normalizeMST(raw) ?? raw).toUpperCase();
      const rows = ANCHORS.filter(
        (a) =>
          (!status || a.status === status) &&
          (!q || a.anchor_key.startsWith(q)),
      );
      const { slice, next } = pageSlice(rows, url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/htx-anchors$/,
    handler: ({ persona, body }) => {
      seed();
      const b = (body ?? {}) as Body;
      const by =
        b.created_by === "mkg" ? "mkg" : b.created_by === "kn" ? "kn" : null;
      if (!by)
        return problem(
          422,
          "cooperative.htx_anchor_created_by",
          "created_by phải là kn hoặc mkg",
        );
      if (by === "mkg" ? !hasRole(persona, "platform_admin") : !reader(persona))
        return problem(403, "forbidden", "Forbidden");
      const org = String(b.org_ref ?? "");
      if (!COOPERATIVES.some((c) => c.api.ref === org))
        return problem(
          422,
          "cooperative.htx_anchor_org_not_cooperative",
          "Nút không phải HTX có hồ sơ",
        );
      if (ANCHORS.some((a) => a.aliases.includes(org)))
        return problem(
          409,
          "cooperative.htx_anchor_org_anchored",
          "Nút HTX đã thuộc một neo khác",
        );
      let mst: string | null = null;
      if (typeof b.mst === "string" && b.mst.trim() !== "") {
        mst = normalizeMST(b.mst);
        if (!mst) return malformed();
        const dup = ANCHORS.find((a) => a.mst === mst);
        if (dup)
          return problem(
            409,
            "cooperative.htx_anchor_mst_taken",
            "Mã số thuế đã có neo HTX",
            {
              existing_anchor_id: dup.anchor_id,
              existing_created_by: dup.created_by,
            },
          );
      }
      const id = hexId(93000 + ANCHORS.length, "a2c0");
      const a: HTXAnchor = {
        anchor_id: id,
        anchor_key: mst ?? tempCode(id),
        mst,
        temporary: mst === null,
        status: "provisional",
        created_by: by,
        provisional_org_ref: org,
        org_ref: org,
        aliases: [org],
        htx_account_ref: null,
        agreement_ref:
          typeof b.agreement_ref === "string" && b.agreement_ref
            ? b.agreement_ref
            : null,
        co_owners: ["mkg", "htx"],
        confirmation_status: "unconfirmed",
        confirmation_label: "Chưa xác nhận",
        claimed_at: null,
        created_at: nowIso(),
      };
      ANCHORS.push(a);
      addEvent(id, "provisional_created", a.anchor_key, mst, org, null);
      return json(a, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/htx-anchors\/([0-9a-f-]{36})$/,
    handler: ({ persona, params }) => {
      if (!reader(persona)) return problem(403, "forbidden", "Forbidden");
      seed();
      const a = ANCHORS.find((x) => x.anchor_id === params[0]);
      if (!a)
        return problem(
          404,
          "cooperative.htx_anchor_not_found",
          "Không có neo HTX này",
        );
      return json({ ...a, events: EVENTS.get(a.anchor_id) ?? [] });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/htx-anchors\/([0-9a-f-]{36})\/mst$/,
    handler: ({ persona, params, body }) => {
      if (!reader(persona)) return problem(403, "forbidden", "Forbidden");
      const a = ANCHORS.find((x) => x.anchor_id === params[0]);
      if (!a)
        return problem(
          404,
          "cooperative.htx_anchor_not_found",
          "Không có neo HTX này",
        );
      const mst = normalizeMST(String((body as Body | undefined)?.mst ?? ""));
      if (!mst) return malformed();
      if (a.mst && a.mst !== mst)
        return problem(
          409,
          "cooperative.htx_anchor_mst_present",
          "Neo đã có mã số thuế",
        );
      if (ANCHORS.some((x) => x !== a && x.mst === mst))
        return problem(
          409,
          "cooperative.htx_anchor_mst_taken",
          "Mã số thuế đã có neo HTX",
        );
      a.mst = mst;
      a.anchor_key = mst;
      a.temporary = false;
      addEvent(a.anchor_id, "mst_attached", mst, mst, a.org_ref, null);
      return json(a);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/htx-anchors\/([0-9a-f-]{36})\/claim$/,
    handler: ({ persona, params, body }) => {
      if (!admin(persona)) return problem(403, "forbidden", "Forbidden");
      const a = ANCHORS.find((x) => x.anchor_id === params[0]);
      if (!a)
        return problem(
          404,
          "cooperative.htx_anchor_not_found",
          "Không có neo HTX này",
        );
      const b = (body ?? {}) as Body;
      const account = String(b.account_ref ?? "");
      const target =
        typeof b.target_org_ref === "string" && b.target_org_ref
          ? b.target_org_ref
          : a.provisional_org_ref;
      if (a.status === "claimed") {
        if (a.htx_account_ref === account && a.org_ref === target) {
          const ev = (EVENTS.get(a.anchor_id) ?? []).find(
            (e) => e.action === "claimed",
          );
          return json({ ...(ev?.details ?? {}), replayed: true });
        }
        return problem(
          409,
          "cooperative.htx_anchor_already_claimed",
          "Neo đã được nhận quyền bởi tài khoản / Nút khác",
        );
      }
      if (!a.mst)
        return problem(
          422,
          "cooperative.htx_anchor_mst_required",
          "Nhận quyền cần mã số thuế — gắn mã số thuế cho neo mã tạm trước",
        );
      if (normalizeMST(String(b.verified_mst ?? "")) !== a.mst)
        return problem(
          422,
          "cooperative.htx_anchor_mst_mismatch",
          "Mã số thuế đã xác minh khác mã số thuế của neo",
        );
      const coop = COOPERATIVES.find((c) => c.api.ref === a.org_ref);
      const counts = {
        plots: coop?.plots ?? 0,
        memberships: coop?.member_households ?? 0,
        seasons: 0,
        farm_logs: 0,
        season_areas: 0,
      };
      const conflicts =
        target !== a.org_ref &&
        COOPERATIVES.some(
          (c) => c.api.ref === target && c.member_households > 0,
        )
          ? [
              {
                org_ref: target,
                reason: "target_has_data" as const,
                counts: {
                  plots: 0,
                  memberships: 1,
                  seasons: 0,
                  farm_logs: 0,
                  season_areas: 0,
                },
              },
            ]
          : [];
      const rep: HTXClaimReport = {
        anchor_id: a.anchor_id,
        dry_run: b.dry_run === true,
        would_succeed: conflicts.length === 0,
        replayed: false,
        from: {
          party: "provisional",
          created_by: a.created_by,
          org_ref: a.org_ref,
        },
        to: { party: "htx", account_ref: account, org_ref: target },
        moved: [{ org_ref: a.org_ref, counts }],
        conflicts,
        confirmation: { before: "unconfirmed", after: "confirmed" },
        data_copied: false,
        ...(target !== a.org_ref ? { alias_added: target } : {}),
      };
      if (rep.dry_run) return json(rep);
      if (conflicts.length > 0)
        return problem(
          409,
          "cooperative.htx_anchor_claim_conflict",
          "Nút HTX đích đã có dữ liệu",
          { conflict_report: rep },
        );
      rep.event_id = hexId(94000 + ++seq, "e0e0");
      Object.assign(a, {
        status: "claimed",
        org_ref: target,
        htx_account_ref: account,
        claimed_at: nowIso(),
        confirmation_status: "confirmed",
        confirmation_label: "Đã xác nhận",
        aliases: target !== a.org_ref ? [...a.aliases, target] : a.aliases,
      });
      addEvent(
        a.anchor_id,
        "claimed",
        a.anchor_key,
        a.mst,
        target,
        rep as unknown as Record<string, unknown>,
        String(b.verification_ref ?? ""),
      );
      return json(rep);
    },
  },
];
