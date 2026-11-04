import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { createKhtcPeriodApi } from "../periods/api";
import { createReportApi } from "./api";
import { KhtcReportsScreen } from "./KhtcReportsScreen";
import type { AuthContextValue } from "@/auth/AuthProvider";
type Auth232 = Pick<AuthContextValue, "status" | "principal" | "kn"> & {api: ApiClient | null};
const auth = {} as Auth232;
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
function api232(fetch: typeof mockFetchNoDelay = mockFetchNoDelay) {
    return createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => null, fetch });
}
beforeEach(() => {
    Object.assign(auth, {api: api232(), status: "authenticated", principal: {
        subject: "actor232", displayName: "Nhãn người dùng", tenantId: "tenant232", orgRef: "org232", roles: ["kn_officer"], mfa: true,
        level: "tinh", unitName: null, orgName: null, scopeUnits: ["X1"], scopeAll: false,
    }, kn: {status: "active", assignments: [], menu: null, badges: null, context: {
        context_id: "context232", assignment_id: "assignment232", role: "kn_tinh", department_code: "ke_hoach_tai_chinh",
        department_name: "KHTC", org_ref: "org232", org_name: "Nhãn tổ chức", communes: [], default_route: "/kn/khtc/bao-cao", expires_at: "2099-01-01T00:00:00Z",
    }}});
});
afterEach(() => vi.useRealTimers());

describe("W11.66 minimal editor", () => {
    it("preserves unsaved text, blocks refresh/export until save, and resets administrative overrides", async () => {
        const year = new Date().getFullYear() + 5;
        const p = await createKhtcPeriodApi(auth.api!).open({ report_type: "week", year, no: 5, departments: ["trong_trot"] });
        render(<KhtcReportsScreen />);
        fireEvent.change(screen.getByLabelText("Năm"), { target: { value: String(year) } });
        fireEvent.click(await screen.findByRole("button", { name: p.name }));
        const signer = await screen.findByLabelText("Người ký");
        fireEvent.change(signer, { target: { value: "Người ký kiểm thử" } });
        fireEvent.change(screen.getByLabelText(/B Hạn chế/), { target: { value: "Lời văn đang soạn" } });
        expect(screen.getByRole("button", { name: "Cập nhật số liệu" })).toBeDisabled();
        expect(screen.getByRole("button", { name: "Xuất Excel" })).toBeDisabled();
        expect(screen.getByText(/Có thay đổi chưa lưu/)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Lưu bản soạn" }));
        await waitFor(() => expect(screen.queryByText(/Có thay đổi chưa lưu/)).not.toBeInTheDocument());
        expect(screen.getByLabelText("Người ký")).toHaveValue("Người ký kiểm thử");
        expect((await createReportApi(auth.api!).get(p.id)).content.texts.B).toBe("Lời văn đang soạn");
        const reset = screen.getByLabelText("Người ký").closest("div")!.querySelector("button")!;
        fireEvent.click(reset);
        expect(screen.getByLabelText("Người ký")).toHaveValue("");
        fireEvent.click(screen.getByRole("button", { name: "Lưu bản soạn" }));
        await waitFor(() => expect(screen.queryByText(/Có thay đổi chưa lưu/)).not.toBeInTheDocument());
        expect((await createReportApi(auth.api!).get(p.id)).content.admin.signer?.override).toBeNull();
    });
});

it("keeps the232 save acknowledgement after reloading the saved version and content", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2030-10-14T03:00:00Z"));
    try {
        const year = new Date().getFullYear() + 5;
        const p = await createKhtcPeriodApi(auth.api!).open({ report_type: "week", year, no: 6, departments: ["trong_trot"] });
        const initial = await createReportApi(auth.api!).get(p.id);
        render(<KhtcReportsScreen />);
        fireEvent.change(screen.getByLabelText("Năm"), { target: { value: String(year) } });
        fireEvent.click(await screen.findByRole("button", { name: p.name }));
        const text = await screen.findByLabelText(/B Hạn chế/);
        fireEvent.change(text, { target: { value: "Lời văn đã lưu để kiểm phản hồi232" } });
        fireEvent.click(screen.getByRole("button", { name: "Lưu bản soạn" }));
        await screen.findByText(new RegExp(`^Phiên bản soạn ${initial.version + 1} ·`));
        expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Lời văn đã lưu để kiểm phản hồi232");
        expect((await createReportApi(auth.api!).get(p.id)).content.texts.B).toBe("Lời văn đã lưu để kiểm phản hồi232");
        expect(screen.getByText("Đã lưu bản soạn", { exact: true }).closest('[role="status"]')).toBeVisible();
    } finally {
        vi.useRealTimers();
    }
});


