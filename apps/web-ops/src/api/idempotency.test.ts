import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "./client";
import { IDEMPOTENCY_HEADER, IDEMPOTENCY_KEY_PATTERN, uuidv7 } from "./idempotency";

function recordingFetch(status = 201, body: unknown = { ok: true }) {
  const seen: Request[] = [];
  const fetch = async (req: Request) => {
    seen.push(req.clone());
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  };
  return { seen, fetch };
}

describe("uuidv7", () => {
  it("đúng định dạng, phiên bản 7, biến thể RFC", () => {
    const id = uuidv7();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(id).toMatch(IDEMPOTENCY_KEY_PATTERN);
  });

  it("48 bit đầu là thời gian ms → sắp xếp theo thời gian", () => {
    const t = 1_790_000_000_000;
    const a = uuidv7(t);
    const b = uuidv7(t + 1);
    expect(parseInt(a.replace(/-/g, "").slice(0, 12), 16)).toBe(t);
    expect(a < b).toBe(true);
  });

  it("không trùng", () => {
    const set = new Set(Array.from({ length: 500 }, () => uuidv7()));
    expect(set.size).toBe(500);
  });
});

describe("Idempotency-Key trên POST (R-51)", () => {
  it("tự thêm khóa cho POST", async () => {
    const { seen, fetch } = recordingFetch();
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "tok", fetch });
    await unwrap(
      api.POST("/v1/params/{key}/proposals", {
        params: { path: { key: "kn.sla" } },
        body: { value: "36", effective_from: "2026-11-01", source_doc: "VB 1", reason: "Thử" },
      }),
    );
    const key = seen[0]?.headers.get(IDEMPOTENCY_HEADER);
    expect(key).toBeTruthy();
    expect(key).toMatch(IDEMPOTENCY_KEY_PATTERN);
    expect(seen[0]?.headers.get("Authorization")).toBe("Bearer tok");
  });

  it("mỗi thao tác một khóa mới", async () => {
    const { seen, fetch } = recordingFetch();
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "tok", fetch });
    const call = () => unwrap(api.POST("/v1/params/proposals/{id}/approve", { params: { path: { id: "x" } }, body: {} }));
    await call();
    await call();
    expect(seen[0]?.headers.get(IDEMPOTENCY_HEADER)).not.toBe(seen[1]?.headers.get(IDEMPOTENCY_HEADER));
  });

  it("giữ nguyên khóa do nơi gọi đặt (thử lại cùng thao tác)", async () => {
    const { seen, fetch } = recordingFetch();
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "tok", fetch });
    await unwrap(
      api.POST("/v1/params/proposals/{id}/reject", {
        params: { path: { id: "x" }, header: { "Idempotency-Key": "retry-key-0001" } },
        body: {},
      }),
    );
    expect(seen[0]?.headers.get(IDEMPOTENCY_HEADER)).toBe("retry-key-0001");
  });

  it("không thêm khóa cho GET", async () => {
    const { seen, fetch } = recordingFetch(200, { items: [], next_cursor: null });
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => null, fetch });
    await unwrap(api.GET("/v1/tasks"));
    expect(seen[0]?.headers.get(IDEMPOTENCY_HEADER)).toBeNull();
    expect(seen[0]?.headers.get("Authorization")).toBeNull();
  });
});
