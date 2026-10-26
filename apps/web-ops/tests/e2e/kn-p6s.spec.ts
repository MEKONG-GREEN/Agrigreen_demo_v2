import { expect, test, type Page } from "@playwright/test";
import { accountMenu, sidebar } from "./helpers";

// KN v7.7 P6s — vòng đời báo cáo đầu–cuối trên backend giả lập (một tab: đổi người dùng bằng Đăng xuất → Đăng nhập):
// Tỉnh phát hành → Xã nhập + gửi (2 tab xã) → Tỉnh "Cập nhật", yêu cầu điều chỉnh → Xã gửi lại → Tỉnh "Cập nhật" có nhật ký
// ô → chốt kỳ → Chi cục xuất + gửi → TW xem dashboard/báo cáo chỉ đọc. Admin: tạo tài khoản 2 vai trò → người dùng đổi vai
// trò trong menu tài khoản; Menu các cấp: dải vàng hỗ trợ + nhật ký.
const PASSWORD = "KhuyenNong@2026";
const menuNav = (page: Page) => sidebar(page).getByRole("navigation", { name: "Menu chức năng" });
// BF-MENU (#98): Cấp Tỉnh Trồng trọt — "Báo cáo tổng hợp các xã" là nhóm: Báo cáo tuần › T01 · Tiến độ XG (/kn/bao-cao/tong-hop).
const TONG_HOP_TUAN = [/^Báo cáo tổng hợp các xã/, /^Báo cáo tuần/] as const;

async function login(page: Page, identifier: string): Promise<void> {
  await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
  await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
  await page.getByLabel("Mật khẩu", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
}

async function enter(page: Page, identifier: string, role?: RegExp): Promise<void> {
  await login(page, identifier);
  if (role) {
    await expect(page).toHaveURL(/\/chon-vai-tro/);
    await page.getByRole("button", { name: role }).click();
    await page.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }).click();
  }
  await expect(accountMenu(page)).toBeVisible();
}

async function logout(page: Page): Promise<void> {
  await accountMenu(page).click();
  await page.getByRole("menuitem", { name: "Đăng xuất" }).click();
  // Khung chuyển hướng về /login?returnTo=… sau khi phiên kết thúc — chờ xong rồi mới gõ để không mất thao tác.
  await expect(page).toHaveURL(/\/login\?returnTo=/);
  await expect(page.getByRole("heading", { name: "Đăng nhập" })).toBeVisible();
}

async function go(page: Page, label: string | RegExp, parent?: RegExp | readonly RegExp[]): Promise<void> {
  const nav = menuNav(page);
  const parents: readonly RegExp[] = parent === undefined ? [] : parent instanceof RegExp ? [parent] : parent;
  for (const p of parents) {
    const btn = nav.getByRole("button", { name: p });
    if ((await btn.getAttribute("aria-expanded")) === "false") await btn.click();
  }
  await nav.getByRole("link", { name: label }).click();
}

async function typeCell(page: Page, letter: string, value: string): Promise<void> {
  const grid = page.getByRole("grid").first();
  await grid.locator(`tbody [data-letter="${letter}"]`).first().click();
  await page.keyboard.type(value);
  await page.keyboard.press("Enter");
}

