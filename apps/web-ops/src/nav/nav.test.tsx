import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { buildPrincipal, type Principal } from "@/auth/principal";
import { Sidebar } from "@/components/AppShell";
import { canSee, findNavItem, homeFor, visibleNav } from "./nav";

vi.mock("next/navigation", () => ({ usePathname: () => "/", useRouter: () => ({ replace: vi.fn() }) }));

function persona(id: string): Principal {
  const p = MOCK_PERSONAS.find((x) => x.id === id);
  if (!p) throw new Error(id);
  return buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
}

const hrefs = (p: Principal | null) => visibleNav(p).flatMap((g) => g.items.map((i) => i.href));

describe("menu theo phạm vi và vai", () => {
  it("TW/Tỉnh/Xã thấy màn hình khuyến nông, không thấy cổng Cục và quản trị", () => {
    for (const id of ["tw", "tinh", "xa"]) {
      const h = hrefs(persona(id));
      expect(h).toEqual(expect.arrayContaining(["/", "/htx", "/ban-do", "/nhiem-vu", "/thu-vien"]));
      expect(h).not.toContain("/cgh");
      expect(h.some((x) => x.startsWith("/quan-tri"))).toBe(false);
    }
  });

  it("Cục KTHT chỉ thấy cổng CGH + xuất báo cáo CGH, được đưa thẳng tới /cgh", () => {
    const cuc = persona("cuc");
    expect(hrefs(cuc).sort()).toEqual(["/bao-cao", "/cgh"]);
    expect(homeFor(cuc)).toBe("/cgh");
  });

  it("chỉ đạo, báo cáo cho KN; đồng bộ ngay chỉ cho quản lý tỉnh/TW; hộp thư không nằm trong menu", () => {
    expect(hrefs(persona("xa"))).toEqual(expect.arrayContaining(["/chi-dao", "/bao-cao"]));
    expect(hrefs(persona("xa"))).not.toContain("/dong-bo");
    expect(hrefs(persona("tinh"))).toContain("/dong-bo");
    expect(hrefs(persona("tw"))).toContain("/dong-bo");
    expect(hrefs(persona("setter")).includes("/thong-bao")).toBe(false);
    const inbox = findNavItem("/thong-bao");
    expect(inbox && canSee(inbox, persona("cuc"))).toBe(true);
  });

  it("quản trị tài khoản tỉnh chỉ thấy màn hình tài khoản trong nhóm quản trị", () => {
    const h = hrefs(persona("account-tinh")).filter((x) => x.startsWith("/quan-tri"));
    expect(h).toEqual(["/quan-tri/tai-khoan"]);
  });

  it("platform_admin thấy mọi nhóm, kể cả quản trị", () => {
    const h = hrefs(persona("setter"));
    expect(h).toEqual(expect.arrayContaining(["/", "/cgh", "/quan-tri/danh-muc", "/quan-tri/tham-so", "/quan-tri/to-chuc"]));
  });

  it("param_approver không phải admin: chỉ thêm Tham số, không có Danh mục/Tổ chức", () => {
    const h = hrefs(persona("approver"));
    expect(h).toContain("/quan-tri/tham-so");
    expect(h).not.toContain("/quan-tri/danh-muc");
    expect(h).not.toContain("/quan-tri/to-chuc");
    expect(h).not.toContain("/cgh");
  });

  it("chưa xác định phạm vi và không có vai → không thấy gì (mặc định chặn)", () => {
    const p = buildPrincipal({ sub: "u1" }, null);
    expect(hrefs(p)).toEqual([]);
    expect(homeFor(p)).toBe("/khong-co-quyen");
    expect(hrefs(null)).toEqual([]);
  });

  it("findNavItem khớp tiền tố dài nhất; trang chi tiết thừa hưởng quyền của danh sách", () => {
    expect(findNavItem("/htx/abc")?.href).toBe("/htx");
    expect(findNavItem("/")?.href).toBe("/");
    expect(findNavItem("/quan-tri/tham-so")?.href).toBe("/quan-tri/tham-so");
    const item = findNavItem("/cgh");
    expect(item && canSee(item, persona("tinh"))).toBe(false);
  });
});

describe("menu App HTX theo vai", () => {
  const HTX_PAGES = ["/htx/dashboard", "/htx/mua-vu", "/htx/nhat-ky", "/htx/phe-duyet", "/htx/san-luong", "/htx/tai-khoan-nong-dan", "/htx/yeu-cau-ho-tro"];

  it("Quản lý HTX chỉ thấy màn hình HTX của mình, vào thẳng bảng điều hành sản xuất", () => {
    const p = persona("htx");
    expect(hrefs(p)).toEqual(HTX_PAGES);
    expect(homeFor(p)).toBe("/htx/dashboard");
  });

  it("cán bộ KN xem được (chỉ đọc) nhưng không có màn hình phê duyệt", () => {
    for (const id of ["xa", "tinh", "tw"]) {
      const h = hrefs(persona(id));
      expect(h).toEqual(expect.arrayContaining(HTX_PAGES.filter((x) => x !== "/htx/phe-duyet")));
      expect(h).not.toContain("/htx/phe-duyet");
    }
    expect(hrefs(persona("cuc")).some((x) => x.startsWith("/htx"))).toBe(false);
  });

  it("Quy trình SOP cho người soạn và người phê duyệt", () => {
    expect(hrefs(persona("setter"))).toContain("/quan-tri/quy-trinh");
    expect(hrefs(persona("approver"))).toContain("/quan-tri/quy-trinh");
    expect(hrefs(persona("htx"))).not.toContain("/quan-tri/quy-trinh");
  });
});

describe("<Sidebar>", () => {
  it("hiển thị nhóm đánh số và phạm vi của người dùng tỉnh", () => {
    const p = persona("tinh");
    render(<Sidebar groups={visibleNav(p)} pathname="/htx/123" principal={p} onLogout={() => undefined} />);
    const nav = screen.getByRole("navigation");
    expect(within(nav).getByText("1. Điều hành")).toBeInTheDocument();
    expect(within(nav).queryByText("5. Quản trị")).toBeNull();
    expect(within(nav).queryByRole("link", { name: /Bản đồ CGH/ })).toBeNull();
    expect(within(nav).getByRole("link", { name: /Quản lý HTX/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByText(/Tỉnh · Đồng Tháp/)).toBeInTheDocument();
  });

  it("Cục chỉ có cổng CGH và báo cáo", () => {
    const p = persona("cuc");
    render(<Sidebar groups={visibleNav(p)} pathname="/cgh" principal={p} onLogout={() => undefined} />);
    expect(screen.getAllByRole("link")).toHaveLength(2);
    expect(screen.getByRole("link", { name: /Bản đồ CGH/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: /Báo cáo/ })).toBeInTheDocument();
  });
});
