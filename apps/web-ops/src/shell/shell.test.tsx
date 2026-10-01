import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { knMenuFor } from "@/api/mock/kn";
import { KN_ACCOUNTS } from "@/api/mock/kn-accounts";
import type { MenuTree } from "@/kn/contract";
import type { KnSessionState } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { activeMenu, isAllowed, isBareRoute } from "@/components/AppShell";
import { AccountMenu } from "./AccountMenu";
import { Breadcrumb } from "./Breadcrumb";
import { LoginCard } from "./LoginCard";
import { badgeOf, crumbsFor, findActive, legacyMenu, menuAllows, menuLinks } from "./menu";
import { RoleContextPicker } from "./RoleContextPicker";
import { Sidebar } from "./Sidebar";

vi.mock("next/navigation", () => ({ usePathname: () => "/", useRouter: () => ({ replace: vi.fn(), push: vi.fn() }) }));

const xa = knMenuFor("kn_xa", "trong_trot");
const tinh = knMenuFor("kn_tinh", "trong_trot");
const tw = knMenuFor("kn_tw", "trong_trot");
const admin = knMenuFor("admin", null);
const labels = (t: MenuTree) => menuLinks(t).map((l) => l.node.label);

describe("menu theo vai trò (BRD v7.7 §6, Hình 3c–3f)", () => {
  it("nhóm Điều hành / Báo cáo / Dữ liệu; Admin: Điều hành / Phòng ban / Quản trị hệ thống", () => {
    expect(xa.groups.map((g) => g.label)).toEqual(["Điều hành", "Báo cáo", "Dữ liệu"]);
    expect(tinh.groups.map((g) => g.label)).toEqual(["Điều hành", "Báo cáo"]);
    expect(tw.groups.map((g) => g.label)).toEqual(["Điều hành", "Báo cáo"]);
    expect(admin.groups.map((g) => g.label)).toEqual(["Điều hành", "Phòng ban", "Quản trị hệ thống"]);
  });

  it("Cấp Xã không có Dashboard (chốt v7.4); Cấp Quốc gia có Báo cáo năm, không có Nhiệm vụ", () => {
    expect(labels(xa)).not.toContain("Dashboard");
    expect(labels(xa)).toEqual(expect.arrayContaining(["Công việc cần làm", "Lịch công tác", "Báo cáo của xã", "Danh sách HTX", "Vật tư"]));
    expect(labels(tw)).toEqual(["Dashboard", "Báo cáo tuần", "Báo cáo tháng", "Báo cáo hàng vụ", "Báo cáo năm"]);
    expect(labels(tinh)).toEqual(
      expect.arrayContaining(["Dashboard", "Phát hành biểu mẫu", "Trạng thái nộp", "Rà soát & chốt kỳ", "Báo cáo tổng hợp các xã", "Báo cáo Chi cục"]),
    );
  });

  it("Báo cáo đột xuất chỉ hiện khi phòng ban có mẫu loại này (mặc định tắt)", () => {
    expect(labels(xa)).not.toContain("Báo cáo đột xuất");
    expect(labels(tinh)).not.toContain("Báo cáo đột xuất");
  });

  it("Admin: phòng ban chưa mở hiện mờ, gộp 'n phòng khác'; màn hiện có giữ đường dẫn cũ", () => {
    const pb = admin.groups.find((g) => g.key === "phong-ban")!;
    expect(pb.items.map((i) => [i.label, i.status ?? "active"])).toEqual([
      ["Phòng Trồng trọt", "active"],
      ["Chăn nuôi - TS", "not_open"],
      ["Kế hoạch - TC", "not_open"],
      ["3 phòng khác", "not_open"],
    ]);
    const hrefs = menuLinks(admin).map((l) => l.node.href);
    expect(hrefs).toEqual(expect.arrayContaining(["/kn/tong-quan", "/quan-tri/to-chuc", "/quan-tri/tai-khoan", "/quan-tri/danh-muc", "/quan-tri/mau-bao-cao"]));
  });

  it("khớp tiền tố dài nhất, breadcrumb mục cha / mục, kiểm đường dẫn theo menu", () => {
    expect(findActive(xa, "/kn/du-lieu/htx/htx-01")?.node.label).toBe("Danh sách HTX");
    expect(crumbsFor(xa, "/kn/nhiem-vu/cong-viec")).toEqual(["Nhiệm vụ", "Công việc cần làm"]);
    expect(crumbsFor(tw, "/kn/dashboard")).toEqual(["Điều hành", "Dashboard"]);
    expect(menuAllows(tw, "/kn/nhiem-vu/phat-hanh")).toBe(false);
    expect(menuAllows(tinh, "/kn/nhiem-vu/phat-hanh")).toBe(true);
  });

  it("số đếm chỉ cho việc cần xử lý, ẩn khi bằng 0", () => {
    const node = menuLinks(tinh).find((l) => l.node.label === "Trạng thái nộp")!.node;
    expect(badgeOf(node, { counts: { "submissions.missing": 15 } })).toBe(15);
    expect(badgeOf(node, { counts: { "submissions.missing": 0 } })).toBe(0);
    const dash = menuLinks(tinh).find((l) => l.node.label === "Dashboard")!.node;
    expect(badgeOf(dash, { counts: { anything: 5 } })).toBe(0);
  });
});

