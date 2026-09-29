import { expect, test } from "@playwright/test";
import { COOPERATIVES } from "../../src/api/mock/data";
import { loginAs } from "./helpers";
import { coop, household, plot } from "./htx-data";

// Hoàn thiện App HTX trên web (G2): nhập/sao chép hồ sơ HTX, quản lý nông hộ (tìm SĐT, gọi nhanh, sao chép
// hàng loạt), bản đồ lô ruộng, lịch gửi báo cáo của Quản lý HTX, nhóm dữ liệu "Đồng bộ ngay", xem trước mobile.

test("HTX US-HTX-02: tải tệp mẫu, nhập HTX hàng loạt — kết quả theo dòng, lý do Thiếu cột bắt buộc", async ({ page }) => {
  await loginAs(page, "setter", "/htx");
  await page.getByRole("button", { name: "Nhập HTX từ Excel" }).click();
  const form = page.getByRole("form", { name: "Nhập HTX hàng loạt" });
  const download = page.waitForEvent("download");
  await form.getByRole("button", { name: "Tải tệp mẫu" }).click();
  expect((await download).suggestedFilename()).toMatch(/^mau-nhap-htx/);

  const other = COOPERATIVES.find((c) => c.api.ref !== coop.api.ref)!;
  const csv = [
    "Tên HTX,Địa chỉ,Người đại diện,SĐT,Mã xã",
    `HTX Nhập Thử Một,Ấp Một,Nguyễn Văn Đại,0901000001,${coop.api.admin_unit_code}`,
    `HTX Nhập Thử Hai,Ấp Hai,,0901000002,${coop.api.admin_unit_code}`,
    `"${other.api.name}",Ấp Ba,Lê Văn Trùng,0901000003,${coop.api.admin_unit_code}`,
  ].join("\n");
  await form.getByLabel("Tệp danh sách HTX").setInputFiles({ name: "htx.csv", mimeType: "text/csv", buffer: Buffer.from(csv, "utf-8") });
  await form.getByRole("button", { name: "Nhập" }).click();
  await expect(form.getByRole("status")).toContainText("1 dòng thành công · 2 dòng lỗi");
  const result = form.getByRole("table", { name: "Kết quả nhập HTX theo dòng" });
  await expect(result.getByRole("row").filter({ hasText: "Dòng 3" })).toContainText("Thiếu cột bắt buộc: Người đại diện");
  await expect(result.getByRole("row").filter({ hasText: "Dòng 4" })).toContainText("Tên HTX này đã tồn tại, vui lòng chọn tên khác.");

  // Tệp thiếu hẳn cột "Người đại diện": từng dòng bị loại, nêu tên cột.
  const noCol = ["Tên HTX,Địa chỉ,SĐT,Mã xã", `HTX Thiếu Cột,Ấp 4,0901000004,${coop.api.admin_unit_code}`].join("\n");
  await form.getByLabel("Tệp danh sách HTX").setInputFiles({ name: "htx2.csv", mimeType: "text/csv", buffer: Buffer.from(noCol, "utf-8") });
  await form.getByRole("button", { name: "Nhập" }).click();
  await expect(form.getByRole("status")).toContainText("0 dòng thành công · 1 dòng lỗi");
  await expect(form.getByRole("table", { name: "Kết quả nhập HTX theo dòng" })).toContainText("Thiếu cột bắt buộc: Người đại diện");

  // HTX vừa nhập có trong danh sách.
  await page.getByLabel("Tìm HTX").fill("HTX Nhập Thử Một");
  await expect(page.getByRole("link", { name: "HTX Nhập Thử Một" })).toBeVisible();
});

