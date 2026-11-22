import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { PointsScreen } from "./PointsScreen";
import { PricePointDetailScreen } from "./PointDetail";
import { createPricePointApi } from "./api";

const nav = vi.hoisted(() => ({
  query: "at=2026-11-05&region=an_giang",
  push: vi.fn(),
  replace: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/xa-bao-gia",
  useSearchParams: () => new URLSearchParams(nav.query),
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
}));
const auth = vi.hoisted(() => ({
  api: null as ApiClient | null,
  contextId: "admin182",
  subject: "admin182",
}));
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    api: auth.api,
    principal: { tenantId: "tenant182", subject: auth.subject },
    kn: { context: { role: "admin", context_id: auth.contextId } },
  }),
}));
async function setupClient(persona = "kn-admin", asg = "asg-admin") {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${persona}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx182-${persona}-${Math.random()}`,
      },
      body: JSON.stringify({
        assignment_id: asg,
        department_code: "trong_trot",
      }),
    }),
  );
  const context = (await res.json()) as { context_id: string };
  const fetch = vi.fn(mockFetchNoDelay);
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${persona}`,
    getContextId: () => context.context_id,
    fetch,
  });
  auth.api = api;
  auth.contextId = context.context_id;
  auth.subject = persona;
  return { pa: createPricePointApi(api), fetch, api };
}
function postCount(fetch: ReturnType<typeof vi.fn>) {
  return fetch.mock.calls.filter((args) => {
    const r = args[0] as Request;
    return (
      r.method === "POST" && new URL(r.url).pathname.startsWith("/v1/price/")
    );
  }).length;
}
beforeEach(() => {
  vi.setSystemTime(new Date("2026-11-05T02:00:00Z"));
  resetPricePoints();
  nav.query = "at=2026-11-05&region=an_giang";
  nav.push.mockReset();
  nav.replace.mockReset();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("BF-SWEEP-PRICE-FORMS182 — xã báo giá trên trang riêng", () => {
  it("danh sách có STT, link riêng và mang theo bộ lọc, không mở form popup", async () => {
    await setupClient();
    render(<PointsScreen />);
    const table = await screen.findByRole("table", {
      name: "Danh mục xã báo giá",
    });
    expect(
      within(table).getByRole("columnheader", { name: "STT" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: "Thêm xã báo giá" }),
    ).toHaveAttribute(
      "href",
      "/kn/bao-gia/xa-bao-gia/them-moi?at=2026-11-05&tab=points&region=an_giang",
    );
    expect(
      within(table).getByRole("link", { name: "XBG0001" }),
    ).toHaveAttribute(
      "href",
      "/kn/bao-gia/xa-bao-gia/XBG0001?at=2026-11-05&tab=points&region=an_giang",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("thêm: required theo ô, loại ĐVHC đã gắn; Lưu dùng API hiện hữu và Hủy có URL danh sách", async () => {
    const { fetch, pa } = await setupClient();
    render(<PricePointDetailScreen route={{ kind: "point-new" }} />);
    const unit = await screen.findByRole("combobox", { name: /ĐVHC \(xã\)/ });
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText("Bắt buộc nhập.")).toBeInTheDocument();
    expect(postCount(fetch)).toBe(0);
    expect(
      within(unit).queryByRole("option", { name: /30280/ }),
    ).not.toBeInTheDocument();
    await userEvent.selectOptions(unit, "31126");
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() =>
      expect(nav.push).toHaveBeenCalledWith(
        expect.stringContaining("/XBG0004?at=2026-11-05"),
      ),
    );
    expect(postCount(fetch)).toBe(1);
    expect(
      (await pa.point("XBG0004", "2026-11-06")).point.admin_unit_code,
    ).toBe("31126");
    expect(screen.getByRole("link", { name: "Hủy" })).toHaveAttribute(
      "href",
      "/kn/bao-gia/xa-bao-gia?at=2026-11-05&tab=points&region=an_giang",
    );
  });
  it("chi tiết hiện Log thật cũ→mới/actor/time và lịch sử phân công", async () => {
    const { pa } = await setupClient();
    await pa.revisePoint("XBG0001", {
      note: "Điều chỉnh vùng theo danh mục",
      region_code: "kien_giang",
    });
    render(
      <PricePointDetailScreen
        route={{ kind: "point", code: "XBG0001", mode: "view" }}
      />,
    );
    const log = await screen.findByRole("complementary", { name: "Log" });
    await waitFor(() =>
      expect(
        within(log).getByText("Điều chỉnh vùng theo danh mục", {
          exact: false,
        }),
      ).toBeInTheDocument(),
    );
    expect(within(log).getAllByText(/Mã tham chiếu/).length).toBeGreaterThan(0);
    expect(
      await screen.findByRole("table", { name: "Lịch sử phân công" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("Cấp Tỉnh mở URL sửa/thêm trực tiếp vẫn chỉ xem, không có lệnh ghi", async () => {
    const { fetch } = await setupClient("kn-binh", "asg-binh-tinh");
    const r = render(
      <PricePointDetailScreen
        route={{ kind: "point", code: "XBG0001", mode: "edit" }}
      />,
    );
    expect(
      await screen.findByText(/Chỉ xem — bạn chưa có quyền/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Lưu" }),
    ).not.toBeInTheDocument();
    r.rerender(<PricePointDetailScreen route={{ kind: "point-new" }} />);
    expect(
      await screen.findByText(/Chỉ xem — bạn chưa có quyền/),
    ).toBeInTheDocument();
    expect(postCount(fetch)).toBe(0);
  });
  it("nhóm vùng có route và lỗi tại ô; không dựng nhật ký giả khi hợp đồng chưa có", async () => {
    nav.query = "at=2026-11-05&tab=regions";
    const { fetch } = await setupClient();
    render(
      <PricePointDetailScreen
        route={{ kind: "region", code: "an_giang", edit: true }}
      />,
    );
    const name = await screen.findByRole("textbox", { name: /Tên nhóm vùng/ });
    await userEvent.clear(name);
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText("Bắt buộc nhập.")).toBeInTheDocument();
    expect(postCount(fetch)).toBe(0);
    expect(
      screen.getByText(/Chưa có nhật ký thay đổi của nhóm vùng/),
    ).toBeInTheDocument();
    await userEvent.type(name, "Vùng An Giang");
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() =>
      expect(nav.push).toHaveBeenCalledWith(
        expect.stringContaining("nhom-vung/an_giang?"),
      ),
    );
    expect(postCount(fetch)).toBe(1);
  });
  it("mất phản hồi sau commit: thử lại cùng dữ liệu giữ khóa, không tạo xã lần hai", async () => {
    const { fetch, pa } = await setupClient();
    render(<PricePointDetailScreen route={{ kind: "point-new" }} />);
    const unit = await screen.findByRole("combobox", { name: /ĐVHC \(xã\)/ });
    await userEvent.selectOptions(unit, "31126");
    let lost = false;
    fetch.mockImplementation(
      async (...args: Parameters<typeof mockFetchNoDelay>) => {
        const response = await mockFetchNoDelay(...args);
        const request = args[0] as Request;
        if (
          !lost &&
          request.method === "POST" &&
          new URL(request.url).pathname === "/v1/price/points"
        ) {
          lost = true;
          throw new TypeError("lost committed response182");
        }
        return response;
      },
    );
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(lost).toBe(true));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Lưu" })).toBeEnabled(),
    );
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalled());
    const writes = fetch.mock.calls
      .map((args) => args[0] as Request)
      .filter(
        (r) =>
          r.method === "POST" && new URL(r.url).pathname === "/v1/price/points",
      );
    expect(writes).toHaveLength(2);
    expect(writes[0]!.headers.get("Idempotency-Key")).toBe(
      writes[1]!.headers.get("Idempotency-Key"),
    );
    expect(
      (await pa.points({ at: "2026-11-06" })).points.filter(
        (p) => p.admin_unit_code === "31126",
      ),
    ).toHaveLength(1);
  });
  it("đổi phiên trong lúc ghi: phản hồi cũ không điều hướng hoặc mang form Admin sang phiên chỉ xem", async () => {
    const old = await setupClient();
    const view = render(
      <PricePointDetailScreen route={{ kind: "point-new" }} />,
    );
    await userEvent.selectOptions(
      await screen.findByRole("combobox", { name: /ĐVHC \(xã\)/ }),
      "31126",
    );
    let release: (() => void) | undefined;
    old.fetch.mockImplementation(
      async (...args: Parameters<typeof mockFetchNoDelay>) => {
        const response = await mockFetchNoDelay(...args);
        const request = args[0] as Request;
        if (
          request.method === "POST" &&
          new URL(request.url).pathname === "/v1/price/points"
        )
          await new Promise<void>((resolve) => {
            release = resolve;
          });
        return response;
      },
    );
    await userEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => expect(release).toBeTypeOf("function"));
    const current = await setupClient("kn-binh", "asg-binh-tinh");
    view.rerender(<PricePointDetailScreen route={{ kind: "point-new" }} />);
    expect(
      await screen.findByText(/Chỉ xem — bạn chưa có quyền/),
    ).toBeInTheDocument();
    await act(async () => {
      release!();
    });
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Lưu" }),
      ).not.toBeInTheDocument(),
    );
    expect(nav.push).not.toHaveBeenCalled();
    expect(postCount(current.fetch)).toBe(0);
  });
});
