import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { CntsDesignScreen } from "./CntsDesignScreen";
const f = (key: string, label: string) => ({ key, label, type: "decimal" as const, required: true, unit: "tấn" });
const calls = vi.hoisted(() => ({ api: {}, preview: vi.fn(), save: vi.fn(), create: vi.fn(), history: vi.fn(), list: vi.fn() }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: calls.api }) }));
vi.mock("./api", async (importOriginal) => ({ ...await importOriginal<Record<string, unknown>>(), createDesignApi: () => calls }));
beforeEach(() => {
  vi.clearAllMocks();
  calls.list.mockResolvedValue([
    { code: "CNTS-BM04", bm: "BM-04", title: "Biểu đã cài", kind: "report", period_type: "week", due: "12:00", locked: true, can_edit: false, template_id: "id4", version: 7, fields: [{ key: "latest", label: "Cột phiên bản bảy", type: "integer", required: true }] },
    { code: "CNTS-BM17", bm: "BM-17", title: "Sản lượng khai thác", kind: "report", period_type: "week", due: "12:00", locked: false, can_edit: true, template_id: "id17", version: 2, fields: [f("ca", "Cá"), f("tom", "Tôm")] },
  ]);
  calls.history.mockResolvedValue([{ template_id: "id17", version: 2, title: "Sản lượng khai thác", fields: [f("ca", "Cá"), f("tom", "Tôm")] }, { template_id: "old", version: 1, title: "Bản đầu", fields: [f("ca", "Cá Bản Đầu")] }]);
  calls.save.mockResolvedValue({ code: "CNTS-BM17", version: { template_id: "n", version: 3, title: "x", fields: [] } });
  calls.create.mockResolvedValue({ code: "CNTS-DT-TONGDAN", version: { template_id: "n", version: 1, title: "x", fields: [] } });
});
async function pick(code: string, option: RegExp) {
  render(<CntsDesignScreen />); const select = await screen.findByLabelText("Biểu kỳ"); await screen.findByRole("option", { name: option }); fireEvent.change(select, { target: { value: code } });
}
describe("Màn FN07 thiết kế biểu (#174, #192)", () => {
  it("mẫu KNQG chỉ xem và sao chép; xem trước; tệp sai đuôi không gửi", async () => {
    await pick("CNTS-BM04", /BM-04.*KNQG/);
    expect(await screen.findByText("Phiên bản 7")).toBeInTheDocument(); expect(screen.getByText("Cột phiên bản bảy")).toBeInTheDocument();
    expect(screen.getByText(/Mẫu KNQG — không sửa cấu trúc/)).toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Sửa cấu trúc" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Xem trước phiên bản" })); expect(screen.getByLabelText("Xem trước Cột phiên bản bảy")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Sao chép thành biểu điều tra" }));
    fireEvent.change(screen.getByLabelText(/Mã biểu điều tra/), { target: { value: "tongdan" } }); fireEvent.click(screen.getByRole("button", { name: "Sao chép" }));
    await waitFor(() => expect(calls.create).toHaveBeenCalledWith({ code_suffix: "TONGDAN", title: "Bản sao Biểu đã cài", source_code: "CNTS-BM04", source_version: 7 }));
    const file = new File(["x"], "wrong.xls"); fireEvent.change(screen.getByLabelText("Tệp cấu trúc"), { target: { files: [file] } }); expect(screen.getAllByRole("alert").at(-1)).toHaveTextContent(".xlsx"); expect(screen.getByRole("button", { name: "Kiểm tra và xem trước tệp" })).toBeDisabled(); expect(calls.preview).not.toHaveBeenCalled();
  });
  it("BM-17: xem lịch sử; chỉ thêm chỉ tiêu, lưu theo base_version; mã sai không gửi", async () => {
    await pick("CNTS-BM17", /BM-17/);
    const view = await screen.findAllByRole("button", { name: "Xem" }); fireEvent.click(view[1]!);
    expect(screen.getByText("Cấu trúc phiên bản 1")).toBeInTheDocument(); expect(screen.getByText("Cá Bản Đầu")).toBeInTheDocument(); expect(calls.history).toHaveBeenCalledWith("CNTS-BM17");
    fireEvent.click(screen.getByRole("button", { name: "Sửa cấu trúc" }));
    expect(screen.getByLabelText("Mã chỉ tiêu 1")).toBeDisabled(); expect(screen.queryAllByRole("button", { name: "Bỏ" })).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "+ Thêm chỉ tiêu" }));
    fireEvent.change(screen.getByLabelText("Mã chỉ tiêu 3"), { target: { value: "Sai Ma" } }); fireEvent.change(screen.getByLabelText("Tên chỉ tiêu 3"), { target: { value: "Cua" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu phiên bản mới" })); expect(await screen.findByText(/Chỉ tiêu 3: mã/)).toBeInTheDocument(); expect(calls.save).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Mã chỉ tiêu 3"), { target: { value: "cua" } }); fireEvent.change(screen.getByLabelText("Kiểu chỉ tiêu 3"), { target: { value: "decimal" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu phiên bản mới" }));
    await waitFor(() => expect(calls.save).toHaveBeenCalledWith("CNTS-BM17", 2, "Sản lượng khai thác", [f("ca", "Cá"), f("tom", "Tôm"), { key: "cua", label: "Cua", type: "decimal", required: false }]));
  });
});
