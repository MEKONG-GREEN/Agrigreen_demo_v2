import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PriceBoard } from "./api";
import { BoardPanel } from "./BoardPanel";

const api = vi.hoisted(() => ({ board: vi.fn(), status: vi.fn(), requestWebsite: vi.fn(), boardOp: vi.fn() }));
vi.mock("./SheetLines", () => ({ usePriceTicketApi: () => api }));

function approved(): PriceBoard {
  return {
    day: "2026-11-06", quote: true, lock_at: "2026-11-06T05:00:00Z", locked: true,
    now: "2026-11-06T06:00:00Z", state: "approved", seq: 2, version: 1,
    locked_by: null, locked_by_name: null, lock_until: null,
    summary: { points: 1, submitted: 1, late: 0, missed: 0, price_lines: 1, excluded: 0, open_alerts: 0 },
    preview: [], published: { version: 1, approved_at: "2026-11-06T05:05:00Z", reopened: false, week_key: "2026-W45", lines: [] },
    log: [], can: { approve: false, unlock: false, reopen: true, cancel_reopen: false, website: true },
    blockers: [], reopen_days: 30, website: { status: "not_connected", version: 1, requested_at: null },
  };
}

describe("FN-TI-02.07 deferred website acknowledgement", () => {
  beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2026-11-06T06:00:00Z")); api.status.mockResolvedValue({ can_steward: true }); });
  afterEach(() => { cleanup(); vi.useRealTimers(); });

  it("records a local request and keeps the visible status disconnected", async () => {
    let board = approved();
    api.board.mockImplementation(async () => structuredClone(board));
    api.requestWebsite.mockImplementation(async () => {
      board = { ...board, website: { status: "not_connected", version: 1, requested_at: "2026-11-06T06:00:00Z" } };
      return structuredClone(board);
    });
    render(<BoardPanel day={board.day} nonce={0} onChanged={vi.fn()} />);
    expect(await screen.findByText("Website: Chưa kết nối")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /^Đăng lên website$/ }));
    await waitFor(() => expect(api.requestWebsite).toHaveBeenCalledExactlyOnceWith("2026-11-06", { seq: 2 }));
    expect(await screen.findByText(/Đã ghi yêu cầu cho v1/)).toHaveTextContent("Website chưa kết nối.");
    expect(screen.queryByText(/Đã đăng|Đăng thành công|Thành công/)).not.toBeInTheDocument();
    expect(api.boardOp).not.toHaveBeenCalled();
  });

  it.each(["open", "reopened", "approving"] as const)("does not submit an unapproved or updating board (%s)", async (state) => {
    const board = approved(); board.state = state; board.can.website = false;
    api.board.mockResolvedValue(board);
    render(<BoardPanel day={board.day} nonce={0} onChanged={vi.fn()} />);
    const button = await screen.findByRole("button", { name: /^Đăng lên website$/ });
    expect(button).toBeDisabled(); fireEvent.click(button); expect(api.requestWebsite).not.toHaveBeenCalled();
  });

  it("follows server permissions and fails closed if the deferred status is absent", async () => {
    const board = approved(); board.can.website = false;
    api.board.mockResolvedValue(board);
    const view = render(<BoardPanel day={board.day} nonce={0} onChanged={vi.fn()} />);
    expect(await screen.findByRole("button", { name: /^Đăng lên website$/ })).toBeDisabled();
    view.unmount(); api.board.mockResolvedValue({ ...board, website: null });
    render(<BoardPanel day={board.day} nonce={0} onChanged={vi.fn()} />);
    await screen.findByText("Bảng giá ngày");
    expect(screen.queryByRole("button", { name: /^Đăng lên website$/ })).not.toBeInTheDocument();
    expect(api.requestWebsite).not.toHaveBeenCalled();
  });
});