describe("<Sidebar> (Design Sample)", () => {
  it("mục cha tự mở khi con active, một mục active, số đếm hiện/ẩn, footer sản phẩm", () => {
    render(<Sidebar menu={xa} pathname="/kn/nhiem-vu/cong-viec" badges={{ counts: { "report_tasks.todo": 6 } }} />);
    const nav = screen.getByRole("navigation", { name: "Menu chức năng" });
    expect(within(nav).getByRole("button", { name: /Nhiệm vụ/ })).toHaveAttribute("aria-expanded", "true");
    expect(within(nav).getByRole("button", { name: /Thu thập dữ liệu/ })).toHaveAttribute("aria-expanded", "false");
    const active = within(nav).getAllByRole("link").filter((a) => a.getAttribute("aria-current") === "page");
    expect(active).toHaveLength(1);
    expect(active[0]).toHaveTextContent("Công việc cần làm");
    expect(active[0]).toHaveTextContent("6");
    expect(within(nav).getByRole("link", { name: /Lịch công tác/ })).not.toHaveTextContent(/\d/);
    expect(screen.getByText("© 2026 TTKN Quốc gia")).toBeInTheDocument();
  });

  it("mở/đóng mục cha bằng mũi tên", async () => {
    const user = userEvent.setup();
    render(<Sidebar menu={xa} pathname="/kn/nhiem-vu/cong-viec" />);
    const btn = screen.getByRole("button", { name: /Thu thập dữ liệu/ });
    await user.click(btn);
    expect(btn).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("link", { name: /Máy móc HTX/ })).toBeVisible();
    await user.click(btn);
    expect(btn).toHaveAttribute("aria-expanded", "false");
  });

  it("thu gọn: chỉ icon, tên vẫn đọc được; mục cha mở flyout chứa menu con", async () => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    render(<Sidebar menu={xa} pathname="/kn/bao-cao/tuan" collapsed onToggleCollapse={onToggle} />);
    expect(screen.getByRole("complementary", { name: "Điều hướng chính" })).toHaveAttribute("data-collapsed", "true");
    expect(screen.getByRole("link", { name: "Báo cáo tuần" })).toHaveAttribute("aria-current", "page");
    const parent = screen.getByRole("button", { name: /Thu thập dữ liệu/ });
    const flyEl = document.getElementById(parent.getAttribute("aria-controls")!);
    expect(flyEl).toHaveAttribute("hidden");
    await user.hover(parent.closest("li")!);
    const fly = screen.getByRole("group", { name: "Thu thập dữ liệu" });
    expect(within(fly).getByRole("link", { name: "Vẽ thửa ruộng" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Mở rộng menu" }));
    expect(onToggle).toHaveBeenCalled();
  });

  it("Admin: phòng ban chưa mở không bấm được, nhãn Chưa mở", () => {
    render(<Sidebar menu={admin} pathname="/kn/tong-quan" />);
    const closed = screen.getByText("Chăn nuôi - TS").closest(".nav-item")!;
    expect(closed).toHaveAttribute("aria-disabled", "true");
    expect(closed).toHaveTextContent("Chưa mở");
    expect(screen.queryByRole("link", { name: /Chăn nuôi/ })).toBeNull();
  });

  it("menu cũ (nav.ts) dùng cùng khung", () => {
    render(<Sidebar menu={legacyMenu([{ label: "1. Điều hành", items: [{ href: "/", label: "Tổng quan", icon: "dashboard" }] }])} pathname="/" />);
    expect(screen.getByRole("link", { name: "Tổng quan" })).toHaveAttribute("aria-current", "page");
  });
});

describe("<AccountMenu>", () => {
  it("Đang dùng, các cặp khác, Đổi mật khẩu, Đăng xuất", async () => {
    const user = userEvent.setup();
    const onSwitch = vi.fn();
    const onLogout = vi.fn();
    render(
      <AccountMenu
        displayName="Trần Thị Bình"
        subtitle="Cấp Tỉnh · Trồng trọt"
        current="Cấp Tỉnh · Phòng Trồng trọt"
        others={[{ key: "asg-binh-xa|trong_trot", label: "Cấp Xã · Phòng Trồng trọt" }]}
        onSwitch={onSwitch}
        onChangePassword={() => undefined}
        onLogout={onLogout}
      />,
    );
    const btn = screen.getByRole("button", { name: /Tài khoản: Trần Thị Bình/ });
    expect(btn).toHaveTextContent("TB");
    await user.click(btn);
    const menu = screen.getByRole("menu");
    expect(within(menu).getByText("Cấp Tỉnh · Phòng Trồng trọt")).toBeInTheDocument();
    await user.click(within(menu).getByRole("menuitem", { name: "Cấp Xã · Phòng Trồng trọt" }));
    expect(onSwitch).toHaveBeenCalledWith("asg-binh-xa|trong_trot");
    await user.click(btn);
    await user.click(screen.getByRole("menuitem", { name: "Đăng xuất" }));
    expect(onLogout).toHaveBeenCalled();
  });

  it("chỉ 1 vai trò, 1 phòng ban → ghi chú, Esc đóng menu", async () => {
    const user = userEvent.setup();
    render(<AccountMenu displayName="Lê Minh Quân" subtitle="x" current="x" others={[]} onSwitch={() => undefined} onChangePassword={() => undefined} onLogout={() => undefined} />);
    await user.click(screen.getByRole("button", { name: /Lê Minh Quân/ }));
    expect(screen.getByText("Tài khoản chỉ có 1 vai trò, 1 phòng ban")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
  });
});

describe("<Breadcrumb>", () => {
  it("mục cuối đậm, aria-current", () => {
    render(<Breadcrumb items={[{ label: "Điều hành" }, { label: "Dashboard" }]} />);
    const nav = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(nav).getByText("Dashboard")).toHaveAttribute("aria-current", "page");
  });
});

describe("<LoginCard> (Hình 3a)", () => {
  it("Hiện/Ẩn mật khẩu, bắt buộc trường, gửi giá trị", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<LoginCard mode="password" onSubmit={onSubmit} />);
    await user.click(screen.getByRole("button", { name: "Đăng nhập" }));
    expect(screen.getByText("Nhập email hoặc số điện thoại.")).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText(/Email hoặc số điện thoại/), "an.nv@angiang.gov.vn");
    const pw = screen.getByLabelText(/^Mật khẩu/);
    await user.type(pw, "abc");
    expect(pw).toHaveAttribute("type", "password");
    await user.click(screen.getByRole("button", { name: "Hiện" }));
    expect(pw).toHaveAttribute("type", "text");
    await user.click(screen.getByLabelText("Ghi nhớ đăng nhập"));
    await user.click(screen.getByRole("button", { name: "Đăng nhập" }));
    expect(onSubmit).toHaveBeenCalledWith({ identifier: "an.nv@angiang.gov.vn", password: "abc", remember: true });
  });

  it("banner lỗi ghi số lần thử còn lại; khóa tạm; Quên mật khẩu và Liên hệ quản trị viên", () => {
    const { rerender } = render(<LoginCard mode="password" error={{ kind: "invalid", remaining: 4 }} onSubmit={() => undefined} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Sai tài khoản hoặc mật khẩu.Kiểm tra lại thông tin. Còn 4 lần thử.");
    rerender(<LoginCard mode="password" error={{ kind: "locked", minutes: 15 }} onSubmit={() => undefined} />);
    expect(screen.getByRole("alert")).toHaveTextContent("tạm khóa 15 phút");
    expect(screen.getByRole("link", { name: "Quên mật khẩu?" })).toHaveAttribute("href", "/login/lien-he?ly-do=quen-mat-khau");
    expect(screen.getByRole("link", { name: "Liên hệ quản trị viên" })).toHaveAttribute("href", "/login/lien-he");
  });

  it("chế độ OIDC: không có ô mật khẩu (mật khẩu nhập ở Keycloak)", () => {
    render(<LoginCard mode="oidc" onSubmit={() => undefined} />);
    expect(screen.queryByLabelText(/^Mật khẩu/)).toBeNull();
    expect(screen.getByText(/nhập ở trang xác thực/)).toBeInTheDocument();
  });
});

