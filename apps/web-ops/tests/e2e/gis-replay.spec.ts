import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

/** Ngày YYYY-MM-DD theo giờ VN, lệch `days` ngày so với hôm nay. */
function vnDay(days: number): string {
  return new Date(Date.now() + 7 * 3_600_000 + days * 86_400_000).toISOString().slice(0, 10);
}

async function count(page: Page, layer: string): Promise<number> {
  return Number(((await page.getByTestId(`replay-count-${layer}`).textContent()) ?? "0").replace(/\./g, ""));
}

test.describe("Bản đồ GIS: xem lại theo ngày (GIS US-REPLAY-01, US-EVL-01, US-SNAP-01)", () => {
  test("hôm nay = hiện tại; hôm qua có ảnh chụp; ngày cũ dựng từ nhật ký, thửa chưa tạo không hiện", async ({ page }) => {
    await loginAs(page, "setter", "/ban-do-gis");
    const panel = page.getByTestId("gis-replay");
    await expect(panel.getByTestId("retention-note")).toContainText("không bao giờ xoá");

    await panel.getByLabel("Xem lại theo ngày").fill(vnDay(0));
    await panel.getByRole("button", { name: "Xem lại" }).click();
    await expect(panel.getByTestId("replay-banner")).toContainText("như hệ biết lúc hết ngày");
    await expect(panel.getByTestId("snapshot-badge")).toContainText("Hôm nay — trạng thái hiện tại");
    await expect.poll(() => count(page, "plot")).toBeGreaterThan(0);
    await expect.poll(() => count(page, "coop_boundary")).toBeGreaterThan(0);
    await expect(page.getByTestId("replay-map-status")).toContainText(/Đang xem lại ngày \d{2}\/\d{2}\/\d{4}: [1-9]\d* đối tượng/);

    await panel.getByLabel("Xem lại theo ngày").fill(vnDay(-1));
    await panel.getByRole("button", { name: "Xem lại" }).click();
    await expect(panel.getByTestId("snapshot-badge")).toContainText("Ảnh chụp cuối ngày");
    const yesterdayPlots = await count(page, "plot");
    expect(yesterdayPlots).toBeGreaterThan(0);
    await expect(panel.getByRole("table", { name: "Trạng thái các lớp theo ngày" })).toContainText("Ảnh chụp cuối ngày");

    // 36 ngày trước: thửa (tạo 35 ngày trước) chưa có; ranh giới HTX đã có.
    await panel.getByLabel("Xem lại theo ngày").fill(vnDay(-36));
    await panel.getByRole("button", { name: "Xem lại" }).click();
    await expect(panel.getByTestId("snapshot-badge")).toContainText("dựng lại từ nhật ký");
    await expect.poll(() => count(page, "plot")).toBe(0);
    expect(await count(page, "coop_boundary")).toBeGreaterThan(0);

    await panel.getByRole("button", { name: "Về hiện tại" }).click();
    await expect(page.getByTestId("replay-map-status")).toHaveCount(0);
  });

  test("dòng thời gian thay đổi lớp: chọn một dòng Sửa → bảng trước/sau đánh dấu thuộc tính đổi", async ({ page }) => {
    await loginAs(page, "setter", "/ban-do-gis");
    const panel = page.getByTestId("gis-replay");
    const list = panel.getByRole("list", { name: "Nhật ký thay đổi lớp" });
    await expect(list.locator("li").first()).toBeVisible();
    await panel.getByLabel("Lớp", { exact: true }).selectOption("waterway");
    const edit = list.locator("li[data-op=update]").first();
    await expect(edit).toContainText("Sửa Đường thủy");
    await edit.getByRole("button").click();
    const diff = panel.getByTestId("event-diff");
    await expect(diff).toContainText("pf/waterway");
    await expect(diff.locator("tr[data-changed=true]", { hasText: "state" })).toHaveCount(1);
  });

  test("ngày tương lai bị chặn trước khi gọi máy chủ", async ({ page }) => {
    await loginAs(page, "tinh", "/ban-do-gis");
    const panel = page.getByTestId("gis-replay");
    await panel.getByLabel("Xem lại theo ngày").fill(vnDay(3));
    await panel.getByRole("button", { name: "Xem lại" }).click();
    await expect(panel.getByRole("alert")).toContainText("Không thể xem lại một ngày trong tương lai");
  });

  test("KNV xã không có vai xem lịch sử lớp → không gọi phát lại", async ({ page }) => {
    await loginAs(page, "xa", "/ban-do-gis");
    await expect(page.getByTestId("replay-locked")).toBeVisible();
    await expect(page.getByTestId("gis-replay")).toHaveCount(0);
  });
});
