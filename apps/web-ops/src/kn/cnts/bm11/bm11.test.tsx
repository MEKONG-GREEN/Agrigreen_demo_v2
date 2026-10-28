// W11.45 — Báo cáo phòng BM-11: kiểm khóa kỳ, ý kiến trả lại (AC-04), chỉ gửi phần viết đã đổi; màn chi tiết: phần tự động chỉ
// đọc (BR-01), "Tổng hợp từ n/N xã" + xã chưa nộp, thao tác theo quyền máy chủ trả về, trả lại bắt buộc ý kiến, giả lập hợp đồng.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAction } from "@/kn/cycle/common";
import { bm11DefaultKey, cntsBm11Routes, resetCntsBm11 } from "@/api/mock/kn-cnts-bm11";
import { Bm11Detail } from "./Bm11Detail";
import { changedTexts, periodKeyError, returnCommentError, type Bm11, type Bm11Api } from "./api";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/cnts/bao-cao-dinh-ky",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useParams: () => ({}),
  useSearchParams: () => new URLSearchParams(),
}));

describe("helpers", () => {
  it("kỳ mặc định giả lập = kỳ vừa kết thúc theo ngày VN, không tràn cuối tháng", () => {
    const at = (iso: string) => Date.parse(iso);
    expect(bm11DefaultKey("month", at("2026-10-31T10:00:00Z"))).toBe("2026-09");
    expect(bm11DefaultKey("month", at("2027-03-31T10:00:00Z"))).toBe("2027-02");
    expect(bm11DefaultKey("month", at("2028-03-30T10:00:00Z"))).toBe("2028-02");
    expect(bm11DefaultKey("month", at("2027-01-15T10:00:00Z"))).toBe("2026-12");
    expect(bm11DefaultKey("month", at("2026-10-31T17:30:00Z"))).toBe("2026-10"); // 00:30 01/11 giờ VN
    expect(bm11DefaultKey("quarter", at("2027-01-01T03:00:00Z"))).toBe("2026-Q4");
    expect(bm11DefaultKey("quarter", at("2026-12-31T17:00:00Z"))).toBe("2026-Q4"); // 00:00 01/01/2027 VN
    expect(bm11DefaultKey("quarter", at("2026-12-31T16:59:00Z"))).toBe("2026-Q3");
    expect(bm11DefaultKey("year", at("2026-12-31T17:00:00Z"))).toBe("2026");
  });
  it("khóa kỳ theo loại", () => {
    expect(periodKeyError("week", "2026-W40")).toBeNull();
    expect(periodKeyError("week", "")).toBeNull();
    expect(periodKeyError("month", "2026-13")).toMatch(/dạng/);
    expect(periodKeyError("quarter", "2026-Q3")).toBeNull();
    expect(periodKeyError("year", "26")).toMatch(/dạng/);
  });
  it("trả lại bắt buộc ý kiến", () => {
    expect(returnCommentError("  ")).toMatch(/ý kiến/);
    expect(returnCommentError("Bổ sung")).toBeNull();
    expect(returnCommentError("x".repeat(2001))).toMatch(/2.000/);
  });
  it("chỉ gửi mục đã đổi", () => {
    expect(changedTexts({ a: "1", b: "2" }, { a: "1", b: "3", c: "" })).toEqual({ b: "3" });
  });
});

