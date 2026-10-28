// BF-TT-HTX (#102): TT-08 (bắt buộc Xã/phường, Loại, Tên, Người đại diện, Điện thoại; STT; có sửa) và TT-09 (ba danh
// sách HTX / Nông hộ / Tổ hợp tác, mỗi danh sách bộ lọc riêng) trên hồ sơ KN riêng của #67 (/v1/kn/producer-records).
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProducerCreateRoute, ProducerListScreen } from "./ProducerScreens";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/htx",
  useRouter: () => ({ push, replace: vi.fn() }),
  useParams: () => ({}),
}));

const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>> } = { role: "kn_xa", collectApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: {},
    kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }, { code: "91302", name: "Bình Đức" }], department_code: "trong_trot" } },
    collectApi: auth.collectApi,
  }),
}));

const base = {
  commune_code: "91301", representative: null, holder: null, parent_ref: null, parent_name: null, parent_kind: null, htx_ref: null, htx_name: null,
  area_ha: "2", tax_code: null, address: null, member_count: 0, status: "active", status_reason: null, version: 1, can_edit: true, updated_at: "2026-10-01T00:00:00Z",
};
const htx = { ...base, ref: "c-1", kind: "cooperative", kind_label: "HTX", name: "HTX Mỹ Hòa", display_name: "HTX Mỹ Hòa", representative: { label: "Nguyễn V. A.", masked_phone: "0912•••678" }, member_count: 1 };
const tht = { ...base, ref: "g-1", kind: "coop_group", kind_label: "Tổ hợp tác", name: "THT Rau sạch", display_name: "THT Rau sạch", representative: { label: "Lê T. M.", masked_phone: "0914•••200" } };
const hh = { ...base, ref: "h-1", kind: "household", kind_label: "Nông hộ", name: null, display_name: "Trần T. B.", holder: { label: "Trần T. B.", masked_phone: "0987•••111" }, parent_ref: "c-1", parent_name: "HTX Mỹ Hòa", parent_kind: "cooperative" };

beforeEach(() => {
  push.mockReset();
  auth.role = "kn_xa";
  auth.collectApi = {
    records: vi.fn().mockImplementation(async (q: { kind: string }) => ({ items: q.kind === "cooperative" ? [htx] : q.kind === "coop_group" ? [tht] : [hh], next_cursor: null })),
    htxCandidates: vi.fn().mockResolvedValue({ items: [] }),
    createRecord: vi.fn().mockImplementation(async (b: { kind: string }) => ({ ...hh, ref: "new-1", kind: b.kind })),
  };
});

