import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "./client";
import { ApiProblem, fieldErrorMap, parseProblemResponse, problemMessage, toProblem } from "./problem";

const problemBody = {
  type: "https://errors.agrigreen.vn/validation_failed",
  title: "Validation failed",
  status: 422,
  code: "validation_failed",
  trace_id: "4bf92f3577b34da6a3ce929d0e0e4736",
  errors: [{ field: "value", reason: "Phải là số" }],
};

function problemResponse(body: unknown, status: number, headers: Record<string, string> = {}): Response {
  return new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/problem+json", ...headers },
  });
}

describe("toProblem — RFC 9457", () => {
  it("đọc đủ trường của problem+json", () => {
    const p = toProblem(problemBody, { status: 422, headers: new Headers() });
    expect(p.code).toBe("validation_failed");
    expect(p.status).toBe(422);
    expect(p.trace_id).toBe("4bf92f3577b34da6a3ce929d0e0e4736");
    expect(p.errors).toEqual([{ field: "value", reason: "Phải là số" }]);
  });

  it("lấy trace_id từ header traceparent khi body không có", () => {
    const headers = new Headers({ traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01" });
    const p = toProblem({ code: "forbidden", status: 403, title: "x", type: "t" }, { status: 403, headers });
    expect(p.trace_id).toBe("0af7651916cd43dd8448eb211c80319c");
  });

  it("body không phải problem → mã theo HTTP status", () => {
    expect(toProblem("<html>Bad gateway</html>", { status: 502, headers: new Headers() }).code).toBe("internal_error");
    expect(toProblem(undefined, { status: 401, headers: new Headers() }).code).toBe("unauthenticated");
    expect(toProblem(null, { status: 409, headers: new Headers() }).code).toBe("conflict");
  });
});

describe("thông báo tiếng Việt", () => {
  it("mã đã biết dùng bảng mã, không dùng title tiếng Anh của server", () => {
    expect(problemMessage({ code: "self_approval_forbidden", status: 403, title: "Self approval forbidden" })).toBe(
      "Không được tự phê duyệt đề xuất do chính mình lập (R-56). Cần một người phê duyệt khác.",
    );
    expect(problemMessage({ code: "mfa_required", status: 403, title: "" })).toMatch(/hai yếu tố/);
  });

  it("mã lạ của module → dùng detail của server", () => {
    expect(problemMessage({ code: "geo.plot_overlap", status: 409, title: "Overlap", detail: "Thửa chồng lấn thửa khác" })).toBe(
      "Thửa chồng lấn thửa khác",
    );
  });

  it("ApiProblem giữ trace_id và lỗi theo trường", () => {
    const e = new ApiProblem(toProblem(problemBody, { status: 422, headers: new Headers() }));
    expect(e.message).toMatch(/chưa hợp lệ/);
    expect(e.traceId).toBe("4bf92f3577b34da6a3ce929d0e0e4736");
    expect(fieldErrorMap(e)).toEqual({ value: "Phải là số" });
  });
});

describe("parseProblemResponse", () => {
  it("parse Response problem+json", async () => {
    const e = await parseProblemResponse(problemResponse(problemBody, 422));
    expect(e.code).toBe("validation_failed");
  });
  it("body hỏng vẫn ra ApiProblem, lấy X-Trace-Id", async () => {
    const e = await parseProblemResponse(problemResponse("{not json", 500, { "X-Trace-Id": "abc123" }));
    expect(e.code).toBe("internal_error");
    expect(e.traceId).toBe("abc123");
  });
});

describe("unwrap với openapi-fetch", () => {
  it("ném ApiProblem khi server trả problem+json", async () => {
    const api = createApiClient({
      baseUrl: "https://api.test",
      getAccessToken: () => "tok",
      fetch: async () => problemResponse({ ...problemBody, status: 403, code: "forbidden" }, 403),
    });
    await expect(unwrap(api.GET("/v1/hub/facilities"))).rejects.toMatchObject({ code: "forbidden", status: 403, traceId: problemBody.trace_id });
  });

  it("lỗi mạng → network_error", async () => {
    const api = createApiClient({
      baseUrl: "https://api.test",
      getAccessToken: () => null,
      fetch: async () => {
        throw new TypeError("Failed to fetch");
      },
    });
    await expect(unwrap(api.GET("/v1/hub/facilities"))).rejects.toMatchObject({ code: "network_error" });
  });

  it("gọi onUnauthenticated khi 401", async () => {
    let called = 0;
    const api = createApiClient({
      baseUrl: "https://api.test",
      getAccessToken: () => "expired",
      onUnauthenticated: () => called++,
      fetch: async () => problemResponse({ type: "t", title: "", status: 401, code: "unauthenticated" }, 401),
    });
    await expect(unwrap(api.GET("/v1/hub/facilities"))).rejects.toBeInstanceOf(ApiProblem);
    expect(called).toBe(1);
  });
});