test("HTX US-HTX-03: sao chép hồ sơ HTX — sửa Tên và Người đại diện, tên trùng bị chặn", async ({ page }) => {
  await loginAs(page, "setter", `/htx/${coop.api.ref}`);
  await page.getByRole("button", { name: "Sao chép hồ sơ" }).click();
  const dlg = page.getByRole("dialog", { name: `Sao chép hồ sơ ${coop.api.name}` });
  await expect(dlg).toContainText("Không sao chép Mã HTX");
  await expect(dlg.getByLabel("Người đại diện", { exact: true })).toHaveValue(""); // người đại diện của hồ sơ nguồn không được chép
  const other = COOPERATIVES.find((c) => c.api.ref !== coop.api.ref)!;
  await dlg.getByLabel("Tên HTX của bản sao").fill(other.api.name);
  await dlg.getByRole("button", { name: "Lưu bản sao" }).click();
  await expect(dlg.getByRole("alert")).toContainText("Tên HTX này đã tồn tại, vui lòng chọn tên khác.");

  await dlg.getByLabel("Tên HTX của bản sao").fill(`${coop.api.name} — cơ sở 2`);
  await dlg.getByLabel("Người đại diện", { exact: true }).fill("Phạm Thị Mới");
  await dlg.getByLabel("SĐT người đại diện").fill("0902000001");
  await dlg.getByRole("button", { name: "Lưu bản sao" }).click();
  await expect(dlg.getByRole("status")).toContainText("Đã tạo");
  await dlg.getByRole("link", { name: `${coop.api.name} — cơ sở 2` }).click();
  await expect(page.getByRole("heading", { level: 1, name: `${coop.api.name} — cơ sở 2` })).toBeVisible();
  await expect(page.getByText(coop.commune_name).first()).toBeVisible(); // giữ địa bàn của hồ sơ nguồn
});

test("HTX US-HH-01 AC-2 + US-HH-03: tìm nông hộ theo SĐT, gọi nhanh (SĐT đọc lúc bấm; máy không gọi được → thông báo)", async ({ page }) => {
  await loginAs(page, "htx", "/htx/nong-ho");
  const h = household(2);
  await page.getByLabel("Tìm nông hộ theo SĐT").fill("Nguyễn Văn");
  await page.getByRole("button", { name: "Tìm" }).click();
  await expect(page.getByRole("alert").first()).toContainText("Chỉ tìm được theo SĐT");

  await page.getByLabel("Tìm nông hộ theo SĐT").fill(h.phone);
  await page.getByRole("button", { name: "Tìm" }).click();
  const table = page.getByRole("table", { name: "Nông hộ thành viên (nhãn đã che)" });
  await expect(table.getByRole("row")).toHaveCount(2); // tiêu đề + đúng một hộ
  await expect(table).toContainText(h.label);
  await expect(page.getByText(h.phone, { exact: true })).toHaveCount(0); // chưa bấm gọi: không có SĐT đầy đủ trên trang

  await table.getByRole("button", { name: `Gọi nhanh ${h.label}` }).click();
  const call = page.getByTestId(`call-${h.ref}`);
  await expect(call.getByRole("link", { name: `Gọi ${h.label}` })).toHaveAttribute("href", `tel:${h.phone}`);
  await expect(call.getByRole("status")).toContainText("Không thể khởi tạo cuộc gọi trên thiết bị này.");
  await call.getByRole("button", { name: `Ẩn số ${h.label}` }).click();
  await expect(page.getByText(h.phone, { exact: true })).toHaveCount(0);

  // Hộ chưa có SĐT hợp lệ trong kho định danh → báo lỗi, không treo màn hình.
  const noPhone = household(3);
  await page.getByLabel("Tìm nông hộ theo SĐT").fill(noPhone.phone);
  await page.getByRole("button", { name: "Tìm" }).click();
  await table.getByRole("button", { name: `Gọi nhanh ${noPhone.label}` }).click();
  await expect(table.getByRole("alert")).toContainText("chưa có số điện thoại");
});

