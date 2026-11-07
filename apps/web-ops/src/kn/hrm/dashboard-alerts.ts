// FN-06 warnings consume FN-09 current regular due dates; no rating/discipline condition.
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
    const day=promo.as_of;
    const start=new Date(`${day}T00:00:00Z`);
    const end=new Date(Date.UTC(start.getUTCFullYear(),start.getUTCMonth()+3,start.getUTCDate())).toISOString().slice(0,10);
    const regular=promo.items.filter(c=>c.kind==="nltx"&&c.status!=="decided"&&c.due<=end);
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
        description: "Kiểm tra ngày hưởng bậc hiện tại (NB-06), thời gian xét (NB-07), ngạch và bậc trong hồ sơ",
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
