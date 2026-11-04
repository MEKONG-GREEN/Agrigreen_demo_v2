import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { createKhtcPeriodApi } from "../periods/api";
import { createReportApi } from "./api";
import { KhtcReportsScreen } from "./KhtcReportsScreen";
const auth: {
    api: ApiClient | null;
} = { api: null };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api, kn: { context: { role: "kn_tinh", department_code: "ke_hoach_tai_chinh" } } }) }));
beforeEach(() => { auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => null, fetch: mockFetchNoDelay }); });
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

