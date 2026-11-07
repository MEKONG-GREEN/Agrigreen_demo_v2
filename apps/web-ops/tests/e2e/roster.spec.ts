import { expect, test } from "@playwright/test";
import { loginAs, navTo } from "./helpers";

test("danh bạ trực (US-DIR-02): tra theo xã, liên hệ → liên kết gọi/nhắn một chạm; xã không ai trực → cán bộ tỉnh dự phòng", async ({ page }) => {
  await loginAs(page, "setter", "/");
  await navTo(page, "Danh bạ trực");
  await page.getByLabel("Tỉnh").selectOption("82");
  await expect(page.getByRole("heading", { level: 1, name: "Danh bạ trực" })).toBeVisible();

  // Xã đầu tiên của Đồng Tháp (82101): một người tự khai, một người theo ca.
  await page.getByLabel("Xã").selectOption("82101");
  await expect(page.getByTestId("on-duty-count")).toHaveText("2 đang trực");
  const table = page.getByRole("table", { name: "Cán bộ trong địa bàn" });
  const first = table.locator('tr[data-actor="mock-xa-0001"]');
  await expect(first).toContainText("Đang trực");
  await expect(first).toContainText("Tự khai");
  await expect(table.locator('tr[data-actor="mock-knv-82101-2"]')).toContainText("Theo ca");

  // Số điện thoại chỉ đọc khi bấm "Liên hệ" (không nạp sẵn cùng danh bạ).
  await expect(page.getByRole("link", { name: /^Gọi / })).toHaveCount(0);
  await first.getByRole("button", { name: "Liên hệ" }).click();
  const call = first.getByRole("link", { name: /^Gọi 0999\d{6}$/ });
  await expect(call).toHaveAttribute("href", /^tel:0999\d{6}$/);
  await expect(first.getByRole("link", { name: "Nhắn tin" })).toHaveAttribute("href", /^sms:0999\d{6}$/);
  // Cán bộ chưa khai số → báo rõ mã lỗi.
  await table.locator('tr[data-actor="mock-knv-82101-2"]').getByRole("button", { name: "Liên hệ" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "extension.contact_unavailable" })).toBeVisible();

  // Xã 82102: không ai đang trực → hiện cán bộ TTKN tỉnh dự phòng, gọi được.
  await page.getByLabel("Xã").selectOption("82102");
  await expect(page.getByTestId("on-duty-count")).toHaveText("0 đang trực");
  const fallback = page.getByTestId("roster-fallback");
  await expect(fallback).toContainText("liên hệ cán bộ TTKN tỉnh");
  const prov = fallback.locator('tr[data-actor="mock-tinh-0082"]');
  await expect(prov).toContainText("Theo ca");
  await prov.getByRole("button", { name: "Liên hệ" }).click();
  await expect(prov.getByRole("link", { name: /^Gọi / })).toHaveAttribute("href", /^tel:0999\d{6}$/);
});

test("danh bạ trực legacy: KN bị chặn; quản trị nền tảng vẫn khai báo ca trực", async ({ page }) => {
  // S-2 removes this legacy feature from KN; no self-service write UI mounts.
  for (const persona of ["xa", "tinh"] as const) {
    await loginAs(page, persona, "/danh-ba");
    await expect(page.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Tạm nghỉ" })).toHaveCount(0);
  }
  // The platform-owned legacy shift editor remains functional.
  await loginAs(page, "setter", "/danh-ba");
  await page.getByLabel("Tỉnh").selectOption("82");
  await page.getByLabel("Xã").selectOption("82103");
  await expect(page.getByRole("heading", { name: "Khai báo ca trực" })).toBeVisible();
  const row = page.getByRole("table", { name: "Cán bộ trong địa bàn" }).locator('tr[data-actor="mock-knv-82103-2"]');
  await expect(row).toContainText("Không xác định");
  await page.getByLabel("Cán bộ", { exact: true }).selectOption("mock-knv-82103-2");
  // Trình duyệt chạy giờ Việt Nam (playwright.config timezoneId); định dạng theo đúng múi đó,
  // không theo múi của tiến trình Node (CI là UTC) — nếu không, ca trực lệch 7 giờ.
  const fmt = (d: Date) => {
    const p = Object.fromEntries(
      new Intl.DateTimeFormat("en-GB", {
        timeZone: "Asia/Ho_Chi_Minh",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      })
        .formatToParts(d)
        .map((x) => [x.type, x.value]),
    );
    return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
  };
  const now = Date.now();
  await page.getByLabel("Bắt đầu").fill(fmt(new Date(now - 3_600_000)));
  await page.getByLabel("Kết thúc").fill(fmt(new Date(now + 4 * 3_600_000)));
  await page.getByRole("button", { name: "Lưu ca trực" }).click();
  await expect(page.getByText("Đã lưu ca trực.")).toBeVisible();
  await expect(row).toContainText("Theo ca");
});
