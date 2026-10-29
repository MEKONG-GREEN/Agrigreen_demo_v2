// AUDIT-LOG-KN (#73): màn Admin "Nhật ký hệ thống" — STT, bộ lọc có giới hạn (≤ 93 ngày, không gọi máy chủ khi sai),
// phân trang bằng cursor máy chủ (giữ bộ lọc), độ phủ nguồn, tên người thao tác từ danh sách tài khoản, trang Chi tiết
// (DetailPage) hiện dòng đã tải mà không gọi lại máy chủ.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AuditItem, AuditPage } from "../cycle/api";
import { AuditDetailScreen } from "./AuditDetailScreen";
import { AUDIT_MAX_DAYS, AuditLogScreen, auditFilterError, auditQuery, defaultAuditFilters } from "./AuditLogScreen";
import { resetAuditSelection } from "./selection";

const params: { id: string } = { id: "" };
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/quan-tri/nhat-ky-he-thong",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => params,
}));
const auth: { knAdminApi: Record<string, ReturnType<typeof vi.fn>> } = { knAdminApi: {} };
const auditMock = () => auth.knAdminApi.audit!;
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: {}, kn: { context: { role: "admin" } }, knAdminApi: auth.knAdminApi }) }));

const AN = "0192a000-0000-7000-8000-0000000000a1";
const item = (i: number, x: Partial<AuditItem> = {}): AuditItem => ({
  source: "kn.period_event",
  module: "kn.report",
  event_ref: `0192a000-0000-7000-8000-00000000000${i}`,
  entity_ref: `0192a000-0000-7000-8000-0000000000e${i}`,
  actor_ref: AN,
  occurred_at: "2026-10-28T03:00:00Z",
  recorded_at: "2026-10-28T03:00:00Z",
  department: "trong_trot",
  action: "period.reopened",
  outcome: "succeeded",
  changes: [{ field: "batch_ref", after: { kind: "ref", ref: AN } }],
  ...x,
});
const page = (items: AuditItem[], next: string | null): AuditPage => ({
  items,
  sources: ["accounts.account_event", "kn.audit_view", "kn.period_event"],
  as_of: "2026-10-29T03:00:00Z",
  next_cursor: next,
});

beforeEach(() => {
  resetAuditSelection();
  auth.knAdminApi = {
    users: vi.fn().mockResolvedValue({ items: [{ id: AN, username: "an", first_name: "An", last_name: "Nguyễn Văn" }], next_cursor: null }),
    departments: vi.fn().mockResolvedValue({ org_ref: "x", items: [{ code: "trong_trot", name: "Trồng trọt" }] }),
    audit: vi
      .fn()
      .mockResolvedValueOnce(page([item(1), item(2, { action: "producer.created", module: "kn.records", source: "kn.producer", department: undefined })], "c2"))
      .mockResolvedValueOnce(page([item(3, { action: "audit.denied", module: "kn.audit", source: "kn.audit_view", outcome: "denied" })], null))
      .mockResolvedValue(page([], null)),
  };
});

describe("AUDIT-LOG-KN — bộ lọc", () => {
  it("khoảng ngày giờ VN, tối đa 93 ngày; bộ lọc rỗng không gửi", () => {
    const f = defaultAuditFilters("2026-10-29");
    expect(f.fromDate).toBe("2026-10-23");
    expect(auditFilterError(f)).toBeNull();
    const q = auditQuery(f);
    expect(q.from).toBe("2026-10-22T17:00:00.000Z");
    expect(q.to).toBe("2026-10-29T17:00:00.000Z");
    expect(q.module).toBeUndefined();
    expect(q.limit).toBe(50);
    expect(auditFilterError({ ...f, fromDate: "2026-07-28" })).toBe(`Khoảng thời gian tối đa ${AUDIT_MAX_DAYS} ngày.`);
    expect(auditFilterError({ ...f, fromDate: "2026-07-29" })).toBeNull();
    expect(auditFilterError({ ...f, fromDate: "2026-10-30" })).toMatch(/trước hoặc bằng/);
    expect(auditFilterError({ ...f, toDate: "" })).toMatch(/Chọn đủ/);
  });
});

