import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { createPriceConfigStore } from "@/api/mock/kn-price";
import { PriceCalendarScreen } from "./CalendarConfig";
import { PriceConfigDetailScreen } from "./CalendarDetail";
import { Decimal } from "@/lib/exactDecimal";

const nav = vi.hoisted(() => ({ path: "/kn/bao-gia/cau-hinh", push: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => nav.path,
  useRouter: () => ({ push: nav.push, replace: vi.fn() }),
}));
const auth = {
  api: null as ApiClient | null,
  manage: true,
  context: "config-a",
};
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: auth.api,
    principal: { subject: "officer", tenantId: "tenant-a" },
    kn: { context: { context_id: auth.context } },
  }),
}));

const T0 = Date.parse("2026-10-12T03:00:00Z");
let store: ReturnType<typeof createPriceConfigStore>;
const writes: { path: string; body: unknown; key: string | null }[] = [];
let failNext = false;
let fieldFailure: string | null = null;

beforeEach(() => {
  nav.path = "/kn/bao-gia/cau-hinh";
  nav.push.mockReset();
  auth.manage = true;
  auth.context = "config-a";
  store = createPriceConfigStore(() => T0);
  writes.length = 0;
  failNext = false;
  fieldFailure = null;
  auth.api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => null,
    fetch: async (req) => {
      const path = new URL(req.url).pathname;
      if (req.method === "GET") {
        if (path === "/v1/price/calendar")
          return Response.json(store.calendar(auth.manage));
        if (path === "/v1/price/thresholds")
          return Response.json(store.thresholds(auth.manage));
        return Response.json({
          from: "2026-10-01",
          to: "2026-11-01",
          items: [],
        });
      }
      const body = await req
        .clone()
        .json()
        .catch(() => null);
      writes.push({ path, body, key: req.headers.get("Idempotency-Key") });
      if (failNext) {
        failNext = false;
        return Response.json(
          {
            status: 503,
            code: "dependency_unavailable",
            title: "Tạm thời chưa sẵn sàng",
          },
          { status: 503 },
        );
      }
      if (fieldFailure)
        return Response.json(
          {
            status: 422,
            code: "validation_failed",
            title: "Dữ liệu không hợp lệ",
            errors: [
              { field: fieldFailure, reason: "Ngưỡng bị từ chối tại máy chủ" },
            ],
          },
          { status: 422 },
        );
      const res =
        path === "/v1/price/calendar/schedule"
          ? store.saveSchedule(auth.manage, body)
          : path === "/v1/price/calendar/exceptions"
            ? store.addException(auth.manage, body)
            : path === "/v1/price/thresholds"
              ? store.saveThreshold(auth.manage, body)
              : /^\/v1\/price\/thresholds\/(group|item)\/[^/]+\/retire$/.test(
                    path,
                  )
                ? store.retireThreshold(
                    auth.manage,
                    path.split("/")[4]!,
                    path.split("/")[5]!,
                  )
                : null;
      if (!res) return Response.json({}, { status: 200 });
      return res.ok
        ? res.status === 204
          ? new Response(null, { status: 204 })
          : Response.json(res.body, { status: res.status })
        : Response.json(res, { status: res.status });
    },
  });
});

