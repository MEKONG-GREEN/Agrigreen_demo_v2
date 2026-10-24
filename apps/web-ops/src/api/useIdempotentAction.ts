"use client";

// P0-WEB-IDEMKEY (#129): hook giữ MỘT phiên thao tác (IdempotentOperation) cho một form/màn hình.
// run(fn): mọi lời gọi ghi khởi phát trong fn dùng khóa của phiên — bấm lại cùng dữ liệu sau lỗi mạng/5xx/hết giờ
// gửi lại ĐÚNG khóa cũ (máy chủ phát lại kết quả, không tạo bản ghi thứ hai); thành công → lần sau khóa mới; đổi
// dữ liệu → khóa mới. reset(): bắt đầu lại (vd mở lại form muốn coi là thao tác mới).
import { useCallback, useMemo, useState } from "react";
import { IdempotentOperation, withIdempotentOperation } from "./idempotency";

export interface IdempotentAction {
  /** Chạy fn trong phiên thao tác của hook (đồng bộ tới await đầu tiên — xem withIdempotentOperation). */
  run: <T>(fn: () => T) => T;
  /** Bỏ mọi khóa đang treo. */
  reset: () => void;
  op: IdempotentOperation;
}

export function useIdempotentAction(): IdempotentAction {
  const [op] = useState(() => new IdempotentOperation());
  const run = useCallback(<T,>(fn: () => T): T => withIdempotentOperation(op, fn), [op]);
  const reset = useCallback(() => op.reset(), [op]);
  return useMemo(() => ({ run, reset, op }), [run, reset, op]);
}
