import { expect, test } from "@playwright/test";
import { alert, labelled, loginAs, navTo, sidebar, status, switchUser } from "./helpers";

// G4 mkg/logistics — W-01: chuyến + chặng (định tuyến lưu nguyên văn + SHA-256), gán ghe (cảnh báo không chặn),
// huỷ có lý do, mốc tài xế từng bậc, hoàn tất có cờ lệch cân, danh mục ghe có phiên bản, cảnh báo, tham số R-56.
test.describe("Vận tải (mkg/logistics)", () => {
  test("chuyến FM-05: gán ghe sắp hết hạn → cảnh báo không chặn; lập chặng đường thuỷ → ảnh chụp băm khớp nguyên văn", async ({ page }) => {
    await loginAs(page, "van-tai", "/van-tai/chuyen");
    await expect(page.getByRole("group", { name: "Kế hoạch" })).toContainText("1");
    await page.getByRole("button", { name: "Mở chuyến CH000101" }).click();
    await expect(page.getByRole("list", { name: "Cảnh báo" }).first()).toContainText("Chưa gán phương tiện");
    await page.getByLabel("Phương tiện", { exact: true }).selectOption({ label: "CT-06789 · Sà lan Cần Thơ 6" });
    await page.getByRole("button", { name: "Gán phương tiện" }).click();
    await expect(page.getByRole("status", { name: "Cảnh báo thao tác" })).toContainText("Sắp hết hạn đăng kiểm");
    await page.getByRole("button", { name: "Lập chặng", exact: true }).click();
    await page.getByLabel("Phương thức chặng 1").selectOption("waterway");
    await page.getByRole("button", { name: "Lưu chặng" }).click();
    await expect(page.getByText("Đã lập chặng — lưu kết quả định tuyến nguyên văn + SHA-256")).toBeVisible();
    const legs = page.getByRole("table", { name: "Chặng của chuyến" });
    await expect(legs).toContainText("Định tuyến mạng đường thuỷ");
    await page.getByRole("button", { name: "Ảnh chụp chặng 1" }).click();
    await expect(labelled(page, "Ảnh chụp định tuyến")).toContainText("Băm khớp nguyên văn");
    await expect(labelled(page, "Ảnh chụp định tuyến")).toContainText("/v1/partner/waterway/route");
  });

  test("km tay phải có lý do; lập lại chặng → kế hoạch lần 2; huỷ chuyến có lý do", async ({ page }) => {
    await loginAs(page, "van-tai", "/van-tai/chuyen");
    await page.getByRole("button", { name: "Mở chuyến CH000101" }).click();
    await page.getByRole("button", { name: "Lập chặng", exact: true }).click();
    await page.getByLabel("Km nhập tay chặng 1").fill("42.5");
    await expect(page.getByText(/Km tay cần lý do/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Lưu chặng" })).toBeDisabled();
    await page.getByLabel("Lý do km tay chặng 1").fill("Kênh cạn, đo theo đồng hồ ghe");
    await page.getByRole("button", { name: "Lưu chặng" }).click();
    await expect(page.getByRole("table", { name: "Chặng của chuyến" })).toContainText("Nhập tay có lý do");
    await expect(page.getByRole("heading", { name: "Chặng (kế hoạch lần 1)" })).toBeVisible();
    await page.getByLabel("Lý do huỷ chuyến").fill("Ghe hỏng máy, chuyển sang chuyến khác");
    await page.getByRole("button", { name: "Huỷ chuyến" }).click();
    await expect(page.getByText("Đã huỷ chuyến (sự kiện cancelled, có lý do)")).toBeVisible();
    await expect(page.getByRole("list", { name: "Sổ chuyến" })).toContainText("Huỷ");
    // Chỉ tiến: chuyến đã huỷ không còn thao tác.
    await expect(page.getByRole("button", { name: "Huỷ chuyến" })).toHaveCount(0);
  });

  test("hoàn tất chuyến đã đến: lệch cân > 5 % → cờ, cước theo đơn giá thuê ghe; bản bù cần lý do", async ({ page }) => {
    await loginAs(page, "van-tai", "/van-tai/chuyen");
    await page.getByRole("button", { name: "Mở chuyến CH000105" }).click();
    await page.getByLabel("Khối lượng thật (kg)").fill("120000");
    await page.getByRole("button", { name: "Hoàn tất chuyến" }).click();
    await expect(page.getByText("Đã hoàn tất chuyến — phát mkg.logistics.trip.completed")).toBeVisible();
    await expect(page.getByRole("status", { name: "Cảnh báo thao tác" })).toContainText("Lệch cân > 5 %");
    await expect(labelled(page, "Thông tin chuyến")).toContainText("Đơn giá thuê ghe (GH-03)");
    await page.getByLabel("Khối lượng thật (kg)").fill("125000");
    await expect(page.getByRole("button", { name: "Ghi bản bù" })).toBeDisabled();
    await page.getByLabel("Lý do bù").fill("Phiếu cân nhà máy in lại sau hiệu chuẩn");
    await page.getByRole("button", { name: "Ghi bản bù" }).click();
    await expect(page.getByText("Đã ghi bản bù hoàn tất — bản gốc giữ nguyên")).toBeVisible();
    await expect(page.getByRole("list", { name: "Sổ chuyến" })).toContainText("bù cho");
  });

  test("tài xế: chỉ xem chuyến + ghi mốc từng bậc; không thấy danh mục ghe, tham số", async ({ page }) => {
    await loginAs(page, "tai-xe", "/van-tai/chuyen");
    await expect(sidebar(page).getByRole("link", { name: "Danh mục ghe / xe" })).toHaveCount(0);
    await expect(sidebar(page).getByRole("link", { name: "Tham số cước" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Lập chuyến mới" })).toHaveCount(0);
    await page.getByRole("button", { name: "Mở chuyến CH000102" }).click();
    await page.getByRole("button", { name: "Ghi mốc đến nơi" }).click();
    await expect(page.getByText("Đã ghi mốc đến nơi")).toBeVisible();
    await expect(labelled(page, "Thông tin chuyến")).toContainText("Đã đến");
    await page.goto("/van-tai/phuong-tien");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  });

  test("danh mục ghe: số hiệu chuẩn hoá duy nhất; sửa là phiên bản mới có lý do; cảnh báo đăng kiểm", async ({ page }) => {
    await loginAs(page, "van-tai", "/van-tai/phuong-tien");
    await page.getByRole("button", { name: "Thêm phương tiện" }).click();
    await page.getByLabel("Số hiệu").fill(" tg - 12 345 ");
    await page.getByRole("button", { name: "Thêm vào danh mục" }).click();
    await expect(alert(page)).toContainText("logistics.vessel_code_taken");
    await page.getByLabel("Số hiệu").fill("an 77 001");
    await page.getByLabel("Loại đơn giá thuê").selectOption("per_ton");
    await page.getByLabel("Đơn giá thuê (VND)").fill("90000");
    await page.getByRole("button", { name: "Thêm vào danh mục" }).click();
    await expect(page.getByText("Đã thêm phương tiện AN77001 (số hiệu đã chuẩn hoá)")).toBeVisible();
    const t = page.getByRole("table", { name: "Danh mục phương tiện" });
    await expect(t.getByRole("row", { name: /AN77001/ })).toContainText("90.000 ₫/tấn");
    await expect(t.getByRole("row", { name: /VL-00111/ })).toContainText("Hết hạn");
    await page.getByRole("button", { name: "Sửa TG-12345" }).click();
    await page.getByLabel("Tải rơm thực chở (t)").fill("95");
    await expect(page.getByRole("button", { name: "Lưu phiên bản mới" })).toBeDisabled();
    await page.getByLabel("Lý do sửa").fill("Đo lại tải thực chở sau khi nâng mạn");
    await page.getByRole("button", { name: "Lưu phiên bản mới" }).click();
    await expect(page.getByText("Đã lưu phiên bản 2 của TG-12345")).toBeVisible();

    await navTo(page, "Cảnh báo vận tải");
    await expect(page.getByRole("group", { name: "Chưa cân > 48 giờ" })).toContainText("1");
    await expect(page.getByRole("table", { name: "Phương tiện cần gia hạn" })).toContainText("VL-00111");
    await expect(page.getByRole("table", { name: "Chuyến chưa cân" })).toContainText("CH000101");
  });

  test("tham số cước R-56: điều phối đề xuất, không duyệt được; kế toán trưởng khác duyệt → áp dụng v1", async ({ page }) => {
    await loginAs(page, "van-tai", "/van-tai/tham-so");
    await expect(labelled(page, "Giá trị đang áp dụng")).toContainText("logistics.rate.road_vnd_per_t_km:default");
    await page.getByLabel("Giá trị mới").fill("2400");
    await page.getByLabel("Lý do đề xuất").fill("Giá dầu tăng quý IV theo báo giá nhà xe");
    await page.getByRole("button", { name: "Gửi đề xuất tham số" }).click();
    await expect(page.getByText(/Đã đề xuất phiên bản mới/)).toBeVisible();
    await expect(page.getByText("Bạn là người đề xuất — cần một người phê duyệt khác (R-56)")).toBeVisible();
    await expect(page.getByText("Bạn không có vai duyệt (kế toán trưởng).")).toBeVisible();
    await switchUser(page, "ke-toan-truong");
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(status(page).filter({ hasText: /Đã duyệt phiên bản v1/ })).toBeVisible();
    await expect(labelled(page, "Giá trị đang áp dụng")).toContainText("logistics.rate.road_vnd_per_t_km:v1");
  });
});
