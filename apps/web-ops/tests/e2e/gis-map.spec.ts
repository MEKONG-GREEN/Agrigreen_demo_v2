import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

const MAP = "Bản đồ GIS nhiều lớp";

type MapLike = { jumpTo(o: object): void; querySourceFeatures(id: string): { geometry: { type: string; coordinates: unknown } }[]; getZoom(): number };

async function jump(page: Page, lng: number, lat: number, zoom: number) {
  await page.evaluate(
    ([label, x, y, z]) => (window as unknown as { __agMaps?: Record<string, MapLike> }).__agMaps?.[label as string]?.jumpTo({ center: [x, y], zoom: z }),
    [MAP, lng, lat, zoom] as const,
  );
}

async function countOf(page: Page, id: string): Promise<number> {
  const t = (await page.getByTestId(id).textContent()) ?? "";
  return Number(/(\d[\d.]*)/.exec(t.split(":")[1] ?? "")?.[1]?.replace(/\./g, "") ?? "0");
}

test.describe("Bản đồ GIS nhiều lớp (GIS FN-01/04/13/14/15, US-ROAD-01, US-WATER-01)", () => {
  test("tải theo khung nhìn: HTX, kho/Hub có % dung lượng, chú giải mùa vụ, cảnh báo chín rộ; không gọi dịch vụ ngoài (RB-7)", async ({ page }) => {
    const foreign: string[] = [];
    page.on("request", (r) => {
      const u = new URL(r.url());
      if (!["localhost", "127.0.0.1"].includes(u.hostname) && !u.protocol.startsWith("data") && !u.protocol.startsWith("blob")) foreign.push(r.url());
    });
    await loginAs(page, "setter", "/ban-do-gis");
    await expect(page.getByRole("heading", { name: "Bản đồ GIS nhiều lớp" })).toBeVisible();
    await expect.poll(() => countOf(page, "count-coops"), { timeout: 20_000 }).toBeGreaterThan(0);
    await expect.poll(() => countOf(page, "count-facilities")).toBeGreaterThan(0);
    // GIS US-HUB-01: widget % trên marker, màu theo lớp.
    const widget = page.locator(".map-widget").first();
    await expect(widget).toBeVisible();
    await expect(widget).toHaveText(/\d+%|\?/);
    await expect(page.locator(".map-widget", { hasText: /%/ }).first()).toHaveAttribute("aria-label", /(Còn chỗ|Sắp đầy|Đã đầy)/);
    // GIS US-SEASON-01: chú giải 5 giai đoạn.
    await expect(page.getByRole("list", { name: "Chú giải giai đoạn mùa vụ" }).locator("li[data-stage]")).toHaveCount(5);
    // GIS US-YIELD-01: vùng chín rộ 1–3 ngày tới kèm sản lượng dự kiến từ App HTX.
    const ripe = page.getByRole("list", { name: "Vùng chín rộ" }).locator("li");
    await expect(ripe.first()).toBeVisible();
    await expect(ripe.first()).toContainText(/Dự kiến thu \d{2}\/\d{2}\/\d{4}/);
    await expect(page.getByRole("heading", { name: /Cảnh báo chín rộ 1–3 ngày tới \([1-9]\d* vùng\)/ })).toBeVisible();
    // GIS BR-24: thửa chỉ khi zoom ≥ 13.
    await expect(page.getByTestId("plots-notice")).toContainText("Phóng to tới mức 13");
    expect(await countOf(page, "count-plots")).toBe(0);
    const center = await page.evaluate((label) => {
      const map = (window as unknown as { __agMaps?: Record<string, MapLike> }).__agMaps?.[label];
      const f = map?.querySourceFeatures("coops").find((x) => x.geometry.type === "Polygon");
      const ring = (f?.geometry.coordinates as number[][][] | undefined)?.[0];
      return ring ? [ring.reduce((s, p) => s + p[0]!, 0) / ring.length, ring.reduce((s, p) => s + p[1]!, 0) / ring.length] : null;
    }, MAP);
    expect(center).not.toBeNull();
    await jump(page, center![0]!, center![1]!, 14);
    await expect.poll(() => countOf(page, "count-plots"), { timeout: 20_000 }).toBeGreaterThan(0);
    await expect(page.getByTestId("plots-notice")).toHaveCount(0);
    expect(foreign).toEqual([]);
  });

  test("GIS US-ROAD-01: lọc đường bộ theo tải trọng, hiện chiều dài; không tuyến phù hợp → thông báo", async ({ page }) => {
    await loginAs(page, "setter", "/ban-do-gis");
    await expect.poll(() => countOf(page, "count-roads"), { timeout: 20_000 }).toBeGreaterThan(0);
    await page.getByLabel("Tải trọng phương tiện đường bộ (tấn)").fill("20");
    await page.getByRole("button", { name: "Tìm tuyến phù hợp" }).click();
    const list = page.getByRole("list", { name: "Tuyến đường bộ phù hợp" });
    await expect(list.locator("li").first()).toContainText(/Quốc lộ · tối đa 30 tấn · [\d,]+ km/);
    await page.getByLabel("Tải trọng phương tiện đường bộ (tấn)").fill("100");
    await page.getByRole("button", { name: "Tìm tuyến phù hợp" }).click();
    await expect(page.getByTestId("roads-notice")).toContainText("Không tìm thấy tuyến đường bộ phù hợp tải trọng 100 tấn");
    await page.getByLabel("Tải trọng phương tiện đường bộ (tấn)").fill("-3");
    await page.getByRole("button", { name: "Tìm tuyến phù hợp" }).click();
    await expect(page.getByText("Tải trọng phải là số lớn hơn 0")).toBeVisible();
  });

  test("GIS US-WATER-01: tuyến thủy đi được với tải trọng sà lan; vượt mọi tuyến → thông báo", async ({ page }) => {
    await loginAs(page, "setter", "/ban-do-gis");
    await expect.poll(() => countOf(page, "count-waterways"), { timeout: 20_000 }).toBeGreaterThan(0);
    await page.getByLabel("Tải trọng sà lan đường thủy (tấn)").fill("300");
    await page.getByRole("button", { name: "Tìm tuyến phù hợp" }).click();
    const list = page.getByRole("list", { name: "Tuyến thủy phù hợp" });
    await expect(list.locator("li").first()).toContainText("Sông");
    await expect(page.getByTestId("count-waterways")).toContainText(/đi được [1-9]/);
    await page.getByLabel("Tải trọng sà lan đường thủy (tấn)").fill("5000");
    await page.getByRole("button", { name: "Tìm tuyến phù hợp" }).click();
    await expect(page.getByTestId("waterways-notice")).toContainText("Không tìm thấy tuyến sông/kênh phù hợp tải trọng sà lan 5000 tấn");
  });

  test("GIS US-INT-01: lớp Tình trạng máy CGH (thiếu/thừa) — chú giải 5 mức, điểm theo HTX; không gọi API khi thiếu vai", async ({ page }) => {
    await loginAs(page, "setter", "/ban-do-gis");
    const toggle = page.getByLabel("Tình trạng máy CGH (thiếu/thừa)");
    await expect(toggle).toBeEnabled();
    await expect(toggle).not.toBeChecked();
    await toggle.check();
    const legend = page.getByRole("list", { name: "Chú giải tình trạng máy CGH" });
    await expect(legend.locator("li")).toHaveText(["Thiếu", "Cần chú ý", "Đủ", "Thừa", "Chưa có dữ liệu"]);
    await expect(legend.locator('li[data-status="thieu"]')).toHaveAttribute("data-color", "#A3372A");
    await expect.poll(() => countOf(page, "count-cgh"), { timeout: 20_000 }).toBeGreaterThan(0);
    await expect(page.getByText("GIS chỉ hiển thị, không tự tính lại")).toBeVisible();
    // GIS US-CFG-01: ngưỡng chú giải kho/Hub theo quy chuẩn hiển thị đã duyệt.
    await expect(page.getByRole("list", { name: "Chú giải dung lượng kho/Hub" }).locator('li[data-capacity="near_full"]')).toContainText("≥ 60%");

    const mech: string[] = [];
    page.on("request", (r) => {
      if (r.url().includes("/v1/mechanization/")) mech.push(r.url());
    });
    await loginAs(page, "tinh", "/ban-do-gis");
    await expect(page.getByLabel("Tình trạng máy CGH (thiếu/thừa)")).toBeDisabled();
    await expect(page.getByTestId("cgh-locked")).toContainText("cần vai Xem hoặc Quản trị Bản đồ CGH");
    await expect.poll(() => countOf(page, "count-coops"), { timeout: 20_000 }).toBeGreaterThan(0);
    expect(mech).toEqual([]);
  });

  test("tắt lớp → không tải lớp đó; cán bộ tỉnh chỉ thấy HTX trong phạm vi", async ({ page }) => {
    await loginAs(page, "tinh", "/ban-do-gis");
    await expect.poll(() => countOf(page, "count-coops"), { timeout: 20_000 }).toBeGreaterThan(0);
    const tinh = await countOf(page, "count-coops");
    await page.getByLabel("Kho / Hub (dung lượng)").uncheck();
    await expect(page.locator(".map-widget")).toHaveCount(0);
    await loginAs(page, "setter", "/ban-do-gis");
    await expect.poll(() => countOf(page, "count-coops"), { timeout: 20_000 }).toBeGreaterThan(tinh);
  });
});