describe("BF-SWEEP-PRICE cấu hình trên trang riêng", () => {
  it("mở xem/sửa bằng route, danh sách không còn form ghi inline", async () => {
    render(<PriceCalendarScreen />);
    expect(
      await screen.findByRole("link", { name: "Xem kỳ báo giá & giờ khóa" }),
    ).toHaveAttribute("href", "/kn/bao-gia/cau-hinh/lich");
    expect(
      screen.queryByRole("button", { name: "Lưu cấu hình" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Lịch ngoại lệ" }));
    expect(screen.getByRole("link", { name: "Thêm ngoại lệ" })).toHaveAttribute(
      "href",
      "/kn/bao-gia/cau-hinh/ngoai-le/them-moi",
    );
    expect(
      within(screen.getByRole("table", { name: "Lịch ngoại lệ" })).getByRole(
        "columnheader",
        { name: "STT" },
      ),
    ).toBeInTheDocument();
    await waitFor(() => expect(writes).toHaveLength(0));
  });

  it("giữ liên kết dưới alias phòng ban", async () => {
    nav.path = "/kn/phong-ban/thong_tin_tuyen_truyen/bao-gia-lich";
    render(<PriceCalendarScreen />);
    expect(
      await screen.findByRole("link", { name: "Sửa kỳ báo giá" }),
    ).toHaveAttribute("href", `${nav.path}/lich/sua`);
  });

  it("kỳ báo giá kiểm tại ô, giữ lịch nhắc và ngày áp dụng trước khi lưu", async () => {
    render(
      <PriceConfigDetailScreen route={{ kind: "schedule", edit: true }} />,
    );
    const lock = await screen.findByLabelText(/Giờ khóa nhập/);
    fireEvent.change(lock, { target: { value: "11:30" } });
    fireEvent.change(screen.getByLabelText(/Hạn phản hồi xác minh/), {
      target: { value: "11:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));
    expect(
      await screen.findByText("Hạn phản hồi xác minh phải sau giờ khóa."),
    ).toBeInTheDocument();
    expect(writes).toHaveLength(0);
    fireEvent.change(screen.getByLabelText(/Hạn phản hồi xác minh/), {
      target: { value: "13:30" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));
    await waitFor(() =>
      expect(nav.push).toHaveBeenCalledWith("/kn/bao-gia/cau-hinh/lich"),
    );
    expect(writes[0]?.body).toMatchObject({
      lock_at: "11:30",
      valid_from: "2026-10-13",
      remind1_at: "07:30",
      remind2_at: "11:00",
    });
  });

  it("thêm ngoại lệ phải đủ ngày/lý do, lưu xong mở trang chi tiết", async () => {
    render(<PriceConfigDetailScreen route={{ kind: "exception-new" }} />);
    await screen.findByRole("button", { name: "Lưu ngoại lệ" });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngoại lệ" }));
    expect(screen.getByLabelText(/Lý do/)).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(writes).toHaveLength(0);
    fireEvent.change(screen.getByLabelText(/Ngày ngoại lệ/), {
      target: { value: "2026-10-13" },
    });
    fireEvent.change(screen.getByLabelText(/Lý do/), {
      target: { value: "Ngày nghỉ thử nghiệm" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngoại lệ" }));
    await waitFor(() =>
      expect(nav.push).toHaveBeenCalledWith(
        "/kn/bao-gia/cau-hinh/ngoai-le/2026-10-13",
      ),
    );
  });

  it("nhắc nhập giá chặn biến lạ, giữ thông tin lịch không sửa", async () => {
    render(
      <PriceConfigDetailScreen route={{ kind: "reminder", edit: true }} />,
    );
    const template = await screen.findByLabelText(/Nội dung mẫu/);
    fireEvent.change(template, { target: { value: "Nhắc {tên}" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));
    expect(
      await screen.findByText(/Nội dung mẫu có biến không hỗ trợ/),
    ).toBeInTheDocument();
    expect(writes).toHaveLength(0);
    fireEvent.change(template, {
      target: { value: "Nhắc {ngày} trước {giờ khóa}" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]?.body).toMatchObject({
      lock_at: "12:00",
      respond_by: "13:30",
      weekdays: ["mon", "tue", "wed", "thu", "fri"],
      valid_from: "2026-10-13",
    });
  });

  it.each([
    "Ngưỡng mặc định ±%",
    "Số ngày tìm giá kỳ trước (N)",
    "Ngưỡng đi ngang %",
    "Ngưỡng phiếu điều tra %",
  ])("%s chặn hostile trước Decimal.parse cả blur và lưu", async (label) => {
    render(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    const input = await screen.findByLabelText(
      new RegExp(label.replace(/[()*+?^$.[\]{}|\\]/g, "\\$&")),
    );
    const parse = vi.spyOn(Decimal, "parse");
    try {
      for (const bad of [
        "1e100000",
        "1e-100000",
        "9".repeat(100000),
        "-1",
        "0",
        label.startsWith("Số ngày") ? "61" : "100.01",
      ]) {
        parse.mockClear();
        fireEvent.change(input, { target: { value: bad } });
        fireEvent.blur(input);
        fireEvent.click(screen.getByRole("button", { name: "Lưu ngưỡng" }));
        expect(parse, bad.slice(0, 30)).not.toHaveBeenCalled();
        expect(input).toHaveAttribute("aria-invalid", "true");
        expect(writes).toHaveLength(0);
      }
    } finally {
      parse.mockRestore();
    }
  });

  it("số đã chuẩn hóa được gửi và lưu tại biên 100%, N60", async () => {
    render(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    fireEvent.change(await screen.findByLabelText(/Ngưỡng mặc định/), {
      target: { value: "100,00" },
    });
    fireEvent.change(screen.getByLabelText(/Số ngày tìm/), {
      target: { value: "60" },
    });
    fireEvent.change(screen.getByLabelText(/Ngưỡng đi ngang/), {
      target: { value: "0,01" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngưỡng" }));
    await waitFor(() =>
      expect(nav.push).toHaveBeenCalledWith(
        "/kn/bao-gia/cau-hinh/nguong/mac-dinh",
      ),
    );
    expect(writes[0]?.body).toEqual({
      scope: "default",
      code: "",
      pct: "100",
      lookback_days: 60,
      flat_pct: "0.01",
      survey_pct: "40",
    });
  });

  it("lỗi tại trường từ máy chủ nằm dưới ô tương ứng", async () => {
    fieldFailure = "flat_pct";
    render(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    const input = await screen.findByLabelText(/Ngưỡng đi ngang/);
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngưỡng" }));
    await waitFor(() => expect(input).toHaveAttribute("aria-invalid", "true"));
    expect(
      within(input.closest(".field")!).getByRole("alert"),
    ).toHaveTextContent("Ngưỡng bị từ chối tại máy chủ");
  });

  it("retry cùng dữ liệu giữ khóa idempotency", async () => {
    failNext = true;
    render(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    await screen.findByRole("button", { name: "Lưu ngưỡng" });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngưỡng" }));
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngưỡng" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalled());
    expect(writes).toHaveLength(2);
    expect(writes[0]?.key).toBeTruthy();
    expect(writes[1]?.key).toBe(writes[0]?.key);
  });

  it("đổi context bỏ draft cũ và không cho thao tác khi capability chỉ xem", async () => {
    const view = render(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    fireEvent.change(await screen.findByLabelText(/Ngưỡng mặc định/), {
      target: { value: "45" },
    });
    auth.context = "config-b";
    auth.manage = false;
    view.rerender(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    expect(await screen.findByRole("note")).toHaveTextContent("Chỉ xem");
    expect(
      screen.queryByRole("button", { name: "Lưu ngưỡng" }),
    ).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  });

  it("đổi context có quyền vẫn nạp lại form và xóa giá trị nháp cũ", async () => {
    const view = render(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    fireEvent.change(await screen.findByLabelText(/Ngưỡng mặc định/), {
      target: { value: "45" },
    });
    auth.context = "config-b";
    view.rerender(
      <PriceConfigDetailScreen
        route={{ kind: "threshold", scope: "default", code: "", edit: true }}
      />,
    );
    expect(await screen.findByLabelText(/Ngưỡng mặc định/)).toHaveValue(
      "30.00",
    );
    expect(writes).toHaveLength(0);
  });

  it("sửa ngưỡng giữ định danh, chi tiết có log người/thời điểm/giá trị trước-sau", async () => {
    store.saveThreshold(true, { scope: "group", code: "rau_mau", pct: "40" });
    const view = render(
      <PriceConfigDetailScreen
        route={{
          kind: "threshold",
          scope: "group",
          code: "rau_mau",
          edit: true,
        }}
      />,
    );
    expect(await screen.findByLabelText(/Mã nhóm hàng/)).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/Ngưỡng ±%/), {
      target: { value: "45" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ngưỡng" }));
    await waitFor(() =>
      expect(nav.push).toHaveBeenCalledWith(
        "/kn/bao-gia/cau-hinh/nguong/group/rau_mau",
      ),
    );
    view.rerender(
      <PriceConfigDetailScreen
        route={{
          kind: "threshold",
          scope: "group",
          code: "rau_mau",
          edit: false,
        }}
      />,
    );
    const log = await screen.findByLabelText("Log");
    await waitFor(() => expect(log).toHaveTextContent("40.00 → 45.00"));
    expect(log).toHaveTextContent("Mã cán bộ");
  });

  it("xóa ngưỡng riêng phải xác nhận, không có thao tác xóa mặc định", async () => {
    store.saveThreshold(true, { scope: "group", code: "rau_mau", pct: "40" });
    render(<PriceCalendarScreen />);
    await screen.findByRole("link", { name: "Xem kỳ báo giá & giờ khóa" });
    fireEvent.click(screen.getByRole("tab", { name: "Ngưỡng cảnh báo" }));
    const table = await screen.findByRole("table", {
      name: "Ngưỡng theo nhóm hàng / mặt hàng",
    });
    fireEvent.click(
      within(table).getByRole("button", { name: "Xóa", exact: true }),
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(writes).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận xóa" }));
    await waitFor(() => expect(store.thresholds(true).items).toHaveLength(0));
    expect(writes[0]?.path).toBe("/v1/price/thresholds/group/rau_mau/retire");
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Xóa", exact: true }),
      ).not.toBeInTheDocument(),
    );
  });
});
