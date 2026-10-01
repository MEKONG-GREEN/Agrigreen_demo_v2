import createClient, { type Client, type Middleware } from "openapi-fetch";
import type { paths as AdminGeneratedPaths } from "./generated/admin";
import type { paths as GeneratedPaths } from "./generated/application";
import { idempotencyMiddleware } from "./idempotency";
import { ApiProblem, networkProblem, toProblem } from "./problem";

/** Application API: route của contracts/openapi/application.yaml — không còn route viết tay. */
export type ApiPaths = GeneratedPaths;
export type ApiClient = Client<ApiPaths>;

/** Admin API (host riêng, MFA bắt buộc): chỉ route của contracts/openapi/admin.yaml. */
export type AdminPaths = AdminGeneratedPaths;
export type AdminApiClient = Client<AdminPaths>;

export interface ApiClientOptions {
  baseUrl: string;
  /** Trả access token hiện tại (giữ trong bộ nhớ), hoặc null khi chưa đăng nhập. */
  getAccessToken: () => Promise<string | null> | string | null;
  /** Gọi khi server trả 401 (token hết hạn/sai) để điều hướng đăng nhập lại. */
  onUnauthenticated?: () => void;
  /** Thay fetch (chế độ giả lập, kiểm thử). */
  fetch?: (input: Request) => Promise<Response>;
  /** Ngữ cảnh phiên KN v7.7 (P1): gửi qua header X-AG-Context khi đã chọn vai trò + phòng ban. */
  getContextId?: () => string | null;
}

export function authMiddleware(
  getAccessToken: ApiClientOptions["getAccessToken"],
  onUnauthenticated?: () => void,
  getContextId?: () => string | null,
): Middleware {
  return {
    async onRequest({ request }) {
      const token = await getAccessToken();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      const ctx = getContextId?.();
      if (ctx) request.headers.set("X-AG-Context", ctx);
      request.headers.set("Accept", "application/json, application/problem+json");
      return request;
    },
    onResponse({ response }) {
      if (response.status === 401) onUnauthenticated?.();
      return response;
    },
  };
}

function build<P extends object>(opts: ApiClientOptions): Client<P> {
  const client = createClient<P>({
    baseUrl: opts.baseUrl.replace(/\/+$/, ""),
    ...(opts.fetch ? { fetch: opts.fetch } : {}),
  });
  client.use(authMiddleware(opts.getAccessToken, opts.onUnauthenticated, opts.getContextId));
  client.use(idempotencyMiddleware());
  return client;
}

export function createApiClient(opts: ApiClientOptions): ApiClient {
  return build<ApiPaths>(opts);
}

/** Client Admin API — cùng middleware (Bearer, Idempotency-Key cho mọi POST, problem+json). */
export function createAdminApiClient(opts: ApiClientOptions): AdminApiClient {
  return build<AdminPaths>(opts);
}

interface FetchResultLike<D> {
  data?: D;
  error?: unknown;
  response: Response;
}

/**
 * Chờ kết quả openapi-fetch: trả `data` khi 2xx, ném ApiProblem (RFC 9457) khi lỗi,
 * ném ApiProblem `network_error` khi không tới được máy chủ.
 */
export async function unwrap<D>(promise: Promise<FetchResultLike<D>>): Promise<D> {
  let result: FetchResultLike<D>;
  try {
    result = await promise;
  } catch (e) {
    if (e instanceof ApiProblem) throw e;
    throw networkProblem();
  }
  if (result.error !== undefined || !result.response.ok) {
    throw new ApiProblem(toProblem(result.error, result.response));
  }
  return result.data as D;
}
