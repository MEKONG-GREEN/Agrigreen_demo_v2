import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

/** Màu tô từng tỉnh trên bản đồ (danh sách vùng cho trình đọc màn hình của MapView) — code → {fill, label}. */
async function mapAreas(page: Page): Promise<Map<string, { fill: string; label: string }>> {
  const items = page.getByTestId("map-areas").locator("li");
  const out = new Map<string, { fill: string; label: string }>();
  for (const li of await items.all()) {
    const code = (await li.getAttribute("data-code")) ?? "";
    const fill = ((await li.getAttribute("data-fill")) ?? "").toUpperCase();
    const text = (await li.textContent()) ?? "";
    out.set(code, { fill, label: text.split(": ").pop() ?? "" });
  }
  return out;
}

/** Chú giải: nhãn mức → màu. */
async function legendColors(page: Page): Promise<Map<string, string>> {
  const legend = page.getByRole("list", { name: "Mức đáp ứng máy", exact: true });
  const out = new Map<string, string>();
  for (const li of await legend.locator("li").all()) {
    const label = ((await li.locator("b").textContent()) ?? "").trim();
    const color = ((await li.locator(".sw").getAttribute("data-color")) ?? "").toUpperCase();
    out.set(label, color);
  }
  return out;
}

test("bản đồ CGH (FN-08): vùng tô đúng màu chú giải theo mức đáp ứng; vùng chưa có dữ liệu tô xám; lọc khâu áp dụng ngay", async ({ page }) => {
  await loginAs(page, "cuc", "/cgh");
  await expect(page.getByRole("heading", { level: 1, name: /Cổng Cục KTHT/ })).toBeVisible();
  const legend = page.getByRole("list", { name: "Mức đáp ứng máy", exact: true });
  await expect(legend.locator("li")).toHaveCount(5);
  await expect(legend).toContainText("≥ 85%");
  await expect(legend).toContainText("60–<85%");
  await expect(legend).toContainText("< 60%");

  // Sau khi có kết quả cân đối đã lưu, mọi tỉnh mang nhãn mức của máy chủ (không còn xám mặc định).
  await expect(page.getByTestId("map-areas").locator("li")).toHaveCount(6);
  await expect.poll(async () => (await mapAreas(page)).get("82")?.label).not.toBe("Chưa có dữ liệu");

  const check = async () => {
    const colors = await legendColors(page);
    const areas = await mapAreas(page);
    const labels = new Set<string>();
    for (const [code, a] of areas) {
      expect(colors.get(a.label), `màu chú giải cho "${a.label}" (tỉnh ${code})`).toBe(a.fill);
      labels.add(a.label);
    }
    // AC-04: Tây Ninh (80) chưa có dữ liệu diện tích → xám, nhãn "Chưa có dữ liệu".
    expect(areas.get("80")).toEqual({ fill: colors.get("Chưa có dữ liệu"), label: "Chưa có dữ liệu" });
    expect(labels.size).toBeGreaterThan(1);
    return areas;
  };
  const before = await check();

  // Bảng "Theo tỉnh" dùng cùng mức với bản đồ.
  const tayNinh = page.getByRole("button", { name: "Xem Tỉnh Tây Ninh" });
  await expect(tayNinh.locator("[data-status]")).toHaveAttribute("data-status", "khong_du_lieu");

  // AC-03: lọc theo khâu → bản đồ và bảng cập nhật, màu vẫn khớp chú giải.
  await page.getByLabel("Khâu sản xuất").selectOption({ index: 1 });
  await expect(page.getByTestId("map-areas").locator("li")).toHaveCount(before.size);
  await expect(async () => {
    await check();
  }).toPass();

  // AC-02: bấm một tỉnh ở bảng → mở chi tiết tỉnh đó.
  await page.getByRole("button", { name: "Xem Tỉnh Đồng Tháp" }).click();
  await expect(page.getByRole("complementary", { name: "Chi tiết theo tỉnh" }).getByRole("heading", { name: "Tỉnh Đồng Tháp" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Tất cả" })).toBeVisible();
});