test("HTX US-HH-02: sao chép hàng loạt nông hộ — sửa riêng họ tên/SĐT, kết quả thành công/lỗi trên màn hình", async ({ page }) => {
  await loginAs(page, "htx", "/htx/nong-ho");
  const a = household(0);
  const b = household(1);
  const table = page.getByRole("table", { name: "Nông hộ thành viên (nhãn đã che)" });
  await expect(table.getByRole("row").nth(2)).toBeVisible();
  await table.getByRole("checkbox").nth(0).check();
  await table.getByRole("checkbox").nth(1).check();
  await page.getByRole("button", { name: "Sao chép hàng loạt (2)" }).click();
  const dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Xác nhận sao chép" }).click();
  await expect(dlg.getByText("Thiếu họ tên").first()).toBeVisible();

  await dlg.getByLabel("Họ tên chủ hộ (hồ sơ 1)").fill("Nguyễn Văn Bản Sao");
  await dlg.getByLabel("SĐT (hồ sơ 1)").fill("0903000001");
  await dlg.getByLabel("Họ tên chủ hộ (hồ sơ 2)").fill("Trần Thị Trùng");
  await dlg.getByLabel("SĐT (hồ sơ 2)").fill(b.phone); // SĐT đã có
  await dlg.getByRole("button", { name: "Xác nhận sao chép" }).click();
  await expect(dlg.getByRole("status")).toContainText("1 hồ sơ thành công · 1 hồ sơ lỗi");
  const res = dlg.getByRole("table", { name: "Kết quả sao chép từng hồ sơ" });
  await expect(res.getByRole("row").filter({ hasText: "Hồ sơ 2" })).toContainText("SĐT đã tồn tại");
  await expect(res.getByRole("row").filter({ hasText: "Hồ sơ 1" })).toContainText("Đã tạo");
  expect(a.ref).not.toBe(b.ref);
  await dlg.getByRole("button", { name: "Đóng" }).click();

  await page.getByLabel("Tìm nông hộ theo SĐT").fill("0903000001");
  await page.getByRole("button", { name: "Tìm" }).click();
  await expect(table).toContainText("Nguyễn V. B. S.");
});

test("HTX US-PLOT-01: bản đồ lô ruộng của Quản lý HTX — lô KN vẽ trước 'Nháp - KN vẽ trước', chọn lô → vụ và nhật ký", async ({ page }) => {
  await loginAs(page, "htx", "/htx/lo-ruong");
  await expect(page.getByRole("heading", { level: 1, name: "Lô ruộng & ranh giới" })).toBeVisible();
  const list = page.getByRole("table", { name: "Danh sách lô ruộng" });
  await expect(list.getByText("Nháp - KN vẽ trước").first()).toBeVisible();
  const p = plot(1); // vụ Hè Thu đang mở, có nhật ký
  await list.getByRole("button", { name: `Xem lô ${p.code}` }).click();
  const sum = page.getByRole("region", { name: `Lô ${p.code}` });
  await expect(sum.getByLabel("Vụ gần nhất")).toContainText("Đang canh tác");
  await expect(sum).toContainText("nhật ký");
  // Chọn lô nháp: không yêu cầu vẽ lại.
  const draftRow = list.getByRole("row").filter({ hasText: "Nháp - KN vẽ trước" }).first();
  await draftRow.getByRole("button", { name: /^Xem lô / }).click();
  await expect(page.getByRole("note")).toContainText("Không cần vẽ lại");
});

test("HTX US-DASH-03: Quản lý HTX lập lịch gửi báo cáo cho tài khoản của HTX, xem lần gửi, ngừng lịch", async ({ page }) => {
  await loginAs(page, "htx", "/htx/lich-bao-cao");
  const form = page.getByRole("form", { name: "Lập lịch gửi báo cáo sản lượng" });
  await form.getByRole("button", { name: "Lập lịch" }).click();
  await expect(form.getByRole("alert")).toContainText("Chọn ít nhất một người nhận.");
  await expect(form).toContainText("ke•••@htx-gia-lap.vn"); // chỉ tài khoản đã đăng ký của HTX, email đã che
  await form.getByRole("checkbox", { name: /Nguyễn Thị Kế Toán/ }).check();
  await form.getByLabel("Ngày trong tháng (1–28)").fill("5");
  await form.getByLabel("Giờ gửi (giờ Việt Nam)").fill("6");
  await form.getByRole("button", { name: "Lập lịch" }).click();
  const row = page.getByRole("listitem", { name: "Lịch Hằng tháng, ngày 5 lúc 6 giờ" });
  await expect(row).toContainText("Đang chạy");
  await expect(row).toContainText("Nguyễn Thị Kế Toán");
  await row.getByRole("button", { name: "Chi tiết" }).click();
  await expect(row.getByRole("alert")).toContainText("Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại.");
  await row.getByLabel("Lý do ngừng lịch (≥ 10 ký tự)").fill("HTX chuyển sang báo cáo quý");
  await row.getByRole("button", { name: "Ngừng lịch" }).click();
  await expect(page.getByRole("listitem", { name: "Lịch Hằng tháng, ngày 5 lúc 6 giờ" })).toContainText("Đã ngừng");
});

