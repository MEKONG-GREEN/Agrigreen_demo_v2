"use client";

// KNSPLIT-2-KN-RECORDS-HTX-READ (#162, mục 1): bộ chọn HTX của màn Máy móc và Thửa đọc hồ sơ KN riêng loại HTX
// (/v1/kn/producer-records, #67) thay GET legacy /v1/kn/cooperatives. Khai báo máy (/v1/kn/machine-counts) và thửa
// (/v1/kn/plots) vẫn tham chiếu HTX đã công bố nên giá trị chọn là `htx_ref` của hồ sơ (mục 2–3 chờ PO chốt chủ dữ liệu).
// - Phân trang/tìm theo máy chủ: mỗi lượt `limit` 50, "Tải thêm HTX" gửi `cursor` = next_cursor; ô tìm gửi `q` (không cắt
//   trang đầu ở client — bài học #67). Phạm vi xã do máy chủ lọc theo ngữ cảnh phiên; web chỉ gửi `commune` đang chọn.
// - Hồ sơ chưa liên kết HTX đã công bố: hiện nhưng không chọn được (khai báo máy/thửa cần HTX đã công bố).
// - Dữ liệu cũ tham chiếu HTX nền tảng: đối chiếu qua `htx_ref`; không có hồ sơ KN liên kết → "chưa liên kết hồ sơ KN".
import { useEffect, useId, useRef, useState } from "react";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import type { KnProducer } from "../api";
import { useLoad } from "../common";

/** Số hồ sơ mỗi lượt đọc của bộ chọn (máy chủ: 1–200). */
export const HTX_PICKER_PAGE = 50;
/** Chờ gõ xong rồi mới gửi `q` (ms). */
export const HTX_SEARCH_DELAY = 250;
/** Nhãn trạng thái dữ liệu cũ chưa đối chiếu được với hồ sơ KN. */
export const UNLINKED_LABEL = "chưa liên kết hồ sơ KN";

export type HtxOption = { value: string; label: string; disabled?: boolean };

/** Một hồ sơ HTX → mục chọn: giá trị = htx_ref; chưa liên kết HTX đã công bố → mục khóa (giá trị riêng, không gửi đi). */
export function htxOptionOf(r: Pick<KnProducer, "ref" | "display_name" | "htx_ref" | "status">): HtxOption {
  if (!r.htx_ref) return { value: `kn:${r.ref}`, label: `${r.display_name} — chưa liên kết HTX đã công bố`, disabled: true };
  return { value: r.htx_ref, label: r.status === "inactive" ? `${r.display_name} (ngừng hoạt động)` : r.display_name };
}

/** Bỏ mục trùng giá trị (hai hồ sơ cùng liên kết một HTX) — giữ mục đầu. */
export function uniqueOptions(list: HtxOption[]): HtxOption[] {
  const seen = new Set<string>();
  return list.filter((o) => (seen.has(o.value) ? false : (seen.add(o.value), true)));
}

/** Nhãn của HTX đang được dữ liệu cũ tham chiếu: tên hồ sơ KN liên kết, hoặc tên dự phòng + "chưa liên kết hồ sơ KN". */
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

/** Hồ sơ KN loại HTX liên kết `htxRef` (mọi trạng thái, trong phạm vi phiên) — null khi chưa liên kết. */
export function useLinkedHtxRecord(htxRef: string) {
  const { collectApi } = useAuth();
  return useLoad(htxRef ? () => collectApi.records({ kind: "cooperative", htx_ref: htxRef, status: "all", limit: 1 }).then((r) => r.items[0] ?? null) : null, [htxRef]);
}

/**
 * Ô chọn HTX theo hồ sơ KN (tìm + tải thêm theo máy chủ). `value` là htx_ref (HTX đã công bố) mà khai báo máy/thửa tham chiếu.
 * `fallbackName`: tên HTX do dữ liệu cũ trả về (vd. tên trong khai báo máy) — dùng khi chưa có hồ sơ KN liên kết.
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
  onChange: (htxRef: string) => void;
  commune?: string;
  disabled?: boolean;
  placeholder?: string;
  fallbackName?: string | null;
  error?: string;
}) {
  const { collectApi } = useAuth();
  const id = useId();
  const [q, setQ] = useState("");
  const pages = useHtxRecordPages(commune, q, !disabled);
  const linked = useLinkedHtxRecord(value);
  const listed = uniqueOptions(pages.items.map(htxOptionOf));
  const inList = !!value && listed.some((o) => o.value === value);
  const unlinked = !!value && !linked.loading && !linked.problem && linked.data === null;
  // Chưa liên kết và màn không có tên dự phòng: lấy tên HTX đã công bố (publication, chỉ đọc) của xã.
  const pub = useLoad(unlinked && !fallbackName && commune ? () => collectApi.htxCandidates(commune).then((r) => r.items.find((x) => x.htx_ref === value)?.name ?? null) : null, [unlinked, fallbackName, commune, value]);
  const current: HtxOption | null = value && !inList ? { value, label: linked.loading ? "Đang đối chiếu hồ sơ KN…" : currentHtxLabel(linked.data, fallbackName ?? pub.data) } : null;
  const options = current ? [current, ...listed] : listed;
  const hint = unlinked ? `HTX đang chọn ${UNLINKED_LABEL}: thêm hồ sơ HTX ở Dữ liệu › HTX và liên kết HTX đã công bố để đối chiếu.` : null;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {disabled ? null : <input className="input" type="search" aria-label={`Tìm ${label} theo tên`} placeholder="Tìm theo tên HTX" maxLength={100} value={q} onChange={(e) => setQ(e.target.value)} />}
      <select id={id} className="select" value={value} disabled={disabled} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-err` : undefined} onChange={(e) => onChange(e.target.value)}>
        <option value="">{placeholder}</option>
        {options.map((o) => (
          <option key={o.value} value={o.value} disabled={o.disabled}>
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
