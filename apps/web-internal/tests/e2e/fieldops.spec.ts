import { expect, test } from "@playwright/test";
import { alert, IDS, loginAs, navTo } from "./helpers";

test.describe("Thu gom hiện trường", () => {
  test("lập lệnh gom: thửa khác HTX bị từ chối; hợp lệ → chờ duyệt", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/gom/lenh-gom");
    await page.getByRole("button", { name: "Lập lệnh gom" }).click();
    await page.getByRole("button", { name: "HTX …000001" }).click();
    await page.getByLabel("Mã thửa (mỗi dòng một UUID)").fill(IDS.plotB2);
    await page.getByRole("button", { name: "Gửi lệnh gom" }).click();
    await expect(alert(page)).toContainText("fieldops.plot_other_htx");
    await page.getByRole("button", { name: "HTX …000002" }).click();
    await page.getByRole("button", { name: "Gửi lệnh gom" }).click();
    await expect(page.getByText(/Đã lập lệnh gom \(chờ duyệt\)/)).toBeVisible();
    await expect(page.getByRole("list", { name: "Lịch sử lệnh gom" })).toContainText("Lập");
    // Huỷ bằng sự kiện có lý do (RB-1), không xoá.
    await page.getByLabel("Lý do (từ chối / huỷ)").fill("HTX xin lùi lịch do chưa gặt xong");
    await page.getByRole("button", { name: "Huỷ lệnh gom" }).click();
    await expect(page.getByText(/Đã huỷ lệnh gom/)).toBeVisible();
  });

  test("đếm cuộn FM-03/FM-04, xếp ghe → bảng kê, HTX xác nhận → đối soát chiều mua khớp", async ({ page }) => {
    await loginAs(page, "hien-truong", "/gom/dem-cuon");
    await page.getByLabel("Chọn lệnh gom").selectOption(IDS.orderAssigned);
    const grid = page.getByRole("table", { name: "Số cuộn theo công đoạn" });
    await expect(grid).toContainText("4.300");
    await page.getByLabel("Công đoạn", { exact: true }).selectOption("gather");
    await page.getByLabel("Số cuộn", { exact: true }).fill("4400");
    await expect(alert(page)).toContainText("nhiều hơn công đoạn trước");
    await expect(page.getByRole("button", { name: "Ghi số cuộn" })).toBeDisabled();
    await page.getByLabel("Số cuộn", { exact: true }).fill("4250");
    await page.getByLabel("Lý do chênh lệch").fill("50 cuộn ướt để lại ruộng");
    await page.getByRole("button", { name: "Ghi số cuộn" }).click();
    await expect(page.getByText("Đã ghi Tập kết: 4250 cuộn")).toBeVisible();
    await page.getByLabel("Công đoạn", { exact: true }).selectOption("boat_load");
    await page.getByLabel("Số cuộn", { exact: true }).fill("4250");
    await page.getByRole("button", { name: "Ghi số cuộn" }).click();
    await expect(page.getByText(/Đã ghi Xếp ghe\/xe: 4250 cuộn — cập nhật đối soát chiều mua và công bố bảng kê HTX/)).toBeVisible();
    await page.getByRole("button", { name: "Ghi xác nhận HTX" }).click();
    await expect(page.getByText("Đã ghi HTX xác nhận số cuộn")).toBeVisible();

    await navTo(page, "Đối soát chiều mua");
    await expect(page.getByRole("group", { name: "Khớp" })).toContainText("2");
    await expect(page.getByRole("group", { name: "Lệch" })).toContainText("1");
  });

  test("bản bù số cuộn: công đoạn sau vẫn phải thỏa FM-04", async ({ page }) => {
    await loginAs(page, "hien-truong", "/gom/dem-cuon");
    await page.getByLabel("Chọn lệnh gom").selectOption(IDS.orderAssigned);
    await page.getByText("Sổ đếm cuộn (cả bản bị bù)").click();
    await page.getByRole("button", { name: /Bù số Cuộn rơm thửa/ }).click();
    await page.getByLabel("Số cuộn", { exact: true }).fill("4310");
    await page.getByLabel("Lý do bù").fill("Đếm lại tại bờ, sót một đống 10 cuộn");
    await page.getByRole("button", { name: "Ghi bản bù" }).click();
    await expect(page.getByText("Đã ghi bản bù (RB-1) — bản gốc giữ nguyên")).toBeVisible();
    await expect(page.getByRole("table", { name: "Số cuộn theo công đoạn" })).toContainText("4.310");
  });

  test("kg/cuộn theo HTX: học theo HTX → toàn tenant; không qua van", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/gom/kg-cuon");
    const t = page.getByRole("table", { name: "kg mỗi cuộn theo HTX" });
    await expect(t).toContainText("22");
    await expect(t).toContainText("Học theo HTX (30 lô gần nhất)");
    await expect(t).toContainText("Toàn tenant");
    await page.getByRole("button", { name: "Kéo khối lượng cân mới" }).click();
    await expect(page.getByText(/1 lô mới có khối lượng/)).toBeVisible();
  });
});
