import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { plot } from "./htx-data";

const selfLogged = plot(1); // nhật ký chờ duyệt do chính Quản lý HTX ghi hộ
const mismatch = plot(2); // nhật ký chờ duyệt có vị trí không khớp
const ready = plot(3);

const item = (page: import("@playwright/test").Page, code: string) => page.getByRole("list", { name: "Nhật ký chờ duyệt" }).getByRole("listitem", { name: new RegExp(`^${code} — Bón phân`) });

test("HTX US-LOG-05 AC-1/2: nhãn cảnh báo vị trí hiện rõ; phê duyệt chuyển Đã duyệt và rời hàng chờ", async ({ page }) => {
  await loginAs(page, "htx", "/htx/phe-duyet");
  const li = item(page, mismatch.code);
  await expect(li).toContainText("Vị trí không khớp");
  await expect(li.getByRole("note")).toContainText("cách ranh giới thửa");
  await li.getByRole("button", { name: "Phê duyệt" }).click();
  await expect(page.getByRole("status").filter({ hasText: `Đã phê duyệt nhật ký lô ${mismatch.code}` })).toBeVisible();
  await expect(li).toHaveCount(0);
});

test("người ghi nhật ký không được tự duyệt (cropcycle.self_review)", async ({ page }) => {
  await loginAs(page, "htx", "/htx/phe-duyet");
  const li = item(page, selfLogged.code);
  await expect(li).toContainText("Quản lý HTX ghi hộ");
  await li.getByRole("button", { name: "Phê duyệt" }).click();
  await expect(li.getByRole("alert")).toContainText("không được tự duyệt");
  await expect(li.getByRole("alert")).toContainText("cropcycle.self_review");
});

test("HTX US-LOG-05 AC-3: yêu cầu bổ sung bắt buộc ghi nội dung", async ({ page }) => {
  await loginAs(page, "htx", "/htx/phe-duyet");
  const li = item(page, ready.code);
  await li.getByRole("button", { name: "Yêu cầu bổ sung" }).click();
  const dlg = page.getByRole("dialog", { name: new RegExp(`Yêu cầu bổ sung — lô ${ready.code}`) });
  await dlg.getByRole("button", { name: "Gửi yêu cầu bổ sung" }).click();
  await expect(dlg).toContainText("Bắt buộc nhập lý do.");
  await dlg.getByLabel(/Nội dung cần bổ sung/).fill("Chụp thêm ảnh bao phân đã bón");
  await dlg.getByRole("button", { name: "Gửi yêu cầu bổ sung" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(page.getByRole("status").filter({ hasText: `Đã yêu cầu bổ sung nhật ký lô ${ready.code}` })).toBeVisible();
  await expect(li).toHaveCount(0);
});

test("ảnh minh chứng mở qua luồng tệp theo mục đích (X-Purpose)", async ({ page }) => {
  await loginAs(page, "htx", "/htx/phe-duyet");
  const li = item(page, mismatch.code);
  await li.getByRole("button", { name: "Chi tiết" }).click();
  await expect(li).toContainText("Ảnh minh chứng");
  await li.getByRole("button", { name: "Xem ảnh" }).first().click();
  await expect(li.getByRole("img", { name: /Ảnh minh chứng/ }).first()).toBeVisible();
});