test("KN US-SYNC-02: chọn nhóm dữ liệu rồi Đồng bộ ngay — sync_now mang entities[]", async ({ page }) => {
  await loginAs(page, "tinh", "/dong-bo");
  await expect(page.getByText("Sẽ đồng bộ: tất cả dữ liệu")).toBeVisible();
  await page.getByRole("checkbox", { name: "Thửa ruộng & ranh giới" }).check();
  await page.getByRole("checkbox", { name: "Mùa vụ, nhật ký & SOP" }).check();
  await expect(page.getByText("Sẽ đồng bộ: Thửa ruộng & ranh giới, Mùa vụ, nhật ký & SOP")).toBeVisible();
  const unit = page.getByRole("region", { name: "Theo đơn vị" });
  await unit.getByRole("button", { name: "Đồng bộ ngay" }).click();
  await expect(unit.getByRole("status")).toContainText("Thửa ruộng & ranh giới, Mùa vụ, nhật ký & SOP");
});

test("HTX US-CMS-01: soạn bài — xem trước khung điện thoại, thiếu chuyên mục bị chặn gửi duyệt", async ({ page }) => {
  await loginAs(page, "setter", "/quan-tri/bai-viet");
  await page.getByLabel("Tiêu đề", { exact: true }).fill("Cảnh báo rầy nâu cuối vụ");
  await page.getByLabel(/^Nội dung/).fill("# Diễn biến\n\n**Rầy nâu** tăng mạnh ở ruộng trũng.\n\n- Thăm đồng 3 ngày/lần\n- Không phun định kỳ");
  await page.getByRole("button", { name: "Gửi duyệt để xuất bản" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Vui lòng nhập tiêu đề và chọn chuyên mục trước khi xuất bản." })).toBeVisible();

  await page.getByLabel("Chuyên mục").selectOption("pest_warning");
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");
  await page.getByLabel(/^Ảnh bìa/).setInputFiles({ name: "bia.png", mimeType: "image/png", buffer: png });
  await page.getByRole("button", { name: "Xem trước trên điện thoại" }).click();
  const phone = page.getByRole("figure", { name: "Xem trước trên điện thoại" });
  await expect(phone.getByRole("heading", { name: "Cảnh báo rầy nâu cuối vụ" })).toBeVisible();
  await expect(phone).toContainText("Cảnh báo sâu bệnh");
  await expect(phone.getByRole("img", { name: "Ảnh bìa bài viết" })).toBeVisible();
  await expect(phone.locator("strong", { hasText: "Rầy nâu" })).toBeVisible();
  await expect(phone.getByRole("listitem")).toHaveCount(2);
  await page.getByRole("dialog").getByRole("button", { name: "Đóng" }).click();

  await page.getByRole("button", { name: "Xem trước bản đã lưu (như khi xuất bản)" }).click();
  await expect(page.getByRole("dialog", { name: /Xem trước phiên bản \d+ \(máy chủ đã lọc\)/ })).toBeVisible();
  await expect(page.getByRole("figure", { name: "Xem trước trên điện thoại" })).toContainText("Thăm đồng 3 ngày/lần");
  await page.getByRole("dialog").getByRole("button", { name: "Đóng" }).click();
  await page.getByRole("button", { name: "Gửi duyệt để xuất bản" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Đã gửi duyệt" })).toBeVisible();
  await expect(page.getByRole("table", { name: "Bài viết tin tức" })).toContainText("Chờ duyệt");
});
