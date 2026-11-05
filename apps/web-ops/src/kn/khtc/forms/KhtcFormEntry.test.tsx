import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KhtcFormEntryScreen } from "./KhtcFormScreens";
import type { KhtcFormSheet } from "./api";

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
vi.mock("./api", async original => ({ ...await original<typeof import("./api")>(), createKhtcFormApi: () => calls }));
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
    content_source: "draft", draft_at: "2030-06-03T08:00:00Z", adj_periods: [{ id: B, name: "Kỳ trước", year: 2030 }], issues: [], submissions: [],
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
    fireEvent.change(input, { target: { value: "7" } }); fireEvent.click(screen.getByRole("button", { name: "Lưu nháp", exact: true }));
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
});
