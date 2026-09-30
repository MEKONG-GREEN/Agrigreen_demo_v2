import { expect, type Page } from "@playwright/test";

/** Mã persona giả lập (src/api/mock/personas.ts). */
export type Persona = "dieu-phoi" | "phe-duyet" | "hub" | "can" | "hien-truong" | "van-tai" | "tai-xe" | "kinh-doanh" | "truong-kinh-doanh" | "ban-hang" | "ke-toan" | "ke-toan-truong";

export const TITLES: Record<Persona, string> = {
  "dieu-phoi": "Điều phối vận hành MKG (đã qua MFA)",
  "phe-duyet": "Trưởng bộ phận — người phê duyệt (đã qua MFA)",
  hub: "Nhân viên hub — chưa qua MFA",
  can: "Giám sát cân (đã qua MFA)",
  "hien-truong": "Tổ gom hiện trường",
  "van-tai": "Điều phối vận tải (đã qua MFA)",
  "tai-xe": "Tài xế / chủ ghe",
  "kinh-doanh": "Nhân viên kinh doanh — có vai duyệt (đã qua MFA)",
  "truong-kinh-doanh": "Trưởng kinh doanh — người duyệt (đã qua MFA)",
  "ban-hang": "Nhân viên bán hàng (không có vai duyệt)",
  "ke-toan": "Kế toán giá thành (đã qua MFA)",
  "ke-toan-truong": "Kế toán trưởng (đã qua MFA)",
};

/** Mã tất định của dữ liệu giả lập (src/api/mock/db.ts — sid(nhóm, số)). */
const sid = (g: number, n: number) => `0192f${g.toString(16).padStart(3, "0")}-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
export const IDS = {
  lotProto: sid(3, 1),
  lotDispute: sid(3, 2),
  lotArriving: sid(3, 3),
  orderAssigned: sid(6, 2),
  orderApproved: sid(6, 3),
  orderPlanned: sid(6, 4),
  htxTanHiep: sid(7, 2),
  plotB2: sid(8, 6),
  crew: sid(9, 2),
} as const;

/** Kỳ tháng theo giờ Việt Nam, lùi `back` tháng (như src/api/mock/g4common.ts monthVn). */
export function monthVn(back = 0): string {
  const t = new Date(Date.now() + 7 * 3_600_000);
  return new Date(Date.UTC(t.getUTCFullYear(), t.getUTCMonth() - back, 1)).toISOString().slice(0, 7);
}

/** Thông báo tự phê duyệt bị chặn (R-56). */
export const SELF_APPROVAL = /Không được tự phê duyệt/;

const MOCK_KEY = "agi.mock.persona";

/** Bỏ qua màn hình đăng nhập: đặt persona vào sessionStorage rồi mở `path` (trạng thái giả lập mới). */
export async function loginAs(page: Page, persona: Persona, path = "/"): Promise<void> {
  await page.goto("/login");
  await page.evaluate(([k, v]) => window.sessionStorage.setItem(k, v), [MOCK_KEY, persona] as const);
  await page.goto(path);
  await expect(sidebar(page)).toBeVisible();
}

/** Đổi người dùng KHÔNG tải lại trang (giữ trạng thái giả lập) — dùng cho kiểm thử phân tách nhiệm vụ (R-56). */
export async function switchUser(page: Page, persona: Persona): Promise<void> {
  await page.getByRole("button", { name: "Đăng xuất" }).click();
  await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
  await page.getByRole("button", { name: TITLES[persona] }).click();
  await expect(sidebar(page)).toBeVisible();
}

export function sidebar(page: Page) {
  return page.getByRole("complementary", { name: "Điều hướng chính" });
}

/** Đi tới màn hình bằng menu (không tải lại trang → giữ trạng thái giả lập). */
export async function navTo(page: Page, label: string | RegExp): Promise<void> {
  await sidebar(page).getByRole("link", { name: label }).click();
}

export function status(page: Page) {
  return page.getByRole("status").filter({ hasNotText: "Đang tải" });
}

/** Thông báo lỗi của trang (bỏ vùng thông báo điều hướng của Next.js cũng mang role="alert"). */
export function alert(page: Page) {
  return page.locator('[role="alert"]:not(#__next-route-announcer__)');
}

/** Vùng không phải ô nhập có aria-label (khối thông tin, bảng kê, panel). */
export function labelled(page: Page, name: string) {
  return page.locator(`[aria-label="${name}"]`);
}
