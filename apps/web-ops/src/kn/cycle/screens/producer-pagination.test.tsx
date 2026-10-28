// KNSPLIT-2-KN-RECORDS (#67, Codex P2): danh sách hồ sơ KN phân trang theo con trỏ của máy chủ (/v1/kn/producer-records
// mặc định 50, tối đa 200, next_cursor) — hồ sơ thứ 51 trở đi phải xem được; đổi bộ lọc về trang 1 (bỏ con trỏ);
// danh mục HTX/THT cha và nông hộ của HTX đọc đủ mọi trang, mỗi lượt vẫn ≤ 200.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { loadAllRecords, ProducerDetailScreen, ProducerFormScreen, ProducerListScreen } from "./ProducerScreens";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/du-lieu/htx",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({ id: "c-001" }),
}));

type Row = { ref: string; kind: string; name: string | null; parent_ref: string | null } & Record<string, unknown>;
type Q = { kind: string; q?: string; parent_ref?: string; cursor?: string; limit?: number };
/** Máy chủ giả: lọc như /v1/kn/producer-records, limit mặc định 50, > 200 → lỗi (như máy chủ 422), cursor = ref cuối trang trước. */
function server(db: Record<string, Row[]>) {
  return vi.fn(async (q: Q) => {
    const limit = q.limit ?? 50;
    if (limit < 1 || limit > 200) throw new Error(`limit ${limit} vượt giới hạn máy chủ`);
    const rows = (db[q.kind] ?? []).filter((r) => (!q.q || (r.name ?? "").includes(q.q)) && (!q.parent_ref || r.parent_ref === q.parent_ref));
    const start = q.cursor ? rows.findIndex((r) => r.ref === q.cursor) + 1 : 0;
    const items = rows.slice(start, start + limit);
    return { items, next_cursor: start + limit < rows.length ? items.at(-1)!.ref : null };
  });
}
const auth: { collectApi: { records: ReturnType<typeof server> } & Record<string, unknown> } = { collectApi: { records: server({}) } };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: {},
    kn: { context: { role: "kn_xa", communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "trong_trot" } },
    collectApi: auth.collectApi,
  }),
}));

const pad = (i: number) => String(i).padStart(3, "0");
const base = {
  commune_code: "91301", representative: null, holder: null, parent_ref: null as string | null, parent_name: null, parent_kind: null, htx_ref: null, htx_name: null,
  area_ha: "2", tax_code: null, address: null, member_count: 0, status: "active", status_reason: null, version: 1, can_edit: true, updated_at: "2026-10-01T00:00:00Z", log: [],
};
const coop = (i: number): Row => ({ ...base, ref: `c-${pad(i)}`, kind: "cooperative", kind_label: "HTX", name: `HTX số ${pad(i)}`, display_name: `HTX số ${pad(i)}` });
const group = (i: number): Row => ({ ...base, ref: `g-${pad(i)}`, kind: "coop_group", kind_label: "Tổ hợp tác", name: `THT số ${pad(i)}`, display_name: `THT số ${pad(i)}` });
const household = (i: number): Row => ({ ...base, ref: `h-${pad(i)}`, kind: "household", kind_label: "Nông hộ", name: null, display_name: `Hộ số ${pad(i)}`, parent_ref: "c-001" });


beforeEach(() => {
  auth.collectApi = {
    records: server({
      cooperative: Array.from({ length: 51 }, (_, i) => coop(i + 1)),
      coop_group: Array.from({ length: 205 }, (_, i) => group(i + 1)),
      household: Array.from({ length: 230 }, (_, i) => household(i + 1)),
    }),
    record: vi.fn().mockResolvedValue({ ...coop(1), log: [] }),
    htxCandidates: vi.fn().mockResolvedValue({ items: [] }),
  };
});

