// Tổng hợp hiển thị sổ sử dụng (pf/metering, ADR-0023). Đơn vị là số nguyên (metering.units.v1) — không tiền.
import type { EgressEvent, MeteringUsageDay, MeteringUsageEvent } from "@/api/types";
import { addDays } from "@/lib/vnDate";

export interface ProductTotal {
  product: string;
  calls: number;
  units: number;
  days: number;
}

/** Tổng theo sản phẩm (sắp theo tên sản phẩm) và tổng chung. */
export function summarizeUsage(days: readonly MeteringUsageDay[]): { products: ProductTotal[]; calls: number; units: number } {
  const by = new Map<string, ProductTotal>();
  for (const d of days) {
    const t = by.get(d.product) ?? { product: d.product, calls: 0, units: 0, days: 0 };
    t.calls += d.calls;
    t.units += d.units;
    t.days += 1;
    by.set(d.product, t);
  }
  const products = [...by.values()].sort((a, b) => a.product.localeCompare(b.product));
  return { products, calls: products.reduce((a, p) => a + p.calls, 0), units: products.reduce((a, p) => a + p.units, 0) };
}

/** Khoảng ngày (giờ VN, bao gồm hai đầu) của kỳ tháng "YYYY-MM"; tháng hiện tại thì tới hôm nay. */
export function periodRange(period: string, today: string): { from: string; to: string } {
  const from = `${period}-01`;
  const [y, m] = period.split("-").map((x) => Number.parseInt(x, 10)) as [number, number];
  const next = m === 12 ? `${y + 1}-01-01` : `${y}-${String(m + 1).padStart(2, "0")}-01`;
  const last = addDays(next, -1);
  return { from, to: last < today ? last : today };
}

/** Danh sách kỳ để chọn: tháng hiện tại và n-1 tháng trước ("YYYY-MM", mới nhất trước). */
export function recentPeriods(today: string, n = 6): string[] {
  const [y, m] = today.split("-").map((x) => Number.parseInt(x, 10)) as [number, number];
  const out: string[] = [];
  for (let i = 0; i < n; i++) {
    const idx = y * 12 + (m - 1) - i;
    out.push(`${Math.floor(idx / 12)}-${String((idx % 12) + 1).padStart(2, "0")}`);
  }
  return out;
}

export const CALL_KIND_LABEL: Record<MeteringUsageEvent["call_kind"], string> = {
  read: "Đọc",
  export_request: "Yêu cầu xuất",
  job_status: "Trạng thái việc",
  file_download: "Tải tệp",
  meta: "Siêu dữ liệu",
};

/** Lớp trạng thái HTTP như bảng kê ("2xx" | "4xx" | "5xx"). */
export function statusClass(status: number): string {
  return `${Math.floor(status / 100)}xx`;
}

/**
 * Lọc sổ xuất theo bên nhận: mã tham chiếu (khớp tiền tố) hoặc tên hiển thị (chứa), không phân biệt hoa thường.
 * Lọc phía client trên các trang đã tải — Admin API chỉ lọc theo kênh và ngày.
 */
export function filterByCounterparty(events: readonly EgressEvent[], q: string, label: (e: EgressEvent) => string = () => ""): EgressEvent[] {
  const s = q.trim().toLowerCase();
  if (!s) return [...events];
  return events.filter((e) => e.counterparty_ref.toLowerCase().startsWith(s) || label(e).toLowerCase().includes(s));
}
