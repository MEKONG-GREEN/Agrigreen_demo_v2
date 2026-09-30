import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ApiProblem, toProblem } from "@/api/problem";
import { Flags, Money, ParamEditor, type ParamView } from "./g4kit";

const ME = "0192f000-0000-7000-8000-00000000a00b";
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ principal: { subject: "0192f000-0000-7000-8000-00000000a00b", displayName: "Kế toán", tenantId: null, roles: ["cost_accountant", "chief_accountant"], mfa: true } }),
}));

describe("thành phần G4", () => {
  it("Money định dạng VND vi-VN từ chuỗi thập phân", () => {
    render(<Money v="24813134.93" strong />);
    expect(screen.getByText("24.813.134,93 ₫")).toBeInTheDocument();
  });

  it("Flags hiện nhãn tiếng Việt, không có cờ → gạch", () => {
    const { rerender } = render(<Flags flags={["payload_exceeded", "registration_expiring"]} />);
    expect(screen.getByRole("list", { name: "Cảnh báo" })).toHaveTextContent("Tấn ước > 1,1 × tải thực chở");
    expect(screen.getByText("Sắp hết hạn đăng kiểm")).toBeInTheDocument();
    rerender(<Flags flags={[]} />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("ParamEditor: người đề xuất thấy nhắc R-56; duyệt bị máy chủ chặn → thông báo tự phê duyệt", async () => {
    const view: ParamView = {
      key: "costing.loss.expected_pct",
      value: "5",
      version: "costing.loss.expected_pct#0",
      provisional: true,
      default: "5",
      versions: [{ id: "1", seq: 1, value: "6", effective_from: "2026-10-01", status: "pending", provisional: false, proposed_by: ME, reason: "Hao hụt thực đo cao hơn", decided_by: null, decided_at: null, decision_reason: null }],
    };
    const decide = vi.fn().mockRejectedValue(new ApiProblem(toProblem({ code: "self_approval_forbidden", status: 403, title: "x", type: "t", trace_id: "abc" })));
    render(<ParamEditor keys={["costing.loss.expected_pct"]} labels={{}} load={() => Promise.resolve(view)} propose={vi.fn()} decide={decide} canPropose canDecide approverLabel="kế toán trưởng" />);
    expect(await screen.findByText("Bạn là người đề xuất — cần một người phê duyệt khác (R-56)")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Gửi đề xuất tham số" })).not.toBeInTheDocument();
    // Từ chối cần lý do ≥ 10 ký tự.
    expect(screen.getByRole("button", { name: "Từ chối" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Duyệt tham số" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Không được tự phê duyệt"));
    expect(decide).toHaveBeenCalledWith("costing.loss.expected_pct", "1", "approved");
  });

  it("ParamEditor: không có phiên bản chờ → đề xuất giá trị mới cần lý do", async () => {
    const propose = vi.fn().mockResolvedValue({});
    const load = vi.fn().mockResolvedValue({ key: "k", value: "2200", version: "k:default", provisional: true, default: "2200", versions: [] } satisfies ParamView);
    render(<ParamEditor keys={["k"]} labels={{ k: "Đơn giá" }} load={load} propose={propose} decide={vi.fn()} canPropose canDecide={false} approverLabel="kế toán trưởng" />);
    await screen.findByText("Chưa có phiên bản nào — đang dùng mặc định tạm, chờ khách.");
    await userEvent.type(screen.getByLabelText("Giá trị mới"), "2400");
    expect(screen.getByRole("button", { name: "Gửi đề xuất tham số" })).toBeDisabled();
    await userEvent.type(screen.getByLabelText("Lý do đề xuất"), "Giá dầu tăng quý IV");
    await userEvent.click(screen.getByRole("button", { name: "Gửi đề xuất tham số" }));
    await waitFor(() => expect(propose).toHaveBeenCalledWith("k", { value: "2400", reason: "Giá dầu tăng quý IV" }));
    expect(load).toHaveBeenCalledTimes(2);
  });
});
