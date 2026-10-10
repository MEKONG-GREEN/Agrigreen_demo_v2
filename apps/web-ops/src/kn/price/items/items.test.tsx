import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { addDay, resetPriceItems, vnToday } from "@/api/mock/kn-price-items";
import { ApiProblem } from "@/api/problem";
import { createPriceItemApi } from "./api";
import { ItemsScreen } from "./ItemsScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/mat-hang",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));

const auth: { api: ApiClient | null; role: string } = {
  api: null,
  role: "kn_tinh",
};
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: auth.role } } }),
}));

async function clientFor(personaId: string, assignmentId: string) {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({
        assignment_id: assignmentId,
        department_code: "trong_trot",
      }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => `mock.${personaId}`,
    getContextId: () => ctx.context_id,
    fetch: mockFetchNoDelay,
  });
  return { api, pa: createPriceItemApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("mặt hàng theo thời kỳ — backend giả lập theo hợp đồng /v1/price/*", () => {
  beforeEach(() => resetPriceItems());

  it("thêm mặt hàng từ ngày báo giá kế tiếp; trùng → 409; thiếu giống → 422; Cơ bản ở mọi xã", async () => {
    const { pa } = await clientFor("kn-binh", "asg-binh-tinh");
    const tomorrow = addDay(vnToday(), 1);
    const w = await pa.createItem({
      name: "Bưởi",
      variety: "Năm Roi",
      group_code: "rau_mau",
      unit_code: "kg",
      kind: "basic",
    });
    expect(w.code).toBe("MH00009");
    expect(w.valid_from).toBe(tomorrow);
    expect(
      await problemCode(
        pa.createItem({
          name: "bưởi",
          variety: "năm  roi",
          group_code: "rau_mau",
          unit_code: "kg",
          kind: "local",
        }),
      ),
    ).toBe("price.item_duplicate");
    expect(
      await problemCode(
        pa.createItem({
          name: "Xoài",
          variety: "",
          group_code: "rau_mau",
          unit_code: "kg",
          kind: "basic",
        }),
      ),
    ).toBe("validation_failed");
    expect(
      await problemCode(
        pa.createItem({
          name: "Mít",
          variety: "Thái",
          group_code: "rau_mau",
          unit_code: "kg",
          kind: "basic",
          valid_from: addDay(vnToday(), -1),
        }),
      ),
    ).toBe("price.item_date_invalid");
    const today = (await pa.pointItems("91302")).items.map((i) => i.code);
    const next = (await pa.pointItems("91302", tomorrow)).items.map(
      (i) => i.code,
    );
    expect(today).not.toContain("MH00009");
    expect(next).toContain("MH00009");
    expect(next).not.toContain("MH00005"); // Địa phương, gán cho 91301
    expect(
      (await pa.pointItems("91301", tomorrow)).items.map((i) => i.code),
    ).toContain("MH00005");
  });

  it("đã có giá → khóa ĐVT; thay thế cho → mặt hàng cũ ngưng, nhãn Giá mới", async () => {
    const { pa } = await clientFor("kn-binh", "asg-binh-tinh");
    expect(
      await problemCode(
        pa.reviseItem("MH00001", {
          name: "Lúa",
          variety: "OM 18 (tươi)",
          group_code: "lua_gao",
          unit_code: "ta",
          kind: "basic",
        }),
      ),
    ).toBe("price.item_locked");
    const w = await pa.createItem({
      name: "Lúa",
      variety: "OM 18 (tươi)",
      group_code: "lua_gao",
      unit_code: "ta",
      kind: "basic",
      replaces_code: "MH00001",
    });
    const d = await pa.item(w.code, w.valid_from);
    expect(d.new_price).toBe(true);
    expect(d.replaces_code).toBe("MH00001");
    const old = await pa.item("MH00001", w.valid_from);
    expect(old.state).toBe("retired");
    expect(old.replaced_by).toEqual([w.code]);
  });

  it("Cấp Xã đề xuất, chỉ thấy đề xuất của mình; Cấp Tỉnh duyệt → mặt hàng gán cho xã đề xuất", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    expect(
      await problemCode(
        xa.pa.createItem({
          name: "A",
          variety: "B",
          group_code: "rau_mau",
          unit_code: "kg",
          kind: "basic",
        }),
      ),
    ).toBe("forbidden");
    const p = await xa.pa.propose({
      point_code: "91303",
      name: "Thanh long",
      variety: "ruột đỏ",
      group_code: "rau_mau",
      unit_code: "kg",
      reason: "Có bán ở chợ",
    });
    expect((await xa.pa.proposals()).proposals.map((x) => x.id)).toEqual([
      p.code,
    ]);
    expect((await tinh.pa.proposals("pending")).proposals).toHaveLength(2);
    const w = await tinh.pa.approve(p.code, {
      name: "Thanh long",
      variety: "ruột đỏ",
      group_code: "rau_mau",
      unit_code: "kg",
      kind: "local",
    });
    expect(
      (await tinh.pa.pointItems("91303", w.valid_from)).items.map(
        (i) => i.code,
      ),
    ).toContain(w.code);
    expect(await problemCode(tinh.pa.reject(p.code, "x"))).toBe(
      "price.proposal_decided",
    );
  });

  it("bộ theo vụ tắt → mặt hàng của bộ không có ở xã", async () => {
    const { pa } = await clientFor("kn-binh", "asg-binh-tinh");
    const tomorrow = addDay(vnToday(), 1);
    await pa.createSet({
      code: "he_thu",
      name: "Bộ Hè Thu",
      season: "Hè Thu 2026",
      item_codes: ["MH00004"],
      enabled: false,
    });
    expect(
      (await pa.pointItems("91302", tomorrow)).items.map((i) => i.code),
    ).not.toContain("MH00004");
    expect(
      (await pa.items({ at: tomorrow })).items.find((i) => i.code === "MH00004")
        ?.gated,
    ).toBe(true);
  });
});

describe("màn FN-AD-04.03", () => {
  beforeEach(() => resetPriceItems());

  it("Cấp Tỉnh xem danh mục, thêm mặt hàng qua hộp thoại", async () => {
    const { api } = await clientFor("kn-binh", "asg-binh-tinh");
    auth.api = api;
    auth.role = "kn_tinh";
    render(<ItemsScreen />);
    const table = await screen.findByRole("table", {
      name: "Danh mục mặt hàng",
    });
    expect(within(table).getByText("MH00001")).toBeInTheDocument();
    expect(
      screen.getByRole("tab", { name: "Đề xuất từ xã (1)" }),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Thêm mặt hàng" }),
    );
    const dlg = screen.getByRole("dialog", { name: "Thêm mặt hàng" });
    await userEvent.type(within(dlg).getByLabelText("Tên loại"), "Chanh");
    await userEvent.type(
      within(dlg).getByLabelText("Giống / phẩm cấp"),
      "không hạt",
    );
    await userEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findByText("MH00009")).toBeInTheDocument();
  });

  it("Cấp Xã chỉ có tab Đề xuất mặt hàng", async () => {
    const { api } = await clientFor("kn-an", "asg-an-xa");
    auth.api = api;
    auth.role = "kn_xa";
    render(<ItemsScreen />);
    expect(
      await screen.findByRole("tab", { name: "Đề xuất mặt hàng" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("tab", { name: "Mặt hàng" }),
    ).not.toBeInTheDocument();
    expect(
      await screen.findByRole("button", { name: "Đề xuất mặt hàng" }),
    ).toBeInTheDocument();
  });
});
