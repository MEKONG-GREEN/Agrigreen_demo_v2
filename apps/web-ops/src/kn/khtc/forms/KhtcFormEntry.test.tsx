import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KhtcFormEntryScreen } from "./KhtcFormScreens";
import type { KhtcFormSheet } from "./api";
import type * as FormApiModule from "./api";
import { ApiProblem } from "@/api/problem";

const state = vi.hoisted(() => ({
  id: "11111111-1111-4111-8111-111111111111", search: "", api: {} as object,
  status: "authenticated", knStatus: "active",
  principal: { actor_ref: "actor-a", tenant_id: "tenant-a", amr: ["mfa"] },
  context: { context_id: "ctx-a", department_code: "trong_trot", role: "kn_tinh", read_only: false },
}));
const calls = vi.hoisted(() => ({ get: vi.fn(), draft: vi.fn(), submit: vi.fn() }));
vi.mock("next/navigation", () => ({ useParams: () => ({ periodId: state.id }), useSearchParams: () => new URLSearchParams(state.search) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: state.api, status: state.status, principal: state.principal, kn: { status: state.knStatus, context: state.context } }) }));
vi.mock("@/shell/Breadcrumb", () => ({ useBreadcrumbTail: () => undefined }));
vi.mock("./api", async original => ({ ...await original<typeof FormApiModule>(), createKhtcFormApi: () => calls }));
const A = "11111111-1111-4111-8111-111111111111", B = "22222222-2222-4222-8222-222222222222";
function sheet(name = "Kỳ A", id = state.id, department = "trong_trot"): KhtcFormSheet {
  return {
    period: { id, name, report_type: "week", year: 2030, from_date: "2030-06-01", to_date: "2030-06-07", cutoff_date: "2030-06-01", due_at: "2030-06-07T10:00:00Z", max_submissions: 3, terminal: "" },
    department, department_name: "Trồng trọt", state: "draft", state_label: "Đang nhập", attempt: 0,
    limit_reached: false, can_request_supplement: false, can_confirm: false, can_review: false, editable: true, overdue: false, request_note: "",
    template: { code: "KHTC-PHIEU", version: 2, department, title: "Phiếu", requirements: "", columns: [
      { letter: "A", title: "Hạng mục", kind: "fixed", dtype: "text", source: "entry_name" },
      { letter: "F", title: "Thực hiện", kind: "input", dtype: "number", input: "done", non_negative: true },
      { letter: "J", title: "Số người", kind: "input", dtype: "integer", input: "people", non_negative: true },
      { letter: "K", title: "Kinh phí", kind: "input", dtype: "number", input: "budget", non_negative: true },
    ], sections: [{ key: "summary", title: "Kết quả", required: true }] },
    grid: true, plan_missing: false, plan: { version: 2, decision_no: "QĐ229", effective_from: "2030-01-01", version_id: A, decision_date: null },
    tabs: [{ key: "program", label: "Chương trình", plan: "p", rows: [{ entry_ref: "r1", no: 1, code: "", name: "Mô hình lúa", unit: "ha", planned_qty: "100", scale_note: "", module_sourced: false, source_key: "", cells: { E: "2", A: "Mô hình lúa" }, over_plan: false }] }],
    content: { cells: { r1: { done: "3", people: "2", budget: "10" } }, adjustments: [], texts: { summary: "Số liệu đã kiểm tra" } },
    content_source: "draft", draft_at: "2030-06-03T08:00:00Z", adj_periods: [{ id: B, name: "Kỳ trước", year: 2030 }], issues: [], submissions: [], valid: true,
  };
}
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-06-03T09:00:00Z"));
  state.id = A; state.search = ""; state.api = {}; state.status = "authenticated"; state.knStatus = "active";
  state.principal = { actor_ref: "actor-a", tenant_id: "tenant-a", amr: ["mfa"] };
  state.context = { context_id: "ctx-a", department_code: "trong_trot", role: "kn_tinh", read_only: false };
  for (const call of Object.values(calls)) call.mockReset(); calls.get.mockImplementation(() => Promise.resolve(sheet()));
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("editor229 — lifetime và ACK", () => {
  it("khóa nội dung đang lưu để ACK không xóa bản sửa gõ sau dispatch", async () => {
    const pending = deferred<KhtcFormSheet>(); calls.draft.mockReturnValue(pending.promise);
    render(<KhtcFormEntryScreen />); const input = await screen.findByRole("textbox", { name: "F Mô hình lúa" });
    fireEvent.change(input, { target: { value: "7" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(calls.draft).toHaveBeenCalledTimes(1)); expect(input).toBeDisabled();
    await act(async () => pending.resolve({ ...sheet(), content: { ...sheet().content, cells: { r1: { done: "7" } } } }));
    expect(screen.getByRole("textbox", { name: "F Mô hình lúa" })).toHaveValue("7");
  });
  it("xóa dữ liệu cũ ngay khi actor đổi, không chờ GET mới", async () => {
    const next = deferred<KhtcFormSheet>(); const view = render(<KhtcFormEntryScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    calls.get.mockReturnValue(next.promise); state.principal = { ...state.principal, actor_ref: "actor-b" }; view.rerender(<KhtcFormEntryScreen />);
    expect(screen.queryByRole("heading", { name: /Kỳ A/ })).not.toBeInTheDocument();
    await act(async () => next.resolve(sheet("Kỳ B"))); expect(await screen.findByRole("heading", { name: /Kỳ B/ })).toBeInTheDocument();
  });

  it.each(["api", "tenant", "context", "role", "mfa", "department"])("bắt đầu lifetime mới khi %s đổi", async kind => {
    const view = render(<KhtcFormEntryScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    const next = deferred<KhtcFormSheet>(); calls.get.mockReturnValue(next.promise);
    if (kind === "api") state.api = {};
    if (kind === "tenant") state.principal = { ...state.principal, tenant_id: "tenant-b" };
    if (kind === "context") state.context = { ...state.context, context_id: "ctx-b" };
    if (kind === "role") state.context = { ...state.context, role: "kn_xa", read_only: true };
    if (kind === "mfa") state.principal = { ...state.principal, amr: [] };
    if (kind === "department") state.context = { ...state.context, department_code: "chan_nuoi_thuy_san" };
    view.rerender(<KhtcFormEntryScreen />); expect(screen.queryByRole("heading", { name: /Kỳ A/ })).not.toBeInTheDocument();
    await act(async () => next.resolve(sheet("Kỳ B", A, state.context.department_code)));
    expect(await screen.findByRole("heading", { name: /Kỳ B/ })).toBeInTheDocument();
    if (kind === "role") expect(screen.queryByRole("button", { name: "Lưu nháp" })).not.toBeInTheDocument();
  });

  it("route A→B→A không nhận ACK lưu của lifetime A cũ", async () => {
    const old = deferred<KhtcFormSheet>(); calls.draft.mockReturnValue(old.promise);
    const view = render(<KhtcFormEntryScreen />); await screen.findByRole("textbox", { name: "F Mô hình lúa" });
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await waitFor(() => expect(calls.draft).toHaveBeenCalledTimes(1));
    state.id = B; view.rerender(<KhtcFormEntryScreen />); await waitFor(() => expect(calls.get).toHaveBeenCalledWith(B, "trong_trot"));
    calls.get.mockResolvedValue({ ...sheet("Fresh A", A), content: { ...sheet().content, cells: { r1: { done: "9" } } } });
    state.id = A; view.rerender(<KhtcFormEntryScreen />); await screen.findByRole("heading", { name: /Fresh A/ });
    await act(async () => old.resolve(sheet("OLD ACK MUST NOT RETURN", A)));
    expect(screen.queryByText(/MUST NOT RETURN/)).not.toBeInTheDocument(); expect(screen.getByRole("textbox", { name: "F Mô hình lúa" })).toHaveValue("9");
  });

  it("GET A→B→A chỉ nhận freshA, xóa cả draft cũ khi query phòng đổi", async () => {
    const old = deferred<KhtcFormSheet>(), middle = deferred<KhtcFormSheet>(), fresh = deferred<KhtcFormSheet>();
    calls.get.mockReset().mockReturnValueOnce(old.promise).mockReturnValueOnce(middle.promise).mockReturnValueOnce(fresh.promise);
    const view = render(<KhtcFormEntryScreen />); await waitFor(() => expect(calls.get).toHaveBeenCalledTimes(1));
    state.id = B; view.rerender(<KhtcFormEntryScreen />); await waitFor(() => expect(calls.get).toHaveBeenCalledTimes(2));
    state.id = A; view.rerender(<KhtcFormEntryScreen />); await waitFor(() => expect(calls.get).toHaveBeenCalledTimes(3));
    await act(async () => { old.resolve(sheet("OLD A MUST NOT RETURN", A)); middle.resolve(sheet("OLD B MUST NOT RETURN", B)); });
    expect(screen.queryByText(/MUST NOT RETURN/)).not.toBeInTheDocument();
    await act(async () => fresh.resolve(sheet("Fresh A", A))); await screen.findByRole("heading", { name: /Fresh A/ });
    fireEvent.change(screen.getByRole("textbox", { name: "F Mô hình lúa" }), { target: { value: "71" } });
    const other = deferred<KhtcFormSheet>(); calls.get.mockReturnValue(other.promise); state.search = "department=chan_nuoi_thuy_san"; view.rerender(<KhtcFormEntryScreen />);
    expect(screen.queryByRole("textbox", { name: "F Mô hình lúa" })).not.toBeInTheDocument();
    await act(async () => other.resolve(sheet("CNTS", A, "chan_nuoi_thuy_san"))); await screen.findByRole("heading", { name: /CNTS/ }); expect(screen.queryByRole("textbox", { name: "F Mô hình lúa" })).not.toBeInTheDocument();
  });

  it.each(["network", "408", "429", "503", "in_progress"])("mất ACK %s khóa nội dung, chỉ retry cùng payload/key", async kind => {
    const error = kind === "network" ? new TypeError("Network unavailable") : new ApiProblem({ type: "about:blank", title: "Chờ", status: kind === "in_progress" ? 409 : Number(kind), code: kind === "in_progress" ? "idempotency_in_progress" : "service_unavailable" });
    calls.draft.mockRejectedValueOnce(error).mockResolvedValueOnce(sheet("Đã lưu đúng lượt"));
    render(<KhtcFormEntryScreen />); const input = await screen.findByRole("textbox", { name: "F Mô hình lúa" });
    fireEvent.change(input, { target: { value: "7" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    const retry = await screen.findByRole("button", { name: "Thử lại Lưu nháp" }); await waitFor(() => expect(retry).toBeEnabled());
    expect(input).toBeDisabled(); expect(screen.getByRole("button", { name: "Gửi KHTC" })).toBeDisabled();
    fireEvent.change(input, { target: { value: "99" } }); expect(input).toHaveValue("7");
    fireEvent.click(retry); await screen.findByRole("heading", { name: /Đã lưu đúng lượt/ });
    expect(calls.draft).toHaveBeenCalledTimes(2); expect(calls.draft.mock.calls[0]).toEqual(calls.draft.mock.calls[1]);
    expect(calls.draft.mock.calls[0]![4]).toMatch(/^[0-9a-f-]{36}$/); expect(calls.submit).not.toHaveBeenCalled();
  });

  it("lỗi nghiệp vụ dứt khoát cho sửa và tạo khóa mới", async () => {
    calls.draft.mockRejectedValueOnce(new ApiProblem({ type: "about:blank", title: "Sai", status: 422, code: "extension.khtc_form_invalid" })).mockResolvedValueOnce(sheet());
    render(<KhtcFormEntryScreen />); const input = await screen.findByRole("textbox", { name: "F Mô hình lúa" });
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await waitFor(() => expect(input).toBeEnabled());
    fireEvent.change(input, { target: { value: "8" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(calls.draft).toHaveBeenCalledTimes(2)); expect(calls.draft.mock.calls[0]![4]).not.toEqual(calls.draft.mock.calls[1]![4]);
  });

  it("ACK sai tài nguyên không thay phiếu hoặc báo thành công, retry giữ đúng key", async () => {
    calls.draft.mockResolvedValue(sheet("WRONG ACK SECRET", B)); render(<KhtcFormEntryScreen />); await screen.findByRole("heading", { name: /Kỳ A/ });
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Thử lại Lưu nháp" })).toBeEnabled());
    expect(screen.queryByText(/WRONG ACK SECRET/)).not.toBeInTheDocument(); expect(screen.queryByText("Đã lưu nháp.")).not.toBeInTheDocument();
  });

  it.each(["period", "department", "denied"])("không lộ dữ liệu khi GET %s không khớp", async kind => {
    if (kind === "denied") calls.get.mockRejectedValue(new ApiProblem({ type: "about:blank", title: "Denied", status: 403, code: "extension.out_of_scope" }));
    else calls.get.mockResolvedValue(sheet("WRONG GET SECRET", kind === "period" ? B : A, kind === "department" ? "chan_nuoi_thuy_san" : "trong_trot"));
    render(<KhtcFormEntryScreen />); await screen.findByRole("alert"); expect(screen.queryByText(/WRONG GET SECRET/)).not.toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Lưu nháp" })).not.toBeInTheDocument();
  });

  it.each(["id", "query", "duplicate", "department", "anonymous", "inactive"])("đầu vào/ngữ cảnh %s bị chặn trước GET", kind => {
    if (kind === "id") state.id = "1e100000";
    if (kind === "query") state.search = "actor=forged";
    if (kind === "duplicate") state.search = "department=trong_trot&department=trong_trot";
    if (kind === "department") state.search = "department=1e-100000";
    if (kind === "anonymous") state.status = "anonymous";
    if (kind === "inactive") state.knStatus = "inactive";
    render(<KhtcFormEntryScreen />); expect(screen.getByRole("alert")).toBeInTheDocument(); expect(calls.get).not.toHaveBeenCalled();
  });

  it("Gửi cần xác nhận đúng lần, đổi nội dung hủy xác nhận, ACK chuyển readonly", async () => {
    calls.submit.mockResolvedValue({ ...sheet(), state: "submitted", state_label: "Đã gửi", attempt: 1, editable: false });
    render(<KhtcFormEntryScreen />); const input = await screen.findByRole("textbox", { name: "F Mô hình lúa" });
    fireEvent.click(screen.getByRole("button", { name: "Gửi KHTC" })); expect(screen.getByRole("dialog", { name: "Xác nhận gửi phiếu" })).toHaveTextContent("lần 1/3"); expect(calls.submit).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "5" } }); expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Gửi KHTC" })); fireEvent.click(screen.getByRole("button", { name: "Xác nhận gửi lần 1" }));
    await waitFor(() => expect(calls.submit).toHaveBeenCalledTimes(1)); expect(calls.submit.mock.calls[0]![2].cells.r1.done).toBe("5");
    await screen.findByText("Đã gửi KHTC lần 1."); expect(screen.queryByRole("textbox", { name: "F Mô hình lúa" })).not.toBeInTheDocument();
  });

  it.each(["F", "J", "K"].flatMap(letter => ["1e100000", "1e-100000", "9".repeat(10000)].map(value => ({ letter, value }))))("ô $letter từ chối số mũ/chuỗi dài trước Gửi", async ({ letter, value }) => {
    render(<KhtcFormEntryScreen />); const input = await screen.findByRole("textbox", { name: `${letter} Mô hình lúa` });
    fireEvent.change(input, { target: { value } }); expect(input).toHaveAttribute("aria-invalid", "true"); expect(input).toHaveAccessibleDescription(/Số quá dài|Nhập số|Chỉ nhập/);
    expect(screen.getByRole("button", { name: "Gửi KHTC" })).toBeDisabled(); expect(calls.submit).not.toHaveBeenCalled();
  });

  it.each(["1e100000", "1e-100000", "9".repeat(10000)])("điều chỉnh từ chối số mũ/chuỗi dài, âm hợp lệ với lý do", async value => {
    render(<KhtcFormEntryScreen />); await screen.findByRole("textbox", { name: "F Mô hình lúa" });
    fireEvent.click(screen.getByRole("button", { name: "Thêm điều chỉnh" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Hạng mục điều chỉnh" }), { target: { value: "r1" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Kỳ được điều chỉnh" }), { target: { value: B } });
    const amount = screen.getByRole("textbox", { name: "Số điều chỉnh" }); fireEvent.change(amount, { target: { value } });
    expect(amount).toHaveAttribute("aria-invalid", "true"); expect(screen.getByRole("button", { name: "Gửi KHTC" })).toBeDisabled();
    fireEvent.change(amount, { target: { value: "-1,5" } }); fireEvent.change(screen.getByRole("textbox", { name: "Lý do điều chỉnh" }), { target: { value: "Điều chỉnh số đã báo" } });
    expect(amount).not.toHaveAttribute("aria-invalid"); expect(screen.getByRole("button", { name: "Gửi KHTC" })).toBeEnabled();
  });

  it("dirty được xóa chỉ sau ACK lưu, trước đó có cảnh báo rời trang", async () => {
    calls.draft.mockResolvedValue({ ...sheet(), content: { ...sheet().content, cells: { r1: { done: "7" } } } });
    render(<KhtcFormEntryScreen />); const input = await screen.findByRole("textbox", { name: "F Mô hình lúa" }); fireEvent.change(input, { target: { value: "7" } });
    expect(screen.getByText("Có thay đổi chưa lưu.")).toBeInTheDocument(); const before = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(before); expect(before.defaultPrevented).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" })); await screen.findByText("Đã lưu nháp."); expect(screen.queryByText("Có thay đổi chưa lưu.")).not.toBeInTheDocument();
    const after = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(after); expect(after.defaultPrevented).toBe(false);
  });
});
