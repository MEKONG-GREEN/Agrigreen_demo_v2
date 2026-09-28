// GIẢ LẬP sổ xung đột đồng bộ pf/sync (Admin API, US-SYNC-03). Hình dạng theo SyncConflict + chi tiết
// (records không payload, versions do module sở hữu cung cấp, không PII). Hạn SLA 72 giờ (C-8).
import type { SlaStatus, SyncConflict, SyncConflictDetail } from "../types";
import { COOPERATIVES, hexagon, ORGS, plotsOf } from "./data";
import { isoHours, opaque } from "./http";

export interface MockSyncConflict {
  c: SyncConflict;
  records: SyncConflictDetail["records"];
  versions: SyncConflictDetail["versions"];
  resolution: SyncConflictDetail["resolution"];
}

/** sla_status theo due_at: quá hạn / còn dưới 24 giờ / đúng tiến độ; đã đóng → đúng hạn / trễ. */
export function slaStatusOf(c: Pick<SyncConflict, "status" | "due_at" | "resolved_at">, now = Date.now()): SlaStatus {
  const due = Date.parse(c.due_at);
  if (c.status === "resolved") return c.resolved_at && Date.parse(c.resolved_at) > due ? "resolved_late" : "resolved_on_time";
  if (due < now) return "overdue";
  if (due - now < 24 * 3_600_000) return "due_soon";
  return "on_track";
}

const coop = COOPERATIVES.find((c) => c.api.active && c.plots > 15)!;
const plots = plotsOf(coop);
const devices = ["01927b3e-de71-7000-8000-000000000001", "01927b3e-de71-7000-8000-000000000002", "01927b3e-de71-7000-8000-000000000003"];
const owner = ORGS.find((o) => o.kind === "extension_team" && o.admin_unit_code === coop.api.admin_unit_code)?.ref ?? null;

function make(i: number, entity: string, openedHoursAgo: number, resolved = false): MockSyncConflict {
  const id = `01927b3e-5c0f-7000-8000-${String(i).padStart(12, "0")}`;
  const recA = `01927b3e-5ec0-7000-8000-${String(i * 10 + 1).padStart(12, "0")}`;
  const recB = `01927b3e-5ec0-7000-8000-${String(i * 10 + 2).padStart(12, "0")}`;
  const opened = isoHours(-openedHoursAgo);
  const due = isoHours(72 - openedHoursAgo);
  const plot = plots[i % plots.length]!;
  const ring = (plot.geometry.coordinates[0] as number[][])[0]!;
  const record = (rid: string, dev: string, hoursAgo: number, supersedes: string | null) => ({
    record_id: rid,
    record_idem_key: `${dev}:${rid.slice(-6)}`,
    entity,
    op: supersedes ? "correct" : "upsert",
    status: "conflict",
    device_id: dev,
    actor_ref: dev === devices[0] ? "mock-xa-0001" : "mock-knv-82102-1",
    event_time: isoHours(-hoursAgo),
    supersedes_id: supersedes,
    payload_sha256: `${rid.replace(/-/g, "").slice(0, 16)}`.padEnd(64, "a"),
  });
  const versions: SyncConflictDetail["versions"] =
    entity === "plot_boundary"
      ? [
          {
            record_id: recA, role: "record", label: `Bản từ máy ${devices[0]!.slice(-4)} (KNV xã)`, current: false, event_time: isoHours(-openedHoursAgo - 1),
            data: opaque({ plot_ref: plot.properties.ref, plot_code: plot.properties.code, area_ha: plot.properties.area_ha, geometry: { type: "Polygon", coordinates: hexagon([ring[0]! + 0.0005, ring[1]! + 0.0003], 0.0013) } }),
          },
          {
            record_id: recB, role: "current", label: "Bản hiện hành trên máy chủ", current: true, event_time: isoHours(-openedHoursAgo - 3),
            data: opaque({ plot_ref: plot.properties.ref, plot_code: plot.properties.code, area_ha: plot.properties.area_ha, geometry: plot.geometry }),
          },
        ]
      : [
          { record_id: recA, role: "record", label: "Bản từ máy (ngoại tuyến)", current: false, event_time: isoHours(-openedHoursAgo - 1), data: opaque({ status: "resolved", note: "Đã phun thuốc theo khuyến cáo" }) },
          { record_id: recB, role: "current", label: "Bản hiện hành", current: true, event_time: isoHours(-openedHoursAgo - 2), data: opaque({ status: "in_progress", note: "Đang theo dõi mật độ rầy" }) },
        ];
  return {
    c: {
      conflict_id: id,
      entity,
      first_record_id: recA,
      device_id: devices[i % devices.length]!,
      reported_by: "mock-xa-0001",
      owner_org_ref: owner,
      opened_at: opened,
      due_at: due,
      status: resolved ? "resolved" : "open",
      overdue: !resolved && Date.parse(due) < Date.now(),
      resolved_at: resolved ? isoHours(-openedHoursAgo + 10) : null,
      resolved_by: resolved ? "mock-admin-setter" : null,
      resolution_note: resolved ? "Giữ bản hiện hành sau khi đối chiếu với KNV xã" : null,
      resolved_record_id: resolved ? recB : null,
    },
    records: [record(recA, devices[0]!, openedHoursAgo + 1, null), record(recB, devices[1]!, openedHoursAgo + 3, null)],
    versions,
    resolution: resolved ? "keep_version" : null,
  };
}

export const SYNC_CONFLICTS_STORE: MockSyncConflict[] = [
  make(1, "plot_boundary", 80), // quá hạn
  make(2, "plot_boundary", 55), // còn dưới 24 giờ
  make(3, "task_update", 20), // đúng tiến độ
  make(4, "field_report", 10),
  make(5, "plot_boundary", 100, true),
];

/** Danh sách phẳng (dùng cho deep link thông báo). */
export const SYNC_CONFLICTS: SyncConflict[] = SYNC_CONFLICTS_STORE.map((x) => x.c);
