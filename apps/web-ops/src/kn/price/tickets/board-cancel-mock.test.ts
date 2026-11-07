import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { KN_ACCOUNTS, KN_DEPARTMENTS, resetKnAccounts } from "@/api/mock/kn-accounts";
import { resetPriceBoards } from "@/api/mock/kn-price-boards";
import { resetPriceItems } from "@/api/mock/kn-price-items";
import { resetPricePoints } from "@/api/mock/kn-price-points";
import { resetPriceTickets } from "@/api/mock/kn-price-tickets";
import { createPriceTicketApi, lineInput } from "./api";
import { draftsOf, fillFromRef } from "./SheetLines";

const accounts = structuredClone(KN_ACCOUNTS);
const departmentStatus = KN_DEPARTMENTS.find((d) => d.code === "thong_tin_truyen_thong")!.status;
const DAY = "2026-10-12";

async function client(persona: string, assignment: string) {
  const res = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
    method: "POST",
    headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": `ctx283-${persona}-${Math.random()}` },
    body: JSON.stringify({ assignment_id: assignment, department_code: "thong_tin_truyen_thong" }),
  }));
  if (!res.ok) throw new Error(`context283 ${res.status}: ${await res.text()}`);
  const ctx = await res.json() as { context_id: string };
  return createPriceTicketApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => ctx.context_id, fetch: mockFetchNoDelay }));
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date(`${DAY}T03:00:00Z`));
  resetKnAccounts(structuredClone(accounts));
  const department = KN_DEPARTMENTS.find((d) => d.code === "thong_tin_truyen_thong")!;
  department.status = "active";
  for (const a of KN_ACCOUNTS) for (const asg of a.assignments) asg.departments = asg.departments.map((d) => d.code === department.code ? department : d);
  KN_ACCOUNTS.find((a) => a.id === "kn-binh")!.assignments.find((a) => a.assignment_id === "asg-binh-tinh")!.departments.push(department);
  resetPriceItems(); resetPricePoints(); resetPriceTickets(); resetPriceBoards();
});
afterEach(() => { resetKnAccounts(structuredClone(accounts)); KN_DEPARTMENTS.find((d) => d.code === "thong_tin_truyen_thong")!.status = departmentStatus; vi.useRealTimers(); });

async function opened() {
  const xa = await client("kn-an", "asg-an-xa");
  const sheet = await xa.sheet({ day: DAY });
  const draft = fillFromRef(sheet, draftsOf(sheet));
  for (const l of sheet.lines) draft[l.item_code] = { ...draft[l.item_code]!, min: draft[l.item_code]?.min || "9000", max: draft[l.item_code]?.max || "9000", note: "Đối chiếu283" };
  const sent = await xa.save({ point: sheet.point.code, day: DAY, version: 0, submit: true, lines: sheet.lines.map((l) => lineInput(l.item_code, draft[l.item_code]!)) });
  vi.setSystemTime(new Date(`${DAY}T05:01:00Z`));
  const tinh = await client("kn-binh", "asg-binh-tinh");
  const approved = await tinh.boardOp(DAY, "approve", { seq: 0 });
  const board = await tinh.boardOp(DAY, "reopen", { seq: approved.seq, reason: "Đối chiếu283" });
  return { tinh, board, id: sent.ticket!.id };
}

describe("FN-TI-02.06 mock giữ cùng hợp đồng quyền hủy", () => {
  it("không có thay đổi vẫn hủy và giữ phiên bản đã duyệt", async () => {
    const { tinh, board } = await opened();
    expect(board.can.cancel_reopen).toBe(true);
    const cancelled = await tinh.boardOp(DAY, "cancel-reopen", { seq: board.seq });
    expect(cancelled.state).toBe("approved");
    expect(cancelled.version).toBe(1);
  });

  it("đổi nguồn cùng một mốc giờ vẫn chặn; mở lại sau duyệt mới bắt đầu cửa sổ mới", async () => {
    const { tinh, board, id } = await opened();
    const ticket = await tinh.ticket(id);
    const line = ticket.lines.find((l) => !!l.min)!;
    await tinh.correct(id, { version: ticket.ticket!.version, reason: "Đã đối chiếu", lines: [{ item_code: line.item_code, min: "9000", max: "9000" }] });
    const changed = await tinh.board(DAY);
    expect(changed.can.cancel_reopen).toBe(false);
    expect(changed.published).toEqual(board.published);
    await expect(tinh.boardOp(DAY, "cancel-reopen", { seq: changed.seq })).rejects.toMatchObject({ problem: { code: "price.board_changed" } });
    expect((await tinh.board(DAY)).log).toEqual(changed.log);
    const approved = await tinh.boardOp(DAY, "approve", { seq: changed.seq });
    const reopened = await tinh.boardOp(DAY, "reopen", { seq: approved.seq, reason: "Đối chiếu lần nữa" });
    expect(reopened.version).toBe(2);
    expect(reopened.can.cancel_reopen).toBe(true);
  });
});
