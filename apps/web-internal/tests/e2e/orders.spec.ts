import { expect, test } from "@playwright/test";
import { alert, labelled, loginAs, SELF_APPROVAL, sidebar, switchUser } from "./helpers";

// G4 mkg/orders — W-01: hợp đồng mua rơm (CT-01..03, R-56), đơn mua + phiếu nhận lệch > 3 % (cảnh báo, không chặn),
// một nguồn khoản phải trả, đơn bán vượt ngưỡng giá/chiết khấu, giao nhận theo phiếu CUSTOMER_IN.
test.describe("Đơn hàng (mkg/orders)", () => {
  test("hợp đồng: người lập không tự duyệt; người khác duyệt → hiệu lực; chồng lấn CT-01 bị từ chối", async ({ page }) => {
    await loginAs(page, "kinh-doanh", "/don-hang/hop-dong");
    await page.getByRole("button", { name: "Mở hợp đồng HDR00003" }).click();
    await expect(page.getByText("Bạn là người đề xuất — cần một người phê duyệt khác (R-56)")).toBeVisible();
    await page.getByRole("button", { name: "Duyệt hợp đồng" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await expect(alert(page)).toContainText("self_approval_forbidden");

    await page.getByRole("button", { name: "Lập hợp đồng" }).click();
    const form = page.getByRole("region", { name: "Lập hợp đồng (phiên bản 1, chờ duyệt)" });
    await form.getByRole("button", { name: "HTX …000002" }).click();
    await form.getByLabel("Đơn giá (VND/tấn)").fill("640000");
    await form.getByLabel("Khối lượng cam kết (kg)").fill("200000");
    await form.getByRole("button", { name: "Gửi hợp đồng" }).click();
    const overlap = alert(page).filter({ hasText: "orders.contract_overlap" });
    await expect(overlap).toBeVisible();
    await expect(overlap).toContainText("chồng thời gian");

    await switchUser(page, "truong-kinh-doanh");
    await page.getByRole("button", { name: "Mở hợp đồng HDR00003" }).click();
    await page.getByRole("button", { name: "Duyệt hợp đồng" }).click();
    await expect(page.getByText(/Đã duyệt phiên bản 1 — hợp đồng hiệu lực/)).toBeVisible();
    await expect(page.getByRole("table", { name: "Phiên bản hợp đồng" })).toContainText("Hiệu lực");
  });

  test("phiếu nhận rơm lệch +7,56 % > 3 %: cần biên bản, KHÔNG chặn duyệt; rơm không sinh phải trả ở đơn mua", async ({ page }) => {
    await loginAs(page, "kinh-doanh", "/don-hang/don-mua");
    await page.getByRole("button", { name: "Mở đơn mua PO000001" }).click();
    const card = labelled(page, "Phiếu nhận GRN000001");
    await expect(card).toContainText("Cần biên bản chênh lệch");
    await expect(card).toContainText("+7,56 %");
    await expect(card).toContainText("cần lập biên bản chênh lệch");
    await card.getByLabel("Biên bản chênh lệch GRN000001").fill("Ghe chở dư 6,8 t do HTX gom thêm thửa lân cận");
    await card.getByRole("button", { name: "Ghi biên bản" }).click();
    await expect(page.getByText("Đã ghi biên bản chênh lệch")).toBeVisible();
    await card.getByRole("button", { name: "Duyệt phiếu nhận GRN000001" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "truong-kinh-doanh");
    await page.getByRole("button", { name: "Mở đơn mua PO000001" }).click();
    await labelled(page, "Phiếu nhận GRN000001").getByRole("button", { name: "Duyệt phiếu nhận GRN000001" }).click();
    await expect(page.getByText("Đã duyệt phiếu nhận", { exact: true })).toBeVisible();
    await expect(labelled(page, "Phiếu nhận GRN000001")).toContainText("chỉ từ quyết toán HTX đã duyệt");
  });

  test("đơn mua hàng khác: tự duyệt bị chặn; người khác duyệt; phiếu nhận duyệt → đúng một khoản phải trả", async ({ page }) => {
    await loginAs(page, "kinh-doanh", "/don-hang/don-mua");
    await page.getByRole("button", { name: "Mở đơn mua PO000002" }).click();
    await page.getByRole("button", { name: "Duyệt đơn mua" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "truong-kinh-doanh");
    await page.getByRole("button", { name: "Mở đơn mua PO000002" }).click();
    await page.getByRole("button", { name: "Duyệt đơn mua" }).click();
    await expect(page.getByText("Đã duyệt đơn mua PO000002")).toBeVisible();
    await page.getByLabel("Số nhận (kg)").fill("480");
    await page.getByRole("button", { name: "Lập phiếu nhận" }).click();
    await expect(page.getByText(/Đã lập phiếu nhận GRN000002 — lệch -4.00 % vượt ngưỡng/)).toBeVisible();
    await switchUser(page, "kinh-doanh");
    await page.getByRole("button", { name: "Mở đơn mua PO000002" }).click();
    await page.getByRole("button", { name: "Duyệt phiếu nhận GRN000002" }).click();
    await expect(page.getByText("Đã duyệt phiếu nhận — ghi một khoản phải trả")).toBeVisible();
    await expect(page.getByRole("table", { name: "Khoản phải trả" })).toContainText("21.600.000 ₫");
    await expect(page.getByRole("table", { name: "Khoản phải trả" }).getByRole("row")).toHaveCount(2);
  });

  test("đơn bán vượt ngưỡng chiết khấu: không có vai duyệt → cần duyệt giá; người lập tự duyệt → chặn; người khác xác nhận", async ({ page }) => {
    await loginAs(page, "ban-hang", "/don-hang/don-ban");
    await page.getByRole("button", { name: "Mở đơn bán SO000002" }).click();
    await expect(labelled(page, "Thông tin đơn bán")).toContainText("Vượt ngưỡng — cần sales_approver khác người lập");
    await page.getByRole("button", { name: "Xác nhận đơn bán" }).click();
    await expect(alert(page)).toContainText("orders.price_approval_required");
    await switchUser(page, "kinh-doanh");
    await page.getByRole("button", { name: "Mở đơn bán SO000003" }).click();
    await page.getByRole("button", { name: "Xác nhận đơn bán" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await page.getByRole("button", { name: "Mở đơn bán SO000002" }).click();
    await page.getByRole("button", { name: "Xác nhận đơn bán" }).click();
    await expect(page.getByText("Đã xác nhận đơn bán SO000002")).toBeVisible();
    await expect(labelled(page, "Thông tin đơn bán")).toContainText("85.500.000 ₫");
  });

  test("giao nhận: chấp nhận = phiếu CUSTOMER_IN (không nhập tay); chưa có phiếu → orders.customer_in_missing", async ({ page }) => {
    await loginAs(page, "kinh-doanh", "/don-hang/don-ban");
    await page.getByRole("button", { name: "Mở đơn bán SO000001" }).click();
    const d1 = labelled(page, "Giao nhận GN000001");
    await d1.getByLabel("Mã tệp chứng từ GN000001").fill("0192f0ff-0000-7000-8000-000000000001");
    await d1.getByRole("button", { name: "Chấp nhận giao nhận GN000001" }).click();
    await expect(page.getByText(/Đã chấp nhận GN000001: 14.800 kg — phát mkg.orders.delivery.accepted/)).toBeVisible();
    await expect(d1).toContainText("26.640.000 ₫");
    const d2 = labelled(page, "Giao nhận GN000002");
    await d2.getByRole("button", { name: "Xuất giao GN000002" }).click();
    await expect(page.getByText("Đã xuất giao GN000002")).toBeVisible();
    await d2.getByLabel("Mã tệp chứng từ GN000002").fill("0192f0ff-0000-7000-8000-000000000002");
    await d2.getByRole("button", { name: "Chấp nhận giao nhận GN000002" }).click();
    await expect(alert(page)).toContainText("orders.customer_in_missing");
  });

  test("tham số đơn hàng: ngưỡng 3 % tạm; người đề xuất không tự duyệt", async ({ page }) => {
    await loginAs(page, "kinh-doanh", "/don-hang/tham-so");
    await expect(labelled(page, "Giá trị đang áp dụng")).toContainText("orders.receipt.po_variance_warn_pct:default");
    await page.getByLabel("Giá trị mới").fill("5");
    await page.getByLabel("Lý do đề xuất").fill("Khách đề nghị nới dung sai nhận hàng mùa mưa");
    await page.getByRole("button", { name: "Gửi đề xuất tham số" }).click();
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "truong-kinh-doanh");
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(labelled(page, "Giá trị đang áp dụng")).toContainText("orders.receipt.po_variance_warn_pct:v1");
  });

  test("phân quyền: nhân viên bán hàng không thấy giá thành / kế toán / vận tải", async ({ page }) => {
    await loginAs(page, "ban-hang", "/gia-thanh/ky");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    await expect(sidebar(page).getByRole("link", { name: "Đơn bán & giao nhận" })).toBeVisible();
    await expect(sidebar(page).getByRole("link", { name: "Kỳ giá thành" })).toHaveCount(0);
    await expect(sidebar(page).getByRole("link", { name: "Chuyến & chặng" })).toHaveCount(0);
    await expect(sidebar(page).getByRole("link", { name: "Bút toán tổng hợp" })).toHaveCount(0);
  });
});