let nextReport232 = 7;
function fixedClock232() {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2030-10-14T03:00:00Z"));
}
async function report232() {
    fixedClock232();
    const year = new Date().getFullYear() + 5;
    const p = await createKhtcPeriodApi(auth.api!).open({ report_type: "week", year, no: nextReport232++, departments: ["trong_trot"] });
    return {p, year, initial: await createReportApi(auth.api!).get(p.id)};
}
function deferred232() {
    let resolve!: () => void;
    const promise = new Promise<void>(done => { resolve = done; });
    return {promise, resolve};
}
function problem232(status: number) {
    return Response.json({type: "about:blank", title: "Không lưu được bản soạn232", status, code: status === 409 ? "conflict" : "validation_failed", detail: "Lỗi lưu kiểm thử232"}, {status});
}
function pendingSave232() {
    const reached = deferred232(), release = deferred232();
    let held = false, listReads = 0, detailReads = 0;
    const fetch: typeof mockFetchNoDelay = async input => {
        const r = input, path = new URL(r.url).pathname;
        if (r.method === "GET" && path === "/v1/extension/khtc/periods") listReads++;
        if (r.method === "GET" && path.endsWith("/report")) detailReads++;
        const response = await mockFetchNoDelay(input);
        if (!held && r.method === "POST" && path.endsWith("/report/draft")) {
            held = true;
            reached.resolve();
            await release.promise;
        }
        return response;
    };
    return {api: api232(fetch), reached, release, reads: () => ({listReads, detailReads})};
}
async function release232(gate: ReturnType<typeof deferred232>) {
    await act(async () => { gate.resolve(); await Promise.resolve(); });
}

it.each([409, 422])("232 failed POST%s preserves entered draft and never claims save success", async status => {
    auth.api = api232(async input => {
        const r = input;
        return r.method === "POST" && new URL(r.url).pathname.endsWith("/report/draft") ? problem232(status) : mockFetchNoDelay(input);
    });
    const {p, initial} = await report232();
    render(<KhtcReportsScreen initialPeriod={p.id} />);
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "Nháp phải giữ khi POST lỗi232"}});
    fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
    expect((await screen.findByText(status === 409 ? "conflict" : "validation_failed", {selector: "code"})).closest('[role="alert"]')).toBeVisible();
    expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Nháp phải giữ khi POST lỗi232");
    expect(screen.getByText(/Có thay đổi chưa lưu/)).toBeVisible();
    expect(screen.queryByText("Đã lưu bản soạn", {exact: true})).not.toBeInTheDocument();
    const stored = await createReportApi(auth.api!).get(p.id);
    expect(stored.version).toBe(initial.version);
    expect(stored.content.texts.B).not.toBe("Nháp phải giữ khi POST lỗi232");
});

it("232 acknowledged POST keeps its success when the subsequent GET fails", async () => {
    let failReload = false;
    auth.api = api232(async input => {
        const r = input, path = new URL(r.url).pathname;
        if (failReload && r.method === "GET" && path.endsWith("/report")) { failReload = false; return Response.json({type: "about:blank", title: "Chưa đọc được bản soạn", status: 503, code: "internal_error", detail: "Không tải được bản soạn sau ACK232"}, {status: 503}); }
        const response = await mockFetchNoDelay(input);
        if (r.method === "POST" && path.endsWith("/report/draft") && response.ok) failReload = true;
        return response;
    });
    const {p, initial} = await report232();
    render(<KhtcReportsScreen initialPeriod={p.id} />);
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "Nội dung đã nhận ACK232"}});
    fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
    expect((await screen.findByText("internal_error", {selector: "code"})).closest('[role="alert"]')).toBeVisible();
    expect(screen.getByText("Đã lưu bản soạn", {exact: true})).toBeVisible();
    expect(screen.queryByLabelText(/B Hạn chế/)).not.toBeInTheDocument();
    const stored = await createReportApi(api232()).get(p.id);
    expect(stored.version).toBe(initial.version + 1);
    expect(stored.content.texts.B).toBe("Nội dung đã nhận ACK232");
});

