"use client";

// KNSPLIT-2-KN-RECORDS-HTX-READ (#162, mục 1): bộ chọn HTX của màn Máy móc và Thửa đọc hồ sơ KN riêng loại HTX
// (/v1/kn/producer-records, #67) thay GET legacy /v1/kn/cooperatives.
// - Phân trang/tìm theo máy chủ: mỗi lượt `limit` 50, "Tải thêm HTX" gửi `cursor` = next_cursor; ô tìm gửi `q` (không cắt
//   trang đầu ở client — bài học #67). Phạm vi xã do máy chủ lọc theo ngữ cảnh phiên; web chỉ gửi `commune` đang chọn.
// - KN-STANDALONE-5-TT-DATA (#271, #306): thửa ruộng và máy móc ở kho KN, HTX là CHÍNH hồ sơ KN — giá trị chọn là mã hồ sơ
//   (`ref`), mọi hồ sơ HTX đang hoạt động chọn được. Không còn đọc/liên kết HTX đã công bố (hoãn tới pha kết nối).
// - Khai báo máy đã chép của HTX cũ chưa có hồ sơ KN: mã đang chọn không ứng với hồ sơ nào → "chưa có hồ sơ KN".
import { useEffect, useId, useRef, useState } from "react";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import type { KnProducer } from "../api";
import { useLoad } from "../common";

/** Số hồ sơ mỗi lượt đọc của bộ chọn (máy chủ: 1–200). */
export const HTX_PICKER_PAGE = 50;
/** Chờ gõ xong rồi mới gửi `q` (ms). */
export const HTX_SEARCH_DELAY = 250;
/** Nhãn trạng thái của khai báo cũ mà mã HTX không ứng với hồ sơ KN nào trong phạm vi. */
export const UNLINKED_LABEL = "chưa có hồ sơ KN";

export type HtxOption = { value: string; label: string };

/** Một hồ sơ HTX → mục chọn: giá trị = mã hồ sơ KN. */
export function htxOptionOf(r: Pick<KnProducer, "ref" | "display_name" | "status">): HtxOption {
  return { value: r.ref, label: r.status === "inactive" ? `${r.display_name} (ngừng hoạt động)` : r.display_name };
}

/** Nhãn của HTX đang chọn mà không có trong danh sách: tên hồ sơ KN, hoặc tên dự phòng + "chưa có hồ sơ KN". */
export function currentHtxLabel(linked: Pick<KnProducer, "display_name" | "status"> | null | undefined, fallbackName?: string | null): string {
  if (linked) return linked.status === "inactive" ? `${linked.display_name} (ngừng hoạt động)` : linked.display_name;
  return `${fallbackName || "HTX hiện tại"} — ${UNLINKED_LABEL}`;
}

type Pages = { key: string; items: KnProducer[]; next: string | null; loading: boolean; problem: string | null };

/** Danh sách hồ sơ HTX theo trang của máy chủ (xã + tìm tên); `more()` đọc trang kế theo next_cursor. */
export function useHtxRecordPages(commune: string | undefined, q: string, enabled = true) {
  const { collectApi } = useAuth();
  const key = `${commune ?? ""}\u0000${q.trim()}`;
  const [st, setSt] = useState<Pages>({ key: "", items: [], next: null, loading: false, problem: null });
  const seq = useRef(0);
  const query = { kind: "cooperative" as const, status: "active" as const, ...(commune ? { commune } : {}), ...(q.trim() ? { q: q.trim() } : {}), limit: HTX_PICKER_PAGE };
  const fail = (e: unknown) => (e instanceof ApiProblem ? e.message : "Không đọc được danh sách hồ sơ HTX.");
  useEffect(() => {
    if (!enabled) return;
    const my = ++seq.current;
    const t = setTimeout(
      () =>
        collectApi.records(query).then(
          (r) => my === seq.current && setSt({ key, items: r.items, next: r.next_cursor, loading: false, problem: null }),
          (e: unknown) => my === seq.current && setSt({ key, items: [], next: null, loading: false, problem: fail(e) }),
        ),
      q.trim() ? HTX_SEARCH_DELAY : 0,
    );
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, enabled]);
  const more = () => {
    if (!st.next || st.loading || st.key !== key) return;
    const my = ++seq.current;
    const cursor = st.next;
    setSt((s) => ({ ...s, loading: true }));
    collectApi.records({ ...query, cursor }).then(
      (r) =>
        my === seq.current &&
        setSt((s) => {
          const have = new Set(s.items.map((x) => x.ref));
          // Con trỏ lặp lại (trang không có gì mới) → dừng, không lặp vô hạn.
          const added = r.items.filter((x) => !have.has(x.ref));
          return { ...s, items: [...s.items, ...added], next: added.length && r.next_cursor !== cursor ? r.next_cursor : null, loading: false };
        }),
      (e: unknown) => my === seq.current && setSt((s) => ({ ...s, loading: false, problem: fail(e) })),
    );
  };
  // Bộ lọc vừa đổi (chưa có kết quả của khóa mới) → coi là đang tải, chưa có mục nào.
  const fresh = st.key === key;
  return { key, items: fresh ? st.items : [], next: fresh ? st.next : null, loading: enabled && (!fresh || st.loading), problem: fresh ? st.problem : null, more };
}

