import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { BoardPanel } from "./BoardPanel";
import type { PriceBoard } from "./api";

const auth = vi.hoisted(() => ({ api: null as ApiClient | null }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => ({ api: auth.api }) }));

const DAY = "2026-10-12";
const reopened = (cancel: boolean): PriceBoard => ({
  day: DAY, quote: true, lock_at: `${DAY}T05:00:00Z`, locked: true,
  now: `${DAY}T06:00:00Z`, state: "reopened", seq: 3, version: 1,
  locked_by: null, locked_by_name: null, lock_until: null, summary: null,
  preview: [], published: { version: 1, approved_at: `${DAY}T05:01:00Z`, reopened: true, week_key: "2026-W42", lines: [] },
  log: [], can: { approve: false, unlock: false, reopen: false, cancel_reopen: cancel },
  blockers: [], reopen_days: 30,
});

function fixture(board: PriceBoard, status: Partial<components["schemas"]["PriceStatus"]> = {}, wait?: Promise<void>) {
  let current = board;
  const onChanged = vi.fn();
  const writes = vi.fn();
  const fetcher = vi.fn(async (req: Request) => {
    if (req.url.endsWith("/status")) return Response.json({ profile: "kn", department: "thong_tin_truyen_thong", level: "TINH", can_manage: false, can_steward: true, rule_version: "v2", day: DAY, units: 1, groups: 1, ...status });
    if (req.method === "GET") return Response.json(current);
    writes(await req.clone().json());
    await wait;
    current = { ...board, seq: 4, state: "approved", can: { ...board.can, cancel_reopen: false } };
    return Response.json(current, { status: 201 });
  });
  auth.api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.price283", fetch: fetcher });
  render(<BoardPanel day={DAY} nonce={0} onChanged={onChanged} />);
  return { fetcher, writes, onChanged };
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date(`${DAY}T06:00:00Z`));
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("FN-TI-02.06 Hủy mở duyệt theo quyền server", () => {
  it("giữ nút vô hiệu khi đã sửa và không POST", async () => {
    const f = fixture(reopened(false));
    const cancel = await screen.findByRole("button", { name: "Hủy mở duyệt" });
    expect(cancel).toBeDisabled();
    fireEvent.click(cancel);
    expect(f.writes).not.toHaveBeenCalled();
    expect(f.onChanged).not.toHaveBeenCalled();
  });

  it("chưa sửa vẫn hủy thành công với seq server", async () => {
    const f = fixture(reopened(true));
    const cancel = await screen.findByRole("button", { name: "Hủy mở duyệt" });
    expect(cancel).toBeEnabled();
    fireEvent.click(cancel);
    await waitFor(() => expect(f.onChanged).toHaveBeenCalledOnce());
    expect(f.writes).toHaveBeenCalledExactlyOnceWith({ seq: 3 });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Hủy mở duyệt" })).not.toBeInTheDocument());
  });

  it.each([
    { level: "TINH", can_steward: false }, // phiên chỉ xem / cán bộ không QTNV
    { level: "", can_steward: true, can_manage: true }, // Admin vận hành chính thức
    { level: "XA", can_steward: false },
    { level: "TW", can_steward: false },
  ])("không hiện thao tác cho phiên không có quyền $level/$can_steward", async (status) => {
    const f = fixture(reopened(false), status);
    await screen.findByText("Đang mở duyệt (v1)");
    await waitFor(() => expect(f.fetcher).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole("button", { name: "Hủy mở duyệt" })).not.toBeInTheDocument();
    expect(f.writes).not.toHaveBeenCalled();
  });

  it("đang gửi thì vô hiệu, không gửi trùng", async () => {
    let finish!: () => void;
    const wait = new Promise<void>((resolve) => { finish = resolve; });
    const f = fixture(reopened(true), {}, wait);
    const cancel = await screen.findByRole("button", { name: "Hủy mở duyệt" });
    fireEvent.click(cancel);
    await waitFor(() => expect(f.writes).toHaveBeenCalledOnce());
    expect(cancel).toBeDisabled();
    fireEvent.click(cancel);
    expect(f.writes).toHaveBeenCalledOnce();
    finish();
    await waitFor(() => expect(f.onChanged).toHaveBeenCalledOnce());
  });
});