it("232 committed period A→B→A ignores the old save ACK and preserves the new draft", async () => {
    const pending = pendingSave232(); auth.api = pending.api;
    const a = await report232(), b = await report232();
    render(<KhtcReportsScreen />);
    fireEvent.change(screen.getByLabelText("Năm"), {target: {value: String(a.year)}});
    fireEvent.click(await screen.findByRole("button", {name: a.p.name}));
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "A đã nhận ở server232"}});
    fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
    await pending.reached.promise;
    try {
        fireEvent.click(screen.getByRole("button", {name: b.p.name}));
        await waitFor(() => expect(screen.getAllByRole("heading", {level: 2}).some(h => h.textContent?.startsWith(b.p.name + " ·"))).toBe(true));
        fireEvent.click(screen.getByRole("button", {name: a.p.name}));
        await waitFor(() => expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("A đã nhận ở server232"));
        fireEvent.change(screen.getByLabelText(/B Hạn chế/), {target: {value: "Nháp mới sau ABA232"}});
        await release232(pending.release);
        expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Nháp mới sau ABA232");
        expect(screen.queryByText("Đã lưu bản soạn", {exact: true})).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
        await screen.findByText("Đã lưu bản soạn", {exact: true});
        expect((await createReportApi(auth.api!).get(a.p.id)).content.texts.B).toBe("Nháp mới sau ABA232");
    } finally { await release232(pending.release); }
});

it("232 changing API identity cannot let a pending old save overwrite the new owner", async () => {
    const pending = pendingSave232(); auth.api = pending.api;
    const {p} = await report232();
    const view = render(<KhtcReportsScreen initialPeriod={p.id} />);
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "Nội dung đã nhận trước đổi API232"}});
    fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
    await pending.reached.promise;
    try {
        auth.api = api232();
        view.rerender(<KhtcReportsScreen initialPeriod={p.id} />);
        await waitFor(() => expect(screen.getByLabelText(/B Hạn chế/)).toBeEnabled());
        const beforeReturn = pending.reads().detailReads;
        auth.api = pending.api;
        view.rerender(<KhtcReportsScreen initialPeriod={p.id} />);
        await waitFor(() => expect(pending.reads().detailReads).toBeGreaterThan(beforeReturn));
        await waitFor(() => expect(screen.getByLabelText(/B Hạn chế/)).toBeEnabled());
        fireEvent.change(screen.getByLabelText(/B Hạn chế/), {target: {value: "Nháp của API mới232"}});
        await release232(pending.release);
        expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Nháp của API mới232");
        expect(screen.queryByText("Đã lưu bản soạn", {exact: true})).not.toBeInTheDocument();
    } finally { await release232(pending.release); }
});

