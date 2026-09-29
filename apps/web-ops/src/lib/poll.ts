// Chờ việc dài (R-52: 202 + job) bằng cách hỏi lại trạng thái theo chu kỳ.

export interface PollOptions<T> {
  /** Khoảng giữa hai lần hỏi (ms). */
  intervalMs?: number;
  /** Thời gian chờ tối đa (ms) — quá hạn thì trả trạng thái cuối, không ném lỗi. */
  timeoutMs?: number;
  /** Mỗi lần có trạng thái mới (hiển thị tiến trình). */
  onUpdate?: (v: T) => void;
  /** Hủy (vd rời trang). */
  signal?: AbortSignal;
  sleep?: (ms: number) => Promise<void>;
}

const defaultSleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** Hỏi `fetch` tới khi `done(v)` đúng, hết giờ hoặc bị hủy; trả trạng thái cuối cùng. */
export async function pollUntil<T>(fetch: () => Promise<T>, done: (v: T) => boolean, opts: PollOptions<T> = {}): Promise<T> {
  const interval = opts.intervalMs ?? 1000;
  const deadline = Date.now() + (opts.timeoutMs ?? 120_000);
  const sleep = opts.sleep ?? defaultSleep;
  let v = await fetch();
  opts.onUpdate?.(v);
  while (!done(v) && Date.now() < deadline && !opts.signal?.aborted) {
    await sleep(interval);
    if (opts.signal?.aborted) break;
    v = await fetch();
    opts.onUpdate?.(v);
  }
  return v;
}

/** Nhãn tiếng Việt cho trạng thái việc (mô phỏng, dựng mạng). */
export const JOB_STATUS: Record<string, string> = {
  queued: "Đang chờ",
  running: "Đang chạy",
  completed: "Hoàn tất",
  failed: "Lỗi",
};
