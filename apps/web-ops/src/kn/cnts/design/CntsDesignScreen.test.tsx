import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CntsDesignScreen } from "./CntsDesignScreen";
const calls = vi.hoisted(() => ({ api: {}, preview: vi.fn(), list: vi.fn(async () => [{ code: "CNTS-BM04", bm: "BM-04", title: "Biểu đã cài", period_type: "week", due: "12:00", template_id: "id", version: 7, fields: [{ key: "latest", label: "Cột phiên bản bảy", type: "integer", required: true }] }]) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: calls.api }) }));
vi.mock("./api", async (importOriginal) => ({ ...await importOriginal<Record<string, unknown>>(), createDesignApi: () => calls }));
describe("Màn FN07 phần xem", () => {
  it("hiện schema/version được cài, xem trước và không có nút ghi giả", async () => {
    render(<CntsDesignScreen />); const select = await screen.findByLabelText("Biểu kỳ"); await screen.findByRole("option", { name: /BM-04/ }); fireEvent.change(select, { target: { value: "CNTS-BM04" } });
    expect(await screen.findByText("Phiên bản 7")).toBeInTheDocument(); expect(screen.getByText("Cột phiên bản bảy")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Xem trước phiên bản" })); expect(screen.getByLabelText("Xem trước Cột phiên bản bảy")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Lưu phiên bản/ })).not.toBeInTheDocument(); expect(screen.getByText(/Sửa, lưu phiên bản, sao chép/)).toBeInTheDocument();
    const file = new File(["x"], "wrong.xls"); fireEvent.change(screen.getByLabelText("Tệp cấu trúc"), { target: { files: [file] } }); expect(screen.getByRole("alert")).toHaveTextContent(".xlsx"); expect(screen.getByRole("button", { name: "Kiểm tra và xem trước tệp" })).toBeDisabled(); expect(calls.preview).not.toHaveBeenCalled();
  });
});
