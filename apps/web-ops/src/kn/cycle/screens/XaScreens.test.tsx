// BF-TT-FORMS (#107, PRD UP-04): bộ lọc Biểu đủ T01–T07, M01, V01–V06; thanh chọn kỳ lùi/tiến có hạn nộp và tên mẫu;
// "Nhập từ Excel" (máy chủ đọc tệp, web ghép dòng vào lưới) qua backend giả lập.
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RcTaskDetail } from "../api";
import { PeriodBar, TasksScreen } from "./XaScreens";

vi.mock("next/navigation", () => ({ usePathname: () => "/kn/nhiem-vu/cong-viec", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }), useParams: () => ({}) }));

const auth: { cycleApi: Record<string, ReturnType<typeof vi.fn>> } = { cycleApi: {} };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: {}, kn: { context: { role: "kn_xa", communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "trong_trot" } }, cycleApi: auth.cycleApi }),
}));

const task = (id: string, slot: number, label: string) => ({ id, slot, period_label: label, period_range: `r${slot}`, batch_id: "b1", form_code: "T01", commune_code: "91301" });

beforeEach(() => {
  auth.cycleApi = {
    tasks: vi.fn().mockResolvedValue({ items: [task("t3", 3, "Tuần 3"), task("t1", 1, "Tuần 1"), task("t2", 2, "Tuần 2")], counts: { todo: 0, overdue: 0, adjustment_requested: 0 }, last_reminder: null }),
  };
});

describe("Biểu TT xã (UP-04)", () => {
  it("bộ lọc Biểu đủ 14 biểu T01–T07, M01, V01–V06", async () => {
    render(<TasksScreen />);
    const chip = screen.getByRole("combobox", { name: "Biểu" });
    expect(within(chip).getAllByRole("option").map((o) => o.textContent)).toEqual(["Tất cả", "T01", "T02", "T03", "T04", "T05", "T06", "T07", "M01", "V01", "V02", "V03", "V04", "V05", "V06"]);
  });

  it("thanh chọn kỳ: lùi/tiến tới đúng công việc cùng đợt × biểu × xã, có hạn nộp và tên mẫu", async () => {
    const detail = { task: task("t2", 2, "Tuần 2"), period: { label: "Tuần 2", range_label: "08/10–14/10", due_at: "2026-10-15T05:00:00Z" }, form_name: "Tiến độ xuống giống lúa" } as unknown as RcTaskDetail;
    render(<PeriodBar detail={detail} />);
    await waitFor(() => expect(auth.cycleApi.tasks).toHaveBeenCalledWith({ batch_id: "b1", form_code: "T01", commune: "91301" }));
    const nav = screen.getByRole("navigation", { name: "Chọn kỳ báo cáo" });
    expect(await within(nav).findByRole("link", { name: "Kỳ trước: Tuần 1 (r1)" })).toHaveAttribute("href", "/kn/nhiem-vu/cong-viec/t1");
    expect(within(nav).getByRole("link", { name: "Kỳ sau: Tuần 3 (r3)" })).toHaveAttribute("href", "/kn/nhiem-vu/cong-viec/t3");
    expect(nav).toHaveTextContent("kỳ 2/3");
    expect(nav).toHaveTextContent(/Hạn nộp .*15\/10/);
    expect(nav).toHaveTextContent("Mẫu T01 — Tiến độ xuống giống lúa");
  });

  it("kỳ đầu: nút Kỳ trước bị khóa", async () => {
    const detail = { task: task("t1", 1, "Tuần 1"), period: { label: "Tuần 1", range_label: "01/10–07/10", due_at: "2026-10-08T05:00:00Z" }, form_name: "T01" } as unknown as RcTaskDetail;
    render(<PeriodBar detail={detail} />);
    const nav = screen.getByRole("navigation", { name: "Chọn kỳ báo cáo" });
    expect(await within(nav).findByRole("link", { name: /Kỳ sau: Tuần 2/ })).toBeInTheDocument();
    expect(within(nav).getByRole("button", { name: "Kỳ trước: không có" })).toBeDisabled();
  });
});
