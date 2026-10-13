import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetContact } from "@/api/mock/kn-contact";
import { ApiProblem } from "@/api/problem";
import { createContactApi } from "./api";
import { ContactDetailScreen, ContactListScreen } from "./ContactScreens";

const params: { id: string } = { id: "" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/dung-chung/contact",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useParams: () => params,
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
}));

async function clientFor(personaId: string, assignmentId: string, department = "trong_trot") {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: department }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, ca: createContactApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("Contact — giả lập theo hợp đồng /v1/contacts/* (lọc trường theo vai)", () => {
  beforeEach(() => resetContact());

  it("Cấp TW, Cấp Xã: chỉ họ tên, năm sinh, SĐT; không chi tiết, không ghi; bộ lọc trường ẩn bị bỏ qua (AC-01)", async () => {
    for (const [persona, asg] of [
      ["kn-quan", "asg-quan-tw"],
      ["kn-an", "asg-an-xa"],
    ] as const) {
      const { ca } = await clientFor(persona, asg);
      const a = await ca.access();
      expect(a).toMatchObject({ field_set: "basic", fields: ["full_name", "birth_year", "phone"], can_manage: false, can_detail: false });
      const l = await ca.persons({ level: "xa" });
      expect(l.total).toBe(4);
      for (const it of l.items) expect(Object.keys(it).sort()).toEqual(["birth_year", "full_name", "id", "phone"]);
      expect((await ca.persons({ q: "ttkn.angiang" })).total).toBe(0);
      expect((await ca.persons({ q: "nguyen thi" })).total).toBe(1);
      expect(await problemCode(ca.person(l.items[0]!.id))).toBe("contact.detail_forbidden");
      expect(await problemCode(ca.createPerson({ full_name: "X", phone: "0912000999", level: "tw" }))).toBe("contact.manage_forbidden");
      const u = await ca.units();
      for (const it of u.items) expect(Object.keys(it).sort()).toEqual(["id", "level", "level_label", "name", "phone"]);
    }
  });

  it("Cấp Tỉnh: đủ trường, lọc cấp Xã (AC-03), không cập nhật; Admin: thêm/sửa/ngừng hiển thị liên hệ ngoài tỉnh, lịch sử", async () => {
    const { ca: tinh } = await clientFor("kn-binh", "asg-binh-tinh");
    expect((await tinh.access()).field_set).toBe("full");
    const xa = await tinh.persons({ level: "xa" });
    expect(xa.items).toHaveLength(1);
    expect(xa.items[0]).toMatchObject({ full_name: "Lê Văn Bình", source: "staff", unit: "Xã Mỹ Hòa Hưng · Phòng Trồng trọt" });
    expect(await problemCode(tinh.createPerson({ full_name: "X", phone: "0912000999", level: "tw" }))).toBe("contact.manage_forbidden");

    const { ca: admin } = await clientFor("kn-admin", "asg-admin");
    expect((await admin.access()).can_manage).toBe(true);
    expect(await problemCode(admin.createPerson({ full_name: "Trùng", phone: "0912000111", level: "tw" }))).toBe("contact.duplicate_person");
    const d = await admin.createPerson({ full_name: "Đỗ Thị Sở", phone: "0296000123", level: "tinh", organization: "Sở NN&MT Đồng Tháp" });
    expect(d).toMatchObject({ source: "external", can_edit: true, unit: "Sở NN&MT Đồng Tháp" });
    const r = await admin.revisePerson(d.id, { title: "Chuyên viên", hidden: true });
    expect(r.history.at(-1)).toMatchObject({ change: "updated", changed_fields: ["title", "hidden"] });
    expect((await tinh.persons({})).total).toBe(4);
    const staff = (await admin.persons({ level: "xa" })).items[0]!;
    expect(await problemCode(admin.revisePerson(staff.id, { title: "X" }))).toBe("contact.staff_read_only");
  });
});

describe("Contact — màn hình", () => {
  beforeEach(() => resetContact());

  it("Cấp TW: 3 cột, không nút Thêm, tên không thành liên kết", async () => {
    auth.api = (await clientFor("kn-quan", "asg-quan-tw")).api;
    render(<ContactListScreen />);
    const table = await screen.findByRole("table", { name: "Danh bạ cán bộ" });
    await waitFor(() => expect(within(table).getByText("Nguyễn Thị Mai")).toBeTruthy());
    const headers = within(table)
      .getAllByRole("columnheader")
      .map((h) => h.textContent);
    expect(headers).toEqual(["STT", "Họ tên", "Năm sinh", "Số điện thoại"]);
    expect(screen.queryByRole("button", { name: "Thêm liên hệ ngoài tỉnh" })).toBeNull();
    expect(within(table).queryByRole("link", { name: "Nguyễn Thị Mai" })).toBeNull();
    expect(within(table).getByRole("link", { name: "0912000111" }).getAttribute("href")).toBe("tel:0912000111");
    expect(screen.queryByText("mai.nt@ttkn.angiang.gov.vn")).toBeNull();
  });

  it("Admin: đủ cột, thêm liên hệ ngoài tỉnh, tìm kiếm, chi tiết có lịch sử", async () => {
    const user = userEvent.setup();
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
    render(<ContactListScreen />);
    const table = await screen.findByRole("table", { name: "Danh bạ cán bộ" });
    await waitFor(() => expect(within(table).getByText("mai.nt@ttkn.angiang.gov.vn")).toBeTruthy());
    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((h) => h.textContent),
    ).toEqual(["STT", "Họ tên", "Năm sinh", "Số điện thoại", "Chức danh", "Đơn vị công tác", "Cấp", "Email"]);
    await user.click(screen.getByRole("button", { name: "Thêm liên hệ ngoài tỉnh" }));
    const dlg = await screen.findByRole("dialog");
    await user.type(within(dlg).getByLabelText("Họ tên"), "Phan Văn Ngoài");
    await user.type(within(dlg).getByLabelText("Số điện thoại"), "0296111222");
    await user.click(within(dlg).getByRole("button", { name: "Lưu" }));
    await screen.findByText("Đã thêm Phan Văn Ngoài.");
    await user.type(screen.getByRole("searchbox"), "phan van");
    await waitFor(() => expect(screen.getByText("1 liên hệ")).toBeTruthy());
    const link = await screen.findByRole("link", { name: "Phan Văn Ngoài" });
    params.id = link.getAttribute("href")!.split("/").at(-1)!;
    render(<ContactDetailScreen />);
    expect(await screen.findByText("Lịch sử thay đổi", { selector: "h2" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Ngừng hiển thị" })).toBeTruthy();
  });
});
