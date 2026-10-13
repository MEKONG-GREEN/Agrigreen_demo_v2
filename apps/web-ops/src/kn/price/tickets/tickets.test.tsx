import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetPriceItems } from "@/api/mock/kn-price-items";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { resetPriceTickets } from "@/api/mock/kn-price-tickets";
import { ApiProblem } from "@/api/problem";
import { createPriceTicketApi, draftProblem, evalDraft, lineInput, pctLabel, priceOf, type PriceSheet } from "./api";
import { EntryScreen } from "./EntryScreen";
import { printNote } from "./PrintView";
import { countDrafts, draftsOf, fillFromRef } from "./SheetLines";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/bao-gia/nhap",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "kn_xa" } } }),
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
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
  return { api, ta: createPriceTicketApi(api) };
}

async function problemCode(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

describe("phiếu báo giá — quy tắc ở trình duyệt (không float)", () => {
  const line = { ref: { min: "9000", max: "9000", mid: "9000", day: "2026-10-09" }, threshold_pct: "30.00" };

  it("AC-03 FN-XA-03.01: 13.000–15.000 so 9.000 → ▲ +55,6 %; kiểm giá; chuẩn hóa số kiểu Việt", () => {
    const ev = evalDraft({ min: "13.000", max: "15.000", noGoods: false, note: "" }, line);
    expect(ev?.direction).toBe("high");
    expect(pctLabel(ev!.pct)).toBe("+55,6%");
    expect(evalDraft({ min: "9000", max: "10000", noGoods: false, note: "" }, line)?.direction).toBe("");
    expect(evalDraft({ min: "6300", max: "6300", noGoods: false, note: "" }, line)?.direction).toBe("low");
    expect(draftProblem({ min: "10000", max: "9000", noGoods: false, note: "" })).toMatch(/lớn hơn/);
    expect(draftProblem({ min: "9000", max: "", noGoods: false, note: "" })).toMatch(/đủ/);
    expect(draftProblem({ min: "0", max: "1", noGoods: false, note: "" })).toMatch(/dương/);
    expect(priceOf("12,5")).toBe("12.5");
    expect(lineInput("MH00001", { min: "13.000", max: "15.000", noGoods: false, note: " Mưa " })).toEqual({
      item_code: "MH00001",
      min: "13000",
      max: "15000",
      no_goods: false,
      note: "Mưa",
    });
  });

  it("in phiếu: giá Xã gửi kèm chú thích hiệu chỉnh / loại (BR-02 FN-XA-03.02, AC-02)", () => {
    const base = { no_goods: false, note: null, adj_min: "56000", adj_max: "56000", alert: null } as unknown as PriceSheet["lines"][number];
    expect(printNote(base)).toBe("Tỉnh hiệu chỉnh: 56.000");
    expect(printNote({ ...base, adj_min: null, adj_max: null, no_goods: true })).toBe("Không có hàng");
  });
});

describe("phiếu báo giá — backend giả lập theo hợp đồng /v1/price/tickets*", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-10-12T03:00:00Z")); // Thứ Hai 10:00 giờ VN
    resetPriceItems();
    resetPricePoints();
    resetPriceTickets();
  });
  afterEach(() => vi.useRealTimers());

  it("Xã lưu nháp, gửi đủ mặt hàng; Lấy giá kỳ trước không ghi đè; khóa 12:00; Tỉnh yêu cầu xác minh, Xã phản hồi", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const sh = await xa.ta.sheet({});
    expect(sh.view).toBe("not_entered");
    expect(sh.can.enter).toBe(true);
    expect(sh.lines.length).toBeGreaterThan(1);
    const first = sh.lines[0]!;
    expect(first.ref).not.toBeNull();
    let d = draftsOf(sh);
    d[first.item_code] = { min: "1", max: "1", noGoods: false, note: "" };
    d = fillFromRef(sh, d);
    expect(d[first.item_code]!.min).toBe("1"); // AC-06
    expect(countDrafts(sh, d).blank).toBe(0);
    expect(countDrafts(sh, d).needNote).toBe(1); // giá 1 so kỳ trước → ▼ cần ghi chú
    expect(
      await problemCode(xa.ta.save({ point: sh.point.code, version: 0, submit: true, lines: sh.lines.map((l) => lineInput(l.item_code, d[l.item_code]!)) })),
    ).toBe("price.ticket_incomplete");
    d[first.item_code] = { ...d[first.item_code]!, note: "Mưa, hàng ít" };
    const after = await xa.ta.save({ point: sh.point.code, version: 0, submit: true, lines: sh.lines.map((l) => lineInput(l.item_code, d[l.item_code]!)) });
    expect(after.view).toBe("submitted");
    expect(after.lines[0]!.alert?.view).toBe("provisional");

    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const { alerts } = await tinh.ta.alerts({});
    expect(alerts).toHaveLength(1);
    expect(await problemCode(tinh.ta.request({ alerts: [{ ticket_id: alerts[0]!.ticket_id, item_code: alerts[0]!.item_code }], message: "" }))).toBe(
      "price.reason_required",
    );
    await tinh.ta.request({ alerts: [{ ticket_id: alerts[0]!.ticket_id, item_code: alerts[0]!.item_code }], message: "Đề nghị kiểm tra lại" });

    vi.setSystemTime(new Date("2026-10-12T05:15:00Z")); // 12:15 — đã khóa
    expect(await problemCode(xa.ta.save({ point: sh.point.code, version: after.ticket!.version, submit: true, lines: [] }))).toBe("price.ticket_locked");
    const req = (await xa.ta.alerts({ view: "requested" })).alerts;
    expect(req).toHaveLength(1);
    await xa.ta.respond({ ticket_id: req[0]!.ticket_id, item_code: req[0]!.item_code, choice: "propose", min: "9000", max: "10000", explanation: "Kiểm lại" });
    const resp = (await tinh.ta.alerts({})).alerts[0]!;
    expect(resp.view).toBe("responded");
    expect(resp.reply?.proposed_min).toBe("9000");
    // Hiệu chỉnh bằng giá Xã đề xuất → Đã hiệu chỉnh; phiếu in vẫn giá Xã gửi.
    const fixed = await tinh.ta.correct(resp.ticket_id, { version: resp.ticket_version, reason: "Dùng giá Xã đề xuất", lines: [{ item_code: resp.item_code, min: "9000", max: "10000" }] });
    expect(fixed.lines[0]!.adj_min).toBe("9000");
    expect(fixed.lines[0]!.alert?.view).toBe("adjusted");
    expect(fixed.lines[0]!.min).toBe("1");
  });

  it("màn Nhập báo giá: xã, giờ khóa, dòng theo nhóm hàng", async () => {
    const { api } = await clientFor("kn-an", "asg-an-xa");
    auth.api = api;
    render(<EntryScreen />);
    expect(await screen.findByText(/Khóa lúc 12:00/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lấy giá kỳ trước" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Gửi báo giá" })).toBeDisabled(); // còn trống
    const rows = screen.getAllByRole("region");
    expect(within(rows[0]!).getAllByRole("textbox").length).toBeGreaterThan(0);
  });
});
