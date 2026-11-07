import { expect, test, type Page } from "@playwright/test";
import { loginAs, sidebar } from "./helpers";

async function openPoints(page: Page, regions = false) {
  await loginAs(page, "kn-admin", "/kn/tong-quan");
  await sidebar(page)
    .getByRole("link", { name: "Phòng ban", exact: true })
    .click();
  await page
    .getByLabel("Trạng thái Phòng Thông tin Tuyên truyền", { exact: true })
    .selectOption("active");
  await expect(
    page.getByRole("status").filter({ hasText: "Đã đổi" }),
  ).toContainText("Đang áp dụng");
  await sidebar(page)
    .getByRole("link", { name: "Menu các cấp", exact: true })
    .click();
  await page.getByLabel("Xem như vai trò*").selectOption("kn_tinh");
  await page
    .getByLabel(/^Lý do hỗ trợ/)
    .fill("Kiểm tra danh mục xã báo giá FN-AD-04.01/02");
  await page
    .getByRole("button", { name: "Vào menu Cấp Tỉnh", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Thoát chế độ hỗ trợ", exact: true })
    .click();
  await sidebar(page)
    .getByRole("button", { name: "Phòng Thông tin Tuyên truyền", exact: true })
    .click();
  await sidebar(page)
    .getByRole("link", { name: "Xã báo giá", exact: true })
    .click();
  await page.getByLabel("Tại ngày", { exact: true }).fill("2026-11-05");
  if (regions)
    await page.getByRole("tab", { name: "Nhóm vùng", exact: true }).click();
  else
    await page
      .getByLabel("Nhóm vùng", { exact: true })
      .selectOption("an_giang");
}

test("price-points182: list → thêm → sửa → ngưng có URL và Log, cán bộ chỉ xem, giữ bộ lọc", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-11-05T02:00:00Z") });
  await openPoints(page);
  await expect(
    page
      .getByRole("table", { name: "Danh mục xã báo giá" })
      .getByRole("columnheader", { name: "STT" }),
  ).toBeVisible();
  await page
    .getByRole("link", { name: "Thêm xã báo giá", exact: true })
    .click();
  await expect(page).toHaveURL(/xa-bao-gia\/them-moi\?at=2026-11-05/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  const unit = page.getByRole("combobox", { name: /ĐVHC \(xã\)/ });
  await expect(unit).toHaveAttribute("aria-invalid", "true");
  await unit.selectOption("31126");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/xa-bao-gia\/XBG0004\?at=2026-11-05/);
  const log = page.getByRole("complementary", { name: "Log", exact: true });
  await expect(log).toContainText("Thêm xã báo giá");
  await page.getByRole("link", { name: "Sửa", exact: true }).click();
  await expect(page).toHaveURL(/XBG0004\/sua\?/);
  await page.getByLabel(/Ghi chú/).fill("Đối chiếu danh mục xã báo giá182");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(log).toContainText("Đối chiếu danh mục xã báo giá182");
  await expect(page.getByRole("link", { name: "Phân công", exact: true })).toHaveCount(0);
  await expect(page.getByRole("status").filter({ hasText: /Đã lưu|Lưu thành công/ })).toHaveCount(0);
  await page.getByRole("button", { name: "Ngưng", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Ngưng xã XBG0004", exact: true });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Xác nhận ngưng", exact: true }).click();
  await expect(dialog.getByRole("textbox", { name: "Lý do ngưng", exact: true })).toHaveAttribute("aria-invalid", "true");
  await dialog.getByRole("textbox", { name: "Lý do ngưng", exact: true }).fill("Kết thúc thu thập giá xã210");
  await dialog.getByLabel("Ngưng từ ngày", { exact: true }).fill("2026-11-07");
  await dialog.getByRole("button", { name: "Xác nhận ngưng", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(log).toContainText("Ngưng xã báo giá");
  await expect(log).toContainText("Kết thúc thu thập giá xã210");
  await page
    .getByRole("link", { name: "Danh mục xã báo giá", exact: true })
    .click();
  await expect(page).toHaveURL(/at=2026-11-05.*region=an_giang/);
  await expect(page.getByLabel("Nhóm vùng", { exact: true })).toHaveValue(
    "an_giang",
  );
  await page.getByLabel("Tại ngày", { exact: true }).fill("2026-11-07");
  const retired = page.getByRole("table", { name: "Danh mục xã báo giá", exact: true }).getByRole("row").filter({ hasText: "XBG0004" });
  await expect(retired).toContainText("Ngưng");
  await retired.getByRole("link", { name: "Chi tiết", exact: true }).click();
  await expect(page.getByRole("button", { name: "Ngưng", exact: true })).toHaveCount(0);
  await expect(page.getByRole("table", { name: "Lịch sử phân công", exact: true })).toBeVisible();
  const removed = await page.request.get("/kn/bao-gia/xa-bao-gia/XBG0004/phan-cong");
  expect(removed.status()).toBe(404);
});

test("price-points182: nhóm vùng trên trang riêng, required và Log không giả", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-11-05T02:00:00Z") });
  await openPoints(page, true);
  await page
    .getByRole("table", { name: "Nhóm vùng", exact: true })
    .getByRole("row")
    .filter({ hasText: "an_giang" })
    .getByRole("link", { name: "Sửa tên", exact: true })
    .click();
  await expect(page).toHaveURL(/nhom-vung\/an_giang\/sua\?/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const name = page.getByLabel(/Tên nhóm vùng/);
  await name.fill("");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(name).toHaveAttribute("aria-invalid", "true");
  await expect(
    page.getByRole("complementary", { name: "Log", exact: true }),
  ).toContainText("Chưa có nhật ký thay đổi");
  await name.fill("Vùng An Giang182");
  await page.getByRole("button", { name: "Lưu", exact: true }).click();
  await expect(page).toHaveURL(/nhom-vung\/an_giang\?/);
  await expect(page.getByRole("main")).toContainText("Vùng An Giang182");
});
