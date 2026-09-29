import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

async function addPoints(page: Page, pts: string[]) {
  const input = page.getByLabel("Thêm điểm theo toạ độ (kinh độ, vĩ độ)");
  for (const p of pts) {
    await input.fill(p);
    await input.press("Enter");
  }
}

test.describe("Biên tập bản đồ (GIS US-BND-02/03/07, US-ROAD-02/03)", () => {
  test("GIS US-BND-03: sửa ranh giới HTX → phiên bản mới kèm lý do; diện tích đo trực tiếp", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/ban-do");
    const pick = page.getByLabel("Hợp tác xã");
    await expect(pick.locator("option")).not.toHaveCount(1);
    const first = await pick.locator("option").nth(1).getAttribute("value");
    await pick.selectOption(first!);
    await expect(page.getByTestId("bnd-state")).toContainText("Phiên bản 1 · đang dùng");
    await page.getByRole("button", { name: "Sửa ranh giới" }).click();
    await expect(page.getByRole("list", { name: "Các đỉnh đã vẽ" }).locator("li")).toHaveCount(6);
    const before = await page.getByTestId("draw-measure").textContent();
    await page.getByRole("button", { name: "Xoá đỉnh 1" }).click();
    await expect(page.getByTestId("draw-measure")).not.toHaveText(before ?? "");
    await expect(page.getByTestId("draw-measure")).toContainText(/Diện tích: .+ ha · Chu vi: .+ km/);
    const save = page.getByRole("button", { name: "Lưu phiên bản mới" });
    await expect(save).toBeDisabled();
    await page.getByLabel("Lý do sửa (ít nhất 10 ký tự)").fill("Hiệu chỉnh theo đo đạc thực địa 09/2026");
    await save.click();
    await expect(page.getByText("Đã lưu phiên bản ranh giới mới.")).toBeVisible();
    await expect(page.getByTestId("bnd-state")).toContainText("Phiên bản 2");
    await expect(page.getByRole("table").getByText("Hiệu chỉnh theo đo đạc thực địa 09/2026")).toBeVisible();
  });

  test("GIS US-BND-02: HTX chưa có ranh giới → vẽ vùng mới; ngừng dùng cần lý do", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/ban-do");
    const pick = page.getByLabel("Hợp tác xã");
    await expect(pick.locator("option")).not.toHaveCount(1);
    const values = await pick.locator("option").evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value).filter(Boolean));
    let found = false;
    for (const v of values.slice(0, 15)) {
      await pick.selectOption(v);
      const state = page.getByTestId("bnd-state");
      await expect(state).toBeVisible();
      if ((await state.textContent())?.includes("chưa có ranh giới")) {
        found = true;
        break;
      }
    }
    expect(found).toBe(true);
    await page.getByRole("button", { name: "Vẽ ranh giới mới" }).click();
    await addPoints(page, ["105.90, 10.45", "105.91, 10.45"]);
    await expect(page.getByText("Vùng cần ít nhất 3 điểm phân biệt.")).toBeVisible();
    await addPoints(page, ["105.91, 10.46"]);
    await page.getByRole("button", { name: "Lưu ranh giới" }).click();
    await expect(page.getByText("Đã lưu ranh giới HTX.")).toBeVisible();
    await expect(page.getByTestId("bnd-state")).toContainText("Phiên bản 1 · đang dùng");
    await page.getByLabel("Lý do ngừng dùng / khôi phục (ít nhất 10 ký tự)").fill("HTX giải thể theo nghị quyết đại hội");
    await page.getByRole("button", { name: "Ngừng dùng ranh giới" }).click();
    await expect(page.getByText(/Đã ngừng dùng ranh giới/)).toBeVisible();
    await expect(page.getByTestId("bnd-state")).toContainText("đã ngừng dùng");
  });

  test("GIS US-ROAD-02: vẽ tuyến đường bộ — thông báo lỗi đúng backlog, chiều dài tự tính", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/ban-do");
    await page.getByRole("tab", { name: "Đường bộ" }).click();
    await page.getByRole("button", { name: "Vẽ tuyến mới" }).click();
    await addPoints(page, ["105.80, 10.40", "105.85, 10.42"]);
    await expect(page.getByTestId("draw-measure")).toContainText(/Chiều dài: [\d,]+ km/);
    await page.getByRole("button", { name: "Lưu tuyến" }).click();
    await expect(page.getByText("Vui lòng nhập đầy đủ Tên tuyến và Phân loại.")).toBeVisible();
    await page.getByLabel("Tên tuyến").fill("Đường trục xã Mỹ An mới");
    await page.getByLabel("Phân loại").selectOption("truc_xa");
    await page.getByLabel("Tải trọng tối đa (tấn)").fill("0");
    await page.getByRole("button", { name: "Lưu tuyến" }).click();
    await expect(page.getByText("Tải trọng tối đa phải là số lớn hơn 0.")).toBeVisible();
    await page.getByLabel("Tải trọng tối đa (tấn)").fill("8");
    await page.getByRole("button", { name: "Lưu tuyến" }).click();
    await expect(page.getByText("Đã thêm tuyến đường bộ.")).toBeVisible();
    await expect(page.getByLabel("Tuyến đang có").locator("option", { hasText: "Đường trục xã Mỹ An mới" })).toHaveCount(1);
  });

  test("GIS US-BND-07: nhập ranh giới HTX từ GeoJSON — kết quả theo đối tượng", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/ban-do");
    await page.getByRole("tab", { name: "Nhập ranh giới HTX từ tệp" }).click();
    const fc = {
      type: "FeatureCollection",
      features: [
        { type: "Feature", properties: { name: "Ranh giới HTX 1", MA_HTX: "HTX8200001" }, geometry: { type: "Polygon", coordinates: [[[105.9, 10.4], [105.92, 10.4], [105.92, 10.42], [105.9, 10.4]]] } },
        { type: "Feature", properties: { name: "Không rõ HTX", MA_HTX: "HTX0000000" }, geometry: { type: "Polygon", coordinates: [[[105.9, 10.4], [105.92, 10.4], [105.92, 10.42], [105.9, 10.4]]] } },
      ],
    };
    await page.getByLabel("Tệp GeoJSON / KML / Shapefile (.zip)").setInputFiles({ name: "ranh-gioi.geojson", mimeType: "application/geo+json", buffer: Buffer.from(JSON.stringify(fc)) });
    await page.getByRole("button", { name: "Nhập ranh giới HTX" }).click();
    const res = page.getByRole("region", { name: "Kết quả nhập ranh giới HTX" });
    await expect(res).toContainText("1 đã nạp");
    await expect(res).toContainText("1 bị loại");
    await expect(res.getByRole("row", { name: /Ranh giới HTX 1/ })).toContainText(/Phiên bản \d+ \(thay bản cũ\)/);
    await expect(res.getByRole("row", { name: /Không rõ HTX/ })).toContainText("Không tìm thấy HTX");
  });

  test("KNV xã không vào được màn hình biên tập", async ({ page }) => {
    await loginAs(page, "xa", "/quan-tri/ban-do");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  });
});