function report(over: Partial<Bm11> = {}): Bm11 {
  return {
    id: "r1", province: "89", type: "week", period_key: "2026-W40", label: "Tuần 40", from: "2026-09-29", to: "2026-10-05", recipients: ["bgd", "hcth"],
    status: "pending", seq: 3, last_event: "submitted", last_at: "2026-10-07T03:00:00Z", created_by: "Cán bộ A", version: 2, content_hash: "h",
    basis: "submitted", communes: "Tổng hợp từ 99/102 xã",
    coverage: [{ form: "CNTS-BM04", expected: 102, reported: 99, missing: ["1", "2", "3"], missing_names: ["Xã A", "Xã B", "Xã C"], periods: 1, closed: 0 }],
    groups: [{ key: "tong_dan", label: "Tổng đàn" }],
    lines: [{ key: "tong_dan_heo", group: "tong_dan", label: "Heo", unit: "con", sub: false, value: "467495" }],
    fishery: { catch_cur: "2858.5", catch_ytd: "103318.86", aqua_cur: "1", aqua_ytd: "2", total_cur: "2859.5", total_ytd: "103320.86", groups: { ca: "1", tom: "1", muc: "0", khac: "0" } },
    programs: [], sections: [{ key: "ke_hoach", label: "Kế hoạch tuần tới", text: "Tiếp tục điều tra." }], supplement: [], return_comment: "",
    recipient_list: [{ code: "bgd", label: "Ban Giám đốc TTKN", received: false, received_at: null, actor_name: "" }],
    history: [], can: { edit: false, refresh: false, submit: false, approve: true, return: true, receive: false, word: false }, replayed: false,
    ...over,
  };
}

function Harness({ v, bapi, onChange }: { v: Bm11; bapi: Bm11Api; onChange: (x: Bm11) => void }) {
  const act = useAction();
  return (
    <>
      {act.view}
      <Bm11Detail v={v} bapi={bapi} act={act} onChange={onChange} />
    </>
  );
}

describe("Bm11Detail", () => {
  let bapi: Bm11Api;
  beforeEach(() => {
    bapi = { ret: vi.fn(async () => report({ status: "returned", return_comment: "Bổ sung" })), approve: vi.fn(async () => report({ status: "sent" })) } as unknown as Bm11Api;
  });

  it("phần tự động chỉ đọc, hiện Tổng hợp từ n/N xã và xã thiếu; lãnh đạo duyệt", async () => {
    const onChange = vi.fn();
    render(<Harness v={report()} bapi={bapi} onChange={onChange} />);
    expect(screen.getByText("Tổng hợp từ 99/102 xã")).toBeTruthy();
    expect(screen.getByText(/Xã A, Xã B, Xã C/)).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("467.495")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Duyệt và gửi" }));
    await waitFor(() => expect(onChange).toHaveBeenCalled());
    expect(bapi.approve).toHaveBeenCalledWith("r1", 3);
  });

  it("trả lại: ý kiến trống không gửi (AC-04)", async () => {
    render(<Harness v={report()} bapi={bapi} onChange={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "Trả lại" }));
    const dlg = await screen.findByRole("dialog");
    await userEvent.click(within(dlg).getByRole("button", { name: "Trả lại" }));
    expect(bapi.ret).not.toHaveBeenCalled();
    await userEvent.type(within(dlg).getByRole("textbox"), "Bổ sung");
    await userEvent.click(within(dlg).getByRole("button", { name: "Trả lại" }));
    await waitFor(() => expect(bapi.ret).toHaveBeenCalledWith("r1", 3, "Bổ sung"));
  });

  it("đang soạn: sửa phần viết, chỉ gửi mục đổi", async () => {
    const edit = vi.fn(async () => report({ status: "draft" }));
    const b = { edit } as unknown as Bm11Api;
    render(<Harness v={report({ status: "draft", can: { edit: true, refresh: true, submit: true, approve: false, return: false, receive: false, word: false } })} bapi={b} onChange={vi.fn()} />);
    const box = screen.getByRole("textbox", { name: /Kế hoạch tuần tới/ });
    await userEvent.type(box, " Thêm.");
    await userEvent.click(screen.getByRole("button", { name: "Lưu phần viết" }));
    await waitFor(() => expect(edit).toHaveBeenCalledWith("r1", 3, { ke_hoach: "Tiếp tục điều tra. Thêm." }, {}));
  });
});

describe("giả lập hợp đồng", () => {
  it("có route cho mọi thao tác", () => {
    resetCntsBm11();
    const paths = cntsBm11Routes.map((r) => `${r.method} ${r.pattern.source}`);
    for (const op of ["reports$", "inbox$", "preview$", "sections$", "submit$", "approve$", "return$", "receive$", "word$"]) {
      expect(paths.some((p) => p.endsWith(op.replace("$", "") + "$"))).toBe(true);
    }
  });
});