describe("<RoleContextPicker> (Hình 3b)", () => {
  const an = KN_ACCOUNTS.find((a) => a.id === "kn-an")!;

  it("thẻ vai trò → thẻ phòng ban; phòng ban chưa mở bị khóa; bấm thẻ là vào", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<RoleContextPicker assignments={an.assignments} onSelect={onSelect} />);
    expect(screen.getByRole("button", { name: /Cấp Xã/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("2 · Phòng ban (của vai trò Cấp Xã)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Phòng Chăn nuôi - Thủy sản — Chưa mở/ })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: /Phòng Trồng trọt — Vào hệ thống/ }));
    expect(onSelect).toHaveBeenCalledWith({ assignment_id: "asg-an-xa", department_code: "trong_trot" });
  });

  it("Admin không chọn phòng ban: bấm thẻ vai trò là vào", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<RoleContextPicker assignments={an.assignments} onSelect={onSelect} />);
    await user.click(screen.getByRole("button", { name: /Admin/ }));
    expect(onSelect).toHaveBeenCalledWith({ assignment_id: "asg-an-admin", department_code: null });
  });

  it("không có phòng ban đang áp dụng → liên hệ Admin", () => {
    const loc = KN_ACCOUNTS.find((a) => a.id === "kn-loc")!;
    render(<RoleContextPicker assignments={loc.assignments} onSelect={() => undefined} />);
    expect(screen.getByRole("alert")).toHaveTextContent("liên hệ quản trị viên");
  });
});

