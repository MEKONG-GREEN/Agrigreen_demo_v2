import { expect, test, type Page } from "@playwright/test";

const BASE = "/kn/bao-gia/cau-hinh";

async function admin(page: Page, path = BASE) {
  // Persona chỉ có trong build mock; không dùng thông tin đăng nhập hay sửa helper chung đang thuộc #163.
  await page.goto("/login");
  await page.evaluate(() =>
    sessionStorage.setItem("ag.mock.persona", "kn-admin"),
  );
  await page.goto(path);
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "Lịch báo giá",
  );
}

test("price-config: route, lỗi từng ô, log, số an toàn và xóa có xác nhận", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const thresholdWrites: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/price/thresholds"
    )
      thresholdWrites.push(request.postData() ?? "");
  });
  await admin(page);
  await page.getByRole("link", { name: "Xem kỳ báo giá & giờ khóa" }).click();
  await expect(page).toHaveURL(/\/cau-hinh\/lich$/);
  await expect(
    page.getByRole("complementary", { name: "Log", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Sửa cấu hình", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByLabel(/Hạn phản hồi xác minh/).fill("11:30");
  await page.getByRole("button", { name: "Lưu cấu hình", exact: true }).click();
  await expect(page.getByLabel(/Hạn phản hồi xác minh/)).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await page.getByLabel(/Hạn phản hồi xác minh/).fill("14:00");
  await page.getByRole("button", { name: "Lưu cấu hình", exact: true }).click();
  await expect(page).toHaveURL(/\/cau-hinh\/lich$/);
  await expect(
    page.getByRole("complementary", { name: "Log", exact: true }),
  ).toContainText("14:00");
  await page
    .getByRole("link", { name: "Cấu hình báo giá", exact: true })
    .click();

  await page.getByRole("tab", { name: "Nhắc nhập giá", exact: true }).click();
  await page
    .getByRole("link", { name: "Sửa cấu hình nhắc", exact: true })
    .click();
  await page.getByLabel(/Nội dung mẫu/).fill("Nhắc {không_hỗ_trợ}");
  await page.getByRole("button", { name: "Lưu cấu hình", exact: true }).click();
  await expect(page.getByLabel(/Nội dung mẫu/)).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await page
    .getByLabel(/Nội dung mẫu/)
    .fill("Nhập giá {ngày} cho {xã} trước {giờ khóa}");
  await page.getByRole("button", { name: "Lưu cấu hình", exact: true }).click();
  await expect(page).toHaveURL(/\/cau-hinh\/nhac$/);
  await page
    .getByRole("link", { name: "Cấu hình báo giá", exact: true })
    .click();

  await page.getByRole("tab", { name: "Lịch ngoại lệ", exact: true }).click();
  await expect(
    page
      .getByRole("table", { name: "Lịch ngoại lệ", exact: true })
      .getByRole("columnheader", { name: "STT", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Thêm ngoại lệ", exact: true }).click();
  await page.getByRole("button", { name: "Lưu ngoại lệ", exact: true }).click();
  await expect(page.getByLabel(/Lý do/)).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  const tomorrow = (await page
    .getByLabel(/Ngày ngoại lệ/)
    .getAttribute("min"))!;
  await page.getByLabel(/Ngày ngoại lệ/).fill(tomorrow);
  const weekday = new Date(`${tomorrow}T00:00:00Z`).getUTCDay();
  await page
    .getByLabel(/Loại ngoại lệ/)
    .selectOption(weekday === 0 || weekday === 6 ? "extra" : "holiday");
  await page.getByLabel(/Lý do/).fill("Ngoại lệ kiểm thử cấu hình Báo giá");
  await page.getByRole("button", { name: "Lưu ngoại lệ", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/ngoai-le/${tomorrow}$`));
  await page
    .getByRole("link", { name: "Cấu hình báo giá", exact: true })
    .click();

  await page.getByRole("tab", { name: "Ngưỡng cảnh báo", exact: true }).click();
  await page
    .getByRole("link", { name: "Sửa tham số chung", exact: true })
    .click();
  const fields = [
    /Ngưỡng mặc định/,
    /Số ngày tìm/,
    /Ngưỡng đi ngang/,
    /Ngưỡng phiếu điều tra/,
  ];
  for (let i = 0; i < fields.length; i++) {
    const input = page.getByLabel(fields[i]!);
    for (const bad of [
      "1e100000",
      "1e-100000",
      "9".repeat(100000),
      i === 1 ? "61" : "100.01",
    ]) {
      await input.fill(bad);
      await input.blur();
      await page
        .getByRole("button", { name: "Lưu ngưỡng", exact: true })
        .click();
      await expect(input).toHaveAttribute("aria-invalid", "true");
      await expect(page).toHaveURL(/\/nguong\/mac-dinh\/sua$/);
    }
    await input.fill(["30", "7", "1", "40"][i]!);
  }
  expect(thresholdWrites).toHaveLength(0);
  await page.screenshot({
    path: test.info().outputPath("price-config-number-guards.png"),
    fullPage: true,
  });
  await page.getByLabel(/Ngưỡng mặc định/).fill("35,25");
  await page.getByRole("button", { name: "Lưu ngưỡng", exact: true }).click();
  await expect(page).toHaveURL(/\/nguong\/mac-dinh$/);
  await expect(
    page.getByRole("complementary", { name: "Log", exact: true }),
  ).toContainText("35.25");
  await page
    .getByRole("link", { name: "Cấu hình báo giá", exact: true })
    .click();
  await page.getByRole("tab", { name: "Ngưỡng cảnh báo", exact: true }).click();
  await page.getByRole("link", { name: "Thêm ngưỡng", exact: true }).click();
  await page.getByLabel(/Mã nhóm hàng/).fill("rau_mau");
  await page.getByLabel(/Ngưỡng ±%/).fill("40");
  await page.getByRole("button", { name: "Lưu ngưỡng", exact: true }).click();
  await expect(page).toHaveURL(/\/nguong\/group\/rau_mau$/);
  await page.getByRole("link", { name: "Sửa ngưỡng", exact: true }).click();
  await expect(page.getByLabel(/Mã nhóm hàng/)).toBeDisabled();
  await page.getByLabel(/Ngưỡng ±%/).fill("45");
  await page.getByRole("button", { name: "Lưu ngưỡng", exact: true }).click();
  await expect(
    page.getByRole("complementary", { name: "Log", exact: true }),
  ).toContainText("40.00 → 45.00");
  await page
    .getByRole("link", { name: "Cấu hình báo giá", exact: true })
    .click();
  await page.getByRole("tab", { name: "Ngưỡng cảnh báo", exact: true }).click();
  const table = page.getByRole("table", {
    name: "Ngưỡng theo nhóm hàng / mặt hàng",
    exact: true,
  });
  await table.getByRole("button", { name: "Xóa", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "Xác nhận xóa", exact: true }).click();
  await expect(table).toContainText("Chưa có ngưỡng riêng");
});

test("price-config: alias phòng ban mở đủ route DetailPage", async ({
  page,
}) => {
  const alias = "/kn/phong-ban/thong_tin_tuyen_truyen/bao-gia-lich";
  await admin(page, alias);
  await page.getByRole("link", { name: "Sửa kỳ báo giá", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${alias}/lich/sua$`));
  await expect(
    page.getByRole("complementary", { name: "Log", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Hủy", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${alias}$`));
});