describe("TT-09: ba danh sách riêng, bộ lọc riêng", () => {
  it("Nông hộ: tab đang chọn, cột Thuộc HTX/THT, lọc theo HTX/THT (không tìm theo tên), STT, nút Thêm và Sửa", async () => {
    const u = userEvent.setup();
    render(<ProducerListScreen kind="household" />);
    const tabs = screen.getByRole("tablist", { name: "Loại hồ sơ" });
    expect(within(tabs).getByRole("tab", { name: "Nông hộ" })).toHaveAttribute("aria-selected", "true");
    expect(within(tabs).getByRole("tab", { name: "HTX" })).toHaveAttribute("href", "/kn/du-lieu/htx");
    expect(within(tabs).getByRole("tab", { name: "Tổ hợp tác" })).toHaveAttribute("href", "/kn/du-lieu/htx/to-hop-tac");
    const table = await screen.findByRole("table", { name: "Danh sách Nông hộ" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(within(table).getByRole("columnheader", { name: "Thuộc HTX/THT" })).toBeInTheDocument();
    expect(within(table).getByText("HTX Mỹ Hòa")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/Tìm theo tên/)).toBeNull();
    expect(screen.getByRole("link", { name: "Thêm Nông hộ" })).toHaveAttribute("href", "/kn/du-lieu/htx/nong-ho/them-moi");
    expect(screen.getByRole("link", { name: "Sửa Nông hộ Trần T. B." })).toHaveAttribute("href", "/kn/du-lieu/htx/nong-ho/h-1/sua");
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX: HTX Mỹ Hòa" })).toBeInTheDocument());
    await u.selectOptions(screen.getByLabelText("Thuộc HTX/THT"), "c-1");
    await waitFor(() => expect(auth.collectApi.records).toHaveBeenLastCalledWith({ kind: "household", commune: undefined, status: "active", q: undefined, parent_ref: "c-1" }));
  });

  it("Tổ hợp tác: tìm theo tên, cột Người đại diện + Nông hộ; Cấp Tỉnh không có nút Thêm/Sửa", async () => {
    auth.role = "kn_tinh";
    const u = userEvent.setup();
    render(<ProducerListScreen kind="coop_group" />);
    const table = await screen.findByRole("table", { name: "Danh sách Tổ hợp tác" });
    expect(within(table).getByRole("columnheader", { name: "Tên Tổ hợp tác" })).toBeInTheDocument();
    expect(within(table).getByRole("columnheader", { name: "Người đại diện" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Thêm Tổ hợp tác" })).toBeNull();
    expect(screen.queryByRole("link", { name: /^Sửa / })).toBeNull();
    await u.type(screen.getByPlaceholderText("Tìm theo tên Tổ hợp tác"), "rau");
    await waitFor(() => expect(auth.collectApi.records).toHaveBeenLastCalledWith({ kind: "coop_group", commune: undefined, status: "active", q: "rau", parent_ref: undefined }));
  });
});

describe("TT-08: Thêm hồ sơ có ô Loại, bắt buộc theo loại", () => {
  it("Loại Tổ hợp tác: thiếu Xã/phường, Tên, Người đại diện, Điện thoại → lỗi theo trường, không gọi API", async () => {
    const u = userEvent.setup();
    render(<ProducerCreateRoute kind="coop_group" />);
    expect(screen.getByLabelText(/Loại/)).toHaveValue("coop_group");
    expect(screen.getByLabelText(/Tên Tổ hợp tác/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/Mã số thuế/)).toBeNull(); // MST chỉ cho HTX
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByRole("alert").filter((a) => a.textContent === "Bắt buộc nhập.")).toHaveLength(4);
    await u.type(screen.getByLabelText(/Tên Tổ hợp tác/), "THT Lúa");
    await u.selectOptions(screen.getByLabelText(/Xã\/phường/), "91302");
    await u.type(screen.getByLabelText(/^Người đại diện/), "Phạm Văn Dũng");
    await u.type(screen.getByLabelText(/Điện thoại/), "12345");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByText("Số điện thoại 10 chữ số, bắt đầu bằng 0.")).toBeInTheDocument();
    expect(auth.collectApi.createRecord).not.toHaveBeenCalled();
  });

  it("đổi Loại sang Nông hộ: ẩn Tên, hiện Chủ hộ + Thuộc HTX/THT; lưu gửi holder + parent_ref rồi mở chi tiết", async () => {
    const u = userEvent.setup();
    render(<ProducerCreateRoute kind="cooperative" />);
    await u.selectOptions(screen.getByLabelText(/Loại/), "household");
    expect(screen.queryByLabelText(/Tên HTX/)).toBeNull();
    await u.selectOptions(screen.getByLabelText(/Xã\/phường/), "91301");
    await u.type(screen.getByLabelText(/^Chủ hộ/), "Trần Thị Bích");
    await u.type(screen.getByLabelText(/Điện thoại/), "0987000111");
    await waitFor(() => expect(screen.getByRole("option", { name: "HTX: HTX Mỹ Hòa" })).toBeInTheDocument());
    await u.selectOptions(screen.getByLabelText(/Thuộc HTX\/Tổ hợp tác/), "c-1");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() =>
      expect(auth.collectApi.createRecord).toHaveBeenCalledWith({ kind: "household", commune_code: "91301", area_ha: null, parent_ref: "c-1", holder: { full_name: "Trần Thị Bích", phone: "0987000111" } }),
    );
    expect(push).toHaveBeenCalledWith("/kn/du-lieu/htx/nong-ho/new-1");
  });
});
