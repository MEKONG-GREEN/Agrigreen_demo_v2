import { expect, test } from "@playwright/test";
import { MOCK_KN_PASSWORD } from "../../src/api/mock/kn-accounts";
import { logout, sidebar } from "./helpers";

test("242 lịch sử: hai lần gửi thật qua UI, so sánh readonly và giữ ghi chú khi chuyển", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
  const login = async (identifier: string) => {
    await page.getByLabel("Email hoặc số điện thoại").fill(identifier);
    await page.getByLabel("Mật khẩu", { exact: true }).fill(MOCK_KN_PASSWORD);
    await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  };
  const enterCNTS = async () => {
    await login("cnts.thu@demo.agrigreen.test");
    await page.getByRole("button", { name: /^Cấp Tỉnh/ }).click();
    await page.getByRole("button", { name: /Phòng Chăn nuôi - Thủy sản — Vào hệ thống/ }).click();
    const folder = sidebar(page).getByRole("button", { name: "Báo cáo cho KHTC", exact: true });
    if (await folder.getAttribute("aria-expanded") !== "true") await folder.click();
    await sidebar(page).getByRole("link", { name: "Phiếu kỳ này", exact: true }).click();
    await page.getByRole("row").filter({ hasText: "Tuần 50" }).getByRole("link", { name: /Nhập|Sửa, gửi lại/, exact: true }).click();
  };
  const send = async (value: string, attempt: number) => {
    const content = page.getByRole("group", { name: "Nội dung phiếu", exact: true });
    await content.locator('input[inputmode="decimal"],input[inputmode="numeric"]').first().fill(value);
    for (const text of await content.locator("textarea").all()) await text.fill(`Nội dung lần gửi ${attempt} đã đối chiếu.`);
    await page.getByRole("button", { name: "Gửi KHTC", exact: true }).click();
    await page.getByRole("dialog", { name: "Xác nhận gửi phiếu" }).getByRole("button", { name: `Xác nhận gửi lần ${attempt}`, exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: `Đã gửi KHTC lần ${attempt}.` })).toBeVisible();
  };
  const openReview = async () => {
    await login("0912345242");
    // Login may honor the previous form's returnTo. Use the public menu to
    // reach the period list while keeping the tab's submitted mock records.
    const periodsLink = sidebar(page).getByRole("link", { name: /^Kỳ báo cáo & phiếu các phòng/ });
    await periodsLink.click();
    const list = page.getByRole("table", { name: "Danh sách kỳ báo cáo", exact: true });
    await list.getByRole("row").filter({ hasText: "Tuần 50" }).getByRole("link").first().click();
    await page.getByRole("table", { name: "Tiến độ nộp phiếu và nguồn module", exact: true }).getByRole("row")
      .filter({ hasText: "Phòng Chăn nuôi - Thủy sản" }).getByRole("link", { name: "Rà soát", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Rà soát phiếu Phòng Chăn nuôi/ })).toBeVisible();
  };
  await page.goto("/login"); await login("admin.mekong@mekonggreen.vn");
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  for (const department of ["Phòng Kế hoạch - Tài chính", "Phòng Chăn nuôi - Thủy sản"]) {
    await page.getByLabel(`Trạng thái ${department}`, { exact: true }).selectOption("active");
    await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  }
  // Real assignments through the public Admin UI; no auth/storage/STORE bypass.
  await sidebar(page).getByRole("link", { name: "Người dùng", exact: true }).click();
  await page.getByRole("link", { name: "Tạo tài khoản", exact: true }).click();
  const main = page.getByRole("main");
  await expect(main.getByRole("button", { name: "Lưu", exact: true })).toBeEnabled();
  await main.getByLabel("Họ tên").fill("Cán bộ KHTC kiểm thử 242");
  await main.getByLabel("Email").fill("khtc.history242@example.test");
  await main.getByLabel("Số điện thoại (tên đăng nhập)").fill("0912345242");
  const assignment = main.getByRole("group", { name: "Vai trò và phạm vi" });
  await assignment.getByRole("combobox", { name: "Vai trò", exact: true }).selectOption("kn_tinh");
  await assignment.getByRole("checkbox", { name: "trong_trot", exact: true }).uncheck();
  await assignment.getByRole("checkbox", { name: "ke_hoach_tai_chinh", exact: true }).check();
  await expect(main.getByLabel("Họ tên")).toHaveValue("Cán bộ KHTC kiểm thử 242");
  await main.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Cán bộ KHTC kiểm thử 242", exact: true })).toBeVisible();
  await logout(page); await login("0912345242");
  await page.getByRole("button", { name: "Mở kỳ", exact: true }).click();
  await page.getByRole("combobox", { name: "Loại báo cáo", exact: true }).selectOption("week");
  await page.getByRole("combobox", { name: "Số kỳ", exact: true }).selectOption("50");
  await page.getByRole("button", { name: "Xem trước", exact: true }).click();
  await page.getByRole("button", { name: "Mở kỳ và giao phiếu", exact: true }).click();
  await expect(page.getByRole("table", { name: "Tiến độ nộp phiếu và nguồn module", exact: true })).toBeVisible();
  await logout(page); await enterCNTS(); await send("7", 1);
  await logout(page); await openReview();
  await page.getByRole("textbox", { name: /Nội dung yêu cầu bổ sung/ }).fill("Bổ sung lại khối lượng theo nguồn.");
  await page.getByRole("button", { name: "Yêu cầu bổ sung", exact: true }).click();
  await page.getByRole("dialog", { name: "Xác nhận yêu cầu bổ sung" }).getByRole("button", { name: "Gửi yêu cầu bổ sung", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã gửi yêu cầu bổ sung cho lần gửi 1." })).toBeVisible();
  await logout(page); await enterCNTS(); await send("9", 2);
  await logout(page); await openReview();
  const note = page.getByRole("textbox", { name: /Nội dung yêu cầu bổ sung/ });
  await note.fill("Ghi chú chưa gửi cần được giữ.");
  await page.getByRole("button", { name: "Lịch sử và so sánh lần gửi", exact: true }).click();
  const leaving = page.getByRole("dialog", { name: "Bỏ ghi chú chưa gửi?" });
  await leaving.getByRole("button", { name: "Giữ ghi chú", exact: true }).click();
  await expect(note).toHaveValue("Ghi chú chưa gửi cần được giữ.");
  await page.getByRole("button", { name: "Lịch sử và so sánh lần gửi", exact: true }).click();
  await leaving.getByRole("button", { name: "Bỏ ghi chú và chuyển", exact: true }).click();
  const history = page.getByRole("table", { name: "Các lần gửi phiếu", exact: true });
  await expect(history.getByRole("row")).toHaveCount(3);
  await expect(history.getByRole("columnheader", { name: "STT", exact: true })).toBeVisible();
  await history.getByRole("link", { name: "Lần 1", exact: true }).click();
  await expect(page.getByRole("table", { name: "Nội dung đã gửi", exact: true })).toBeVisible();
  const compare = page.getByRole("combobox", { name: "So sánh với", exact: true });
  const other = await compare.getByRole("option").filter({ hasText: "Lần gửi 2" }).getAttribute("value");
  expect(other).toBeTruthy(); await compare.selectOption(other!);
  const diff = page.getByRole("table", { name: "So sánh nội dung các lần gửi", exact: true });
  const changed = diff.getByRole("row").filter({ hasText: "Mô hình bò thịt · Thực hiện trong kỳ" });
  await expect(changed.getByText("7", { exact: true })).toBeVisible();
  await expect(changed.getByText("9", { exact: true })).toBeVisible();
  await expect(changed.getByText("Đã thay đổi", { exact: true })).toBeVisible();
  await expect(page.getByRole("textbox")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Lưu|^Gửi|^Xác nhận/ })).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
  await expect(page.getByRole("heading", { level: 1 })).toHaveCSS("font-family", /Noto Serif/);
  await expect(page.getByRole("complementary", { name: "Log gửi và rà soát", exact: true })).toContainText("Yêu cầu bổ sung");
  await page.screenshot({ path: testInfo.outputPath("242-history-compare-desktop.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(diff).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("242-history-compare-mobile.png"), fullPage: true });
});
