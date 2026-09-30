import { expect, test } from "@playwright/test";
import { alert, labelled, loginAs, monthVn, SELF_APPROVAL, switchUser } from "./helpers";

// G4 mkg/costing — W-01: kỳ + đóng kỳ (202 + job, R-56 người đóng ≠ người điều chỉnh), giá thành lô + điều chỉnh
// (supersedes), phân bổ + tính lại, quyết toán HTX PM-01..04 (theo cuộn / theo tấn chờ cân / không hợp đồng → nhập giá),
// chia carbon 45 % / 70-15-15, tham số có phiên bản ở bề mặt quản trị.
const M1 = monthVn(1);
const M0 = monthVn(0);

test.describe("Giá thành & quyết toán (mkg/costing)", () => {
  test("đóng kỳ: người lập điều chỉnh của kỳ không đóng được; kỳ chưa hết tháng → 409; người khác → 202 + job → đã đóng", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/ky");
    await page.getByRole("button", { name: `Đóng kỳ ${M1}` }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await page.getByRole("button", { name: `Đóng kỳ ${M0}` }).click();
    await expect(alert(page)).toContainText("costing.period_not_ended");
    await switchUser(page, "ke-toan-truong");
    await page.getByRole("button", { name: `Đóng kỳ ${M1}` }).click();
    await expect(page.getByText(`Đã nhận yêu cầu đóng kỳ ${M1} (202) — worker đang tổng hợp`)).toBeVisible();
    await expect(labelled(page, "Job đóng kỳ")).toContainText("Hoàn tất");
    await expect(page.getByRole("table", { name: "Kỳ giá thành" }).getByRole("row", { name: new RegExp(M1) })).toContainText("Đã đóng");
    await expect(page.getByRole("table", { name: "Dòng tổng hợp kỳ" })).toContainText("Nhân công gom");
    await expect(labelled(page, "Kết quả đóng kỳ")).toContainText("SHA-256 kết quả");
    // Kỳ đã đóng không mở lại: không còn nút đóng.
    await expect(page.getByRole("button", { name: `Đóng kỳ ${M1}` })).toHaveCount(0);
  });

  test("giá thành lô: đ/t = tổng / tấn giao; điều chỉnh ghi kỳ đang mở; tính lại phân bổ → kết quả mới", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/lo");
    await page.getByRole("button", { name: "LO-TB-0921" }).click();
    await expect(page.getByRole("group", { name: "Giá thành đ/t" })).toContainText("307.357,95 ₫");
    await expect(page.getByRole("group", { name: "Tổng chi phí" })).toContainText("29.752.250 ₫");
    await page.getByRole("button", { name: "Điều chỉnh bút toán 5" }).click();
    await page.getByLabel("Số tiền mới (VND)").fill("2500000");
    await page.getByLabel("Lý do điều chỉnh").fill("Bốc xếp phát sinh thêm ca đêm");
    await page.getByRole("button", { name: "Ghi điều chỉnh" }).click();
    await expect(page.getByText(`Đã ghi bút toán điều chỉnh ở kỳ ${M0} (Δ 80.000 ₫) — dòng cũ giữ nguyên`)).toBeVisible();
    await expect(page.getByRole("table", { name: "Bút toán giá thành" })).toContainText("Bị thay");
    await page.getByRole("button", { name: "Xem phân bổ bút toán 2" }).click();
    await page.getByLabel("Lý do tính lại").fill("Cước chuyến có hoá đơn điều chỉnh");
    await page.getByRole("button", { name: "Tính lại phân bổ" }).click();
    await expect(page.getByText("Đã ghi kết quả phân bổ mới (supersedes_id) + bút toán delta")).toBeVisible();
    await expect(page.getByRole("list", { name: "Lịch sử kết quả phân bổ" })).toContainText("bị thay");
  });

  test("lô có giá thành: danh sách mới nhất trước, 5 dòng/trang + Tải thêm", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/lo");
    const t = page.getByRole("table", { name: "Lô có giá thành" });
    await expect(t.getByRole("row")).toHaveCount(6);
    await expect(t.getByRole("row").nth(1)).toContainText("LO-CT-0905");
    await expect(t).not.toContainText("LO-CT-0910");
    await page.getByRole("button", { name: "Tải thêm" }).click();
    await expect(t.getByRole("row")).toHaveCount(7);
    await expect(t).toContainText("LO-CT-0910");
    await expect(page.getByRole("button", { name: "Tải thêm" })).toHaveCount(0);
    await t.getByRole("button", { name: "LO-CT-0910" }).click();
    await expect(page.getByRole("group", { name: "Tổng chi phí" })).toContainText("16.920.000 ₫");
  });

  test("job đóng kỳ: danh sách job đã chạy; đóng kỳ mới → job xuất hiện đầu danh sách", async ({ page }) => {
    await loginAs(page, "ke-toan-truong", "/gia-thanh/ky");
    const jobs = page.getByRole("table", { name: "Job đóng kỳ đã chạy" });
    await expect(jobs.getByRole("row")).toHaveCount(3);
    await expect(jobs).toContainText(monthVn(2));
    await expect(jobs).toContainText(monthVn(3));
    await page.getByRole("button", { name: `Đóng kỳ ${M1}` }).click();
    await expect(labelled(page, "Job đóng kỳ")).toContainText("Hoàn tất");
    await expect(jobs.getByRole("row").nth(1)).toContainText(M1);
    await expect(jobs.getByRole("row").nth(1)).toContainText("Hoàn tất");
    await page.getByLabel("Lọc trạng thái job").selectOption("failed");
    await expect(page.getByText("Chưa có job đóng kỳ.")).toBeVisible();
  });

  test("quyết toán theo cuộn: người lập không tự duyệt; kế toán trưởng khác duyệt → một khoản phải trả; ghi đã trả", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/quyet-toan");
    await page.getByRole("button", { name: "Mở quyết toán …000001" }).click();
    await expect(labelled(page, "Chi tiết quyết toán HTX")).toContainText("66.000.000 ₫");
    await page.getByRole("button", { name: "Duyệt quyết toán" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "ke-toan-truong");
    await page.getByRole("button", { name: "Mở quyết toán …000001" }).click();
    await page.getByRole("button", { name: "Duyệt quyết toán" }).click();
    await expect(page.getByText(/Đã duyệt quyết toán — phải trả 66.000.000 ₫, hạn/)).toBeVisible();
    await expect(labelled(page, "Chi tiết quyết toán HTX")).toContainText("Đã duyệt — phải trả");
    await page.getByRole("button", { name: "Ghi đã trả" }).click();
    await expect(page.getByText("Đã ghi đã trả")).toBeVisible();
  });

  test("không hợp đồng → không có số tiền, nhập giá thoả thuận; theo tấn còn chờ cân → chưa duyệt được", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/quyet-toan");
    await expect(page.getByRole("group", { name: "Không hợp đồng — cần nhập giá" })).toContainText("1");
    await page.getByRole("button", { name: "Mở quyết toán …000003" }).click();
    await expect(labelled(page, "Chi tiết quyết toán HTX")).toContainText("Chưa có giá — hệ thống không đoán giá");
    await page.getByLabel("Giá thoả thuận (VND/tấn)").fill("600000");
    await page.getByLabel("Căn cứ giá").fill("Biên bản họp HTX Giồng Riềng ngày 20/09");
    await page.getByRole("button", { name: "Ghi giá thoả thuận" }).click();
    await expect(page.getByText("Đã nhập giá — phải trả 54.900.000 ₫")).toBeVisible();
    await switchUser(page, "ke-toan-truong");
    await page.getByRole("button", { name: "Mở quyết toán …000002" }).click();
    await expect(labelled(page, "Chi tiết quyết toán HTX")).toContainText("Chờ cân đủ mọi ghe");
    await page.getByRole("button", { name: "Duyệt quyết toán" }).click();
    await expect(alert(page)).toContainText("costing.settlement_not_ready");
  });

  test("chia doanh thu carbon 45 % / 70-15-15 (tạm, chờ khách)", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/carbon");
    await expect(labelled(page, "Tỷ lệ chia carbon")).toContainText("45 %");
    await page.getByLabel("Doanh thu carbon (VND)").fill("100000000");
    await page.getByRole("button", { name: "Ghi và chia" }).click();
    await expect(page.getByText(/Đã ghi doanh thu carbon kỳ .* \(tạm, chờ khách\)/)).toBeVisible();
    const t = page.getByRole("table", { name: "Phần chia doanh thu carbon" });
    await expect(t).toContainText("45.000.000 ₫");
    await expect(t).toContainText("38.500.000 ₫");
    await expect(t.getByRole("row", { name: /VVB/ })).toContainText("8.250.000 ₫");
    // Danh sách khoản đã ghi (GET /v1/costing/carbon-revenue): khoản mới đầu danh sách, 5 dòng/trang.
    const list = page.getByRole("table", { name: "Doanh thu carbon đã ghi" });
    await expect(list.getByRole("row").nth(1)).toContainText("100.000.000 ₫");
    await expect(list.getByRole("row")).toHaveCount(6);
    await page.getByRole("button", { name: "Tải thêm" }).click();
    await expect(list.getByRole("row")).toHaveCount(8);
    await expect(list).toContainText("HĐ-TC-2026-01");
    await page.getByLabel("Lọc theo kỳ").selectOption({ label: monthVn(2) });
    await expect(list.getByRole("row")).toHaveCount(3);
    await expect(list).toContainText("HĐ-TC-2026-02");
  });

  test("tham số giá thành (bề mặt quản trị): kế toán giá thành đề xuất, tự duyệt bị chặn, kế toán trưởng duyệt", async ({ page }) => {
    await loginAs(page, "ke-toan", "/gia-thanh/tham-so");
    await expect(page.getByText("Bề mặt quản trị · MFA")).toBeVisible();
    await page.getByLabel("Khoá tham số").selectOption("costing.loss.expected_pct");
    await expect(labelled(page, "Giá trị đang áp dụng")).toContainText("costing.loss.expected_pct#0");
    await page.getByLabel("Giá trị mới").fill("6");
    await page.getByLabel("Lý do đề xuất").fill("Hao hụt thực đo 3 tháng gần nhất cao hơn");
    await page.getByRole("button", { name: "Gửi đề xuất tham số" }).click();
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "ke-toan-truong");
    await page.getByLabel("Khoá tham số").selectOption("costing.loss.expected_pct");
    await page.getByRole("button", { name: "Duyệt tham số" }).click();
    await expect(labelled(page, "Giá trị đang áp dụng")).toContainText("costing.loss.expected_pct#1");
  });
});