describe("AUDIT-LOG-KN — màn Nhật ký hệ thống", () => {
  it("STT, tên người thao tác, nhãn thao tác, độ phủ nguồn; Trang sau gửi cursor cùng bộ lọc, STT nối tiếp", async () => {
    const u = userEvent.setup();
    render(<AuditLogScreen />);
    const table = await screen.findByRole("table", { name: "Nhật ký hệ thống" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("1");
    expect(rows[1]).toHaveTextContent("Mở lại kỳ báo cáo");
    expect(rows[1]).toHaveTextContent("Trồng trọt");
    await waitFor(() => expect(rows[1]).toHaveTextContent("Nguyễn Văn An"));
    expect(rows[2]).toHaveTextContent("Tạo hồ sơ");
    expect(screen.getByTestId("audit-sources")).toHaveTextContent("Sổ tài khoản, Sổ lượt xem nhật ký, Sổ kỳ báo cáo");
    expect(screen.getByTestId("audit-sources")).toHaveTextContent("không phải mọi thao tác");
    expect(auth.knAdminApi.audit).toHaveBeenCalledTimes(1);
    const first = auditMock().mock.calls[0]![0];
    expect(first.cursor).toBeUndefined();

    await u.click(screen.getByRole("button", { name: "Trang sau" }));
    await waitFor(() => expect(auth.knAdminApi.audit).toHaveBeenCalledTimes(2));
    const second = auditMock().mock.calls[1]![0];
    expect(second).toEqual({ ...first, cursor: "c2" });
    const r2 = within(await screen.findByRole("table", { name: "Nhật ký hệ thống" })).getAllByRole("row");
    await waitFor(() => expect(r2[1]).toHaveTextContent("51"));
    expect(screen.getByRole("button", { name: "Trang sau" })).toBeDisabled();
  }, 30_000);

  it("bộ lọc sai (> 93 ngày) → báo lỗi, không gọi máy chủ; bộ lọc đúng → về trang đầu với module + thao tác", async () => {
    const u = userEvent.setup();
    render(<AuditLogScreen />);
    await screen.findByRole("table", { name: "Nhật ký hệ thống" });
    fireEvent.change(screen.getByLabelText(/Từ ngày/), { target: { value: "2026-01-01" } });
    await u.click(screen.getByRole("button", { name: "Xem" }));
    expect(screen.getByRole("alert")).toHaveTextContent("tối đa 93 ngày");
    expect(auth.knAdminApi.audit).toHaveBeenCalledTimes(1);

    const today = defaultAuditFilters();
    fireEvent.change(screen.getByLabelText(/Từ ngày/), { target: { value: today.fromDate } });
    await u.selectOptions(screen.getByLabelText("Module"), "kn.cnts");
    await u.selectOptions(screen.getByLabelText("Thao tác"), "cnts_grant.revoked");
    await u.click(screen.getByRole("button", { name: "Xem" }));
    await waitFor(() => expect(auth.knAdminApi.audit).toHaveBeenCalledTimes(2));
    expect(auditMock().mock.calls[1]![0]).toMatchObject({ module: "kn.cnts", action: "cnts_grant.revoked", cursor: undefined });
  }, 30_000);

  it("Chi tiết (DetailPage) hiện dòng đã tải, không gọi lại máy chủ; dòng không còn trong bộ nhớ → yêu cầu mở lại", async () => {
    const { unmount } = render(<AuditLogScreen />);
    await screen.findByRole("table", { name: "Nhật ký hệ thống" });
    await waitFor(() => expect(screen.getAllByText("Nguyễn Văn An").length).toBeGreaterThan(0));
    unmount();
    params.id = item(1).event_ref;
    render(<AuditDetailScreen />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Mở lại kỳ báo cáo");
    expect(screen.getByText("Đợt báo cáo")).toBeInTheDocument();
    expect(screen.getAllByText("Nguyễn Văn An").length).toBeGreaterThan(0);
    expect(auth.knAdminApi.audit).toHaveBeenCalledTimes(1);
  }, 30_000);

  it("Chi tiết khi không có dữ liệu trong bộ nhớ", () => {
    params.id = "0192a000-0000-7000-8000-0000000000ff";
    render(<AuditDetailScreen />);
    expect(screen.getByText(/Mở lại từ danh sách/)).toBeInTheDocument();
  });
});
