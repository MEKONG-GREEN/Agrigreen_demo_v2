import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { KhtcDashboardScreen } from "./KhtcDashboardScreen";
import type { Dashboard } from "./api";
const auth: {
    api: ApiClient | null;
} = { api: null };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api }) }));
const requests: Request[] = [];
let sent = 1, canAct = false;
const data = (): Dashboard => ({ year: 2026, quarter: 4, department: "", field: "", as_of: "2026-10-13T03:00:00Z", can_act: canAct, counts: { exported: 2, open: 1, sent, forms: 4, achieved: 1, items: 3, slow: 1 }, progress: { items: 3, achieved: 1, slow: 1, missing: 1, programs: [{ ref: "ct1", name: "Chương trình lúa", items: 3, known: 2, percent: "23.81" }], rows: [] }, progress_period: { id: "p1", name: "Mẫu 5 đến 13/10", type: "program", state: "collecting", cutoff: "2026-10-13", exported_at: null }, reports: [], status: [{ period_id: "p1", period_name: "Tuần 41", department: "trong_trot", name: "Trồng trọt", kind: "form", state: "submitted", at: "2026-10-13T02:00:00Z" }] });
beforeEach(() => { vi.setSystemTime(new Date("2026-10-13T03:00:00Z")); requests.length = 0; sent = 1; canAct = false; auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "test", fetch: async (r) => { requests.push(r); if (r.url.includes("/dashboard"))
        return Response.json(data()); return Response.json({ counts: [], items: [{ code: "trong_trot", name: "Trồng trọt", active: true }] }); } }); });
afterEach(() => { vi.useRealTimers(); });
describe("Trang chủ tổng hợp KHTC", () => {
    it("hiện 5 thẻ, số thập phân và mốc Mẫu 5; lãnh đạo chỉ xem", async () => { render(<KhtcDashboardScreen />); await screen.findByText("23,81%"); expect(screen.getByRole("region", { name: "Chỉ số nóng" }).querySelectorAll("a")).toHaveLength(5); expect(screen.getByText("1/4")).toBeInTheDocument(); expect(screen.queryByRole("link", { name: "Mở kỳ mới" })).not.toBeInTheDocument(); expect(screen.getByRole("link", { name: "Chương trình lúa" })).toHaveAttribute("href", "/kn/khtc/tong-hop/p1"); expect(requests.every(r => r.method === "GET")).toBe(true); });
    it("gửi bộ lọc và làm mới đọc lại trạng thái, không sửa bản soạn", async () => { render(<KhtcDashboardScreen />); await screen.findByText("23,81%"); fireEvent.change(screen.getByRole("combobox", { name: "Quý" }), { target: { value: "3" } }); await waitFor(() => expect(requests.some(r => r.url.includes("quarter=3"))).toBe(true)); fireEvent.change(screen.getByRole("combobox", { name: "Lĩnh vực" }), { target: { value: "trong_trot" } }); await waitFor(() => expect(requests.some(r => r.url.includes("field=trong_trot"))).toBe(true)); sent = 2; fireEvent.click(screen.getByRole("button", { name: "Làm mới" })); await screen.findByText("2/4"); expect(requests.some(r => r.url.includes("/draft") || r.url.includes("/refresh"))).toBe(false); });
});
