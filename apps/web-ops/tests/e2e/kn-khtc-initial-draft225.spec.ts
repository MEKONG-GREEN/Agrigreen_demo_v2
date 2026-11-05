import { expect, test } from "@playwright/test";
import { logout, sidebar } from "./helpers";

test("KHTC225: nháp máy chủ ban đầu giữ cây qua F5, Log và hủy đúng thế hệ",async({page},info)=>{
  test.setTimeout(180_000);
  await page.clock.setFixedTime(new Date("2030-06-01T09:00:00Z"));
  const login = async () => {
    await page.getByLabel("Email hoặc số điện thoại").fill("admin.mekong@mekonggreen.vn");
    await page.getByLabel("Mật khẩu", { exact: true }).fill("KhuyenNong@2026");
    await page.getByRole("button", { name: "Đăng nhập", exact: true }).click();
  };
  await page.goto("/login"); await login();
  await sidebar(page).getByRole("link", { name: "Phòng ban", exact: true }).click();
  await page.getByLabel("Trạng thái Phòng Kế hoạch - Tài chính", { exact: true }).selectOption("active");
  await expect(page.getByRole("status").filter({ hasText: "Đã đổi" })).toBeVisible();
  await logout(page); await login();
  await sidebar(page).getByRole("button", { name: "Phòng Kế hoạch - Tài chính", exact: true }).click();
  await sidebar(page).getByRole("link", { name: "Kế hoạch năm", exact: true }).click();
  await expect(page).toHaveURL(/\/ke-hoach-nam$/);
  await page.getByRole("button",{name:"Năm sau",exact:true}).click();
  await expect(page.getByText("Năm 2031",{exact:true})).toBeVisible();
  await page.getByRole("button",{name:"Nháp máy chủ ban đầu",exact:true}).click();
  await expect(page).toHaveURL(/\/nhap-may-chu\?year=2031$/);
  await expect(page.getByRole("heading",{name:"Nháp máy chủ ban đầu — năm 2031"})).toBeVisible();
  await expect(page.getByRole("button",{name:/Áp dụng chưa được hỗ trợ/})).toBeDisabled();
  await page.getByRole("button",{name:"Chọn CT/DA vào tab",exact:true}).click();
  await page.getByRole("button",{name:"CT/DA mới…",exact:true}).click();
  await page.getByRole("textbox",{name:/Mã CT\/DA/}).fill("CT225");
  await page.getByRole("textbox",{name:/Tên CT\/DA/}).fill("Chương trình nháp225");
  await page.getByRole("button",{name:"Lưu CT/DA chủ",exact:true}).click();
  await page.getByRole("button",{name:"Chọn CT225",exact:true}).click();
  const program=page.getByRole("row").filter({has:page.getByRole("cell",{name:"CT/DA · Chương trình nháp225",exact:true})});
  await program.getByRole("button",{name:"Thêm nhóm",exact:true}).click();
  await page.getByRole("textbox",{name:/^Tên/}).fill("Nhóm nháp225");
  await page.getByRole("button",{name:"Lưu vào nháp",exact:true}).click();
  const group=page.getByRole("row").filter({has:page.getByRole("cell",{name:"Nhóm · Nhóm nháp225",exact:true})});
  await group.getByRole("button",{name:"Thêm hạng mục",exact:true}).click();
  await page.getByRole("textbox",{name:/^Tên/}).fill("Hạng mục nháp225");
  await page.getByRole("combobox",{name:/Phòng phụ trách/}).selectOption("trong_trot");
  const cells=["Thứ tự trong cây","Khối lượng KH","Kinh phí KH (đồng)","Số điểm"];
  for(const bad of ["1e100000","1e-100000","9".repeat(100000)]){
    for(const label of cells)await page.getByRole("textbox",{name:label,exact:true}).fill(bad);
    await page.getByRole("button",{name:"Lưu vào nháp",exact:true}).click();
    for(const label of cells)await expect(page.getByRole("textbox",{name:label,exact:true})).toHaveAttribute("aria-invalid","true");
    await expect(page.getByRole("button",{name:"Tạo nháp máy chủ",exact:true})).toBeDisabled();
  }
  await page.getByRole("textbox",{name:cells[0],exact:true}).fill("10");
  await page.getByRole("textbox",{name:cells[1],exact:true}).fill("123,0001");
  await page.getByRole("textbox",{name:cells[2],exact:true}).fill("999999999999999999.99");
  await page.getByRole("textbox",{name:cells[3],exact:true}).fill("2");
  await page.getByRole("button",{name:"Lưu vào nháp",exact:true}).click();
  await expect(page.getByText("Có thay đổi trên màn chưa lưu.",{exact:false})).toBeVisible();
  await page.getByRole("button",{name:"Tạo nháp máy chủ",exact:true}).click();
  const ack=page.getByRole("status").filter({hasText:"Đã xác nhận lưu nháp máy chủ"});
  await expect(ack).toContainText("lần ghi 1");
  const generation=(await ack.innerText()).match(/thế hệ ([0-9a-f-]{36})/i)?.[1];
  expect(generation).toBeTruthy();
  await page.screenshot({path:info.outputPath("225-server-draft-ack.png"),fullPage:true});
  await page.getByRole("button",{name:"Log nháp máy chủ",exact:true}).click();
  await expect(page.getByText(`create — thế hệ ${generation}; lần ghi 1`,{exact:true})).toBeVisible();
  // A real browser reload recreates modules, API adapters and selected-context resources.
  await page.reload();
  try {
    await expect(page.getByRole("heading",{name:"Nháp máy chủ ban đầu — năm 2031"})).toBeVisible();
  } catch (error) {
    try {
      await page.screenshot({path:info.outputPath("225-after-f5-failure.png"),fullPage:true,mask:[page.locator('input[type="password"], input[type="email"]')]});
    } catch { /* Keep the original business assertion even if artifact capture fails. */ }
    throw error;
  }
  await expect(page.getByText(`create — thế hệ ${generation}; lần ghi 1`,{exact:true})).toBeVisible();
  await page.getByRole("button",{name:"Cây nháp",exact:true}).click();
  const item=page.getByRole("row").filter({has:page.getByRole("cell",{name:"Hạng mục · Hạng mục nháp225",exact:true})});
  await expect(item).toContainText("123,0001");
  await expect(program).toBeVisible();await expect(group).toBeVisible();
  await page.getByRole("button",{name:"Lưu nháp máy chủ",exact:true}).click();
  await expect(ack).toContainText(`thế hệ ${generation}, lần ghi 2`);
  await page.screenshot({path:info.outputPath("225-browser-reload-save2.png"),fullPage:true});
  await page.getByRole("button",{name:"Hủy đúng nháp máy chủ lần 2",exact:true}).click();
  await expect(page.getByRole("status").filter({hasText:"Chưa có nháp máy chủ đang giữ"})).toContainText("mốc ghi 3");
  await expect(item).toHaveCount(0);
  await page.getByRole("button",{name:"Tạo nháp máy chủ",exact:true}).click();
  await expect(ack).toContainText("lần ghi 4");
  expect((await ack.innerText()).includes(`thế hệ ${generation},`)).toBe(false);
  await page.getByRole("button",{name:"Log nháp máy chủ",exact:true}).click();
  await expect(page.getByText(`discard — thế hệ ${generation}; lần ghi 3`,{exact:true})).toBeVisible();
  await expect(page.getByText(`create — thế hệ ${generation}; lần ghi 1`,{exact:true})).toBeVisible();

});
