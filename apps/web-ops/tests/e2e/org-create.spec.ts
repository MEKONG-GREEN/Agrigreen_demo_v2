import { expect, test, type Page } from "@playwright/test";
import { loginAs } from "./helpers";

// ORGUI — hộp thoại "Tạo tổ chức" trên /quan-tri/to-chuc (Admin POST /v1/orgs). Tiêu chí: docs/uat/to-chuc-acceptance.md.

async function openDialog(page: Page) {
  await loginAs(page, "setter", "/quan-tri/to-chuc");
  await expect(page.getByRole("tree", { name: "Cây tổ chức" })).toBeVisible();
  await page.getByRole("button", { name: "Tạo tổ chức" }).click();
  const dlg = page.getByRole("dialog", { name: "Tạo tổ chức" });
  await expect(dlg).toBeVisible();
  return dlg;
}

test("ORGUI: tạo Tổ KN xã → mã trùng báo trên ô → sửa và gửi lại → hiện trong cây, được chọn → đổi tên", async ({ page }) => {
  const dlg = await openDialog(page);
  await dlg.getByLabel("Loại tổ chức").selectOption("extension_team");
  // Kiểm phía client: mã sai dạng.
  await dlg.getByLabel("Mã tổ chức").fill("mã có dấu");
  await dlg.getByLabel("Tên tổ chức").fill("Tổ khuyến nông E2E");
  await dlg.getByRole("button", { name: "Tạo", exact: true }).click();
  await expect(dlg.getByText("Mã 1–40 ký tự, chỉ gồm chữ không dấu, số và . _ -")).toBeVisible();
  // Cấp trên: chỉ TTKN tỉnh (không có TTKN Quốc gia); xã lọc theo tỉnh của cấp trên.
  const parent = dlg.getByLabel("Tổ chức cấp trên");
  await expect(parent.locator("option", { hasText: "Trung tâm Khuyến nông Quốc gia" })).toHaveCount(0);
  await parent.selectOption({ label: "TTKN tỉnh Đồng Tháp (TTKN-82)" });
  const unit = dlg.getByLabel("Xã/phường (ĐVHC)");
  await expect(unit.locator("option", { hasText: "(82101)" })).toHaveCount(1);
  await expect(unit.locator("option", { hasText: "(91101)" })).toHaveCount(0);
  await unit.selectOption("82101");
  // 409: mã đã dùng → lỗi trên ô Mã.
  await dlg.getByLabel("Mã tổ chức").fill("TTKNQG");
  await dlg.getByRole("button", { name: "Tạo", exact: true }).click();
  await expect(dlg.getByText(/Mã tổ chức đã được dùng/)).toBeVisible();
  await expect(dlg.getByLabel("Mã tổ chức")).toHaveAttribute("aria-invalid", "true");
  // Sửa mã, gửi lại (cùng Idempotency-Key) → tạo được.
  await dlg.getByLabel("Mã tổ chức").fill("TKN-E2E-1");
  await dlg.getByRole("button", { name: "Tạo", exact: true }).click();
  await expect(dlg).toBeHidden();

  const tree = page.getByRole("tree", { name: "Cây tổ chức" });
  // Nút mới hiện trong cây (cấp trên tự mở) và đang được chọn.
  await expect(tree.getByRole("button", { name: "Chi tiết Tổ khuyến nông E2E", exact: true })).toBeVisible();
  await expect(tree.locator('[role="treeitem"][aria-selected="true"]')).toHaveCount(1);
  await expect(tree.locator('[role="treeitem"][aria-selected="true"] > .node')).toContainText("TKN-E2E-1");
  const panel = page.getByRole("complementary", { name: "Chi tiết tổ chức" });
  await expect(panel.getByRole("heading", { name: "Tổ khuyến nông E2E" })).toBeVisible();
  await expect(panel.getByText("Tạo mới")).toBeVisible();

  await panel.getByLabel("Tên mới").fill("Tổ khuyến nông E2E (đổi tên)");
  await panel.getByRole("button", { name: "Đổi tên" }).click();
  await expect(tree.getByRole("button", { name: "Chi tiết Tổ khuyến nông E2E (đổi tên)" })).toBeVisible();
  await expect(panel.getByText("Đổi tên", { exact: true }).first()).toBeVisible();
});

test("ORGUI: sai cấp trên bị chặn trước khi gửi; lỗi 422 của server hiện trên ô", async ({ page }) => {
  const dlg = await openDialog(page);
  await dlg.getByLabel("Loại tổ chức").selectOption("extension_team");
  await dlg.getByLabel("Mã tổ chức").fill("TKN-E2E-2");
  await dlg.getByLabel("Tên tổ chức").fill("Tổ không cấp trên");
  await expect(dlg.getByLabel("Xã/phường (ĐVHC)")).toBeDisabled();
  await dlg.getByRole("button", { name: "Tạo", exact: true }).click();
  await expect(dlg.getByText("Chọn tổ chức cấp trên.")).toBeVisible();
  await expect(dlg.getByLabel("Tổ chức cấp trên")).toHaveAttribute("aria-invalid", "true");

  // Đối tác: mã ĐVHC tự nhập không còn hiệu lực → server 422 validation_failed trên ô ĐVHC.
  await dlg.getByLabel("Loại tổ chức").selectOption("partner");
  await expect(dlg.getByLabel("Tổ chức cấp trên")).toHaveCount(0);
  await dlg.getByLabel("Mã tổ chức").fill("DT-E2E-1");
  await dlg.getByLabel("Tên tổ chức").fill("Đối tác E2E");
  await dlg.getByLabel("Mã ĐVHC (tuỳ chọn, tỉnh hoặc xã)").fill("99999");
  await dlg.getByRole("button", { name: "Tạo", exact: true }).click();
  await expect(dlg.getByText("không có đơn vị hành chính còn hiệu lực với mã này")).toBeVisible();
  await expect(dlg).toBeVisible();
});

test("ORGUI: HTX không tạo trong hộp thoại — dẫn sang Neo HTX theo mã số thuế", async ({ page }) => {
  const dlg = await openDialog(page);
  await dlg.getByLabel("Loại tổ chức").selectOption("cooperative");
  await expect(dlg.getByTestId("org-create-coop-redirect")).toBeVisible();
  await expect(dlg.getByLabel("Mã tổ chức")).toHaveCount(0);
  await expect(dlg.getByRole("button", { name: "Tạo", exact: true })).toHaveCount(0);
  await dlg.getByRole("link", { name: "Mở Neo HTX theo mã số thuế" }).click();
  await expect(page).toHaveURL(/\/quan-tri\/neo-htx$/);
  await expect(page.getByRole("heading", { name: "Neo HTX theo mã số thuế" })).toBeVisible();
});

test("ORGUI: vai không có quyền (quản trị KN tỉnh) không mở được trang và không thấy nút Tạo tổ chức", async ({ page }) => {
  await loginAs(page, "tinh", "/quan-tri/to-chuc");
  await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Tạo tổ chức" })).toHaveCount(0);
});