test.describe("Kho / Hub & dung lượng (GIS US-WH-01, BR-52, US-BND-05/06/08)", () => {
  test("nhả số nhập tay khi số đồng bộ khác (sync_differs)", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/kho-hub");
    const row = page.getByRole("row", { name: /Kho Long Hưng/ });
    await expect(row).toContainText("Số đồng bộ khác số nhập tay");
    await row.getByRole("button", { name: "Kho Long Hưng" }).click();
    const d = page.getByTestId("facility-detail");
    await expect(d.getByTestId("capacity-view")).toContainText("Admin nhập tay");
    const release = d.getByRole("button", { name: "Nhả số nhập tay (áp dụng số đồng bộ)" });
    await expect(release).toBeDisabled();
    await d.getByLabel(/Lý do/).fill("ERP đã cập nhật số kiểm kê mới nhất");
    await release.click();
    await expect(page.getByTestId("capacity-view")).toContainText("Đồng bộ (van công bố)");
    await expect(page.getByRole("row", { name: /Kho Long Hưng/ })).not.toContainText("Số đồng bộ khác số nhập tay");
  });

  test("nhập tay dung lượng → % và màu theo ngưỡng; sức chứa ≤ 0 bị chặn", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/kho-hub");
    await page.getByRole("row", { name: /Kho lúa Mỹ An/ }).getByRole("button", { name: "Kho lúa Mỹ An" }).click();
    const d = page.getByTestId("facility-detail");
    await d.getByLabel("Sức chứa").fill("0");
    await d.getByLabel("Đã sử dụng").fill("10");
    await d.getByRole("button", { name: "Ghi số nhập tay" }).click();
    await expect(page.getByText("Sức chứa phải là số lớn hơn 0.")).toBeVisible();
    await page.getByTestId("facility-detail").getByLabel("Sức chứa").fill("5000");
    await page.getByTestId("facility-detail").getByLabel("Đã sử dụng").fill("4500");
    await page.getByTestId("facility-detail").getByRole("button", { name: "Ghi số nhập tay" }).click();
    await expect(page.getByTestId("capacity-view")).toContainText("90% · Đã đầy");
  });

  test("GIS US-BND-05: thêm Hub trên bản đồ — thiếu tên/địa chỉ bị chặn đúng thông báo", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/kho-hub");
    await page.getByRole("tab", { name: "Thêm / sửa trên bản đồ" }).click();
    await addPoints(page, ["105.95, 10.48"]);
    await page.getByLabel("Sức chứa").fill("1200");
    await page.getByRole("button", { name: "Lưu kho/Hub" }).click();
    await expect(page.getByText("Vui lòng nhập đầy đủ Tên Hub và Địa chỉ.")).toBeVisible();
    await page.getByLabel("Tên kho/Hub").fill("Hub thử nghiệm Mỹ An");
    await page.getByLabel("Tỉnh").selectOption("82");
    await page.getByLabel("Xã").selectOption({ index: 1 });
    await page.getByRole("button", { name: "Lưu kho/Hub" }).click();
    await expect(page.getByRole("row", { name: /Hub thử nghiệm Mỹ An/ })).toBeVisible();
  });

  test("GIS US-BND-08: nhập vị trí Hub từ KML", async ({ page }) => {
    await loginAs(page, "setter", "/quan-tri/kho-hub");
    await page.getByRole("tab", { name: "Nhập vị trí từ tệp" }).click();
    const kml = `<?xml version="1.0"?><kml><Document>
<Placemark><name>Hub KML 1</name><ExtendedData><Data name="suc_chua"><value>2500</value></Data><Data name="ma_tinh"><value>82</value></Data><Data name="ma_xa"><value>82101</value></Data></ExtendedData><Point><coordinates>105.9,10.4</coordinates></Point></Placemark>
<Placemark><name>Hub thiếu sức chứa</name><ExtendedData><Data name="ma_tinh"><value>82</value></Data><Data name="ma_xa"><value>82101</value></Data></ExtendedData><Point><coordinates>105.91,10.41</coordinates></Point></Placemark>
</Document></kml>`;
    await page.getByLabel("Tệp GeoJSON / KML / Shapefile (.zip)").setInputFiles({ name: "hub.kml", mimeType: "application/vnd.google-earth.kml+xml", buffer: Buffer.from(kml) });
    await page.getByRole("button", { name: "Nhập kho/Hub" }).click();
    const res = page.getByRole("region", { name: "Kết quả nhập kho/Hub" });
    await expect(res).toContainText("1 đã nạp");
    await expect(res.getByRole("row", { name: /Hub thiếu sức chứa/ })).toContainText("Sức chứa phải là số lớn hơn 0.");
  });
});
