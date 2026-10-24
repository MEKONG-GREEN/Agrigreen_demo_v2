// P0-WEB-IDEMKEY (#129): bấm Lưu lại sau lỗi mạng / 503 / hết giờ dùng LẠI Idempotency-Key của lần trước → backend
// giả lập phát lại kết quả (không 409, không bản ghi thứ hai); thành công → lần sau khóa mới; đổi dữ liệu → khóa mới.
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { IDEMPOTENCY_HEADER, IdempotentOperation, outcomeUnknown, withIdempotentOperation } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import { useIdempotentAction } from "@/api/useIdempotentAction";
import { createCollectApi, createCycleApi } from "@/kn/cycle/api";
import { useAction } from "@/kn/cycle/common";
import { mockFetchNoDelay } from "./handler";
import { resetMockForms } from "./forms/routes";
import { resetKnCollect } from "./kn-collect";
import { resetReportCycle } from "./kn-reports/store";

type Mode = "pass" | "lost" | "503";

/**
 * Mạng giả: "lost" = máy chủ ĐÃ xử lý nhưng phản hồi mất (hết giờ) → client thấy lỗi mạng; "503" = cổng trả 503
 * trước khi tới máy chủ. Ghi lại khóa của mọi lần ghi.
 */
function flaky(plan: Mode[]) {
  const keys: string[] = [];
  const replayed: boolean[] = [];
  const fetch = async (req: Request): Promise<Response> => {
    if (req.method !== "GET") keys.push(req.headers.get(IDEMPOTENCY_HEADER) ?? "");
    const mode = req.method === "GET" ? "pass" : (plan.shift() ?? "pass");
    if (mode === "503") return new Response(JSON.stringify({ type: "about:blank", title: "Service Unavailable", status: 503, code: "unavailable" }), { status: 503, headers: { "Content-Type": "application/problem+json" } });
    const res = await mockFetchNoDelay(req);
    if (req.method !== "GET") replayed.push(res.headers.get("Idempotent-Replayed") === "true");
    if (mode === "lost") throw new TypeError("Failed to fetch");
    return res;
  };
  return { fetch, keys, replayed, plan };
}

async function contextFor(personaId: string, assignmentId: string): Promise<string> {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${personaId}-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}

const code = (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e)));

beforeEach(() => {
  resetKnCollect();
  resetMockForms();
  resetReportCycle(() => Date.parse("2026-10-02T03:00:00Z"));
});

describe("IdempotentOperation", () => {
  it("cùng dấu vân tay → cùng khóa đến khi có kết quả dứt khoát; khác dấu → khóa khác", () => {
    const op = new IdempotentOperation();
    const k1 = op.keyFor("POST /a {}");
    expect(op.keyFor("POST /a {}")).toBe(k1);
    expect(op.keyFor("POST /a {\"x\":1}")).not.toBe(k1);
    op.settle("POST /a {}", "network");
    expect(op.keyFor("POST /a {}")).toBe(k1);
    op.settle("POST /a {}", { status: 503 });
    expect(op.keyFor("POST /a {}")).toBe(k1);
    op.settle("POST /a {}", { status: 409, code: "idempotency_in_progress" });
    expect(op.keyFor("POST /a {}")).toBe(k1);
    op.settle("POST /a {}", { status: 201 });
    const k2 = op.keyFor("POST /a {}");
    expect(k2).not.toBe(k1);
    op.settle("POST /a {}", { status: 422, code: "validation_failed" });
    expect(op.keyFor("POST /a {}")).not.toBe(k2);
    op.reset();
    expect(op.size).toBe(0);
  });

  it("kết quả chưa rõ: mạng, 0, 408, 425, 429, 5xx, đang xử lý; còn lại dứt khoát", () => {
    expect(["network", { status: 0 }, { status: 408 }, { status: 425 }, { status: 429 }, { status: 500 }, { status: 504 }, { status: 409, code: "idempotency_in_progress" }].every((o) => outcomeUnknown(o as never))).toBe(true);
    expect([{ status: 200 }, { status: 201 }, { status: 400 }, { status: 403 }, { status: 409, code: "party.cooperative_name_taken" }, { status: 422 }].some((o) => outcomeUnknown(o))).toBe(false);
  });
});