describe("#67 phân trang máy chủ theo con trỏ", () => {
  it("51 HTX: đi tới trang 6 thấy hồ sơ thứ 51 (gửi cursor), Trang sau tắt ở trang cuối; Trang trước quay lại", async () => {
    const u = userEvent.setup();
    render(<ProducerListScreen kind="cooperative" />);
    const table = await screen.findByRole("table", { name: "Danh sách HTX" });
    expect(within(table).getByText("HTX số 001")).toBeInTheDocument();
    expect(auth.collectApi.records).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "cooperative", limit: 10 }));
    expect(auth.collectApi.records.mock.lastCall![0]).not.toHaveProperty("cursor");
    for (let p = 2; p <= 6; p++) {
      await u.click(screen.getByRole("button", { name: "Trang sau" }));
      expect(await screen.findByText(`HTX số ${pad((p - 1) * 10 + 1)}`)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: `Trang ${p}` })).toHaveAttribute("aria-current", "page");
    }
    expect(await screen.findByText("HTX số 051")).toBeInTheDocument();
    expect(screen.queryByText("HTX số 050")).toBeNull();
    expect(auth.collectApi.records).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "cooperative", cursor: "c-050", limit: 10 }));
    // STT nối tiếp theo trang (dòng 51).
    expect(within(screen.getByRole("table", { name: "Danh sách HTX" })).getByText("51")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Trang sau" })).toBeDisabled();
    await u.click(screen.getByRole("button", { name: "Trang trước" }));
    expect(await screen.findByText("HTX số 050")).toBeInTheDocument();
    expect(auth.collectApi.records).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: "c-040" }));
  });

  it("đổi bộ lọc (tìm theo tên) → về trang 1, bỏ cursor", async () => {
    const u = userEvent.setup();
    render(<ProducerListScreen kind="cooperative" />);
    await screen.findByText("HTX số 001");
    await u.click(screen.getByRole("button", { name: "Trang sau" }));
    expect(await screen.findByText("HTX số 011")).toBeInTheDocument();
    expect(auth.collectApi.records).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: "c-010" }));
    await u.type(screen.getByPlaceholderText("Tìm theo tên HTX"), "05");
    await waitFor(() => expect(auth.collectApi.records).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "cooperative", q: "05", limit: 10 })));
    expect(auth.collectApi.records.mock.lastCall![0]).not.toHaveProperty("cursor");
    expect(await screen.findByRole("button", { name: "Trang 1" })).toBeInTheDocument();
    expect(await screen.findByText("HTX số 050")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Trang trước" })).toBeDisabled();
  });

  it("đổi Trạng thái cũng bỏ cursor", async () => {
    const u = userEvent.setup();
    render(<ProducerListScreen kind="cooperative" />);
    await screen.findByText("HTX số 001");
    await u.click(screen.getByRole("button", { name: "Trang sau" }));
    await screen.findByText("HTX số 011");
    await u.selectOptions(screen.getByLabelText("Trạng thái"), "all");
    await waitFor(() => expect(auth.collectApi.records).toHaveBeenLastCalledWith(expect.objectContaining({ status: "all" })));
    expect(auth.collectApi.records.mock.lastCall![0]).not.toHaveProperty("cursor");
    expect(await screen.findByText("HTX số 001")).toBeInTheDocument();
  });
});

describe("#67 danh mục cha và nông hộ đọc đủ mọi trang (mỗi lượt ≤ 200)", () => {
  it("loadAllRecords đi theo next_cursor tới hết; con trỏ lặp lại → lỗi, không trả thiếu", async () => {
    const all = await loadAllRecords(auth.collectApi as never, { kind: "coop_group" });
    expect(all).toHaveLength(205);
    expect(auth.collectApi.records.mock.calls.map((c) => (c[0] as Q).limit)).toEqual([200, 200]);
    const loop = vi.fn().mockResolvedValue({ items: [coop(1)], next_cursor: "c-001" });
    await expect(loadAllRecords({ records: loop } as never, { kind: "cooperative" })).rejects.toThrow(/lặp lại/);
  });

  it("Danh sách Nông hộ: ô lọc Thuộc HTX/THT có THT thứ 205 (sau trang 200 đầu)", async () => {
    render(<ProducerListScreen kind="household" />);
    expect(await screen.findByRole("option", { name: "Tổ hợp tác: THT số 205" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "HTX: HTX số 051" })).toBeInTheDocument();
  });

  it("Thêm nông hộ: ô chọn HTX/THT có THT thứ 205", async () => {
    render(<ProducerFormScreen kind="household" back={{ href: "/kn/du-lieu/nong-ho", label: "Nông hộ" }} />);
    expect(await screen.findByRole("option", { name: "Tổ hợp tác: THT số 205" })).toBeInTheDocument();
  });

  it("Chi tiết HTX: tab Nông hộ đếm đủ 230 hộ (hai lượt 200 + 30)", async () => {
    render(<ProducerDetailScreen kind="cooperative" />);
    expect(await screen.findByRole("tab", { name: "Nông hộ (230)" })).toBeInTheDocument();
    expect(screen.getByText(/Hộ số 230/)).toBeInTheDocument();
  });
});
