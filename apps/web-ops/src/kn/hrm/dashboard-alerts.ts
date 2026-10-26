// Khối "Cảnh báo" của Dashboard nhân sự theo prototype HCTH (màn `dash`, W11.29). Chỉ ghép từ API có sẵn: danh sách nâng
// bậc khung 3 tháng (W11.25 — /v1/hrm/promotion?window=3, chỉ HCTH/Admin) và số vào làm/nghỉ việc của dashboard
// (W11.28). Cảnh báo về phiếu đánh giá (W11.26) và kế hoạch nhân lực (W11.27) chưa có API trong gói này.
import type { DashboardAlert } from "@/components/patterns/dashboard";
import type { PromotionList } from "./promotion-api";

export const PROMO_ROUTE = "/kn/nhan-su/nang-bac";
export const WORK_ROUTE = "/kn/nhan-su/ho-so/thong-tin-lam-viec";

export function hrmDashboardAlerts(
  promo: PromotionList | null,
  moves: { joined: number; left: number },
  at: string,
): DashboardAlert[] {
  const out: DashboardAlert[] = [];
  if (promo) {
    const regular = promo.items.filter((c) => c.kind !== "nlth" && c.status !== "decided");
    const bad = regular.find((c) => c.cond === "fail") ?? regular.find((c) => c.cond === "review");
    if (bad)
      out.push({
        id: `promo-${bad.key}`,
        level: bad.cond === "fail" ? "urgent" : "attention",
        title: `${bad.name || bad.code} ${bad.cond === "fail" ? "chưa đủ điều kiện" : "cần xem xét"} nâng bậc`,
        description: `Đến kỳ ${bad.due.split("-").reverse().join("/")}${bad.why.length ? ` · ${bad.why.join("; ")}` : ""}`,
        at,
        href: `${PROMO_ROUTE}/${encodeURIComponent(bad.key)}`,
      });
    out.push({
      id: "promo-due",
      level: regular.length ? "attention" : "info",
      title: `${regular.length} viên chức đến hạn nâng bậc trong 3 tháng tới`,
      description: "Mở danh sách nâng bậc (FN-09)",
      at,
      href: PROMO_ROUTE,
    });
    if (promo.missing.length)
      out.push({
        id: "promo-missing",
        level: "info",
        title: `${promo.missing.length} viên chức chưa tính được kỳ nâng bậc`,
        description: "Thiếu ngạch, bậc, bảng hệ số hoặc mốc xếp bậc trong hồ sơ",
        at,
        href: PROMO_ROUTE,
      });
  }
  if (moves.joined || moves.left)
    out.push({
      id: "work-moves",
      level: "info",
      title: `Biến động trong tháng: +${moves.joined} vào làm / −${moves.left} nghỉ việc`,
      description: "Mở Thông tin làm việc (FN-08)",
      at,
      href: WORK_ROUTE,
    });
  return out;
}
