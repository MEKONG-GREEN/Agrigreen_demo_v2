import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { loginAs, loginViaUi, logout, navTo, sidebar } from "./helpers";

// G5 (ADR-0022/0023/0024): màn hình quản trị Public API trên Admin API (MFA). Dữ liệu giả lập; trạng thái mock
// G5 lưu ở sessionStorage của tab (chỉ tiền tố khóa — không bao giờ có bí mật).

const A = /Quản trị khách hàng Public API \(A\)/;
const B = /Người duyệt khách hàng API & bảng kê \(B\)/;
const FACTORY = "Nhà máy điện sinh khối Hậu Giang (giả lập)";
const VERIFIER = "Đơn vị thẩm định độc lập A (giả lập)";

async function openClient(page: Page, name: string) {
  await page.getByRole("button", { name: `Xem chi tiết ${name}` }).click();
  const detail = page.getByTestId("client-detail");
  await expect(detail.getByRole("heading", { name })).toBeVisible();
  return detail;
}

async function storageDump(page: Page): Promise<string> {
  return page.evaluate(() => JSON.stringify({ s: { ...window.sessionStorage }, l: { ...window.localStorage } }));
}

test("G5 W-01: vòng đời khách hàng — A đề xuất, không tự duyệt (R-56), B duyệt; tạm ngưng → bật lại → thu hồi", async ({ page }) => {
  await page.goto("/login");
  await loginViaUi(page, A);
  await navTo(page, "Khách hàng Public API");
  await page.getByRole("button", { name: "Đề xuất khách hàng mới" }).click();
  const form = page.getByRole("form", { name: "Đề xuất khách hàng Public API" });
  await form.getByLabel("Tên tổ chức").fill("Hợp tác xã dịch vụ dữ liệu E2E (giả lập)");
  // Kiểm phía client: mã đầu mối phải là UUID, không nhập tên/SĐT (RB-3).
  await form.getByLabel(/Mã tenant bên thứ ba/).fill("0192a5b0-0000-7000-8000-0000000000e2");
  await form.getByLabel(/Mã đầu mối/).fill("Nguyễn Văn A 0901234567");
  await form.getByLabel("Mục đích sử dụng dữ liệu").fill("Kiểm thử E2E vòng đời khách hàng Public API");
  await form.getByRole("button", { name: "Đề xuất và gửi duyệt" }).click();
  await expect(form).toContainText("Mã đầu mối phải là UUID");
  await form.getByLabel(/Mã đầu mối/).fill("0192a5d0-0000-7000-8000-0000000000e2");
  await form.getByRole("button", { name: "Đề xuất và gửi duyệt" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã đề xuất" })).toBeVisible();

  const name = "Hợp tác xã dịch vụ dữ liệu E2E (giả lập)";
  let detail = await openClient(page, name);
  await expect(detail).toContainText("Chờ duyệt");
  const approveA = detail.getByRole("button", { name: "Phê duyệt khách hàng" });
  await expect(approveA).toBeDisabled();
  await expect(detail.getByTestId("sod-hint").first()).toHaveText("Người duyệt phải khác người đề xuất (R-56).");
  // Chưa active → chưa phát hành khóa được.
  await expect(detail.getByRole("button", { name: "Phát hành khóa mới" })).toBeDisabled();

  await logout(page);
  await loginViaUi(page, B);
  await navTo(page, "Khách hàng Public API");
  // B không có vai api_client_admin → không có form đề xuất.
  await expect(page.getByRole("button", { name: "Đề xuất khách hàng mới" })).toHaveCount(0);
  detail = await openClient(page, name);
  await detail.getByRole("button", { name: "Phê duyệt khách hàng" }).click();
  await expect(detail.getByText("Đang hoạt động", { exact: true })).toBeVisible();
  await expect(detail.getByRole("button", { name: "Tạm ngưng" })).toBeDisabled();

  await logout(page);
  await loginViaUi(page, A);
  await navTo(page, "Khách hàng Public API");
  detail = await openClient(page, name);
  await detail.getByRole("button", { name: "Tạm ngưng" }).click();
  let dlg = page.getByRole("dialog", { name: `Tạm ngưng ${name}` });
  await dlg.getByLabel(/Lý do/).fill("ngắn");
  await dlg.getByRole("button", { name: "Tạm ngưng" }).click();
  await expect(dlg).toContainText("Lý do tối thiểu 10 ký tự");
  await dlg.getByLabel(/Lý do/).fill("Chờ ký phụ lục hợp đồng dữ liệu");
  await dlg.getByRole("button", { name: "Tạm ngưng" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(detail).toContainText("Lý do: Chờ ký phụ lục hợp đồng dữ liệu");
  // Bật lại là việc của người duyệt — A có cả vai duyệt nên bật lại được (không phải tự duyệt bản đề xuất).
  await detail.getByRole("button", { name: "Bật lại" }).click();
  await expect(detail.getByText("Đang hoạt động", { exact: true })).toBeVisible();
  await detail.getByRole("button", { name: "Thu hồi khách hàng" }).click();
  dlg = page.getByRole("dialog", { name: `Thu hồi ${name}` });
  await dlg.getByLabel(/Lý do/).fill("Kết thúc hợp đồng dữ liệu thử nghiệm");
  await dlg.getByRole("button", { name: "Thu hồi" }).click();
  await expect(detail.getByText("Đã thu hồi", { exact: true })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: name })).toContainText("Đã thu hồi");
});

test("G5 W-01: phát hành khóa — bí mật hiện MỘT lần, đóng/tải lại là mất; đăng ký sản phẩm và gói hạn mức qua người duyệt khác", async ({ page }) => {
  await loginAs(page, "api-admin", "/quan-tri/khach-hang-api");
  let detail = await openClient(page, VERIFIER);
  await detail.getByRole("button", { name: "Phát hành khóa mới" }).click();
  const dlg = page.getByRole("dialog", { name: /chỉ hiển thị một lần/ });
  await expect(dlg).toBeVisible();
  const secret = await dlg.getByLabel(/Khóa API/).inputValue();
  expect(secret).toMatch(/^agk_[a-z2-7]{12}_[A-Za-z0-9_-]{43}$/);
  const prefix = secret.split("_")[1]!;
  // Esc và bấm nền không đóng hộp thoại.
  await page.keyboard.press("Escape");
  await expect(dlg).toBeVisible();
  await dlg.getByRole("button", { name: "Tôi đã lưu khóa" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(detail.getByText(`agk_${prefix}_••••`)).toBeVisible();
  expect(await page.content()).not.toContain(secret);
  expect(await storageDump(page)).not.toContain(secret);

  await page.reload();
  await expect(sidebar(page)).toBeVisible();
  detail = await openClient(page, VERIFIER);
  await expect(detail.getByRole("row").filter({ hasText: `agk_${prefix}_••••` })).toContainText("Hiệu lực");
  expect(await page.content()).not.toContain(secret);
  expect(await storageDump(page)).not.toContain(secret);

  // Đăng ký thêm sản phẩm: A đề xuất, không tự duyệt.
  await detail.getByRole("tab", { name: "Gói hạn mức" }).click();
  await expect(detail).toContainText("tạm, chờ khách");
  const planForm = detail.getByRole("form", { name: "Đề xuất phiên bản gói hạn mức" });
  await planForm.getByLabel("Lời gọi/phút").fill("45");
  await planForm.getByRole("button", { name: "Đề xuất phiên bản gói" }).click();
  const v2 = detail.locator('tr[data-plan-version="2"]');
  await expect(v2).toContainText("Chờ duyệt");
  await expect(v2.getByRole("button", { name: "Phê duyệt v2" })).toBeDisabled();
  await expect(v2.getByTestId("sod-hint")).toHaveText("Người duyệt phải khác người đề xuất (R-56).");

  await logout(page);
  await loginViaUi(page, B);
  await navTo(page, "Khách hàng Public API");
  detail = await openClient(page, VERIFIER);
  // B không phát hành khóa (cần api_client_admin), và không thấy bí mật nào.
  await expect(detail.getByRole("button", { name: "Phát hành khóa mới" })).toBeDisabled();
  await detail.getByRole("tab", { name: "Gói hạn mức" }).click();
  await detail.locator('tr[data-plan-version="2"]').getByRole("button", { name: "Phê duyệt v2" }).click();
  await expect(detail.locator('tr[data-plan-version="2"]')).toContainText("Đã duyệt");
  await expect(detail.locator('tr[data-plan-version="2"]')).toContainText("đang áp dụng");
});

test("G5 W-01: xoay khóa (khóa cũ chồng lấn 7 ngày), tối đa 2 khóa hiệu lực, thu hồi khóa có lý do; duyệt đăng ký sản phẩm do người khác đề xuất", async ({ page }) => {
  await loginAs(page, "api-admin", "/quan-tri/khach-hang-api");
  const detail = await openClient(page, FACTORY);
  const oldRow = detail.locator("tr[data-key-prefix]").first();
  const oldPrefix = (await oldRow.getAttribute("data-key-prefix"))!;
  await detail.getByRole("button", { name: `Xoay khóa ${oldPrefix}` }).click();
  const dlg = page.getByRole("dialog", { name: /chỉ hiển thị một lần/ });
  await expect(dlg).toContainText("Khóa cũ còn dùng được tới");
  const secret = await dlg.getByLabel(/Khóa API/).inputValue();
  const newPrefix = secret.split("_")[1]!;
  await dlg.getByRole("button", { name: "Tôi đã lưu khóa" }).click();
  await expect(detail.locator(`tr[data-key-prefix="${oldPrefix}"]`)).toContainText("Đã xoay (còn chồng lấn)");
  await expect(detail.locator(`tr[data-key-prefix="${newPrefix}"]`)).toContainText("Hiệu lực");
  // Khóa cũ (chồng lấn) + khóa mới = 2 khóa hiệu lực → không phát hành thêm.
  await expect(detail.getByRole("button", { name: "Phát hành khóa mới" })).toBeDisabled();
  await expect(detail).toContainText("Đã có tối đa 2 khóa hiệu lực");

  await detail.getByRole("button", { name: `Thu hồi khóa ${oldPrefix}` }).click();
  const rd = page.getByRole("dialog", { name: `Thu hồi khóa ${oldPrefix}` });
  await rd.getByLabel(/Lý do/).fill("Kết thúc thời gian chồng lấn sớm");
  await rd.getByRole("button", { name: "Thu hồi khóa" }).click();
  await expect(detail.locator(`tr[data-key-prefix="${oldPrefix}"]`)).toContainText("Đã thu hồi");
  await expect(detail.locator(`tr[data-key-prefix="${oldPrefix}"]`)).toContainText("Kết thúc thời gian chồng lấn sớm");
  await expect(detail.getByRole("button", { name: "Phát hành khóa mới" })).toBeEnabled();
  expect(await page.content()).not.toContain(secret);

  // Đăng ký hub_capacity do người C đề xuất → A (có vai duyệt) duyệt được.
  await detail.getByRole("tab", { name: "Đăng ký sản phẩm" }).click();
  const hub = detail.locator('li[data-product="hub_capacity"]');
  await expect(hub).toContainText("Chờ duyệt");
  await hub.getByRole("button", { name: "Phê duyệt" }).click();
  await expect(detail.locator('li[data-product="hub_capacity"]')).toContainText("Hiệu lực");
});

test("G5 W-02: bảng kê — A yêu cầu (202 + job), không tự duyệt; B duyệt, kiểm băm, tải CSV", async ({ page }) => {
  await page.goto("/login");
  await loginViaUi(page, A);
  await navTo(page, "Bảng kê sử dụng");
  const form = page.getByRole("form", { name: "Yêu cầu lập bảng kê" });
  await form.getByLabel("Khách hàng").selectOption({ label: VERIFIER });
  const periodLabel = await form.getByLabel(/Kỳ \(đã kết thúc/).locator("option:checked").textContent();
  await form.getByRole("button", { name: "Yêu cầu lập bảng kê" }).click();
  await expect(page.getByTestId("statement-job")).toContainText("Hoàn tất · 1 bảng kê");
  const detail = page.getByTestId("statement-detail");
  await expect(detail.getByRole("heading")).toHaveText(`Bảng kê ${periodLabel} — ${VERIFIER}`);
  await expect(detail).toContainText("Nháp — chờ duyệt");
  await expect(detail.getByRole("button", { name: "Duyệt bảng kê" })).toBeDisabled();
  await expect(detail.getByTestId("sod-hint")).toHaveText("Người duyệt bảng kê phải khác người yêu cầu lập (R-56).");

  await logout(page);
  await loginViaUi(page, B);
  await navTo(page, "Bảng kê sử dụng");
  await page.getByRole("button", { name: `Xem bảng kê ${periodLabel} — ${VERIFIER}` }).click();
  await detail.getByRole("button", { name: "Duyệt bảng kê" }).click();
  await expect(detail).toContainText("Đã duyệt (đóng băng)");
  await expect(detail.getByRole("button", { name: "Duyệt bảng kê" })).toHaveCount(0);
  await detail.getByRole("button", { name: "Kiểm lại băm từ sổ" }).click();
  await expect(detail.getByTestId("verify-result")).toContainText("Khớp");

  const [download] = await Promise.all([page.waitForEvent("download"), detail.getByRole("button", { name: "Tải CSV" }).click()]);
  expect(download.suggestedFilename()).toMatch(/^bang-ke-\d{4}-\d{2}-[0-9a-f]{8}\.csv$/);
  const csv = readFileSync((await download.path())!, "utf-8");
  const rows = csv.trimEnd().split("\n");
  expect(rows[0]).toMatch(/^statement_id,client_id,client_tenant_id,period,status,product,status_class,calls,units/);
  expect(rows.some((r) => r.includes(",approved,mrv_package_status,2xx,"))).toBe(true);
  expect(rows[rows.length - 1]).toContain(",_total,");
  await expect(detail).toContainText("Lần tải được ghi vào nhật ký xuất");

  // Bảng kê có giá thử nghiệm: tiền dạng vi-VN từ chuỗi thập phân.
  await detail.getByRole("button", { name: "Đóng chi tiết" }).click();
  await page.locator("tr[data-status=approved]").filter({ hasText: FACTORY }).getByRole("button", { name: /Xem bảng kê/ }).click();
  await expect(detail.getByTestId("statement-total")).toHaveText(/^\d{1,3}(\.\d{3})*(,\d{1,2})? ₫$/);
  await expect(detail).toContainText("1.500,5 ₫");
});

test("G5 W-03: nhật ký xuất và cảnh báo bất thường — lọc kênh, bên nhận", async ({ page }) => {
  await loginAs(page, "metering-viewer", "/quan-tri/nhat-ky-xuat");
  const table = page.getByRole("table", { name: "Sổ xuất dữ liệu" });
  await expect(table).toBeVisible();
  await page.getByLabel("Kênh").selectOption({ label: "Van MKG → nền tảng" });
  await expect(table.locator("tbody tr").first()).toHaveAttribute("data-channel", "valve");
  expect(await table.locator('tbody tr:not([data-channel="valve"])').count()).toBe(0);
  await page.getByLabel("Kênh").selectOption({ label: "Mọi kênh" });
  await page.getByLabel(/Bên nhận/).fill("0192a5c0-0000-7000-8000-0000000000c1");
  await expect(table.locator("tbody tr").first()).toHaveAttribute("data-channel", "public_api");
  expect(await table.locator('tbody tr:not([data-channel="public_api"])').count()).toBe(0);
  await expect(table).toContainText("bất thường");
  await expect(page.locator('tr[data-decision="denied"]')).toHaveCount(0);
  await page.getByLabel(/Bên nhận/).fill("");
  await expect(page.locator('tr[data-decision="denied"]').first()).toContainText("vượt hạn mức");

  await page.getByRole("tab", { name: "Cảnh báo bất thường" }).click();
  const list = page.getByRole("list", { name: "Cảnh báo bất thường" });
  await expect(list.getByRole("listitem").first()).toBeVisible();
  expect(await list.getByRole("listitem").count()).toBeGreaterThanOrEqual(2);
  await page.getByLabel("Kênh").selectOption({ label: "Public API" });
  await expect(list.getByRole("listitem")).toHaveCount(1);
  await expect(list).toContainText("× trung vị 28 ngày");
  await page.getByLabel("Kênh").selectOption({ label: "Gói bằng chứng MRV" });
  await expect(list).toContainText("Cưỡng chế");
});

test("G5: phân quyền màn hình — người xem đo lường chỉ đọc; vai khác không thấy; chưa MFA bị Admin API từ chối", async ({ page }) => {
  await loginAs(page, "metering-viewer", "/quan-tri/su-dung-api");
  const nav = sidebar(page);
  for (const l of ["Sử dụng Public API", "Bảng kê sử dụng", "Nhật ký xuất dữ liệu"]) await expect(nav.getByRole("link", { name: l })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Khách hàng Public API" })).toHaveCount(0);
  await expect(page.getByRole("group", { name: "Lời gọi trong kỳ" })).toBeVisible();
  await page.getByRole("tab", { name: "Sổ sử dụng (từng lời gọi)" }).click();
  const events = page.getByRole("table", { name: "Sổ sử dụng" });
  await expect(events.locator("tbody tr")).toHaveCount(50);
  await page.getByRole("button", { name: "Tải thêm" }).click();
  await expect(events.locator("tbody tr")).toHaveCount(100);
  await page.getByRole("tab", { name: "Tham số đo lường" }).click();
  await expect(page.getByRole("table", { name: "Tham số đo lường" })).toContainText("Chưa đặt — chờ khách (Q5-4)");

  await navTo(page, "Bảng kê sử dụng");
  await expect(page.getByRole("table", { name: "Bảng kê sử dụng" })).toBeVisible();
  await expect(page.getByRole("form", { name: "Yêu cầu lập bảng kê" })).toHaveCount(0);
  await page.getByRole("button", { name: /Xem bảng kê/ }).first().click();
  const detail = page.getByTestId("statement-detail");
  await expect(detail.getByRole("button", { name: "Kiểm lại băm từ sổ" })).toBeVisible();
  await expect(detail.getByRole("button", { name: "Tải CSV" })).toHaveCount(0);

  await page.goto("/quan-tri/khach-hang-api");
  await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();

  await loginAs(page, "tinh", "/");
  for (const l of ["Khách hàng Public API", "Sử dụng Public API", "Bảng kê sử dụng", "Nhật ký xuất dữ liệu"]) await expect(sidebar(page).getByRole("link", { name: l })).toHaveCount(0);

  await loginAs(page, "admin-no-mfa", "/quan-tri/nhat-ky-xuat");
  await expect(page.getByTestId("mfa-notice")).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "mfa_required" })).toBeVisible();
});
