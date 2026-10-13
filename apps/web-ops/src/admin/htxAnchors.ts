// D2 (CEO 13/10/2026, ADR-0052): neo HTX theo mã số thuế — Admin API /v1/htx-anchors* (pf/cooperative).
// HTX tạm do Khuyến nông hoặc MKG lập ("Chưa xác nhận"), gắn MST, HTX thật nhận quyền có chạy thử.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { AdminGet, AdminPost } from "@/api/types";

export type HTXAnchor = AdminGet<"/v1/htx-anchors">["items"][number];
export type HTXAnchorDetail = AdminGet<"/v1/htx-anchors/{anchor_id}">;
export type HTXClaimReport = AdminPost<"/v1/htx-anchors/{anchor_id}/claim">;
export type HTXAnchorStatus = "provisional" | "claimed";

/** Chuẩn hóa MST như máy chủ (domain.NormalizeMST): bỏ khoảng trắng và gạch; 10 số, hoặc 13 số → "10-3"; hậu tố 000 sai. */
export function normalizeMST(raw: string): string | null {
  const s = raw.replace(/[\s\- ]/g, "");
  if (!/^[0-9]+$/.test(s)) return null;
  if (s.length === 10) return s;
  if (s.length === 13 && s.slice(10) !== "000")
    return `${s.slice(0, 10)}-${s.slice(10)}`;
  return null;
}

export const STATUS_LABEL: Record<HTXAnchorStatus, string> = {
  provisional: "HTX tạm",
  claimed: "Đã nhận quyền",
};
export const CREATED_BY_LABEL: Record<"kn" | "mkg", string> = {
  kn: "Khuyến nông",
  mkg: "MKG",
};

export function listAnchors(
  admin: AdminApiClient,
  q: { status?: HTXAnchorStatus; q?: string; after?: string },
) {
  return unwrap(
    admin.GET("/v1/htx-anchors", {
      params: {
        query: { status: q.status, q: q.q || undefined, after: q.after },
      },
    }),
  );
}

export function getAnchor(admin: AdminApiClient, id: string) {
  return unwrap(
    admin.GET("/v1/htx-anchors/{anchor_id}", {
      params: { path: { anchor_id: id } },
    }),
  );
}

export function createProvisional(
  admin: AdminApiClient,
  body: {
    org_ref: string;
    created_by: "kn" | "mkg";
    mst?: string;
    agreement_ref?: string;
  },
  idemKey: string,
) {
  return unwrap(
    admin.POST("/v1/htx-anchors", {
      params: { header: { "Idempotency-Key": idemKey } },
      body,
    }),
  );
}

export function attachMST(
  admin: AdminApiClient,
  id: string,
  mst: string,
  idemKey: string,
) {
  return unwrap(
    admin.POST("/v1/htx-anchors/{anchor_id}/mst", {
      params: {
        path: { anchor_id: id },
        header: { "Idempotency-Key": idemKey },
      },
      body: { mst },
    }),
  );
}

export function claimAnchor(
  admin: AdminApiClient,
  id: string,
  body: {
    account_ref: string;
    verified_mst: string;
    verification_ref: string;
    target_org_ref?: string;
    dry_run: boolean;
  },
  idemKey: string,
) {
  return unwrap(
    admin.POST("/v1/htx-anchors/{anchor_id}/claim", {
      params: {
        path: { anchor_id: id },
        header: { "Idempotency-Key": idemKey },
      },
      body,
    }),
  );
}

/** Tổng số dòng chuyển chủ (không sao chép) trong báo cáo nhận quyền. */
export function movedTotal(rep: Pick<HTXClaimReport, "moved">): number {
  return rep.moved.reduce(
    (n, m) =>
      n +
      m.counts.plots +
      m.counts.memberships +
      m.counts.seasons +
      m.counts.farm_logs +
      m.counts.season_areas,
    0,
  );
}