test.describe("KN v7.7 P6s: vòng đời báo cáo các cấp", () => {
  test("Tỉnh phát hành → Xã nhập 2 xã → điều chỉnh → gửi lại → Cập nhật có nhật ký ô → chốt → Chi cục → TW chỉ xem", async ({ page }) => {
    test.setTimeout(240_000);
    await page.goto("/login");

    // 1. Cấp Tỉnh phát hành T01 cho 2 xã (Xem trước: 1 biểu × 2 xã × 4 kỳ).
    await enter(page, "binh.tt@angiang.gov.vn", /^Cấp Tỉnh/);
    await go(page, "Phát hành biểu mẫu", /Nhiệm vụ/);
    await expect(page.getByRole("heading", { level: 1, name: "Phát hành biểu mẫu báo cáo" })).toBeVisible();
    await page.getByLabel("Chọn T01").check();
    await page.getByRole("radio", { name: "Chọn từng xã" }).click();
    await page.getByLabel("Mã xã (cách nhau dấu phẩy)").fill("91301,91302");
    // BF-UI-KIT (#97) TextField: dấu * bắt buộc chỉ hiển thị (aria-hidden), trường đánh dấu aria-required.
    await expect(page.getByLabel("Tên vụ / đợt")).toHaveAttribute("aria-required", "true");
    await page.getByLabel("Tên vụ / đợt").fill("Đợt E2E");
    await page.getByRole("button", { name: /Xem trước/ }).click();
    await expect(page.getByTestId("task-count")).toHaveText("8");
    await page.getByLabel("T01 D 91301").fill("126");
    await page.getByLabel("T01 D 91302").fill("290");
    await page.getByRole("button", { name: "Phát hành", exact: true }).click();
    await expect(page.getByText(/Đã phát hành 8 công việc cho 2 xã, 4 kỳ/)).toBeVisible();
    await logout(page);

    // 2. Cấp Xã nhập T01 Tuần 1 cho 2 xã (mỗi xã một tab) và gửi.
    await enter(page, "an.nv@angiang.gov.vn", /^Cấp Xã/);
    await expect(page.getByRole("heading", { level: 1, name: "Công việc cần làm" })).toBeVisible();
    await page.getByRole("link", { name: "Nhập: T01 — Mỹ Hòa Hưng — Tuần 1" }).click();
    await expect(page.getByRole("tablist", { name: "Xã nhập liệu" })).toBeVisible();
    await typeCell(page, "F", "28");
    await page.getByRole("tab", { name: /Bình Đức/ }).click();
    await typeCell(page, "F", "30");
    await page.getByRole("button", { name: "Gửi báo cáo" }).click();
    await expect(page.getByText("Đã gửi báo cáo: Mỹ Hòa Hưng, Bình Đức.")).toBeVisible();
    await logout(page);

    // 3. Cấp Tỉnh: Cập nhật lần 1 → yêu cầu Bình Đức điều chỉnh (lý do bắt buộc).
    await enter(page, "binh.tt@angiang.gov.vn", /^Cấp Tỉnh/);
    await go(page, "T01 · Tiến độ XG", TONG_HOP_TUAN);
    await page.getByLabel("Vụ/đợt").selectOption({ label: "Đợt E2E" });
    await page.getByRole("button", { name: "Cập nhật" }).click();
    await expect(page.getByText(/Đã cập nhật T01: 2 xã/)).toBeVisible();
    await go(page, /Rà soát & chốt kỳ/, /Nhiệm vụ/);
    await page.getByRole("link", { name: "Rà soát T01 Bình Đức Tuần 1" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Bình Đức · T01 · Tuần 1" })).toBeVisible();
    await page.getByRole("button", { name: "Yêu cầu điều chỉnh" }).click();
    // BF-SWEEP-TT (#100): lý do bắt buộc được kiểm ngay trên trường (không gọi API), lỗi gắn với ô.
    const adjReason = page.getByLabel("Lý do yêu cầu điều chỉnh");
    await expect(adjReason).toHaveAttribute("aria-required", "true");
    await expect(adjReason).toHaveAttribute("aria-invalid", "true");
    await expect(page.getByRole("alert").filter({ hasText: "Bắt buộc nhập." })).toBeVisible();
    await page.getByLabel("Lý do yêu cầu điều chỉnh").fill("Kiểm tra lại diện tích tuần 1");
    await page.getByRole("button", { name: "Yêu cầu điều chỉnh" }).click();
    await expect(page.getByText("Đã gửi yêu cầu điều chỉnh tới Cán bộ xã.")).toBeVisible();
    await logout(page);

    // 4. Cấp Xã thấy lý do, sửa Bình Đức và gửi lại.
    await enter(page, "an.nv@angiang.gov.vn", /^Cấp Xã/);
    await expect(page.getByText("Yêu cầu điều chỉnh: Kiểm tra lại diện tích tuần 1")).toBeVisible();
    await page.getByRole("link", { name: "Sửa: T01 — Bình Đức — Tuần 1" }).click();
    await page.getByRole("tab", { name: /Bình Đức/ }).click();
    await typeCell(page, "F", "25");
    await page.getByRole("button", { name: "Gửi báo cáo" }).click();
    await expect(page.getByText("Đã gửi báo cáo: Bình Đức.")).toBeVisible();
    await logout(page);

    // 5. Cấp Tỉnh: "Có 1 thay đổi mới" → Cập nhật → nhật ký ô (Điều chỉnh theo yêu cầu, Tự tính), ô viền vàng → chốt kỳ.
    await enter(page, "binh.tt@angiang.gov.vn", /^Cấp Tỉnh/);
    await go(page, "T01 · Tiến độ XG", TONG_HOP_TUAN);
    await page.getByLabel("Vụ/đợt").selectOption({ label: "Đợt E2E" });
    await expect(page.getByRole("status", { name: "Thay đổi chờ cập nhật" })).toContainText("Có 1 thay đổi mới từ 1 xã");
    await page.getByRole("button", { name: "Cập nhật" }).click();
    await expect(page.getByText("Đã cập nhật T01: 1 xã, 2 ô thay đổi.")).toBeVisible();
    const detail = page.getByRole("table", { name: "Chi tiết lần cập nhật" });
    await expect(detail).toContainText("Điều chỉnh theo yêu cầu");
    await expect(detail).toContainText("Tự tính");
    await expect(page.locator('[data-changed="true"]')).toHaveCount(2);
    await go(page, /Rà soát & chốt kỳ/, /Nhiệm vụ/);
    await page.getByRole("link", { name: "Rà soát T01 Bình Đức Tuần 1" }).click();
    await page.getByRole("button", { name: /Chốt kỳ ngay/ }).click();
    await expect(page.getByText("Đã chốt kỳ — Hệ thống đã tổng hợp một lần.")).toBeVisible();

    // 6. Báo cáo Chi cục: Google Sheet tắt; xuất Excel + PDF; gửi email (202) → Đã gửi.
    await go(page, "Báo cáo Chi cục");
    await page.getByLabel("Vụ/đợt").selectOption({ label: "Đợt E2E" });
    await expect(page.getByLabel("Tạo đường link Google Sheet")).toBeDisabled();
    await page.getByRole("button", { name: "Xuất file" }).click();
    await expect(page.getByText(/Đã xuất 2 file/)).toBeVisible();
    await page.getByRole("button", { name: "Gửi email" }).click();
    await expect(page.getByText(/Đã xếp gửi email \(202\)/)).toBeVisible();
    await expect(async () => {
      await page.getByRole("button", { name: "Làm mới trạng thái" }).click();
      await expect(page.getByTestId("dispatch-status")).toHaveText("Đã gửi", { timeout: 1000 });
    }).toPass({ timeout: 15_000 });
    await logout(page);

    // 7. Cấp Quốc gia: dashboard + báo cáo tuần chỉ xem (không có Cập nhật / thêm / sửa).
    await enter(page, "quan.lm@ttknqg.gov.vn");
    await expect(page.getByText("Chỉ số nóng")).toBeVisible();
    await expect(page.getByText(/Chỉ xem — dữ liệu lần cập nhật gần nhất/)).toBeVisible();
    await go(page, "Báo cáo tuần");
    await page.getByLabel("Vụ/đợt").selectOption({ label: "Đợt E2E" });
    await expect(page.getByText(/Chỉ xem — số liệu lấy từ lần cập nhật gần nhất/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Cập nhật" })).toHaveCount(0);
    await expect(page.getByText("Đã chốt").first()).toBeVisible();
  });

  test("Admin tạo tài khoản 2 vai trò → người dùng đổi vai trò; Menu các cấp: dải vàng, lý do, nhật ký hỗ trợ", async ({ page }) => {
    test.setTimeout(180_000);
    await page.goto("/login");
    await enter(page, "admin.mekong@mekonggreen.vn");
    await expect(page).toHaveURL(/\/kn\/tong-quan$/);
    await expect(page.getByText("Phòng ban đang áp dụng")).toBeVisible();
    await go(page, "Người dùng");
    await expect(page.getByRole("heading", { level: 1, name: "Người dùng" })).toBeVisible();
    // BF-SWEEP-TT (#100): Tạo tài khoản mở trang riêng (thay pop-up), "Lưu" là nút chính.
    await page.getByRole("link", { name: "Tạo tài khoản" }).click();
    await expect(page).toHaveURL(/\/kn\/quan-tri\/nguoi-dung\/them-moi$/);
    await expect(page.getByRole("heading", { level: 1, name: "Tạo tài khoản" })).toBeVisible();
    const main = page.getByRole("main");
    await expect(main.getByLabel("Họ tên")).toHaveAttribute("aria-required", "true");
    await expect(main.getByLabel("Email")).toHaveAttribute("aria-required", "true");
    await main.getByLabel("Họ tên").fill("Võ Thị Hạnh");
    await main.getByLabel("Email").fill("hanh.vt@angiang.gov.vn");
    await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345699");
    await main.getByLabel("Xã Châu Phong").check();
    await main.getByRole("button", { name: "+ Thêm vai trò" }).click();
    await expect(main.getByRole("group", { name: "Vai trò và phạm vi" })).toHaveCount(2);
    await main.getByRole("button", { name: "Lưu", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Võ Thị Hạnh" })).toBeVisible();
    const roles = page.getByRole("table", { name: "Vai trò và phạm vi" });
    await expect(roles).toContainText("Cấp Xã");
    await expect(roles).toContainText("Cấp Tỉnh");
    await expect(roles).toContainText("Châu Phong");
    await expect(page.getByRole("complementary", { name: "Log chỉnh sửa" })).toContainText("Tạo tài khoản");

    // Menu các cấp: vào vai Cấp Xã thay Nguyễn Văn An, lưu nháp có lý do → nhật ký hỗ trợ.
    await go(page, "Menu các cấp");
    await page.getByLabel("Tài khoản / xã*").selectOption({ label: "Nguyễn Văn An · Mỹ Hòa Hưng, Bình Đức" });
    await page.getByLabel("Lý do hỗ trợ").fill("Cán bộ xã mất mạng, nhập thay T04");
    await page.getByRole("button", { name: /Vào menu Cấp Xã/ }).click();
    await expect(page.getByRole("status", { name: "Chế độ hỗ trợ" })).toContainText("Đang hỗ trợ thay Nguyễn Văn An");
    await expect(page.getByRole("heading", { level: 1, name: "Công việc cần làm" })).toBeVisible();
    await page.getByRole("link", { name: /^(Nhập|Tiếp tục): T04 — Bình Đức/ }).first().click();
    await page.getByRole("tab", { name: /Bình Đức/ }).click();
    await typeCell(page, "K", "12");
    await page.getByRole("button", { name: "Lưu nháp" }).click();
    await expect(page.getByText(/Đã lưu nháp: /)).toBeVisible();
    await page.getByRole("button", { name: "Thoát chế độ hỗ trợ" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Menu các cấp (hỗ trợ người dùng)" })).toBeVisible();
    await expect(page.getByRole("status", { name: "Chế độ hỗ trợ" })).toHaveCount(0);
    await expect(page.getByRole("table").last()).toContainText("POST /v1/extension/report-tasks/:id/submissions");
    await expect(page.getByRole("table").last()).toContainText("Cán bộ xã mất mạng, nhập thay T04");
    await logout(page);

    // Người dùng mới: đăng nhập bằng SĐT, 2 vai trò → chọn Cấp Xã → đổi sang Cấp Tỉnh trong menu tài khoản.
    await enter(page, "0912345699", /^Cấp Xã/);
    await expect(page).toHaveURL(/\/kn\/nhiem-vu\/cong-viec$/);
    await accountMenu(page).click();
    await page.getByRole("menuitem", { name: "Cấp Tỉnh · Phòng Trồng trọt" }).click();
    // Trang vào Cấp Tỉnh phòng chuyên môn = "Báo cáo cho KHTC" (#44, internal/kn/extension/menus/navigation.json).
    await expect(page).toHaveURL(/\/kn\/bao-cao-khtc$/);
    await expect(accountMenu(page)).toContainText("Cấp Tỉnh · Trồng trọt");
  });
});
