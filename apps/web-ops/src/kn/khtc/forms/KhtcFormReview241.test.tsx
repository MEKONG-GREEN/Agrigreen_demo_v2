import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KhtcFormReviewScreen } from "./KhtcFormScreens";
import type { KhtcFormReviewInput, KhtcFormReviewResult, KhtcFormSheet } from "./api";
import type * as FormApi from "./api";
import type * as PeriodApi from "../periods/api";
import { ApiProblem } from "@/api/problem";

const state = vi.hoisted(() => ({
  id: "11111111-1111-4111-8111-111111111111", search: "", api: {} as object,
  status: "authenticated", knStatus: "active",
  principal: { actor_ref: "actor-a", tenant_id: "tenant-a", amr: ["mfa"] },
  context: { context_id: "ctx-a", department_code: "ke_hoach_tai_chinh", role: "kn_tinh", read_only: false },
}));
const calls = vi.hoisted(() => ({ get: vi.fn(), review: vi.fn(), period: vi.fn(), push: vi.fn() }));
vi.mock("next/navigation", () => ({ useParams: () => ({ periodId: state.id }), useSearchParams: () => new URLSearchParams(state.search), useRouter: () => ({ push: calls.push }) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: state.api, status: state.status, principal: state.principal, kn: { status: state.knStatus, context: state.context } }) }));
vi.mock("@/shell/Breadcrumb", () => ({ useBreadcrumbTail: () => undefined }));
vi.mock("./api", async original => ({ ...await original<typeof FormApi>(), createKhtcFormApi: () => calls }));
vi.mock("../periods/api", async original => ({ ...await original<typeof PeriodApi>(), createKhtcPeriodApi: () => ({ get: calls.period }) }));
const A = "11111111-1111-4111-8111-111111111111", B = "22222222-2222-4222-8222-222222222222", SID = "33333333-3333-4333-8333-333333333333";
function sheet(name = "Kỳ A", id = state.id, department = "trong_trot"): KhtcFormSheet {
  return {
    period: { id, name, report_type: "week", year: 2030, from_date: "2030-06-01", to_date: "2030-06-07", cutoff_date: "2030-06-01", due_at: "2030-06-07T10:00:00Z", max_submissions: 3, terminal: "" },
    department, department_name: department === "trong_trot" ? "Trồng trọt" : "Chăn nuôi", state: "submitted", state_label: "Đã gửi", attempt: 1,
    limit_reached: false, can_request_supplement: true, can_confirm: true, can_review: true, editable: false, overdue: false, request_note: "",
    template: { code: "KHTC-PHIEU", version: 2, department, title: "Phiếu", requirements: "", columns: [], sections: [{ key: "summary", title: "Kết quả", required: true }] },
    grid: false, plan_missing: false, plan: null, tabs: [], content: { cells: {}, adjustments: [], texts: { summary: "Nguồn phòng chuyên môn" } },
    content_source: "submitted", draft_at: null, adj_periods: [], issues: [],
    submissions: [{ id: SID, attempt: 1, submitted_at: "2030-06-03T08:00:00Z", late: false, decision: "", review_note: "", reviewed_at: null }], valid: true,
  };
}
function ack(input: KhtcFormReviewInput, id = state.id, dept = "trong_trot"): KhtcFormReviewResult {
  return { ...sheet("Kỳ A", id, dept), can_review: false, can_confirm: false, can_request_supplement: false,
    reviewed_submission_id: input.expected_submission_id, reviewed_attempt: input.base_attempt, review_decision: input.decision };
}
function deferred<T>() { let resolve!: (v: T) => void; let reject!: (e: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-06-03T09:00:00Z"));
  state.id = A; state.search = ""; state.api = {}; state.status = "authenticated"; state.knStatus = "active";
  state.principal = { actor_ref: "actor-a", tenant_id: "tenant-a", amr: ["mfa"] };
  state.context = { context_id: "ctx-a", department_code: "ke_hoach_tai_chinh", role: "kn_tinh", read_only: false };
  for (const call of Object.values(calls)) call.mockReset();
  calls.period.mockImplementation(id => Promise.resolve({ id, departments: [{ code: "trong_trot", name: "Trồng trọt" }, { code: "chan_nuoi_thuy_san", name: "Chăn nuôi" }] }));
  calls.get.mockImplementation((id, dept) => Promise.resolve(sheet("Kỳ A", id, dept)));
  calls.review.mockImplementation((id, dept, input) => Promise.resolve(ack(input, id, dept)));
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("review241 — quyết định gắn với lần gửi đã đọc", () => {
  it("xác nhận trước POST và giữ đúng target, phiên bản, nội dung", async () => {
    render(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "  Kiểm tra số liệu  " } });
    fireEvent.click(screen.getByRole("button", { name: "Yêu cầu bổ sung" }));
    expect(calls.review).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog", { name: "Xác nhận yêu cầu bổ sung" });
    expect(dialog).toHaveTextContent("Kiểm tra số liệu");
    expect(dialog).toHaveFocus();
    expect(dialog.compareDocumentPosition(screen.getByRole("textbox")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Gửi yêu cầu bổ sung" }));
    await waitFor(() => expect(calls.review).toHaveBeenCalledTimes(1));
    expect(calls.review.mock.calls[0]).toEqual([A, "trong_trot", { expected_submission_id: SID, base_attempt: 1, decision: "returned", note: "Kiểm tra số liệu" }, expect.any(String)]);
    expect(await screen.findByText("Đã gửi yêu cầu bổ sung cho lần gửi 1.")).toBeInTheDocument();
  });
  it("đổi phòng có ghi chú phải xác nhận bỏ, không mang ghi chú sang phòng khác", async () => {
    render(<KhtcFormReviewScreen />); const note = await screen.findByRole("textbox");
    fireEvent.change(note, { target: { value: "Ghi chú riêng TT" } }); fireEvent.click(screen.getByRole("button", { name: "Phiếu tiếp theo" }));
    expect(calls.get).not.toHaveBeenCalledWith(A, "chan_nuoi_thuy_san");
    fireEvent.click(screen.getByRole("button", { name: "Bỏ ghi chú và chuyển" }));
    await waitFor(() => expect(calls.get).toHaveBeenCalledWith(A, "chan_nuoi_thuy_san"));
    expect(await screen.findByRole("textbox")).toHaveValue("");
  });
  it("ẩn ngay dữ liệu cũ khi actor đổi", async () => {
    const view = render(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    calls.period.mockReturnValue(new Promise(() => undefined)); state.principal = { ...state.principal, actor_ref: "actor-b" }; view.rerender(<KhtcFormReviewScreen />);
    expect(screen.queryByRole("heading", { name: /Kỳ A/ })).not.toBeInTheDocument();
  });
});

describe("review241 — phản hồi và vòng đời tài nguyên", () => {
  async function confirmAccepted() {
    await screen.findByRole("button", { name: "Xác nhận" });
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận lần gửi" }));
    await waitFor(() => expect(calls.review).toHaveBeenCalledTimes(1));
  }
  it.each(["network", 408, 425, 429, 503, "idempotency_in_progress"])("kết quả %s giữ nguyên target, ghi chú và khóa khi thử lại", async outcome => {
    const late = deferred<KhtcFormReviewResult>(); calls.review.mockReturnValueOnce(late.promise);
    render(<KhtcFormReviewScreen />); const note = await screen.findByRole("textbox");
    fireEvent.change(note, { target: { value: "Ghi chú của lần gửi 1" } }); await confirmAccepted();
    expect(note).toBeDisabled(); expect(screen.getByRole("button", { name: "Phiếu tiếp theo" })).toBeDisabled();
    expect(screen.getByRole("combobox", { name: "Phòng được giao phiếu" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Yêu cầu bổ sung" })).toBeDisabled();
    await act(async () => late.reject(outcome === "network" ? new TypeError("offline") : new ApiProblem({ type: "about:blank", title: "Chờ đối chiếu", status: typeof outcome === "number" ? outcome : 409, code: outcome === "idempotency_in_progress" ? outcome : "unavailable" })));
    const first = calls.review.mock.calls[0];
    expect(note).toHaveValue("Ghi chú của lần gửi 1");
    fireEvent.click(screen.getByRole("button", { name: "Thử lại thao tác rà soát" }));
    await waitFor(() => expect(calls.review).toHaveBeenCalledTimes(2));
    expect(calls.review.mock.calls[1]).toEqual(first);
    expect(await screen.findByText("Đã xác nhận lần gửi 1.")).toBeInTheDocument();
  });
  it.each([401, 403, 409])("HTTP %s buộc đọc lại và không báo thành công", async status => {
    calls.review.mockRejectedValue(new ApiProblem({ type: "about:blank", title: "Trạng thái đã đổi", status, code: "conflict" }));
    render(<KhtcFormReviewScreen />); await confirmAccepted();
    expect(await screen.findByRole("button", { name: "Tải lại phiếu" })).toBeInTheDocument();
    expect(screen.queryByText("Nguồn phòng chuyên môn")).not.toBeInTheDocument();
    expect(screen.queryByText("Đã xác nhận lần gửi 1.")).not.toBeInTheDocument();
  });
  it.each(["target", "attempt", "decision", "period", "department"])("ACK sai %s không được nhận là thao tác thành công", async mismatch => {
    calls.review.mockImplementation((id, dept, input) => {
      const response = ack(input, id, dept);
      if (mismatch === "target") response.reviewed_submission_id = B;
      if (mismatch === "attempt") response.reviewed_attempt = 2;
      if (mismatch === "decision") response.review_decision = "returned";
      if (mismatch === "period") response.period.id = B;
      if (mismatch === "department") response.department = "chan_nuoi_thuy_san";
      return Promise.resolve(response);
    });
    render(<KhtcFormReviewScreen />); await confirmAccepted();
    expect(await screen.findByRole("button", { name: "Tải lại phiếu" })).toBeInTheDocument();
    expect(screen.queryByText("Đã xác nhận lần gửi 1.")).not.toBeInTheDocument();
  });
  it.each(["api", "tenant", "context", "role", "mfa", "department"])("%s đổi thì không còn phiếu của lifetime cũ", async kind => {
    const view = render(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    calls.period.mockReturnValue(new Promise(() => undefined));
    if (kind === "api") state.api = {};
    if (kind === "tenant") state.principal = { ...state.principal, tenant_id: "tenant-b" };
    if (kind === "context") state.context = { ...state.context, context_id: "ctx-b" };
    if (kind === "role") state.context = { ...state.context, role: "kn_tw" };
    if (kind === "mfa") state.principal = { ...state.principal, amr: [] };
    if (kind === "department") state.context = { ...state.context, department_code: "trong_trot" };
    view.rerender(<KhtcFormReviewScreen />); expect(screen.queryByRole("heading", { name: /Kỳ A/ })).not.toBeInTheDocument();
  });
  it("route A→B→A không nhận ACK của lifetime A cũ", async () => {
    const old = deferred<KhtcFormReviewResult>(); calls.review.mockReturnValueOnce(old.promise);
    const view = render(<KhtcFormReviewScreen />); await confirmAccepted();
    state.id = B; view.rerender(<KhtcFormReviewScreen />); await waitFor(() => expect(calls.get).toHaveBeenCalledWith(B, "trong_trot"));
    calls.get.mockImplementation((id, dept) => Promise.resolve(sheet("A mới", id, dept)));
    state.id = A; view.rerender(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /A mới/ });
    await act(async () => old.resolve(ack(calls.review.mock.calls[0]![2])));
    expect(screen.getByRole("heading", { name: /A mới/ })).toBeInTheDocument();
    expect(screen.queryByText("Đã xác nhận lần gửi 1.")).not.toBeInTheDocument();
  });
  it("phòng A→B→A không nhận GET muộn của A cũ", async () => {
    const old = deferred<KhtcFormSheet>(); calls.get.mockReturnValueOnce(old.promise);
    const view = render(<KhtcFormReviewScreen />); await waitFor(() => expect(calls.get).toHaveBeenCalledWith(A, "trong_trot"));
    state.search = "department=chan_nuoi_thuy_san"; view.rerender(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /Chăn nuôi/ });
    calls.get.mockImplementation((id, dept) => Promise.resolve(sheet("A mới", id, dept)));
    state.search = ""; view.rerender(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /A mới/ });
    await act(async () => old.resolve(sheet("GET A cũ")));
    expect(screen.queryByRole("heading", { name: /GET A cũ/ })).not.toBeInTheDocument();
  });
  it("GET sai tài nguyên không lộ nội dung", async () => {
    calls.get.mockResolvedValue(sheet("Sai tài nguyên", B));
    render(<KhtcFormReviewScreen />); expect(await screen.findByRole("alert")).toHaveTextContent("extension.out_of_scope");
    expect(screen.queryByText("Nguồn phòng chuyên môn")).not.toBeInTheDocument();
  });
});

describe("review241 — đường dẫn, quyền và ghi chú", () => {
  it.each(["department=trong_trot&department=chan_nuoi_thuy_san", "department=bad", "department=", "x=1"])("không GET khi query không hợp lệ: %s", async query => {
    state.search = query; render(<KhtcFormReviewScreen />);
    expect(screen.getByRole("alert")).toHaveTextContent("Đường dẫn rà soát không hợp lệ");
    expect(calls.period).not.toHaveBeenCalled(); expect(calls.get).not.toHaveBeenCalled();
  });
  it("phòng ngoài kỳ không được đọc phiếu", async () => {
    state.search = "department=hanh_chinh_tong_hop"; render(<KhtcFormReviewScreen />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Phòng không được giao phiếu"); expect(calls.get).not.toHaveBeenCalled();
  });
  it.each(["anonymous", "inactive", "read-only", "national", "other-department"])("ngữ cảnh %s không được rà soát", async kind => {
    if (kind === "anonymous") state.status = "anonymous";
    if (kind === "inactive") state.knStatus = "loading";
    if (kind === "read-only") state.context = { ...state.context, read_only: true };
    if (kind === "national") state.context = { ...state.context, role: "kn_tw" };
    if (kind === "other-department") state.context = { ...state.context, department_code: "trong_trot" };
    render(<KhtcFormReviewScreen />);
    if (kind === "anonymous" || kind === "inactive") expect(calls.period).not.toHaveBeenCalled();
    else await screen.findByRole("heading", { name: /Kỳ A/ });
    expect(screen.queryByRole("button", { name: "Xác nhận" })).not.toBeInTheDocument(); expect(calls.review).not.toHaveBeenCalled();
  });
  it("nội dung nguồn chỉ đọc và ghi chú bắt buộc có nhãn/lỗi nối đúng ô", async () => {
    render(<KhtcFormReviewScreen />); await screen.findByText("Nguồn phòng chuyên môn");
    expect(screen.getAllByRole("textbox")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Yêu cầu bổ sung" }));
    const note = screen.getByRole("textbox", { name: /Nội dung yêu cầu bổ sung/ });
    expect(note).toHaveAttribute("aria-invalid", "true"); expect(note).toHaveAccessibleDescription("Bắt buộc nhập nội dung yêu cầu bổ sung.");
    expect(note).toHaveFocus();
    expect(calls.review).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    expect(screen.getByRole("dialog", { name: "Xác nhận phiếu số liệu" })).toBeInTheDocument();
  });
  it("1001 Unicode chặn cả hai quyết định, 1000 Unicode vẫn gửi trọn", async () => {
    render(<KhtcFormReviewScreen />); const note = await screen.findByRole("textbox");
    fireEvent.change(note, { target: { value: "🌾".repeat(1001) } });
    for (const name of ["Yêu cầu bổ sung", "Xác nhận"]) fireEvent.click(screen.getByRole("button", { name }));
    expect(note).toHaveAccessibleDescription("Tối đa 1000 ký tự."); expect(calls.review).not.toHaveBeenCalled();
    fireEvent.change(note, { target: { value: "🌾".repeat(1000) } });
    fireEvent.click(screen.getByRole("button", { name: "Yêu cầu bổ sung" })); fireEvent.click(screen.getByRole("button", { name: "Gửi yêu cầu bổ sung" }));
    await waitFor(() => expect(calls.review).toHaveBeenCalledTimes(1)); expect(calls.review.mock.calls[0]![2].note).toBe("🌾".repeat(1000));
  });
  it.each([["số mũ lớn", "1e100000"], ["số mũ nhỏ", "1e-100000"], ["chuỗi dài", "9".repeat(10000)]])("phiên bản metadata không an toàn không thành quyết định: %s", async (_name, value) => {
    const f = sheet(); f.submissions[0]!.attempt = Number(value); f.attempt = Number(value); calls.get.mockResolvedValue(f);
    render(<KhtcFormReviewScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    expect(screen.queryByRole("button", { name: "Xác nhận" })).not.toBeInTheDocument();
  });
});

describe("history242 — điều hướng khỏi ghi chú rà soát", () => {
  it("giữ ghi chú khi hủy chuyển, chỉ mở lịch sử sau xác nhận bỏ", async () => {
    render(<KhtcFormReviewScreen />); const note = await screen.findByRole("textbox");
    fireEvent.change(note, { target: { value: "Ghi chú chưa gửi" } });
    fireEvent.click(screen.getByRole("button", { name: "Lịch sử và so sánh lần gửi" }));
    expect(calls.push).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Giữ ghi chú" }));
    expect(note).toHaveValue("Ghi chú chưa gửi");
    fireEvent.click(screen.getByRole("button", { name: "Lịch sử và so sánh lần gửi" }));
    fireEvent.click(screen.getByRole("button", { name: "Bỏ ghi chú và chuyển" }));
    expect(calls.push).toHaveBeenCalledExactlyOnceWith(`/kn/khtc/ra-soat/${A}/lan-gui?department=trong_trot`);
    expect(calls.review).not.toHaveBeenCalled();
  });
  it("không mở lịch sử trong khi quyết định rà soát chưa có kết quả", async () => {
    const operation = deferred<KhtcFormReviewResult>(); calls.review.mockReturnValueOnce(operation.promise);
    render(<KhtcFormReviewScreen />);
    await screen.findByRole("heading", { name: /Kỳ A/ });
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận lần gửi" }));
    await waitFor(() => expect(calls.review).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "Lịch sử và so sánh lần gửi" })).toBeDisabled();
    expect(calls.push).not.toHaveBeenCalled();
    await act(async () => operation.resolve(ack(calls.review.mock.calls[0]![2])));
  });
});
