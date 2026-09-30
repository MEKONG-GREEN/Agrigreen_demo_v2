import { expect, test } from "@playwright/test";
import { alert, IDS, loginAs, navTo } from "./helpers";

test.describe("Cân & đối soát", () => {
  test("phiếu cân ba điểm của lô prototype 96,8 t; phiếu bù CUSTOMER_IN → đối soát cùng giao dịch", async ({ page }) => {
    await loginAs(page, "can", "/can/phieu-can");
    await page.getByRole("button", { name: "LO-TB-0921" }).click();
    await expect(page.getByRole("group", { name: "HUB_IN" })).toContainText("96.800 kg");
    await expect(page.getByRole("group", { name: "CUSTOMER_IN" })).toContainText("93.520 kg");

    await page.getByRole("button", { name: "LO-TB-0918" }).click();
    await expect(page.getByRole("group", { name: "CUSTOMER_IN" })).toContainText("82.900 kg");
    await page.getByLabel("Khối lượng tịnh (kg)").fill("87500");
    await page.getByLabel("Mã tệp bằng chứng").fill("0192f0ff-0000-7000-8000-00000000e001");
    await page.getByLabel("Lý do nhập tay").fill("Nhà máy cân lại theo biên bản hai bên");
    await expect(page.getByRole("button", { name: "Ghi phiếu bù" })).toBeDisabled();
    await page.getByLabel("Lý do bù phiếu").fill("Phiếu cũ ghi nhầm số xe của nhà máy");
    await page.getByRole("button", { name: "Ghi phiếu bù" }).click();
    await expect(page.getByText(/Đã ghi phiếu CUSTOMER_IN 87\.500 kg — lô đủ phiếu, đã đối soát/)).toBeVisible();
    await expect(page.getByLabel("Kết quả đối soát")).toContainText("Trong dung sai");
  });

  test("đối soát: ghe 4 400 cuộn cân 96,8 t bị cờ +10 % so với ước 20 kg/cuộn (theo tấn, RB-6)", async ({ page }) => {
    await loginAs(page, "can", "/can/doi-soat");
    await page.getByRole("button", { name: "LO-TB-0921" }).click();
    const roll = page.getByLabel("Cuộn và khối lượng");
    await expect(roll).toContainText("Gắn cờ");
    await expect(roll).toContainText("+10,00 %");
    await expect(roll).toContainText("88.000 kg");
    await expect(page.getByLabel("Hao hụt nội bộ")).toContainText("Bình thường");
    await page.getByRole("button", { name: "Đối soát lại" }).click();
    await expect(page.getByText("Đầu vào không đổi — giữ kết quả đối soát trước")).toBeVisible();
    // Lô theo cuộn: kiểm cuộn chỉ tham khảo; lô hao hụt bất thường: cảnh báo.
    await page.getByRole("button", { name: "LO-CT-0905" }).click();
    await expect(page.getByLabel("Cuộn và khối lượng")).toContainText("Chỉ tham khảo (theo cuộn)");
    await page.getByRole("button", { name: "LO-TB-0908" }).click();
    await expect(page.getByLabel("Hao hụt nội bộ")).toContainText("Hao hụt bất thường");
  });

  test("tranh chấp: đang mở → xem xét → giải quyết, có lịch sử", async ({ page }) => {
    await loginAs(page, "can", "/can/tranh-chap");
    await page.getByRole("button", { name: "Mở hồ sơ LO-TB-0918" }).click();
    await expect(page.getByText(/-5,90 %/).first()).toBeVisible();
    await page.getByRole("button", { name: "Bắt đầu xem xét" }).click();
    await expect(page.getByText("Bắt đầu xem xét — trạng thái Đang xem xét")).toBeVisible();
    await page.getByLabel("Cách giải quyết").selectOption("agreed_weight");
    await page.getByLabel("Khối lượng thoả thuận (kg)").fill("85500");
    await page.getByLabel("Ghi chú").fill("Hai bên thống nhất lấy trung bình sau kiểm camera");
    await page.getByRole("button", { name: "Giải quyết hồ sơ" }).click();
    await expect(page.getByText("Giải quyết — trạng thái Đã giải quyết")).toBeVisible();
    await expect(page.getByRole("list", { name: "Lịch sử hồ sơ" })).toContainText("Giải quyết");
  });

  test("nhập số cân khách: dòng sai chặn cả lần; hợp lệ → phiếu CUSTOMER_IN + đối soát tự động", async ({ page }) => {
    await loginAs(page, "can", "/can/nhap-so-khach");
    await page.getByLabel("Mã tệp bằng chứng").fill("0192f0ff-0000-7000-8000-00000000e002");
    await page.getByLabel("Bảng số cân (CSV)").fill(`lot_ref,mass_kg,weighed_at,route\n${IDS.orderApproved},abc,2026-09-28T08:00:00+07:00,DIRECT`);
    await expect(alert(page)).toContainText("Dòng 2: mass_kg không hợp lệ");
    await expect(page.getByRole("button", { name: "Nhập số cân" })).toBeDisabled();
    await page.getByLabel("Bảng số cân (CSV)").fill(`lot_ref,mass_kg,weighed_at,route,customer_ticket_no\n${IDS.orderApproved},41250.5,2026-09-28T08:00:00+07:00,DIRECT,PC-7781`);
    await expect(page.getByText("1 dòng hợp lệ")).toBeVisible();
    await page.getByRole("button", { name: "Nhập số cân" }).click();
    await expect(page.getByText(/Đã nhập 1 dòng → 1 phiếu CUSTOMER_IN, 1 lô đã đối soát tự động/)).toBeVisible();
    await expect(page.getByRole("table", { name: "Đối soát sau khi nhập" })).toContainText("Giao thẳng — gộp");
  });

  test("phiên cân: mở, trùng thiết bị bị chặn, đóng → phiếu cân", async ({ page }) => {
    await loginAs(page, "can", "/can/phien-can");
    await page.getByLabel("Thiết bị cân (kỳ đang hiệu lực)").selectOption({ label: "SCALE-TB-OUT-01 · kỳ 1 · HUB_OUT" });
    await page.getByLabel("Bộ đếm bắt đầu").fill("1000");
    await page.getByLabel("Mã lô cân", { exact: true }).fill(IDS.lotArriving);
    await page.getByRole("button", { name: "Mở phiên cân" }).click();
    await expect(alert(page)).toContainText("measurement.session_conflict");
    await page.getByLabel("Thiết bị cân (kỳ đang hiệu lực)").selectOption({ label: "SCALE-TB-IN-01 · kỳ 1 · HUB_IN" });
    await page.getByRole("button", { name: "Mở phiên cân" }).click();
    await expect(page.getByText(/Đã mở phiên cân HUB_IN trên SCALE-TB-IN-01 từ bộ đếm 1000/)).toBeVisible();
    await page.getByLabel("Bộ đếm kết thúc").fill("1064");
    await page.getByLabel("Bì (kg)").fill("200");
    await page.getByRole("button", { name: "Đóng phiên cân" }).click();
    await expect(page.getByText(/Đã đóng phiên cân — ingest tích phân cửa sổ/)).toBeVisible();
    await expect(page.getByText("1000 → 1064")).toBeVisible();
  });

  test("sức khoẻ thiết bị: mất tín hiệu, đệm, lệch đồng hồ", async ({ page }) => {
    await loginAs(page, "can", "/can/suc-khoe");
    const t = page.getByRole("table", { name: "Sức khoẻ thiết bị cân" });
    await expect(t.getByRole("row", { name: /SCALE-TB-IN-01/ })).toContainText("Bình thường");
    const out = t.getByRole("row", { name: /SCALE-TB-OUT-01/ });
    await expect(out).toContainText("Sự cố");
    await expect(out).toContainText("Mất tín hiệu");
    await expect(out).toContainText("Lệch đồng hồ 2400 ms");
    await navTo(page, "Sổ thiết bị cân");
    await expect(page.getByRole("table", { name: "Kỳ đăng ký thiết bị" })).toContainText("Đã thu hồi");
  });
});
