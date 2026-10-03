import { expect, type Page } from "@playwright/test";

/** Mã persona giả lập (src/api/mock/personas.ts). */
export type Persona =
  | "tw"
  | "tinh"
  | "xa"
  | "htx"
  | "cuc"
  | "setter"
  | "approver"
  | "account-tinh"
  | "admin-no-mfa"
  | "api-admin"
  | "api-approver"
  | "metering-viewer";

const MOCK_KEY = "ag.mock.persona";

/**
 * Bỏ qua màn hình đăng nhập ở chế độ giả lập: đặt persona vào sessionStorage trước khi trang chạy,
 * rồi mở `path`. Dữ liệu giả lập nằm trong bộ nhớ tab — mỗi lần page.goto là trạng thái mới.
 */
export async function loginAs(page: Page, persona: Persona, path = "/"): Promise<void> {
  await page.goto("/login");
  await page.evaluate(([k, v]) => window.sessionStorage.setItem(k, v), [MOCK_KEY, persona] as const);
  await page.goto(path);
  await expect(sidebar(page)).toBeVisible();
}

/** Đăng nhập qua giao diện /login (chọn vai thử) — dùng khi cần giữ nguyên trạng thái giả lập trong tab. */
export async function loginViaUi(page: Page, title: string | RegExp): Promise<void> {
  await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
  await page.getByRole("button", { name: title }).click();
}

/** Đăng xuất qua menu tài khoản ở thanh trên (điều hướng phía client, không tải lại trang). */
export async function logout(page: Page): Promise<void> {
  await accountMenu(page).click();
  await page.getByRole("menuitem", { name: "Đăng xuất" }).click();
  await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
}

/** Nút menu tài khoản (họ tên, vai trò · phòng ban) ở thanh trên — khung P6. */
export function accountMenu(page: Page) {
  return page.getByTestId("account-menu");
}

export function sidebar(page: Page) {
  return page.getByRole("complementary", { name: "Điều hướng chính" });
}

/** Đi tới màn hình bằng menu (không tải lại trang → giữ trạng thái giả lập). */
export async function navTo(page: Page, label: string | RegExp): Promise<void> {
  await sidebar(page).getByRole("link", { name: label }).click();
}