describe("AppShell: nguồn menu và kiểm hiển thị", () => {
  const tinhPrincipal = buildPrincipal({ sub: "u", realm_access: { roles: ["kn_user", "kn_province_admin"] } }, null);
  const active: KnSessionState = { status: "active", assignments: [], context: null, menu: tinh, badges: null };
  const legacy: KnSessionState = { status: "legacy", assignments: [], context: null, menu: null, badges: null };

  it("có ngữ cảnh KN → menu cấu hình; không có → menu cũ theo vai realm (giữ đăng nhập một vai)", () => {
    expect(activeMenu(active, tinhPrincipal)).toBe(tinh);
    expect(activeMenu(legacy, tinhPrincipal).version).toBe("legacy");
  });

  it("/kn/* theo menu của ngữ cảnh; màn hiện có ngoài /kn vẫn theo vai (không xóa tính năng)", () => {
    expect(isAllowed("/kn/nhiem-vu/phat-hanh", active, tinh, tinhPrincipal)).toBe(true);
    expect(isAllowed("/kn/nhiem-vu/cong-viec", active, tinh, tinhPrincipal)).toBe(false);
    expect(isAllowed("/thong-bao", active, tinh, tinhPrincipal)).toBe(true);
    expect(isAllowed("/quan-tri/tai-khoan", active, tinh, tinhPrincipal)).toBe(false);
  });

  it("trang không khung: đăng nhập, chọn vai trò", () => {
    expect(isBareRoute("/login/lien-he")).toBe(true);
    expect(isBareRoute("/chon-vai-tro")).toBe(true);
    expect(isBareRoute("/kn/dashboard")).toBe(false);
  });
});
