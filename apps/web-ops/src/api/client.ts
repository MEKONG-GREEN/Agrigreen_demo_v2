import createClient, { type Client, type Middleware } from "openapi-fetch";
import type { paths as GeneratedPaths } from "./generated/application";
import { idempotencyMiddleware } from "./idempotency";
import type { PendingPaths } from "./pending/paths";
import { ApiProblem, networkProblem, toProblem } from "./problem";

/** Route đã có trong contracts/openapi + route đang chờ hợp đồng (src/api/pending). */
export type ApiPaths = GeneratedPaths & PendingPaths;
export type ApiClient = Client<ApiPaths>;

export interface ApiClientOptions {
  baseUrl: string;
  /** Trả access token hiện tại (giữ trong bộ nhớ), hoặc null khi chưa đăng nhập. */
  getAccessToken: () => Promise<string | null> | string | null;
  /** Gọi khi server trả 401 (token hết hạn/sai) để điều hướng đăng nhập lại. */
  onUnauthenticated?: () => void;
  /** Thay fetch (chế độ giả lập, kiểm thử). */
  fetch?: (input: Request) => Promise<Response>;
}

export function authMiddleware(
  getAccessToken: ApiClientOptions["getAccessToken"],
  onUnauthenticated?: () => void,
): Middleware {
  return {
    async onRequest({ request }) {
      const token = await getAccessToken();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      request.headers.set("Accept", "application/json, application/problem+json");
      return request;
    },
    onResponse({ response }) {
      if (response.status === 401) onUnauthenticated?.();
      return response;
    },
  };
}

export function createApiClient(opts: ApiClientOptions): ApiClient {
  const client = createClient<ApiPaths>({
    baseUrl: opts.baseUrl.replace(/\/+$/, ""),
    ...(opts.fetch ? { fetch: opts.fetch } : {}),
  });
  client.use(authMiddleware(opts.getAccessToken, opts.onUnauthenticated));
  client.use(idempotencyMiddleware());
  return client;
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