describe("Tạo HTX (FN-XA-03.03) — bấm Lưu lại sau lỗi", () => {
  const body = { name: "HTX Thử Lại", commune_code: "91302", area_ha: "10", representative: { full_name: "Trần Văn Lại", phone: "0912000333" } };

  async function setup(plan: Mode[]) {
    const ctx = await contextFor("kn-an", "asg-an-xa");
    const net = flaky(plan);
    const xa = createCollectApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-an", getContextId: () => ctx, fetch: net.fetch });
    return { xa, net };
  }
  const count = async (xa: ReturnType<typeof createCollectApi>) => (await xa.cooperatives({ status: "all", q: "Thử Lại" })).items.filter((c) => c.name === body.name).length;

  it("mất phản hồi (máy chủ đã tạo) → bấm lại cùng khóa → phát lại kết quả, không 409, chỉ một HTX", async () => {
    const { xa, net } = await setup(["lost", "pass"]);
    const op = new IdempotentOperation();
    expect(await code(withIdempotentOperation(op, () => xa.createCooperative(body)))).toBe("0 network_error");
    const again = await withIdempotentOperation(op, () => xa.createCooperative(body));
    expect(again.name).toBe(body.name);
    expect(net.keys[0]).toBe(net.keys[1]);
    expect(net.replayed).toEqual([false, true]);
    expect(await count(xa)).toBe(1);
  });

  it("đối chứng — không có phiên thao tác (khóa mới mỗi lần) thì bấm lại ra 409 (lỗi cũ của #129)", async () => {
    const { xa, net } = await setup(["lost", "pass"]);
    await code(xa.createCooperative(body));
    expect(await code(xa.createCooperative(body))).toBe("409 party.cooperative_name_taken");
    expect(net.keys[0]).not.toBe(net.keys[1]);
  });

  it("503 trước khi tới máy chủ → thử lại cùng khóa, tạo một lần; thành công → thao tác sau khóa mới", async () => {
    const { xa, net } = await setup(["503", "pass", "pass"]);
    const op = new IdempotentOperation();
    expect(await code(withIdempotentOperation(op, () => xa.createCooperative(body)))).toBe("503 unavailable");
    await withIdempotentOperation(op, () => xa.createCooperative(body));
    expect(net.keys[0]).toBe(net.keys[1]);
    expect(net.replayed).toEqual([false]);
    expect(op.size).toBe(0);
    // Thành công rồi: lần gửi sau (cùng dữ liệu) là thao tác mới → khóa mới → máy chủ báo trùng tên đúng nghiệp vụ.
    expect(await code(withIdempotentOperation(op, () => xa.createCooperative(body)))).toBe("409 party.cooperative_name_taken");
    expect(net.keys[2]).not.toBe(net.keys[1]);
    expect(await count(xa)).toBe(1);
  });

  it("đổi dữ liệu sau lỗi → khóa mới (không 422 idempotency_key_reused)", async () => {
    const { xa, net } = await setup(["lost", "pass"]);
    const op = new IdempotentOperation();
    await code(withIdempotentOperation(op, () => xa.createCooperative(body)));
    const other = await withIdempotentOperation(op, () => xa.createCooperative({ ...body, name: "HTX Thử Lại Hai" }));
    expect(other.name).toBe("HTX Thử Lại Hai");
    expect(net.keys[1]).not.toBe(net.keys[0]);
  });

  it("bấm hai lần liên tiếp (lần đầu chưa xong) → cùng khóa → một HTX; lần hai 409 đang xử lý hoặc phát lại", async () => {
    const { xa, net } = await setup(["pass", "pass"]);
    const op = new IdempotentOperation();
    const [a, b] = await Promise.all([code(withIdempotentOperation(op, () => xa.createCooperative(body))), code(withIdempotentOperation(op, () => xa.createCooperative(body)))]);
    expect(a).toBe("ok");
    expect(["ok", "409 idempotency_in_progress"]).toContain(b);
    expect(net.keys[0]).toBe(net.keys[1]);
    expect(await count(xa)).toBe(1);
  });

  it("khóa tường minh của nơi gọi được giữ nguyên", async () => {
    const { xa, net } = await setup(["pass"]);
    await xa.createCooperative(body, "explicit-key-0001");
    expect(net.keys[0]).toBe("explicit-key-0001");
  });

  it("useAction: act.run bấm lại sau mất phản hồi → cùng khóa, một HTX; thành công rồi → khóa mới", async () => {
    const { xa, net } = await setup(["lost", "pass"]);
    const { result } = renderHook(() => useAction());
    let r: unknown;
    await act(async () => {
      r = await result.current.run(() => xa.createCooperative(body));
    });
    expect(r).toBeNull();
    expect(result.current.problem?.code).toBe("network_error");
    await act(async () => {
      r = await result.current.run(() => xa.createCooperative(body));
    });
    expect((r as { name: string }).name).toBe(body.name);
    expect(net.keys[0]).toBe(net.keys[1]);
    expect(net.replayed).toEqual([false, true]);
    expect(await count(xa)).toBe(1);
  });
});