const authorityChanges232 = [
    ["actor", () => { auth.principal = {...auth.principal!, subject: "actor232-new"}; }],
    ["tenant", () => { auth.principal = {...auth.principal!, tenantId: "tenant232-new"}; }],
    ["org", () => { auth.principal = {...auth.principal!, orgRef: "org232-new"}; }],
    ["MFA", () => { auth.principal = {...auth.principal!, mfa: false}; }],
    ["roles", () => { auth.principal = {...auth.principal!, roles: ["kn_province_admin"]}; }],
    ["scope", () => { auth.principal = {...auth.principal!, scopeUnits: ["X2"]}; }],
    ["context", () => { auth.kn.context = {...auth.kn.context!, context_id: "context232-new"}; }],
    ["assignment", () => { auth.kn.context = {...auth.kn.context!, assignment_id: "assignment232-new"}; }],
    ["read-only", () => { auth.kn.context = {...auth.kn.context!, read_only: true}; }],
    ["support binding", () => { auth.kn.context = {...auth.kn.context!, support_session_id: "support232-new", on_behalf_of: "subject232-target"}; }],
] as const;
it.each(authorityChanges232)("232 %s authority switch reloads list/detail and retires the old pending ACK", async (_name, change) => {
    const pending = pendingSave232(); auth.api = pending.api;
    const {p} = await report232();
    const view = render(<KhtcReportsScreen initialPeriod={p.id} />);
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "Đã nhận trước đổi quyền232"}});
    fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
    await pending.reached.promise;
    try {
        const before = pending.reads();
        change(); view.rerender(<KhtcReportsScreen initialPeriod={p.id} />);
        await waitFor(() => { expect(pending.reads().detailReads).toBeGreaterThan(before.detailReads); expect(pending.reads().listReads).toBeGreaterThan(before.listReads); });
        await waitFor(() => expect(screen.getByLabelText(/B Hạn chế/)).toBeEnabled());
        fireEvent.change(screen.getByLabelText(/B Hạn chế/), {target: {value: "Nháp của quyền hiện tại232"}});
        await release232(pending.release);
        expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Nháp của quyền hiện tại232");
        expect(screen.queryByText("Đã lưu bản soạn", {exact: true})).not.toBeInTheDocument();
    } finally { await release232(pending.release); }
});

it("232 display labels and support reason changes do not discard a draft", async () => {
    auth.kn.context = {...auth.kn.context!, support_session_id: "support232", on_behalf_of: "target232"};
    auth.kn.support = {target: "Nhãn đối tượng", reason: "Lý do hỗ trợ ban đầu232"};
    const {p} = await report232();
    const view = render(<KhtcReportsScreen initialPeriod={p.id} />);
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "Nháp không phụ thuộc nhãn232"}});
    auth.principal = {...auth.principal!, displayName: "Nhãn đổi", orgName: "Tên tổ chức đổi"};
    auth.kn.context = {...auth.kn.context!, org_name: "Nhãn nút đổi", department_name: "Nhãn phòng đổi"};
    auth.kn.support = {...auth.kn.support!, reason: "Lý do hỗ trợ mới232"};
    view.rerender(<KhtcReportsScreen initialPeriod={p.id} />);
    expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Nháp không phụ thuộc nhãn232");
    expect(screen.getByText(/Có thay đổi chưa lưu/)).toBeVisible();
});


it("232 committed authority A→B→A creates a fresh owner on the same API", async () => {
    const pending = pendingSave232(); auth.api = pending.api;
    const {p} = await report232();
    const original = {...auth.kn.context!};
    const view = render(<KhtcReportsScreen initialPeriod={p.id} />);
    fireEvent.change(await screen.findByLabelText(/B Hạn chế/), {target: {value: "Nội dung đã nhận ở quyền A232"}});
    fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
    await pending.reached.promise;
    try {
        let before = pending.reads().detailReads;
        auth.kn.context = {...original, context_id: "context232-B"};
        view.rerender(<KhtcReportsScreen initialPeriod={p.id} />);
        await waitFor(() => expect(pending.reads().detailReads).toBeGreaterThan(before));
        await waitFor(() => expect(screen.getByLabelText(/B Hạn chế/)).toBeEnabled());
        before = pending.reads().detailReads;
        auth.kn.context = original;
        view.rerender(<KhtcReportsScreen initialPeriod={p.id} />);
        await waitFor(() => expect(pending.reads().detailReads).toBeGreaterThan(before));
        await waitFor(() => expect(screen.getByLabelText(/B Hạn chế/)).toBeEnabled());
        fireEvent.change(screen.getByLabelText(/B Hạn chế/), {target: {value: "Nháp mới của quyền A232"}});
        await release232(pending.release);
        expect(screen.getByLabelText(/B Hạn chế/)).toHaveValue("Nháp mới của quyền A232");
        expect(screen.queryByText("Đã lưu bản soạn", {exact: true})).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", {name: "Lưu bản soạn"}));
        await screen.findByText("Đã lưu bản soạn", {exact: true});
        expect((await createReportApi(auth.api!).get(p.id)).content.texts.B).toBe("Nháp mới của quyền A232");
    } finally { await release232(pending.release); }
});
