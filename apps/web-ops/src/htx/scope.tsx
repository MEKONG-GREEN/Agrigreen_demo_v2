"use client";

// Phạm vi HTX cho màn hình App HTX trên web vận hành:
//  - coop_manager: CHỈ HTX của mình (Principal.orgRef), không có ô chọn HTX;
//  - platform_admin: mọi HTX, ghi được;
//  - cán bộ KN: HTX trong phạm vi xã/tỉnh, CHỈ XEM (backend trả cropcycle.read_only nếu ghi).
// Đây chỉ là ẩn/hiện — backend kiểm quyền từng lời gọi (R-55).
import { useMemo, useState } from "react";
import { cooperativePlotsPage, householdsPage, listAllCooperatives } from "@/api/adapters/cooperatives";
import { collectPages } from "@/api/pagination";
import type { Cooperative, HouseholdListItem, PlotFeature } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { useAsync } from "@/lib/useAsync";

export interface HtxScope {
  /** HTX đang xem; "" = mọi HTX trong phạm vi (chỉ khi không phải quản lý HTX). */
  coopRef: string;
  setCoopRef: (ref: string) => void;
  isManager: boolean;
  canWrite: boolean;
  coops: Cooperative[];
  coopsLoading: boolean;
  coopName: (ref: string | null | undefined) => string;
}

export function useHtxScope(): HtxScope {
  const { api, principal } = useAuth();
  const isAdmin = hasRole(principal, ROLES.platformAdmin);
  const isManager = hasRole(principal, ROLES.coopManager) && !isAdmin;
  const canWrite = isAdmin || isManager;
  const list = useAsync(() => (isManager ? Promise.resolve([] as Cooperative[]) : listAllCooperatives(api, { max: 500 })), [api, isManager]);
  const [picked, setPicked] = useState("");
  const coops = useMemo(() => list.data ?? [], [list.data]);
  const coopRef = isManager ? (principal?.orgRef ?? "") : picked;
  const names = useMemo(() => new Map(coops.map((c) => [c.ref, c.name])), [coops]);
  return {
    coopRef,
    setCoopRef: setPicked,
    isManager,
    canWrite,
    coops,
    coopsLoading: list.loading,
    coopName: (ref) => (!ref ? "—" : isManager && ref === principal?.orgRef ? (principal?.orgName ?? ref) : (names.get(ref) ?? ref.slice(0, 8))),
  };
}

/** Ô chọn HTX (ẩn với quản lý HTX — chỉ hiện tên HTX của mình). */
export function CoopPicker({ scope, allowAll = true, id = "htx-coop" }: { scope: HtxScope; allowAll?: boolean; id?: string }) {
  const { principal } = useAuth();
  if (scope.isManager) {
    return (
      <div className="field">
        <span className="lbl">HTX</span>
        <b>{principal?.orgName ?? "HTX của tôi"}</b>
      </div>
    );
  }
  return (
    <div className="field" style={{ minWidth: 240 }}>
      <label htmlFor={id}>HTX</label>
      <select id={id} className="select" value={scope.coopRef} onChange={(e) => scope.setCoopRef(e.target.value)}>
        {allowAll ? <option value="">Tất cả HTX trong phạm vi</option> : <option value="">— Chọn HTX —</option>}
        {scope.coops.map((c) => (
          <option key={c.ref} value={c.ref}>
            {c.name} ({c.code})
          </option>
        ))}
      </select>
    </div>
  );
}

/** Thông báo chỉ xem cho cán bộ KN (pf/cropcycle: KN chỉ đọc theo phạm vi xã). */
export function ReadOnlyNotice({ scope }: { scope: HtxScope }) {
  if (scope.canWrite) return null;
  return (
    <div className="alert alert-info" style={{ marginBottom: 14 }} role="note">
      <Icon name="info" />
      <span>Cán bộ khuyến nông chỉ xem dữ liệu sản xuất của HTX trong phạm vi được phân quyền; ghi và duyệt do Quản lý HTX thực hiện.</span>
    </div>
  );
}

/** Thửa đang dùng + nông hộ của một HTX (gom trang) để chọn lô khi mở vụ / ghi nhật ký. */
export function useCoopFields(coopRef: string): {
  plots: PlotFeature[];
  households: HouseholdListItem[];
  loading: boolean;
  householdLabel: (ref: string | null | undefined) => string;
} {
  const { api } = useAuth();
  const plots = useAsync(
    () => (coopRef ? collectPages((c) => cooperativePlotsPage(api, coopRef, c, 200), 2000) : Promise.resolve([] as PlotFeature[])),
    [api, coopRef],
  );
  const households = useAsync(
    () => (coopRef ? collectPages((c) => householdsPage(api, coopRef, c, 200), 2000) : Promise.resolve([] as HouseholdListItem[])),
    [api, coopRef],
  );
  const labels = useMemo(() => new Map((households.data ?? []).map((h) => [h.household_ref, h.label_unavailable || !h.label ? "Nông hộ (không hiện nhãn)" : h.label])), [households.data]);
  return {
    plots: (plots.data ?? []).filter((p) => p.properties.status === "active"),
    households: households.data ?? [],
    loading: plots.loading || households.loading,
    householdLabel: (ref) => (ref ? (labels.get(ref) ?? `Nông hộ ${ref.slice(-4)}`) : "—"),
  };
}