describe("Nộp báo cáo Cấp Xã (kn/cycle submit) — bấm Gửi lại sau lỗi", () => {
  it("mất phản hồi → gửi lại cùng khóa → phát lại, lịch sử chỉ thêm một bản", async () => {
    const ctx = await contextFor("kn-an", "asg-an-xa");
    const net = flaky(["lost", "pass"]);
    const xa = createCycleApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-an", getContextId: () => ctx, fetch: net.fetch });
    // Tỉnh phát hành một đợt (như cycle.test.ts) → Xã có phiếu Tuần 1 đang mở.
    const tinhCtx = await contextFor("kn-binh", "asg-binh-tinh");
    const tinh = createCycleApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-binh", getContextId: () => tinhCtx, fetch: mockFetchNoDelay });
    const t01 = (await tinh.reportForms("weekly")).items.find((f) => f.code === "T01")!;
    const { batch, periods } = await tinh.issue({
      report_type: "weekly",
      template_ids: [t01.template_id],
      scope: { mode: "communes", communes: ["91301", "91302"] },
      from_date: "2026-09-28",
      to_date: "2026-10-18",
      season_label: "Đợt thử lại",
    });
    const task = (await xa.tasks({ batch_id: batch.id, period_id: periods[0]!.id })).items[0]!;
    const before = (await xa.history(task.id)).items.length;
    const { result } = renderHook(() => useAction());
    await act(async () => {
      await result.current.run(() => xa.submit(task.id, { kind: "draft", rows: [{ input: { F: "20" } }] }));
    });
    expect(result.current.problem?.code).toBe("network_error");
    let r: unknown;
    await act(async () => {
      r = await result.current.run(() => xa.submit(task.id, { kind: "draft", rows: [{ input: { F: "20" } }] }));
    });
    expect(r).not.toBeNull();
    expect(net.keys[0]).toBe(net.keys[1]);
    expect(net.replayed).toEqual([false, true]);
    expect((await xa.history(task.id)).items.length).toBe(before + 1);
  });
});

describe("openapi-fetch (createApiClient) trong phiên thao tác", () => {
  function recording(statuses: number[]) {
    const keys: string[] = [];
    const fetch = async (req: Request) => {
      keys.push(req.headers.get(IDEMPOTENCY_HEADER) ?? "");
      const status = statuses.shift() ?? 201;
      return new Response(JSON.stringify(status >= 400 ? { status, code: status === 503 ? "unavailable" : "validation_failed", title: "x" } : { ok: true }), {
        status,
        headers: { "Content-Type": status >= 400 ? "application/problem+json" : "application/json" },
      });
    };
    return { keys, fetch };
  }
  const post = (api: ReturnType<typeof createApiClient>, note: string) =>
    unwrap(api.POST("/v1/extension/directives/{id}/ack", { params: { path: { id: "d1" }, header: {} as { "Idempotency-Key": string } }, body: { note } as never }));

  it("503 → thử lại cùng khóa; thành công → khóa mới; đổi thân → khóa mới; ngoài phiên → khóa mới mỗi lần", async () => {
    const net = recording([503, 201, 201, 422, 201, 201, 201]);
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: async () => "tok", fetch: net.fetch });
    const { result } = renderHook(() => useIdempotentAction());
    const idem = result.current;
    expect(await code(idem.run(() => post(api, "a")))).toBe("503 unavailable");
    await idem.run(() => post(api, "a"));
    expect(net.keys[1]).toBe(net.keys[0]);
    await idem.run(() => post(api, "a"));
    expect(net.keys[2]).not.toBe(net.keys[1]);
    expect(await code(idem.run(() => post(api, "b")))).toBe("422 validation_failed");
    await idem.run(() => post(api, "b"));
    expect(net.keys[4]).not.toBe(net.keys[3]); // 4xx dứt khoát → không giữ khóa
    await post(api, "c");
    await post(api, "c");
    expect(net.keys[6]).not.toBe(net.keys[5]);
    expect(net.keys.every((k) => /^[A-Za-z0-9._:-]{8,200}$/.test(k))).toBe(true);
  });

  it("lỗi mạng (fetch ném) → giữ khóa cho lần thử lại", async () => {
    const keys: string[] = [];
    let fail = true;
    const api = createApiClient({
      baseUrl: "https://api.test",
      getAccessToken: async () => "tok",
      fetch: async (req) => {
        keys.push(req.headers.get(IDEMPOTENCY_HEADER) ?? "");
        if (fail) {
          fail = false;
          throw new TypeError("Failed to fetch");
        }
        return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
      },
    });
    const op = new IdempotentOperation();
    expect(await code(withIdempotentOperation(op, () => post(api, "x")))).toBe("0 network_error");
    await withIdempotentOperation(op, () => post(api, "x"));
    expect(keys[1]).toBe(keys[0]);
    expect(op.size).toBe(0);
  });
});
