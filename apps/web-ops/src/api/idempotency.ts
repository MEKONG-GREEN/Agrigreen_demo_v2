import type { Middleware } from "openapi-fetch";

/**
 * Sinh UUIDv7 (RFC 9562): 48 bit thời gian Unix ms + 74 bit ngẫu nhiên.
 * Dùng cho Idempotency-Key (R-51) và id do client sinh (R-11).
 */
export function uuidv7(now: number = Date.now()): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  let ts = Math.floor(now);
  for (let i = 5; i >= 0; i--) {
    bytes[i] = ts & 0xff;
    ts = Math.floor(ts / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70; // phiên bản 7
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80; // biến thể RFC
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** Khóa hợp lệ theo contracts/openapi: ^[A-Za-z0-9._:-]{8,200}$ */
export const IDEMPOTENCY_KEY_PATTERN = /^[A-Za-z0-9._:-]{8,200}$/;

export const IDEMPOTENCY_HEADER = "Idempotency-Key";

export function newIdempotencyKey(): string {
  return uuidv7();
}

// ---------------------------------------------------------------------------------------------------------------------
// P0-WEB-IDEMKEY (#129): MỘT khóa cho mỗi thao tác của người dùng.
// Khóa gắn với "dấu vân tay" của yêu cầu (phương thức + đường dẫn + ngữ cảnh + thân). Bấm lại sau lỗi mạng/5xx/hết giờ
// → cùng thân → cùng khóa → máy chủ trả lại kết quả cũ thay vì tạo bản ghi thứ hai (409 + node mồ côi). Khóa sống theo
// CẢ thao tác (một lần bấm có thể gồm nhiều lệnh): chỉ nhả khi cả thao tác thành công; lỗi nghiệp vụ dứt khoát (4xx) của
// một bước → bỏ khóa bước đó. Đổi dữ liệu form → thân khác → khóa mới. Kết quả chỉ tác động lên đúng khóa đã cấp.
// ---------------------------------------------------------------------------------------------------------------------

/** Kết quả một lần gửi: mã HTTP (+ code problem+json) hoặc "network" (không tới được máy chủ / hết giờ). */
export type AttemptOutcome = { status: number; code?: string | null } | "network";

/**
 * Chưa biết máy chủ đã xử lý hay chưa → GIỮ khóa để lần bấm lại được phát lại: lỗi mạng, hết giờ (408), 425, 429,
 * 5xx, và idempotency_in_progress (lần gửi trước còn đang chạy). Còn lại (2xx, 4xx nghiệp vụ) là kết quả dứt khoát.
 */
export function outcomeUnknown(o: AttemptOutcome): boolean {
  if (o === "network") return true;
  if (o.code === "idempotency_in_progress") return true;
  return o.status === 0 || o.status === 408 || o.status === 425 || o.status === 429 || o.status >= 500;
}

/** Dấu vân tay yêu cầu ghi: cùng thao tác bấm lại ⇔ cùng dấu. */
export function requestFingerprint(method: string, url: string, context: string | null | undefined, body: string): string {
  let path = url;
  try {
    const u = new URL(url, "http://local");
    path = `${u.pathname}${u.search}`;
  } catch {
    /* giữ nguyên */
  }
  return `${method.toUpperCase()} ${path} ctx=${context ?? ""} ${body}`;
}

/**
 * Giới hạn MỀM số khóa treo: chỉ loại khóa không thuộc lượt nào còn được bảo vệ. Khóa của lượt đang dở không bao giờ bị
 * loại âm thầm (lượt 65 lệnh vẫn phát lại đủ 65 khóa — #129 P2 re-review), nên số khóa có thể tạm vượt ngưỡng.
 */
const MAX_PENDING = 64;
/** Số lượt chưa complete gần nhất được bảo vệ: lượt đang chạy và lượt bỏ dở ngay trước nó (lần bấm lại sẽ phát lại). */
const MAX_OPEN_RUNS = 2;

/**
 * Một "phiên thao tác" (form/màn hình): giữ khóa theo dấu vân tay cho các bước của thao tác người dùng ĐANG dở.
 * - keyFor(fp): khóa đang giữ cho fp, hoặc khóa mới (uuidv7) nếu chưa có.
 * - settle(fp, khóa, kết quả): chỉ tác động khi entry còn giữ ĐÚNG khóa đã cấp cho lần gửi đó (phản hồi muộn của một lần
 *   gửi cũ không xóa khóa của lần gửi mới — #129 P2). 2xx → GIỮ khóa (bước đã xong nhưng cả thao tác có thể chưa xong:
 *   bấm lại form nhiều lệnh phải phát lại bước này bằng đúng khóa cũ — #129 P1); chưa rõ → giữ; 4xx dứt khoát → bỏ.
 * - Khóa chỉ được nhả khi CẢ thao tác thành công (OperationRun.complete) hoặc reset() (mở lại form / thao tác mới).
 */
export class IdempotentOperation {
  private readonly pending = new Map<string, string>();
  /** Các lượt chưa complete gần nhất (tối đa MAX_OPEN_RUNS); khóa chúng đã dùng không bị loại theo giới hạn. */
  private readonly openRuns: OperationRun[] = [];

  constructor(private readonly generate: () => string = newIdempotencyKey) {}

  keyFor(fingerprint: string): string {
    const k = this.pending.get(fingerprint);
    if (k) return k;
    const key = this.generate();
    this.pending.set(fingerprint, key);
    this.evict(fingerprint);
    return key;
  }

  /** Loại khóa cũ nhất KHÔNG thuộc lượt được bảo vệ cho tới khi về ngưỡng; hết ứng viên thì dừng (tạm vượt ngưỡng). */
  private evict(keep: string): void {
    for (const fp of [...this.pending.keys()]) {
      if (this.pending.size <= MAX_PENDING) return;
      if (fp === keep || this.openRuns.some((r) => r.uses(fp))) continue;
      this.pending.delete(fp);
    }
  }

  /** Khóa đang giữ (để kiểm thử/hiển thị), không sinh mới. */
  peek(fingerprint: string): string | undefined {
    return this.pending.get(fingerprint);
  }

  /** Báo kết quả một lần gửi đã dùng `key`. Entry đã đổi khóa (hoặc đã nhả) → bỏ qua. */
  settle(fingerprint: string, key: string, outcome: AttemptOutcome): void {
    if (this.pending.get(fingerprint) !== key) return;
    if (outcomeUnknown(outcome)) return;
    if (outcome !== "network" && outcome.status >= 200 && outcome.status < 300) return; // giữ tới khi cả thao tác xong
    this.pending.delete(fingerprint);
  }

  /** Nhả các khóa một lượt thao tác đã dùng — chỉ entry còn giữ đúng khóa đó. */
  release(issued: ReadonlyMap<string, string>): void {
    for (const [fp, key] of issued) if (this.pending.get(fp) === key) this.pending.delete(fp);
  }

  /** Bắt đầu một lượt thao tác của người dùng (một lần bấm Lưu/Gửi, có thể gồm nhiều lệnh ghi). */
  begin(): OperationRun {
    const run = new OperationRun(this);
    this.openRuns.push(run);
    if (this.openRuns.length > MAX_OPEN_RUNS) this.openRuns.shift();
    return run;
  }

  /** Lượt đã complete: thôi bảo vệ (các khóa của nó đã được release). */
  finish(run: OperationRun): void {
    const i = this.openRuns.indexOf(run);
    if (i >= 0) this.openRuns.splice(i, 1);
  }

  reset(): void {
    this.pending.clear();
    this.openRuns.length = 0;
  }

  get size(): number {
    return this.pending.size;
  }
}

/**
 * Một lượt thao tác (một lần bấm): ghi lại các khóa đã dùng; complete() khi CẢ lượt thành công → nhả đúng các khóa đó
 * (lần bấm sau là thao tác mới). Lượt thất bại giữa chừng không complete → bấm lại phát lại mọi bước bằng khóa cũ
 * (bước đã xong được máy chủ trả lại kết quả cũ, không ghi lần hai).
 */
export class OperationRun {
  private readonly issued = new Map<string, string>();

  constructor(readonly op: IdempotentOperation) {}

  record(fingerprint: string, key: string): void {
    this.issued.set(fingerprint, key);
  }

  /** Lượt này đã dùng khóa cho dấu vân tay này chưa. */
  uses(fingerprint: string): boolean {
    return this.issued.has(fingerprint);
  }

  complete(): void {
    this.op.release(this.issued);
    this.issued.clear();
    this.op.finish(this);
  }
}

let activeRun: OperationRun | null = null;

function isThenable(x: unknown): x is PromiseLike<unknown> {
  return typeof (x as { then?: unknown } | null)?.then === "function";
}

/**
 * Chạy fn trong một lượt thao tác. Client đọc lượt hiện hành ĐỒNG BỘ lúc bắt đầu gửi (trước await đầu tiên), nên mọi lời
 * gọi ghi khởi phát ngay trong fn dùng khóa của phiên. Lời gọi nằm SAU một await khác trong fn không còn trong phạm vi
 * → bọc lại bằng withIdempotentOperation(run, …) với CÙNG lượt (useAction truyền sẵn hàm `idem` cho việc này).
 * - target là IdempotentOperation: mở lượt mới (hoặc nhập lượt đang chạy đồng bộ của cùng phiên); fn trả Promise
 *   thành công → complete() lượt đó; thất bại → giữ khóa cho lần bấm lại.
 * - target là OperationRun: nhập lượt đó, không tự complete (nơi mở lượt quyết định).
 */
export function withIdempotentOperation<T>(target: IdempotentOperation | OperationRun, fn: () => T): T {
  const prev = activeRun;
  let run: OperationRun;
  let owns = false;
  if (target instanceof OperationRun) run = target;
  else if (prev && prev.op === target) run = prev;
  else {
    run = target.begin();
    owns = true;
  }
  activeRun = run;
  let result: T;
  try {
    result = fn();
  } finally {
    activeRun = prev;
  }
  if (owns) {
    if (isThenable(result)) result.then(() => run.complete(), () => undefined);
    else run.complete();
  }
  return result;
}

/** Lượt thao tác hiện hành (đọc đồng bộ khi bắt đầu gửi). */
export function currentIdempotentRun(): OperationRun | null {
  return activeRun;
}

export function currentIdempotentOperation(): IdempotentOperation | null {
  return activeRun?.op ?? null;
}

/** Một lần gửi: khóa dùng + báo kết quả cho phiên thao tác. */
export interface IdempotencyAttempt {
  key: string;
  settle(outcome: AttemptOutcome): void;
}

/**
 * Chọn khóa cho một lần gửi: khóa nơi gọi truyền tường minh > khóa của phiên thao tác (theo dấu vân tay) > khóa mới
 * (chỉ cho lời gọi một lần, không có nút "thử lại"). settle gắn với ĐÚNG khóa đã cấp cho lần gửi này.
 */
export function beginAttempt(scope: IdempotentOperation | OperationRun | null, fingerprint: string, explicit?: string | null): IdempotencyAttempt {
  if (explicit) return { key: explicit, settle: () => undefined };
  if (!scope) return { key: newIdempotencyKey(), settle: () => undefined };
  const op = scope instanceof OperationRun ? scope.op : scope;
  const key = op.keyFor(fingerprint);
  if (scope instanceof OperationRun) scope.record(fingerprint, key);
  return { key, settle: (o) => op.settle(fingerprint, key, o) };
}

async function problemCode(res: Response): Promise<string | null> {
  if (res.ok) return null;
  try {
    const j = (await res.clone().json()) as { code?: unknown };
    return typeof j?.code === "string" ? j.code : null;
  } catch {
    return null;
  }
}

const WRITE_METHODS = new Set(["POST", "PUT", "PATCH"]);
/** Phiên thao tác bắt được lúc yêu cầu khởi phát (theo id yêu cầu của openapi-fetch). */
const capturedRuns = new Map<string, OperationRun>();

/**
 * Middleware đặt ĐẦU TIÊN trong client openapi-fetch: onRequest của middleware đầu chạy đồng bộ trong lời gọi
 * api.POST(...), nên bắt được phiên thao tác hiện hành trước khi các middleware async (Bearer) chen await.
 */
export function idempotencyScopeMiddleware(): Middleware {
  return {
    onRequest({ request, id }) {
      const run = currentIdempotentRun();
      if (run && WRITE_METHODS.has(request.method.toUpperCase())) capturedRuns.set(id, run);
      return undefined;
    },
  };
}

/**
 * Middleware: mọi POST đều mang Idempotency-Key. Trong phiên thao tác (useAction / useIdempotentAction) khóa lấy từ
 * phiên theo dấu vân tay (POST/PUT/PATCH) và kết quả được báo lại để giữ/bỏ khóa. Ngoài phiên: nếu nơi gọi đã đặt
 * khóa (vd để thử lại đúng một thao tác sau lỗi mạng) thì giữ nguyên khóa đó, không thì sinh khóa mới.
 */
export function idempotencyMiddleware(generate: () => string = newIdempotencyKey): Middleware {
  const attempts = new Map<string, IdempotencyAttempt>();
  return {
    async onRequest({ request, id }) {
      const method = request.method.toUpperCase();
      const run = capturedRuns.get(id) ?? currentIdempotentRun();
      capturedRuns.delete(id);
      if (run && WRITE_METHODS.has(method)) {
        const body = await request.clone().text();
        const att = beginAttempt(run, requestFingerprint(method, request.url, request.headers.get("X-AG-Context"), body));
        attempts.set(id, att);
        request.headers.set(IDEMPOTENCY_HEADER, att.key);
        return request;
      }
      if (method === "POST" && !request.headers.get(IDEMPOTENCY_HEADER)) {
        request.headers.set(IDEMPOTENCY_HEADER, generate());
      }
      return request;
    },
    async onResponse({ response, id }) {
      const att = attempts.get(id);
      if (att) {
        attempts.delete(id);
        att.settle({ status: response.status, code: await problemCode(response) });
      }
      return undefined;
    },
    onError({ id }) {
      const att = attempts.get(id);
      if (att) {
        attempts.delete(id);
        att.settle("network");
      }
      return undefined;
    },
  };
}
