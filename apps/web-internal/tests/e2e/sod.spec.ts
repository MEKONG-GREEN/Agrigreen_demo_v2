import { expect, test } from "@playwright/test";
import { alert, IDS, loginAs, switchUser } from "./helpers";

// Phân tách nhiệm vụ (R-56): người đề xuất không tự duyệt — máy chủ trả 403 self_approval_forbidden;
// người KHÁC có vai duyệt thì duyệt được. Đổi người dùng không tải lại trang để giữ trạng thái giả lập.
const SELF = /Không được tự phê duyệt/;

test.describe("Phân tách nhiệm vụ (R-56)", () => {
  test("phiên bản cơ sở: tự duyệt bị chặn, người khác duyệt → xuất van", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/co-so");
    await page.getByRole("button", { name: "Xem phiên bản HUB-TB" }).click();
    await expect(page.getByText("Bạn là người đề xuất — cần một người phê duyệt khác (R-56)")).toBeVisible();
    await page.getByRole("button", { name: "Duyệt phiên bản 2" }).click();
    await expect(alert(page)).toContainText(SELF);
    await expect(alert(page)).toContainText("self_approval_forbidden");

    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "Xem phiên bản HUB-TB" }).click();
    await page.getByRole("button", { name: "Duyệt phiên bản 2" }).click();
    await expect(page.getByText(/Đã duyệt phiên bản 2 — xuất van hub_facility \+ hub_capacity \(2 bản ghi\)/)).toBeVisible();
    await expect(page.getByRole("row", { name: /HUB-TB/ })).toContainText("600,0 t");
  });

  test("đề xuất cơ sở mới rồi người khác duyệt", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/co-so");
    await page.getByRole("button", { name: "Đề xuất cơ sở mới" }).click();
    await page.getByLabel("Mã cơ sở").fill("KHO-VL");
    await page.getByLabel("Tên cơ sở").fill("Kho Vĩnh Long");
    await page.getByLabel("Loại cơ sở").selectOption("warehouse");
    await page.getByLabel("Mã tỉnh").fill("86");
    await page.getByLabel("Vĩ độ (WGS84)").fill("10.25");
    await page.getByLabel("Kinh độ (WGS84)").fill("105.97");
    await page.getByLabel("Sức chứa (kg)").fill("200000");
    await page.getByRole("button", { name: "Gửi đề xuất" }).click();
    await expect(page.getByText(/Đã gửi đề xuất cơ sở KHO-VL/)).toBeVisible();
    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "KHO-VL (chờ duyệt)" }).click();
    await page.getByRole("button", { name: "Duyệt phiên bản 1" }).click();
    await expect(page.getByText(/Đã duyệt phiên bản 1/)).toBeVisible();
    await expect(page.getByRole("row", { name: /KHO-VL/ }).first()).toContainText("Kho Vĩnh Long");
  });

  test("tham số hub.risk.v2: đề xuất, tự duyệt bị chặn, người khác duyệt → #2 áp dụng", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/tham-so");
    await page.getByLabel("Trọng số tuổi").fill("45");
    await expect(alert(page)).toContainText("bằng 100");
    await page.getByLabel("Trọng số ẩm").fill("30");
    await page.getByLabel("Lý do đề xuất").fill("Tăng ưu tiên xuất lô tồn lâu mùa mưa");
    await page.getByRole("button", { name: "Gửi đề xuất tham số" }).click();
    await expect(page.getByText("Đã đề xuất phiên bản 2")).toBeVisible();
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(alert(page)).toContainText(SELF);
    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(page.getByText(/Đã duyệt phiên bản 2/)).toBeVisible();
    await expect(page.getByText("hub.risk.v2#2").first()).toBeVisible();
  });

  test("kiểm kê: người đếm không tự duyệt; người khác duyệt → điều chỉnh", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/hub/kiem-ke");
    await page.getByLabel("Đếm được — Rơm cuộn (kg)").fill("64000");
    await page.getByLabel("Đếm được — Rơm đã sấy (kg)").fill("30500");
    await page.getByLabel("Đếm được — Kiện nén (kg)").fill("12000");
    await page.getByRole("button", { name: "Gửi phiên kiểm kê" }).click();
    await expect(page.getByText(/Đã ghi phiên kiểm kê — chênh -200 kg/)).toBeVisible();
    await page.getByRole("button", { name: "Duyệt kiểm kê" }).click();
    await expect(alert(page)).toContainText(SELF);
    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "Duyệt kiểm kê" }).click();
    await expect(page.getByText("Đã duyệt kiểm kê — 1 dịch chuyển điều chỉnh")).toBeVisible();
  });

  test("thiết bị cân: đăng ký → tự duyệt bị chặn → người khác duyệt; thu hồi không cần người thứ hai", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/can/thiet-bi");
    await page.getByLabel("Mã thiết bị").fill("SCALE-LV-IN-01");
    await page.getByLabel("Cơ sở").selectOption({ label: "BAI-LV · Bãi tập kết Lấp Vò" });
    await page.getByLabel("Khóa công khai Ed25519 (base64)").fill(`${"A".repeat(43)}=`);
    await page.getByLabel("SHA-256 chứng thư mTLS").fill("b".repeat(64));
    await page.getByLabel("Mẫu cân").fill("Cân sàn 60 t (tạm)");
    await page.getByRole("button", { name: "Đề xuất đăng ký" }).click();
    await expect(page.getByText("Đã đề xuất SCALE-LV-IN-01 kỳ 1 — chờ device_approver khác duyệt")).toBeVisible();
    await page.getByRole("button", { name: "Duyệt SCALE-LV-IN-01 kỳ 1" }).click();
    await expect(alert(page)).toContainText(SELF);

    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "Duyệt SCALE-LV-IN-01 kỳ 1" }).click();
    await expect(page.getByText(/Đã duyệt SCALE-LV-IN-01 kỳ 1/)).toBeVisible();
    await page.getByLabel("Lý do (từ chối / thu hồi)").fill("Nghi lộ khóa ký nguồn, thay kỳ mới");
    await page.getByRole("button", { name: "Thu hồi SCALE-TB-OUT-01 kỳ 1" }).click();
    await expect(page.getByText(/Đã thu hồi SCALE-TB-OUT-01 kỳ 1/)).toBeVisible();
  });

  test("dung sai C-4: đề xuất, tự duyệt bị chặn, người khác duyệt", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/can/tham-so");
    await page.getByLabel("Thương mại — cờ (%)").fill("3");
    await page.getByLabel("Lý do đề xuất").fill("Khách đề nghị siết dung sai thương mại");
    await page.getByRole("button", { name: "Gửi đề xuất" }).click();
    await expect(page.getByText("Đã đề xuất phiên bản 2")).toBeVisible();
    await page.getByRole("button", { name: "Duyệt phiên bản 2" }).click();
    await expect(alert(page)).toContainText(SELF);
    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "Duyệt phiên bản 2" }).click();
    await expect(page.getByText(/Đã duyệt phiên bản 2 — áp theo THỜI ĐIỂM CÂN/)).toBeVisible();
    await page.getByRole("tab", { name: "Định mức hao hụt sấy" }).click();
    await expect(page.getByRole("table", { name: "Phiên bản định mức hao hụt" })).toContainText("[15.00–20.00) 3.00 %");
  });

  test("lệnh gom: người lập không tự duyệt; người khác duyệt → giao tổ → đóng", async ({ page }) => {
    await loginAs(page, "dieu-phoi", "/gom/lenh-gom");
    await page.getByRole("button", { name: "Mở lệnh …000004" }).click();
    await page.getByRole("button", { name: "Duyệt lệnh gom" }).click();
    await expect(alert(page)).toContainText(SELF);
    await switchUser(page, "phe-duyet");
    await page.getByRole("button", { name: "Mở lệnh …000004" }).click();
    await page.getByRole("button", { name: "Duyệt lệnh gom" }).click();
    await expect(page.getByText("Đã duyệt lệnh gom — công bố collection_schedule")).toBeVisible();
    await page.getByLabel("Mã tổ gom").fill(IDS.crew);
    await page.getByRole("button", { name: "Giao tổ gom" }).click();
    await expect(page.getByText("Đã giao tổ gom", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Đóng lệnh gom" }).click();
    await expect(page.getByText("Đã đóng lệnh gom", { exact: true })).toBeVisible();
  });
});
