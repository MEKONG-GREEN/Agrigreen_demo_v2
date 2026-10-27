// BF-SWEEP-CNTS (#101): màn Chăn nuôi – Thủy sản dùng UI-KIT — ô số không nhận số âm (CN-01), trường bắt buộc báo lỗi theo
// trường và KHÔNG gọi API khi còn lỗi (R-A4), lý do ngắn trong pop-up thay window.prompt, bảng có STT (R-A5).
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { AlertScreen } from "./alert/AlertScreen";
import { ProjectsScreen } from "./model/ProjectsScreen";
import { OutbreakCreateScreen } from "./outbreak/OutbreakCreateScreen";
import { OutbreakScreen } from "./outbreak/OutbreakScreen";
import { CntsPublishScreen } from "./periods/CntsPublishScreen";
import { VaccinationScreen } from "./schedules/VaccinationScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

const calls: string[] = [];
const auth: { api: ApiClient | null; role: string } = { api: null, role: "kn_tinh" };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role, communes: [{ code: "91301", name: "Mỹ Hòa Hưng" }], department_code: "chan_nuoi_thuy_san" } } }),
}));

function useApi(persona: string) {
  auth.api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${persona}`,
    getContextId: () => null,
    fetch: async (req: Request) => {
      calls.push(`${req.method} ${new URL(req.url).pathname}`);
      return mockFetchNoDelay(req);
    },
  });
}

const writes = (path: string) => calls.filter((c) => !c.startsWith("GET ") && c.includes(path));
const alerts = () => screen.getAllByRole("alert").map((a) => a.textContent);

beforeEach(() => {
  calls.length = 0;
  auth.role = "kn_tinh";
  useApi("kn-binh");
});

describe("Biểu kỳ CNTS — phát hành", () => {
  it("bảng biểu mẫu có STT; thiếu ngày / đơn vị nhận hoặc giờ sai → lỗi theo trường, không gọi xem trước", async () => {
    const u = userEvent.setup();
    render(<CntsPublishScreen />);
    const table = await screen.findByRole("table", { name: "Biểu mẫu" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    await u.clear(screen.getByLabelText(/Từ ngày/));
    await u.type(screen.getByLabelText(/Giờ hạn nộp/), "25:00");
    await u.click(screen.getByRole("button", { name: "Xem trước" }));
    expect(alerts()).toEqual(expect.arrayContaining(["Bắt buộc nhập.", "Chọn ít nhất một đơn vị nhận.", "Giờ dạng HH:MM, vd 16:00."]));
    expect(writes("/preview")).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Dịch bệnh — tin báo nghi dịch", () => {
  it("số con âm và thiếu trường bắt buộc bị chặn trước khi gửi", async () => {
    auth.role = "kn_xa";
    useApi("kn-an");
    const u = userEvent.setup();
    render(<OutbreakScreen />);
    await u.click(screen.getByRole("button", { name: "Ghi nhận tin báo nghi dịch" }));
    await u.type(screen.getByLabelText(/Số con chết tin báo/), "-3");
    await u.click(screen.getByRole("button", { name: "Ghi nhận" }));
    expect(alerts()).toEqual(expect.arrayContaining(["Không được nhập số âm.", "Bắt buộc nhập."]));
    expect(writes("/suspects")).toHaveLength(0);
  });

  it("BM-10: quy mô đàn 0, số con chết âm → lỗi ngay tại ô, không gửi Cấp Tỉnh", async () => {
    auth.role = "kn_xa";
    useApi("kn-an");
    const u = userEvent.setup();
    // BF-CN-OUTBREAK (UP-08): lập báo cáo trên trang riêng; ô số mặc định 0.
    render(<OutbreakCreateScreen />);
    expect(screen.getByLabelText(/DB-06 Quy mô đàn/)).toHaveValue("0");
    await u.clear(screen.getByLabelText(/DB-06 Quy mô đàn/));
    await u.type(screen.getByLabelText(/DB-06 Quy mô đàn/), "0");
    await u.clear(screen.getByLabelText(/DB-07 Số con chết/));
    await u.type(screen.getByLabelText(/DB-07 Số con chết/), "-1");
    await u.click(screen.getByRole("button", { name: "Gửi Cấp Tỉnh" }));
    expect(alerts()).toEqual(expect.arrayContaining(["Phải lớn hơn 0.", "Không được nhập số âm."]));
    expect(writes("/outbreaks")).toHaveLength(0);
  });
});

describe("Cảnh báo — cấu hình nhắc nhở", () => {
  it("bảng có STT; giờ nhắc trước hạn âm / quá 168 bị chặn, không lưu", async () => {
    const u = userEvent.setup();
    render(<AlertScreen />);
    await u.click(screen.getByRole("tab", { name: "Cấu hình nhắc nhở" }));
    const table = await screen.findByRole("table", { name: "Nhắc nhở theo biểu" });
    expect(within(table).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    await u.click(within(table).getAllByRole("button", { name: "Sửa" })[0]!);
    const before = screen.getByLabelText(/Nhắc trước hạn/);
    for (const [raw, msg] of [
      ["-1", "Không được nhập số âm."],
      ["200", "Không được lớn hơn 168."],
      ["1,5", "Chỉ nhập số nguyên."],
    ] as const) {
      await u.clear(before);
      await u.type(before, raw);
      await u.click(screen.getByRole("button", { name: "Lưu" }));
      expect(alerts()).toContain(msg);
    }
    expect(writes("/alert-config")).toHaveLength(0);
  });
});

describe("Lịch tiêm — tạo lịch theo quy trình", () => {
  it("tuổi lúc nhận âm, số con 0 và thiếu trường bắt buộc → lỗi theo trường, không gọi API", async () => {
    const u = userEvent.setup();
    render(<VaccinationScreen />);
    await u.click(screen.getByRole("button", { name: "Tạo lịch theo quy trình" }));
    const age = screen.getByLabelText(/Tuổi lúc nhận/);
    await u.clear(age);
    await u.type(age, "-1");
    await u.type(screen.getByLabelText(/Số con/), "0");
    await u.click(screen.getByRole("button", { name: "Tạo lịch" }));
    expect(alerts()).toEqual(expect.arrayContaining(["Không được nhập số âm.", "Phải lớn hơn 0.", "Bắt buộc nhập."]));
    expect(writes("/vaccination")).toHaveLength(0);
  });
});

describe("Chương trình, dự án — tạo dự án", () => {
  it("kinh phí âm và thiếu mã/tên → lỗi theo trường, không gọi API", async () => {
    const u = userEvent.setup();
    render(<ProjectsScreen />);
    await u.click(await screen.findByRole("button", { name: "Tạo dự án" }));
    await u.type(screen.getByLabelText(/Kinh phí/), "-5");
    await u.click(screen.getByRole("button", { name: "Lưu" }));
    expect(alerts()).toEqual(expect.arrayContaining(["Không được nhập số âm.", "Bắt buộc nhập."]));
    expect(writes("/projects")).toHaveLength(0);
  });
});

describe("Danh sách CNTS — bảng có STT", () => {
  it("ổ dịch và tin báo hiển thị bằng DataTable", async () => {
    render(<OutbreakScreen />);
    for (const name of ["Danh sách ổ dịch", "Danh sách tin báo nghi dịch"]) {
      const t = await screen.findByRole("table", { name });
      expect(within(t).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    }
    await waitFor(() => expect(calls.some((c) => c.includes("/outbreaks"))).toBe(true));
  });
});
