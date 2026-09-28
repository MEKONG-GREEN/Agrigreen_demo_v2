import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

const MAP = "Bản đồ mức đáp ứng máy theo tỉnh";

interface Rendered {
  clusters: number;
  points: number;
  colors: string[];
  provisional: number;
}

/** Đọc cụm/điểm MapLibre đã dựng ở nguồn "points" (chế độ giả lập lộ window.__agMaps). */
async function rendered(page: Page): Promise<Rendered | null> {
  return page.evaluate((label) => {
    type F = { properties: Record<string, unknown> };
    const map = (window as unknown as { __agMaps?: Record<string, { querySourceFeatures(id: string): F[]; loaded(): boolean }> }).__agMaps?.[label];
    if (!map) return null;
    const fs = map.querySourceFeatures("points");
    const clusters = fs.filter((f) => f.properties.cluster === true);
    const points = fs.filter((f) => f.properties.cluster !== true);
    return {
      clusters: new Set(clusters.map((f) => f.properties.cluster_id)).size,
      points: new Set(points.map((f) => f.properties.id)).size,
      colors: Array.from(new Set(points.map((f) => String(f.properties.color)))),
      provisional: new Set(points.filter((f) => f.properties.provisional === true).map((f) => f.properties.id)).size,
    };
  }, MAP);
}

async function view(page: Page, lng: number, lat: number, zoom: number) {
  await page.evaluate(
    ([label, x, y, z]) => {
      const map = (window as unknown as { __agMaps?: Record<string, { jumpTo(o: object): void }> }).__agMaps?.[label as string];
      map?.jumpTo({ center: [x, y], zoom: z });
    },
    [MAP, lng, lat, zoom] as const,
  );
}

test.describe("bản đồ CGH cấp HTX (FN-08 B2/B4/B5)", () => {
  test("ghim HTX gom cụm khi thu nhỏ, tách khi phóng to; tô màu mức đáp ứng; vị trí tạm", async ({ page }) => {
    await loginAs(page, "cuc", "/cgh");
    await expect(page.getByTestId("pin-count")).toContainText(/^[1-9]\d* HTX trên bản đồ · [1-9]\d* vị trí tạm/);
    // Toàn vùng (zoom 6): HTX gần nhau gom thành cụm.
    await view(page, 105.8, 10.1, 6);
    await expect.poll(async () => (await rendered(page))?.clusters ?? 0, { timeout: 15_000 }).toBeGreaterThan(0);
    // Phóng to quanh Đồng Tháp (zoom 12 > clusterMaxZoom 11): cụm tách thành ghim từng HTX, tô theo màu mức đáp ứng.
    await view(page, 105.9, 10.45, 12);
    await expect.poll(async () => (await rendered(page))?.points ?? 0, { timeout: 15_000 }).toBeGreaterThan(0);
    const r = (await rendered(page))!;
    const allowed = ["#1F7A38", "#9C6414", "#A3372A", "#2A78D6", "#9AA5A0"];
    for (const c of r.colors) expect(allowed).toContain(c.toUpperCase());
  });

  test("tra cứu nhanh → bay tới ghim → panel HTX: máy theo khâu, tình trạng + nguồn, chủ sở hữu đã che", async ({ page }) => {
    await loginAs(page, "cuc", "/cgh");
    await expect(page.getByTestId("pin-count")).toContainText("HTX trên bản đồ");
    const search = page.getByLabel("Tra cứu nhanh HTX (tên, Mã HTX, xã)");
    await search.fill("không-có-htx-này");
    await expect(page.getByText("Không có kết quả phù hợp")).toBeVisible();

    await search.fill("htx8200001");
    const results = page.getByRole("list", { name: "Kết quả tra cứu HTX" });
    await expect(results.getByRole("button")).toHaveCount(1);
    await results.getByRole("button").click();
    const panel = page.getByTestId("coop-panel");
    await expect(panel).toContainText("HTX8200001");
    await expect(panel.locator("[data-status]").first()).toBeVisible();
    await expect(panel.getByRole("list", { name: "Mức đáp ứng theo khâu" }).locator("li")).toHaveCount(4);
    const harvest = panel.getByRole("region", { name: /^Máy khâu Thu hoạch/ });
    await expect(harvest.locator("[data-machine]").first()).toContainText(/MAY-MGD-\d{5}/);
    await expect(harvest.locator("[data-machine]").first()).toContainText(/\[(Nhập tay|App HTX)\]/);
    // Chủ sở hữu chỉ có nhãn đã che và SĐT che (mục 10.4).
    await expect(panel.locator("[data-machine]").first()).toContainText("•••");
    await expect(panel).not.toContainText(/0\d{9}/);

    // Bản đồ đã bay tới ghim (zoom ≥ 12 → ghim hiện riêng, không trong cụm).
    await expect
      .poll(
        () =>
          page.evaluate((label) => {
            const map = (window as unknown as { __agMaps?: Record<string, { getZoom(): number }> }).__agMaps?.[label];
            return map ? Math.round(map.getZoom()) : 0;
          }, MAP),
        { timeout: 10_000 },
      )
      .toBeGreaterThanOrEqual(12);

    // HTX chưa có toạ độ → "vị trí tạm" (đặt ở tâm xã).
    await search.fill("HTX8200009");
    await results.getByRole("button").first().click();
    await expect(page.getByTestId("coop-panel")).toContainText("vị trí tạm");
    await page.getByRole("button", { name: "Đóng" }).click();
    await expect(page.getByTestId("coop-panel")).toHaveCount(0);
    await expect(page.getByRole("complementary", { name: "Chi tiết theo tỉnh" }).getByRole("heading", { name: "Theo tỉnh" })).toBeVisible();
  });
});
