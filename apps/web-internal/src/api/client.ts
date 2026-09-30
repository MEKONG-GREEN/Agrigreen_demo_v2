import createClient, { type Client, type Middleware } from "openapi-fetch";
import type { paths as GeneratedPaths } from "./generated/mkg-internal";
import { idempotencyMiddleware, newIdempotencyKey } from "./idempotency";
import { ApiProblem, networkProblem, toProblem } from "./problem";

/**
 * api-internal (vùng mkg): route của contracts/openapi/mkg-internal.yaml — sinh tự động, không viết tay.
 * Hai bề mặt dùng CHUNG tệp hợp đồng nhưng hai host:
 *   - bề mặt ứng dụng (AG_HTTP_ADDR) — OIDC + JWT;
 *   - bề mặt quản trị (AG_ADMIN_HTTP_ADDR) — OIDC + MFA bắt buộc (route có `servers: mkg-admin` trong hợp đồng).
 */
export type ApiPaths = GeneratedPaths;
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

export function authMiddleware(getAccessToken: ApiClientOptions["getAccessToken"], onUnauthenticated?: () => void): Middleware {
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

/** Client một bề mặt của api-internal — Bearer, Idempotency-Key cho mọi POST, problem+json. */
export function createApiClient(opts: ApiClientOptions): ApiClient {
  const client = createClient<ApiPaths>({
    baseUrl: opts.baseUrl.replace(/\/+$/, ""),
    ...(opts.fetch ? { fetch: opts.fetch } : {}),
  });
  client.use(authMiddleware(opts.getAccessToken, opts.onUnauthenticated));
  client.use(idempotencyMiddleware());
  return client;
}

/** Tham số header Idempotency-Key (hợp đồng khai bắt buộc cho mọi POST). */
export function idem(key: string = newIdempotencyKey()): { "Idempotency-Key": string } {
  return { "Idempotency-Key": key };
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

/**
 * Như unwrap, cho route mà hợp đồng chưa khai lược đồ phản hồi (`content` trống): thân JSON vẫn được
 * đọc, kiểu do nơi gọi khai (xem src/api/types.ts — mô tả đúng DTO của backend).
 */
export async function unwrapAs<T>(promise: Promise<FetchResultLike<unknown>>): Promise<T> {
  return (await unwrap(promise)) as T;
}
