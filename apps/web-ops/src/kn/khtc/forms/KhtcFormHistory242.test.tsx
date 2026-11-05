import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KhtcFormHistoryScreen } from "./KhtcFormHistoryScreen";
import type { SubmissionSnapshot } from "./submission-history";
import { DEPT242, FIRST242, ITEM242, PERIOD242, SECOND242, history242, snapshot242 } from "./history242-testdata";

const state = vi.hoisted(() => ({ period: "", submission: undefined as string | undefined, search: "", api: {} as object,
  status: "authenticated", knStatus: "active", principal: { tenant_id: "tenant-a", actor_ref: "actor-a", amr: ["otp"] },
  context: { context_id: "ctx-a", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", read_only: false } }));
const calls = vi.hoisted(() => ({ history: vi.fn(), submission: vi.fn(), push: vi.fn() }));
vi.mock("next/navigation", () => ({ useParams: () => ({ periodId: state.period, submissionId: state.submission }), useSearchParams: () => new URLSearchParams(state.search), useRouter: () => ({ push: calls.push }) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: state.api, status: state.status, principal: state.principal, kn: { status: state.knStatus, context: state.context } }) }));
vi.mock("@/shell/Breadcrumb", () => ({ useBreadcrumbTail: () => undefined }));
vi.mock("./submission-history", async original => ({ ...await original<typeof import("./submission-history")>(), createSubmissionHistoryApi: () => calls }));
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-06-03T09:00:00Z"));
  state.period = PERIOD242; state.submission = undefined; state.search = `department=${DEPT242}`; state.api = {};
  state.status = "authenticated"; state.knStatus = "active";
  state.principal = { tenant_id: "tenant-a", actor_ref: "actor-a", amr: ["otp"] };
  state.context = { context_id: "ctx-a", role: "kn_tinh", department_code: "ke_hoach_tai_chinh", read_only: false };
  Object.values(calls).forEach(call => call.mockReset());
  calls.history.mockResolvedValue(history242());
  calls.submission.mockImplementation((_period, _dept, id) => Promise.resolve(snapshot242(id, id === FIRST242 ? "10" : "12")));
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("history242 — trang lịch sử và nội dung chỉ đọc", () => {
  it("danh sách có STT, deep link đúng lần gửi và thông tin actor đã ghi nhận", async () => {
    render(<KhtcFormHistoryScreen />);
    const table = await screen.findByRole("table", { name: "Các lần gửi phiếu" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "Lần 1" })).toHaveAttribute("href", `/kn/khtc/ra-soat/${PERIOD242}/lan-gui/${FIRST242}?department=${DEPT242}`);
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(calls.submission).not.toHaveBeenCalled();
  });
  it("chi tiết và so sánh dùng hai payload cố định, tô vàng kèm nhãn và không có thao tác ghi", async () => {
    state.submission = FIRST242; state.search += `&compare_with=${SECOND242}`;
    render(<KhtcFormHistoryScreen />);
    const table = await screen.findByRole("table", { name: "So sánh nội dung các lần gửi" });
    const changed = within(table).getByText("Diện tích mô hình · Thực hiện trong kỳ").closest("tr")!;
    expect(within(changed).getByText("10")).toBeInTheDocument(); expect(within(changed).getByText("12")).toBeInTheDocument();
    expect(within(changed).getByText("Đã thay đổi")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("1 ô thay đổi");
    expect(screen.getByRole("complementary", { name: "Log gửi và rà soát" })).toHaveTextContent("Gửi phiếu lần 1");
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Lưu|Gửi|Xác nhận/ })).not.toBeInTheDocument();
    expect(screen.queryByText("Đạt 3 quy tắc kiểm tra.")).not.toBeInTheDocument();
    expect(calls.submission.mock.calls).toEqual([[PERIOD242, DEPT242, FIRST242], [PERIOD242, DEPT242, SECOND242]]);
  });
  it("chọn lần gửi khác giữ deep link, bỏ so sánh xóa đúng query", async () => {
    state.submission = FIRST242; render(<KhtcFormHistoryScreen />);
    const select = await screen.findByRole("combobox", { name: "So sánh với" });
    fireEvent.change(select, { target: { value: SECOND242 } });
    expect(calls.push).toHaveBeenLastCalledWith(`/kn/khtc/ra-soat/${PERIOD242}/lan-gui/${FIRST242}?department=${DEPT242}&compare_with=${SECOND242}`);
    fireEvent.change(select, { target: { value: "" } });
    expect(calls.push).toHaveBeenLastCalledWith(`/kn/khtc/ra-soat/${PERIOD242}/lan-gui/${FIRST242}?department=${DEPT242}`);
  });
  it("không đọc nội dung của lần gửi ngoài danh sách native hiện tại", async () => {
    state.submission = ITEM242; render(<KhtcFormHistoryScreen />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Lần gửi không thuộc phiếu đang đọc"); expect(calls.submission).not.toHaveBeenCalled();
  });
  it.each(["department=unknown", "department=trong_trot&department=hanh_chinh_tong_hop", `department=${DEPT242}&compare_with=1e100000`])("query không hợp lệ dừng trước I/O: %s", query => {
    state.search = query; render(<KhtcFormHistoryScreen />); expect(screen.getByRole("alert")).toHaveTextContent("Đường dẫn lịch sử gửi không hợp lệ"); expect(calls.history).not.toHaveBeenCalled();
  });
  it.each(["anonymous", "inactive", "national", "commune"])("ngữ cảnh %s không gọi đọc dữ liệu", kind => {
    if (kind === "anonymous") state.status = "anonymous";
    if (kind === "inactive") state.knStatus = "inactive";
    if (kind === "national") state.context.role = "kn_tw";
    if (kind === "commune") state.context.role = "kn_xa";
    render(<KhtcFormHistoryScreen />); expect(screen.getByRole("alert")).toBeInTheDocument(); expect(calls.history).not.toHaveBeenCalled();
  });
});

