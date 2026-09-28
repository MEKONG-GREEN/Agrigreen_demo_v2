import { expect, test } from "@playwright/test";
import { COOPERATIVES, plotsOf } from "../../src/api/mock/data";
import { loginAs } from "./helpers";

// HTX có thửa nháp để gán: trong mock, thửa nháp ở vị trí chia hết cho 3 chồng lấn HTX khác (không bỏ qua được),
// các thửa nháp khác chồng lấn cùng HTX → cần "Xác nhận vẫn lưu".
const coop = COOPERATIVES.find((c) => c.api.active && c.plots >= 21)!;
const plots = plotsOf(coop);
const confirmable = plots.find((p, i) => p.properties.status === "draft" && i % 3 !== 0)!;
const crossCoop = plots.find((p, i) => p.properties.status === "draft" && i % 3 === 0)!;

test("chi tiết HTX: tên địa bàn, số hộ; nông hộ có huy hiệu đồng ý (G-03/G-04)", async ({ page }) => {
  await loginAs(page, "tw", "/htx");
  await page.getByLabel("Tìm HTX").fill(coop.api.code);
  const link = page.getByRole("link", { name: coop.api.name });
  await expect(link).toBeVisible();
  await expect(page.getByRole("row").filter({ has: link })).toContainText(coop.commune_name);
  await link.click();
  await expect(page.getByRole("heading", { level: 1, name: coop.api.name })).toBeVisible();
  await expect(page.getByText(`${coop.commune_name}, ${coop.province_name}`).first()).toBeVisible();
  await page.getByRole("tab", { name: "Nông hộ" }).click();
  await expect(page.getByRole("columnheader", { name: "Đồng ý chia sẻ dữ liệu" })).toBeVisible();
  await expect(page.getByText("Đồng ý · MRV").first()).toBeVisible();
});

test("gán thửa nháp: chồng lấn cùng HTX → Xác nhận vẫn lưu; chồng lấn HTX khác → không bỏ qua được", async ({ page }) => {
  await loginAs(page, "tw", `/htx/${coop.api.ref}`);
  await page.getByRole("tab", { name: "Thửa ruộng" }).click();
  await expect(page.getByRole("heading", { name: "Thửa nháp trong xã (chưa gán HTX)" })).toBeVisible();

  const crossRow = page.getByRole("row").filter({ hasText: crossCoop.properties.code });
  await crossRow.getByRole("button", { name: "Gán về HTX này" }).click();
  const crossAlert = crossRow.getByRole("alert");
  await expect(crossAlert).toContainText("HTX khác");
  await expect(crossAlert).toContainText("geo.overlap_cross_coop");
  await expect(crossRow.getByRole("button", { name: "Xác nhận vẫn lưu" })).toHaveCount(0);

  const row = page.getByRole("row").filter({ hasText: confirmable.properties.code });
  await row.getByRole("button", { name: "Gán về HTX này" }).click();
  const alert = row.getByRole("alert");
  await expect(alert).toContainText("chồng lấn thửa cùng HTX");
  await expect(alert).toContainText("geo.overlap_needs_confirm");
  await row.getByRole("button", { name: "Xác nhận vẫn lưu" }).click();
  // Thửa rời danh sách nháp và vào danh sách thửa của HTX, gắn cờ chờ xem xét.
  const drafts = page.getByRole("region", { name: "Thửa nháp trong xã (chưa gán HTX)" });
  await expect(drafts.getByRole("row").filter({ hasText: confirmable.properties.code })).toHaveCount(0);
  const owned = page.getByRole("table", { name: "Thửa ruộng của HTX" }).getByRole("row").filter({ hasText: confirmable.properties.code });
  await expect(owned).toContainText("Có chồng lấn chờ xem xét");
  await expect(owned).toContainText("Đang dùng");
});
