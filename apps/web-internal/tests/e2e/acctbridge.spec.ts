import { expect, test } from "@playwright/test";
import { alert, labelled, loginAs, monthVn, navTo, SELF_APPROVAL, sidebar, switchUser } from "./helpers";

// G4 mkg/acctbridge — W-01: bút toán tổng hợp theo kỳ (Σ Nợ = Σ Có, source_hash), duyệt ≠ người dựng (R-56),
// đẩy → kỳ ghi sổ, DLQ → đẩy lại, tải tệp CSV/XML có X-Content-SHA256, ánh xạ tài khoản có phiên bản (đề xuất ≠ duyệt).
const M1 = monthVn(1);
const M3 = monthVn(3);

test.describe("Cầu nối kế toán (mkg/acctbridge)", () => {
  test("đóng kỳ → bút toán dựng tự động cân; người đóng kỳ không tự duyệt; người khác duyệt → đẩy → kỳ ghi sổ", async ({ page }) => {
    await loginAs(page, "ke-toan-truong", "/gia-thanh/ky");
    await page.getByRole("button", { name: `Đóng kỳ ${M1}` }).click();
    await expect(labelled(page, "Job đóng kỳ")).toContainText("Hoàn tất");
    await navTo(page, "Bút toán tổng hợp");
    await page.getByRole("button", { name: `Mở bút toán ${M1} v1` }).click();
    const info = labelled(page, "Thông tin bút toán");
    await expect(info).toContainText("Đã dựng — chờ duyệt");
    await expect(info).toContainText("Cân (kiểm lại ở trình duyệt)");
    await expect(page.getByRole("table", { name: "Dòng bút toán" })).toContainText("622");
    await page.getByRole("button", { name: "Duyệt bút toán" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "ke-toan");
    await page.getByRole("button", { name: `Mở bút toán ${M1} v1` }).click();
    await page.getByRole("button", { name: "Duyệt bút toán" }).click();
    await expect(page.getByText("Đã duyệt bút toán — job đẩy lần 1 (202)")).toBeVisible();
    await expect(labelled(page, "Job cầu nối kế toán")).toContainText("Xong");
    await expect(labelled(page, "Thông tin bút toán")).toContainText("Đã đẩy sang kế toán");
    await navTo(page, "Kỳ giá thành");
    await expect(page.getByRole("table", { name: "Kỳ giá thành" }).getByRole("row", { name: new RegExp(M1) })).toContainText("Đã ghi sổ kế toán");
  });

  test("đẩy lỗi vĩnh viễn (DLQ) → đẩy lại → đã đẩy; tải CSV có băm khớp", async ({ page }) => {
    await loginAs(page, "ke-toan-truong", "/ke-toan/but-toan");
    await page.getByRole("button", { name: `Mở bút toán ${M3} v1` }).click();
    await expect(labelled(page, "Thông tin bút toán")).toContainText("Đẩy lỗi (DLQ)");
    await expect(page.getByRole("list", { name: "Các lần đẩy" }).getByRole("listitem")).toHaveCount(3);
    await page.getByRole("button", { name: "Đẩy lại" }).click();
    await expect(labelled(page, "Job cầu nối kế toán")).toContainText("Xong");
    await expect(labelled(page, "Thông tin bút toán")).toContainText("Đã đẩy sang kế toán");
    await expect(page.getByRole("list", { name: "Các lần đẩy" }).getByRole("listitem")).toHaveCount(4);
    const dl = page.waitForEvent("download");
    await page.getByRole("button", { name: "Tải CSV" }).click();
    expect((await dl).suggestedFilename()).toMatch(new RegExp(`^${M3}_[0-9a-f-]{36}\\.csv$`));
    await expect(labelled(page, "Kết quả tải tệp")).toContainText("Băm khớp nội dung");
    const dx = page.waitForEvent("download");
    await page.getByRole("button", { name: "Tải XML" }).click();
    expect((await dx).suggestedFilename()).toMatch(/\.xml$/);
  });

  test("ánh xạ tài khoản: đề xuất ≠ duyệt (R-56); duyệt v2 → dựng lại bút toán chưa đẩy, bản cũ bị thay", async ({ page }) => {
    await loginAs(page, "ke-toan", "/ke-toan/anh-xa");
    await expect(page.getByText("Bề mặt quản trị · MFA")).toBeVisible();
    await page.getByRole("button", { name: "Soạn từ phiên bản hiện hành" }).click();
    await page.getByLabel("TK Nợ loss").fill("811");
    await page.getByLabel("Lý do đề xuất ánh xạ").fill("Hao hụt bất thường hạch toán chi phí khác theo ý kiến kiểm toán");
    await page.getByRole("button", { name: "Gửi đề xuất ánh xạ" }).click();
    await expect(page.getByText("Đã đề xuất ánh xạ v2 — chờ kế toán trưởng khác duyệt")).toBeVisible();
    await page.getByRole("button", { name: "Duyệt ánh xạ" }).click();
    await expect(alert(page)).toContainText(SELF_APPROVAL);
    await switchUser(page, "ke-toan-truong");
    await page.getByRole("button", { name: "Duyệt ánh xạ" }).click();
    await expect(page.getByText("Đã duyệt ánh xạ v2")).toBeVisible();
    await expect(page.getByRole("table", { name: "Quy tắc ánh xạ hiệu lực" })).toContainText("811");

    await navTo(page, "Bút toán tổng hợp");
    await page.getByRole("button", { name: `Mở bút toán ${M3} v1` }).click();
    await page.getByRole("button", { name: "Dựng lại theo ánh xạ hiện hành" }).click();
    await expect(labelled(page, "Job cầu nối kế toán")).toContainText("Xong");
    await expect(page.getByRole("table", { name: "Bút toán tổng hợp" }).getByRole("row", { name: new RegExp(`${M3} v1`) })).toContainText("Bị thay");
    await expect(page.getByRole("table", { name: "Bút toán tổng hợp" }).getByRole("row", { name: new RegExp(`${M3} v2`) })).toContainText("Đã dựng — chờ duyệt");
  });

  test("phân quyền: tài xế không vào được kế toán; kế toán thấy khối kế toán", async ({ page }) => {
    await loginAs(page, "tai-xe", "/ke-toan/but-toan");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    await expect(sidebar(page).getByRole("link", { name: "Ánh xạ tài khoản" })).toHaveCount(0);
  });
});