describe("history242 — tài nguyên và quyền tạo lifetime mới", () => {
  it.each(["api", "tenant", "context", "role", "mfa", "department"])("đổi %s loại nội dung cũ ngay trước GET mới", async kind => {
    state.submission = FIRST242; const view = render(<KhtcFormHistoryScreen />); await screen.findByRole("table", { name: "Nội dung đã gửi" });
    calls.history.mockReturnValue(new Promise(() => undefined));
    if (kind === "api") state.api = {};
    if (kind === "tenant") state.principal = { ...state.principal, tenant_id: "tenant-b" };
    if (kind === "context") state.context = { ...state.context, context_id: "ctx-b" };
    if (kind === "role") state.context = { ...state.context, role: "admin" };
    if (kind === "mfa") state.principal = { ...state.principal, amr: [] };
    if (kind === "department") state.context = { ...state.context, department_code: "trong_trot" };
    view.rerender(<KhtcFormHistoryScreen />); expect(screen.queryByRole("table", { name: "Nội dung đã gửi" })).not.toBeInTheDocument();
  });
  it("route A→B→A không nhận GET muộn của lifetime A cũ", async () => {
    const old = deferred<SubmissionSnapshot>(); calls.submission.mockReturnValueOnce(old.promise);
    state.submission = FIRST242; const view = render(<KhtcFormHistoryScreen />); await waitFor(() => expect(calls.submission).toHaveBeenCalledTimes(1));
    state.submission = SECOND242; view.rerender(<KhtcFormHistoryScreen />); await screen.findByRole("heading", { name: /Lần gửi 2/ });
    state.submission = FIRST242; view.rerender(<KhtcFormHistoryScreen />); await screen.findByRole("heading", { name: /Lần gửi 1/ });
    const stale = snapshot242(); stale.content.texts.summary = "Nội dung lifetime cũ";
    await act(async () => old.resolve(stale));
    expect(screen.queryByText("Nội dung lifetime cũ")).not.toBeInTheDocument(); expect(screen.getByText("Lời văn đã gửi")).toBeInTheDocument();
  });
});
