import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";
import { coop, household } from "./htx-data";

// Mock: nông hộ i % 5 ∈ {0, 1} đã có tài khoản; hộ 2, 3 chưa có. SĐT tổng hợp 09990xxxxx.
const hasAccount = household(0);
const free = household(2);
const free2 = household(3);
const pendingActivation = household(1); // có tài khoản, lượt kích hoạt đang chờ

test("HTX US-ACC-01: tạo tài khoản nông dân → QR + liên kết một lần kèm cảnh báo không chia sẻ", async ({ page }) => {
  await loginAs(page, "htx", "/htx/tai-khoan-nong-dan");
  await page.getByRole("button", { name: "Tạo tài khoản" }).click();
  const form = page.getByRole("form", { name: "Tạo tài khoản nông dân" });

  await form.getByLabel("SĐT chủ hộ").fill("12345");
  await form.getByRole("button", { name: "Tạo tài khoản & phát QR" }).click();
  await expect(form).toContainText("SĐT phải có dạng 0xxxxxxxxx");

  // SĐT không phải của chủ hộ đã chọn.
  await form.getByLabel("Nông hộ").selectOption(free.ref);
  await form.getByLabel("SĐT chủ hộ").fill(free2.phone);
  await form.getByRole("button", { name: "Tạo tài khoản & phát QR" }).click();
  await expect(form.getByRole("alert")).toContainText("SĐT không phải SĐT chủ hộ");

  // Hộ đã có tài khoản.
  await form.getByLabel("Nông hộ").selectOption("");
  await form.getByLabel("SĐT chủ hộ").fill(hasAccount.phone);
  await form.getByRole("button", { name: "Tạo tài khoản & phát QR" }).click();
  await expect(form.getByRole("alert")).toContainText("đã có tài khoản nông dân");

  await form.getByLabel("Nông hộ").selectOption(free.ref);
  await form.getByLabel("SĐT chủ hộ").fill(`+84${free.phone.slice(1)}`);
  await form.getByRole("button", { name: "Tạo tài khoản & phát QR" }).click();
  const card = page.getByRole("region", { name: /Kích hoạt tài khoản/ });
  await expect(card.getByRole("alert")).toContainText("Chỉ hiển thị một lần — không chia sẻ cho người khác.");
  await expect(card.getByRole("img", { name: /Mã QR kích hoạt tài khoản/ })).toBeVisible();
  await expect(card.getByLabel("Liên kết kích hoạt")).toHaveValue(/action-token/);
  await card.getByRole("button", { name: "Đã giao cho nông dân — đóng" }).click();
  await expect(card).toHaveCount(0);
  await expect(page.getByLabel("Liên kết kích hoạt")).toHaveCount(0); // không còn liên kết ở đâu trên trang
});

test("phát lại QR khi nông dân mất liên kết — lượt cũ bị thu hồi", async ({ page }) => {
  await loginAs(page, "htx", "/htx/tai-khoan-nong-dan");
  await page.getByRole("button", { name: `Phát lại QR ${pendingActivation.label}` }).first().click();
  await expect(page.getByRole("region", { name: /Kích hoạt tài khoản/ }).getByRole("img", { name: /Mã QR/ })).toBeVisible();
  await page.getByRole("button", { name: `Lượt kích hoạt ${pendingActivation.label}` }).first().click();
  const dlg = page.getByRole("dialog");
  await expect(dlg).toContainText("Chờ kích hoạt");
  await expect(dlg).toContainText("thu hồi do phát lượt mới");
});

test("HTX US-ACC-02: nhập tài khoản hàng loạt — kết quả theo dòng, dòng hợp lệ có QR", async ({ page }) => {
  await loginAs(page, "htx", "/htx/tai-khoan-nong-dan");
  await page.getByRole("button", { name: "Nhập từ tệp" }).click();
  const form = page.getByRole("form", { name: "Nhập tài khoản nông dân từ tệp" });
  const csv = ["SĐT", free2.phone, hasAccount.phone, "0123"].join("\n");
  await form.getByLabel("Tệp tài khoản nông dân").setInputFiles({ name: "tai-khoan.csv", mimeType: "text/csv", buffer: Buffer.from(csv, "utf-8") });
  await form.getByRole("button", { name: "Nhập" }).click();
  await expect(page.getByRole("status").filter({ hasText: "tài khoản đã tạo" })).toContainText("1 tài khoản đã tạo · 2 dòng lỗi");
  const table = page.getByRole("table", { name: "Kết quả nhập tài khoản nông dân" });
  await expect(table.getByRole("row").filter({ hasText: "Dòng 3" })).toContainText("đã có tài khoản");
  await expect(table.getByRole("row").filter({ hasText: "Dòng 4" })).toContainText("SĐT không đúng định dạng");
  await table.getByRole("button", { name: "Xem QR dòng 2" }).click();
  await expect(page.getByRole("region", { name: /Kích hoạt tài khoản/ }).getByRole("img", { name: /Mã QR/ })).toBeVisible();
});

test("cán bộ KN xem tài khoản nông dân của HTX trong xã, không tạo/phát lại được", async ({ page }) => {
  await loginAs(page, "xa", "/htx/tai-khoan-nong-dan");
  await expect(page.getByText("Chọn một HTX để xem tài khoản nông dân.")).toBeVisible();
  await page.getByLabel("HTX", { exact: true }).selectOption(coop.api.ref);
  await expect(page.getByRole("table", { name: "Tài khoản nông dân" }).getByRole("row").nth(1)).toBeVisible();
  await expect(page.getByRole("button", { name: "Tạo tài khoản" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Phát lại QR/ })).toHaveCount(0);
});
