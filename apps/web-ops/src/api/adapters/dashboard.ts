// Bảng điều hành gộp — GET /v1/dashboard (G-02, US-DASH-01): KPI pf/extension + HTX/hộ pf/party + thửa số hoá pf/geo
// + mục tiêu diện tích (tham số có phê duyệt). Tên đơn vị con do backend trả. Số thập phân giữ chuỗi (R-10).
import type { ApiClient } from "../client";
import { unwrap } from "../client";
import type { UnifiedDashboard, UnifiedFigures } from "../types";
import { toScaled } from "@/lib/decimal";

export function loadDashboard(api: ApiClient, query: { unit?: string; period?: string } = {}): Promise<UnifiedDashboard> {
  return unwrap(api.GET("/v1/dashboard", { params: { query } }));
}

/**
 * Tỷ lệ diện tích đã gieo sạ so với mục tiêu, dạng % (số) CHỈ để vẽ thanh tiến độ. Tính trên số nguyên
 * đã nhân tỉ lệ (không cộng/chia float trên chuỗi thập phân). Chưa có mục tiêu hoặc mục tiêu 0 → null.
 */
export function progressPct(f: Pick<UnifiedFigures, "sown_area_ha" | "target_area_ha">): number | null {
  if (f.target_area_ha === null) return null;
  const sown = toScaled(f.sown_area_ha);
  const target = toScaled(f.target_area_ha);
  if (sown === null || target === null || target <= 0n) return null;
  return Number((sown * 10000n) / target) / 100;
}
