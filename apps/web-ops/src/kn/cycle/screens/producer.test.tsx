// KNSPLIT-2-KN-RECORDS (#67): chi tiết và sửa hồ sơ KN riêng (HTX) — Log bên phải, vô hiệu hóa có lý do, sửa giữ người
// đại diện khi để trống và gửi base_version; không còn gọi route ghi HTX legacy.
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProducerDetailScreen, ProducerEditRoute } from "./ProducerScreens";

const push = vi.fn();
const params: Record<string, string> = {};
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/htx",
  useRouter: () => ({ push, replace: vi.fn() }),
  useParams: () => params,
}));

const auth: { role: string; collectApi: Record<string, ReturnType<typeof vi.fn>> } = { role: "kn_xa", collectApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: {},
    kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "trong_trot" } },
    collectApi: auth.collectApi,
  }),
}));

const rec = {
  ref: "rec-1", kind: "cooperative", kind_label: "HTX", commune_code: "91301", name: "HTX Mỹ Hòa", display_name: "HTX Mỹ Hòa",
  representative: { label: "Nguyễn V. A.", masked_phone: "0912•••678" }, holder: null, parent_ref: null, parent_name: null, parent_kind: null,
  htx_ref: null, htx_name: null, area_ha: "120", tax_code: null, address: "Ấp 1", member_count: 1, status: "active", status_reason: null,
  version: 3, can_edit: true, updated_at: "2026-10-01T00:00:00Z",
  log: [
    { version: 3, event: "updated", at: "2026-10-02T00:00:00Z", actor_ref: "a", actor_display: "Cán bộ A", status_reason: null },
    { version: 1, event: "created", at: "2026-10-01T00:00:00Z", actor_ref: "a", actor_display: "Cán bộ A", status_reason: null },
  ],
};
const hh = { ...rec, ref: "hh-1", kind: "household", kind_label: "Nông hộ", name: null, display_name: "Trần T. B.", representative: null, holder: { label: "Trần T. B.", masked_phone: "0987•••111" }, parent_ref: "rec-1", member_count: 0 };

beforeEach(() => {
  push.mockReset();
  params.id = "rec-1";
  auth.role = "kn_xa";
  auth.collectApi = {
    record: vi.fn().mockResolvedValue(rec),
    records: vi.fn().mockResolvedValue({ items: [hh], next_cursor: null }),
    htxCandidates: vi.fn().mockResolvedValue({ items: [] }),
    setRecordStatus: vi.fn().mockResolvedValue({ ...rec, status: "inactive", version: 4 }),
    updateRecord: vi.fn().mockResolvedValue({ ...rec, version: 4 }),
    coopMachines: vi.fn(),
    plots: vi.fn(),
  };
});

describe("Hồ sơ KN riêng — chi tiết, vô hiệu hóa, sửa (#67)", () => {
  it("chi tiết: Log từ nhật ký phiên bản, nông hộ thuộc HTX, nút Sửa / Vô hiệu hóa; lý do < 10 ký tự bị chặn", async () => {
    const u = userEvent.setup();
    render(<ProducerDetailScreen kind="cooperative" />);
    expect(await screen.findByRole("heading", { level: 1, name: /^HTX Mỹ Hòa/ })).toBeInTheDocument();
    expect(screen.getByText("Sửa hồ sơ")).toBeInTheDocument();
    expect(screen.getByText("Tạo hồ sơ")).toBeInTheDocument();
    expect(screen.getByText(/Trần T\. B\./)).toBeInTheDocument();
    expect(auth.collectApi.records).toHaveBeenCalledWith({ kind: "household", parent_ref: "rec-1", status: "all", limit: 200 });
    expect(screen.getByRole("link", { name: "Sửa" })).toHaveAttribute("href", "/kn/du-lieu/htx/rec-1/sua");
    await u.click(screen.getByRole("button", { name: "Vô hiệu hóa" }));
    await u.type(screen.getByLabelText(/Lý do/), "ngắn");
    await u.click(screen.getAllByRole("button", { name: "Vô hiệu hóa" }).at(-1)!);
    expect(screen.getByText("Tối thiểu 10 ký tự.")).toBeInTheDocument();
    expect(auth.collectApi.setRecordStatus).not.toHaveBeenCalled();
    await u.type(screen.getByLabelText(/Lý do/), " - HTX giải thể");
    await u.click(screen.getAllByRole("button", { name: "Vô hiệu hóa" }).at(-1)!);
    await waitFor(() => expect(auth.collectApi.setRecordStatus).toHaveBeenCalledWith("rec-1", true, { reason: "ngắn - HTX giải thể", base_version: 3 }));
  });

  it("Cấp Tỉnh: chỉ xem, không có nút Sửa / Vô hiệu hóa", async () => {
    auth.role = "kn_tinh";
    auth.collectApi.record = vi.fn().mockResolvedValue({ ...rec, can_edit: false });
    render(<ProducerDetailScreen kind="cooperative" />);
    expect(await screen.findByRole("heading", { level: 1, name: /^HTX Mỹ Hòa/ })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Sửa" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Vô hiệu hóa" })).toBeNull();
  });

  it("sửa: điền sẵn, để trống người đại diện = giữ người cũ; gửi base_version; tên rỗng bị chặn", async () => {
    const u = userEvent.setup();
    render(<ProducerEditRoute kind="cooperative" />);
    const name = await screen.findByDisplayValue("HTX Mỹ Hòa");
    await u.clear(name);
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getAllByRole("alert").map((a) => a.textContent)).toContain("Bắt buộc nhập.");
    expect(auth.collectApi.updateRecord).not.toHaveBeenCalled();
    await u.type(name, "HTX Mỹ Hòa Mới");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() =>
      expect(auth.collectApi.updateRecord).toHaveBeenCalledWith("rec-1", { kind: "cooperative", commune_code: "91301", name: "HTX Mỹ Hòa Mới", area_ha: "120", address: "Ấp 1", tax_code: null, htx_ref: null, base_version: 3 }),
    );
    expect(push).toHaveBeenCalledWith("/kn/du-lieu/htx/rec-1");
  });
});