/** Hồ sơ KN loại HTX có mã `ref` (mọi trạng thái, trong phạm vi phiên) — null khi không có. */
export function useLinkedHtxRecord(ref: string) {
  const { collectApi } = useAuth();
  const load = () => collectApi.record(ref).then((r): KnProducer | null => r, (e: unknown) => (e instanceof ApiProblem && e.status === 404 ? null : Promise.reject(e)));
  return useLoad(ref ? load : null, [ref]);
}

/**
 * Ô chọn HTX theo hồ sơ KN (tìm + tải thêm theo máy chủ). `value` là mã hồ sơ KN loại HTX mà khai báo máy/thửa tham chiếu.
 * `fallbackName`: tên HTX do dữ liệu trả về (vd. tên trong khai báo máy) — dùng khi mã đang chọn không ứng với hồ sơ KN nào.
 */
export function HtxRecordPicker({
  label = "HTX",
  value,
  onChange,
  commune,
  disabled,
  placeholder = "Chọn HTX",
  fallbackName,
  error,
}: {
  label?: string;
  value: string;
  onChange: (ref: string) => void;
  commune?: string;
  disabled?: boolean;
  placeholder?: string;
  fallbackName?: string | null;
  error?: string;
}) {
  const id = useId();
  const [q, setQ] = useState("");
  const pages = useHtxRecordPages(commune, q, !disabled);
  const linked = useLinkedHtxRecord(value);
  const listed = pages.items.map(htxOptionOf);
  const inList = !!value && listed.some((o) => o.value === value);
  const unlinked = !!value && !linked.loading && !linked.problem && linked.data === null;
  const current: HtxOption | null = value && !inList ? { value, label: linked.loading ? "Đang đối chiếu hồ sơ KN…" : currentHtxLabel(linked.data, fallbackName) } : null;
  const options = current ? [current, ...listed] : listed;
  const hint = unlinked ? `HTX đang chọn ${UNLINKED_LABEL} trong phạm vi phụ trách.` : null;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {disabled ? null : <input className="input" type="search" aria-label={`Tìm ${label} theo tên`} placeholder="Tìm theo tên HTX" maxLength={100} value={q} onChange={(e) => setQ(e.target.value)} />}
      <select id={id} className="select" value={value} disabled={disabled} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-err` : undefined} onChange={(e) => onChange(e.target.value)}>
        <option value="">{placeholder}</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      {disabled ? null : (
        <span className="row" style={{ gap: 8, alignItems: "center" }}>
          <span className="mute hint" aria-live="polite">
            {pages.loading ? "Đang tải hồ sơ HTX…" : pages.problem ? pages.problem : `${listed.length} hồ sơ HTX${pages.next ? " — còn nữa" : ""}${q.trim() ? ` khớp “${q.trim()}”` : ""}.`}
          </span>
          {pages.next ? (
            <button type="button" className="btn btn-o btn-sm" disabled={pages.loading} onClick={pages.more}>
              Tải thêm HTX
            </button>
          ) : null}
        </span>
      )}
      {hint && !error ? (
        <span className="mute hint" role="note">
          {hint}
        </span>
      ) : null}
      {error ? (
        <span className="err" id={`${id}-err`} role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}
