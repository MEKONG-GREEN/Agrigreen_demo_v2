// Thông báo hàng đến giả lập (mkg/hub arrival_current, proto FM-05): consumer mkg.hub.trip_milestone đọc mốc chuyến
// của mkg/logistics — chuyến có chặng cuối tới cơ sở: xuất phát / đổi đích → expected (cho_den); đến / hoàn tất →
// arrived (da_den); hủy → cancelled (huy). Một bản ghi mỗi chuyến, mới nhất trước.
import type { HubArrival, LogisticsTrip } from "../types";
import { forbidden, hasRole, json, pageSlice, route, validation, UUID_RE, type Route } from "./http";

interface ArrivalRec {
  a: HubArrival;
  touched: string;
}
const arrivals: ArrivalRec[] = [];

const PROTO: Record<HubArrival["status"], HubArrival["proto_status"]> = { expected: "cho_den", arrived: "da_den", cancelled: "huy" };

/** Gọi sau mỗi thay đổi chuyến (mốc, đổi chặng, hoàn tất, hủy) — như consumer của sự kiện milestone_recorded. */
export function syncArrival(t: LogisticsTrip, eventId: string, at: string, vesselCode: string | null): void {
  const legs = [...(t.legs ?? [])].sort((a, b) => a.seq - b.seq);
  const dest = legs.at(-1)?.to;
  const toFacility = dest?.kind === "facility" && !!dest.ref;
  const rec = arrivals.find((x) => x.a.trip_id === t.trip_id);
  let status: HubArrival["status"] | null = null;
  if (t.status === "cancelled") status = rec ? "cancelled" : null;
  else if (!toFacility) status = rec && rec.a.status === "expected" ? "cancelled" : null;
  else if (t.status === "departed") status = "expected";
  else if (t.status === "arrived" || t.status === "completed") status = "arrived";
  if (!status) return;
  const base: HubArrival = rec?.a ?? {
    trip_id: t.trip_id,
    trip_code: t.code,
    facility_ref: dest?.ref ?? "",
    status,
    proto_status: PROTO[status],
    mode: t.mode,
    vessel_code: null,
    collection_order_ref: t.collection_order_ref ?? null,
    lot_refs: [],
    planned_kg: null,
    expected_since: null,
    arrived_at: null,
    cancelled_at: null,
    last_event_id: eventId,
  };
  const a: HubArrival = {
    ...base,
    facility_ref: toFacility ? dest!.ref! : base.facility_ref,
    status,
    proto_status: PROTO[status],
    mode: t.mode,
    vessel_code: vesselCode,
    collection_order_ref: t.collection_order_ref ?? null,
    lot_refs: Array.from(new Set((t.cargo ?? []).flatMap((c) => (c.lot_ref ? [c.lot_ref] : [])))),
    planned_kg: t.planned_kg ?? null,
    expected_since: status === "expected" ? (base.status === "expected" && base.facility_ref === dest?.ref ? base.expected_since : (t.departed_at ?? at)) : base.expected_since ?? t.departed_at ?? null,
    arrived_at: status === "arrived" ? (t.arrived_at ?? t.completed_at ?? at) : null,
    cancelled_at: status === "cancelled" ? (t.cancelled_at ?? at) : null,
    last_event_id: eventId,
  };
  if (rec) {
    rec.a = a;
    rec.touched = at;
  } else arrivals.push({ a, touched: at });
}

/** Số dòng arrival_current (cho POST /v1/hub/projections/rebuild). */
export function arrivalCount(): number {
  return arrivals.length;
}

export const arrivalRoutes: Route[] = [
  route("GET", "/v1/hub/arrivals", ({ url, persona }) => {
    if (!hasRole(persona, "hub_operator", "hub_approver")) return forbidden();
    const fid = url.searchParams.get("facility_id");
    const st = url.searchParams.get("status");
    if (fid && !UUID_RE.test(fid)) return validation([{ field: "facility_id", reason: "UUID" }]);
    if (st && !["expected", "arrived", "cancelled"].includes(st)) return validation([{ field: "status", reason: "expected | arrived | cancelled" }]);
    const list = arrivals
      .filter((x) => (!fid || x.a.facility_ref === fid) && (!st || x.a.status === st))
      .sort((a, b) => b.touched.localeCompare(a.touched) || b.a.trip_id.localeCompare(a.a.trip_id))
      .map((x) => x.a);
    return json(pageSlice(list, url));
  }),
];
