"use client";

// Màn "Quản lý nông hộ" dùng chung (/htx/[id] tab Nông hộ và /htx/nong-ho của Quản lý HTX):
// tìm theo SĐT (HTX US-HH-01 AC-2), gọi nhanh (US-HH-03), sao chép hàng loạt (US-HH-02 AC-2..4).
// Định danh chủ hộ không bao giờ nằm ở web: danh sách chỉ có nhãn đã che; SĐT đọc lúc bấm gọi
// (GET /v1/households/{ref}/contact, no-store) và không lưu lại sau khi đóng.
import { useState, type FormEvent } from "react";
import { householdsPage } from "@/api/adapters/cooperatives";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { ConsentState, Cooperative, HouseholdCopyResult, HouseholdListItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { dialUri } from "@/lib/dial";
import { formatDate, formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { ResultList } from "./common";
import { CANNOT_CALL, copyDraftErrors, copyPayload, householdQuery, likelyCanCall, type CopyDraft } from "./households";

/** Mục đích xử lý dữ liệu (R-42) → nhãn ngắn. */
const PURPOSE: Record<string, string> = {
  "mrv.evidence": "MRV",
  "extension.field_support": "Hỗ trợ hiện trường",
};

function ConsentBadges({ consents }: { consents: ConsentState[] | undefined }) {
  if (!consents || consents.length === 0) return <span className="chip c-mute">Chưa đồng ý</span>;
  return (
    <span className="row" style={{ gap: 4 }}>
      {consents.map((c) => (
        <span
          key={c.purpose}
          className={`chip ${c.status === "granted" ? "c-ok" : "c-warn"}`}
          title={`${c.status === "granted" ? "Đồng ý" : "Đã rút đồng ý"} lúc ${formatDateTime(c.event_time)}`}
        >
          {c.status === "granted" ? "Đồng ý" : "Đã rút"} · {PURPOSE[c.purpose] ?? c.purpose}
        </span>
      ))}
    </span>
  );
}

const labelOf = (h: HouseholdListItem) => (h.label_unavailable || !h.label ? "Nông hộ (không hiện nhãn)" : h.label);

/** Nút gọi nhanh: đọc SĐT lúc bấm, hiện liên kết tel: — thiết bị không gọi được thì báo AC-2 và cho sao chép số. */
function CallButton({ h }: { h: HouseholdListItem }) {
  const { api } = useAuth();
  const [phone, setPhone] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [copied, setCopied] = useState(false);
  const who = labelOf(h);
  const open = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const r = await unwrap(api.GET("/v1/households/{ref}/contact", { params: { path: { ref: h.household_ref } } }));
      setPhone(r.phone);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  const copy = async () => {
    if (!phone) return;
    try {
      await navigator.clipboard.writeText(phone);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  if (phone) {
    const canCall = likelyCanCall(typeof navigator === "undefined" ? undefined : navigator);
    return (
      <div className="stack" style={{ gap: 4 }} data-testid={`call-${h.household_ref}`}>
        <div className="row" style={{ gap: 6 }}>
          <a className="btn btn-p btn-sm" href={dialUri("tel", phone)} aria-label={`Gọi ${who}`}>
            <Icon name="phone" size={14} /> {phone}
          </a>
          <button type="button" className="btn btn-o btn-sm" onClick={() => void copy()}>
            {copied ? "Đã sao chép" : "Sao chép số"}
          </button>
          <button type="button" className="btn btn-o btn-sm" onClick={() => setPhone(null)} aria-label={`Ẩn số ${who}`}>
            Ẩn
          </button>
        </div>
        {!canCall ? (
          <span className="small" role="status" style={{ color: "var(--o700, #9a5b00)" }}>
            {CANNOT_CALL} Hãy gọi bằng điện thoại theo số trên.
          </span>
        ) : null}
      </div>
    );
  }
  return (
    <div className="stack" style={{ gap: 4 }}>
      <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void open()} aria-label={`Gọi nhanh ${who}`}>
        <Icon name="phone" size={14} /> {busy ? "Đang lấy số…" : "Gọi"}
      </button>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </div>
  );
}

/** Hộp thoại sao chép hàng loạt: sửa riêng họ tên/SĐT từng hồ sơ mới rồi gửi một lần (AC-2); kết quả theo hồ sơ (AC-3). */
function CopyDialog({ coop, sources, onClose, onDone }: { coop: Cooperative; sources: HouseholdListItem[]; onClose: () => void; onDone: () => void }) {
  const { api } = useAuth();
  const [rows, setRows] = useState<CopyDraft[]>(() => sources.map((h) => ({ sourceRef: h.household_ref, sourceLabel: labelOf(h), fullName: "", phone: "" })));
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<HouseholdCopyResult | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const errors = copyDraftErrors(rows);
  const edit = (i: number, patch: Partial<CopyDraft>) => {
    setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)));
    setKey(newIdempotencyKey());
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (errors.some(Boolean)) return;
    setBusy(true);
    setProblem(null);
    try {
      const r = await unwrap(api.POST("/v1/households/copy", { params: { header: { "Idempotency-Key": key } }, body: { items: copyPayload(rows) } }));
      setResult(r);
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={`Sao chép ${sources.length} nông hộ — ${coop.name}`}
      onClose={onClose}
      actions={
        result ? null : (
          <button type="submit" form="hh-copy" className="btn btn-p btn-sm" disabled={busy}>
            {busy ? "Đang sao chép…" : "Xác nhận sao chép"}
          </button>
        )
      }
    >
      {result ? (
        <div className="stack" style={{ gap: 8 }}>
          <p role="status">
            <b style={{ display: "inline" }}>{result.created} hồ sơ thành công</b> · {result.rejected} hồ sơ lỗi
          </p>
          <ResultList
            caption="Kết quả sao chép từng hồ sơ"
            rows={result.rows.map((r) => ({
              key: String(r.index),
              label: `Hồ sơ ${r.index}: ${rows[r.index - 1]?.fullName ?? ""}`,
              ok: r.status === "created",
              status: r.status === "created" ? "Đã tạo" : "Bị loại",
              reason: r.reason ?? undefined,
            }))}
          />
        </div>
      ) : (
        <form id="hh-copy" className="stack" style={{ gap: 10 }} onSubmit={(e) => void submit(e)} aria-label="Sao chép hàng loạt nông hộ">
          <p className="small mute">
            Hồ sơ mới giữ HTX, xã và diện tích canh tác của hộ nguồn. Nhập họ tên và SĐT chủ hộ MỚI cho từng hồ sơ — định danh của hộ nguồn không
            được sao chép và được lưu mã hoá trong kho định danh.
          </p>
          {rows.map((r, i) => (
            <fieldset key={`${r.sourceRef}-${i}`} className="plain" aria-label={`Hồ sơ ${i + 1}`}>
              <legend className="small">
                Hồ sơ {i + 1} — sao chép từ <b style={{ display: "inline" }}>{r.sourceLabel}</b>
              </legend>
              <div className="row" style={{ alignItems: "flex-start" }}>
                <div className="field" style={{ flex: "1 1 200px" }}>
                  <label htmlFor={`cp-name-${i}`}>Họ tên chủ hộ (hồ sơ {i + 1})</label>
                  <input id={`cp-name-${i}`} className="input" value={r.fullName} onChange={(e) => edit(i, { fullName: e.target.value })} autoComplete="off" />
                </div>
                <div className="field" style={{ flex: "1 1 160px" }}>
                  <label htmlFor={`cp-phone-${i}`}>SĐT (hồ sơ {i + 1})</label>
                  <input id={`cp-phone-${i}`} className="input" inputMode="tel" value={r.phone} onChange={(e) => edit(i, { phone: e.target.value })} autoComplete="off" />
                </div>
              </div>
              {touched && errors[i] ? <span className="err small">{errors[i]}</span> : null}
            </fieldset>
          ))}
          {problem ? <ProblemAlert problem={problem} /> : null}
        </form>
      )}
    </Dialog>
  );
}

/** Danh sách nông hộ của một HTX kèm tìm SĐT, gọi nhanh, chọn để sao chép hàng loạt. */
export function HouseholdsPanel({ coop }: { coop: Cooperative }) {
  const { api, principal } = useAuth();
  const id = coop.ref;
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState<{ phone: string } | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const [picked, setPicked] = useState<Map<string, HouseholdListItem>>(new Map());
  const [copying, setCopying] = useState(false);
  const canWrite = coop.active && hasRole(principal, ROLES.coopManager, ROLES.knOfficer, ROLES.knProvinceAdmin, ROLES.registryAdmin);
  const list = usePagedList((cursor) => householdsPage(api, id, cursor, 20), [api, id, version]);
  const found = useAsync(
    () => (search ? unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: id }, query: { phone: search.phone } } })) : Promise.resolve(null)),
    [api, id, search, version],
  );
  const communeName = (code: string) => (code === coop.admin_unit_code ? (coop.commune_name ?? code) : code);
  const rows = search ? (found.data?.items ?? []) : list.items;

  const submitSearch = (e: FormEvent) => {
    e.preventDefault();
    const q = householdQuery(query);
    if (!q) {
      setSearch(null);
      setQueryError(null);
      return;
    }
    if ("error" in q) {
      setQueryError(q.error);
      return;
    }
    setQueryError(null);
    setSearch(q);
  };
  const toggle = (h: HouseholdListItem) =>
    setPicked((m) => {
      const next = new Map(m);
      if (next.has(h.household_ref)) next.delete(h.household_ref);
      else next.set(h.household_ref, h);
      return next;
    });

  return (
    <>
      <div className="alert alert-info" style={{ marginBottom: 12 }}>
        <Icon name="info" />
        <span>
          Thông tin định danh nông hộ (họ tên, SĐT, CCCD, địa chỉ) được lưu tách trong kho định danh mã hoá. Web vận hành chỉ hiển thị{" "}
          <b style={{ display: "inline" }}>nhãn đã che</b>; SĐT chỉ đọc khi bấm “Gọi” và không lưu lại.
        </span>
      </div>
      <form className="row" role="search" onSubmit={submitSearch} style={{ marginBottom: 12, alignItems: "flex-end" }}>
        <div className="field" style={{ flex: "1 1 260px" }}>
          <label htmlFor={`hh-q-${id}`}>Tìm nông hộ theo SĐT</label>
          <input id={`hh-q-${id}`} className="input" type="search" inputMode="tel" placeholder="0xxxxxxxxx" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <button type="submit" className="btn btn-o btn-sm">
          <Icon name="search" size={15} /> Tìm
        </button>
        {search ? (
          <button
            type="button"
            className="btn btn-o btn-sm"
            onClick={() => {
              setSearch(null);
              setQuery("");
            }}
          >
            Xem tất cả
          </button>
        ) : null}
        {canWrite ? (
          <button type="button" className="btn btn-p btn-sm" disabled={picked.size === 0} onClick={() => setCopying(true)}>
            Sao chép hàng loạt ({picked.size})
          </button>
        ) : null}
      </form>
      {queryError ? (
        <p className="err small" role="alert">
          {queryError}
        </p>
      ) : null}
      {search && found.data && found.data.items.length === 0 ? <Empty>Không có nông hộ của HTX này với SĐT {search.phone}.</Empty> : null}
      {(search ? found.error : list.error) ? (
        <ProblemAlert problem={(search ? found.error : list.error)!} onRetry={search ? found.reload : list.reload} />
      ) : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Nông hộ thành viên (nhãn đã che)</caption>
          <thead>
            <tr>
              {canWrite ? <th scope="col">Chọn</th> : null}
              <th scope="col">Nông hộ (đã che)</th>
              <th scope="col">SĐT (đã che)</th>
              <th scope="col">Xã</th>
              <th scope="col" className="num">Diện tích canh tác (ha)</th>
              <th scope="col">Vào HTX</th>
              <th scope="col">Đồng ý chia sẻ dữ liệu</th>
              <th scope="col">Trạng thái</th>
              {canWrite ? <th scope="col">Liên hệ</th> : null}
            </tr>
          </thead>
          <tbody>
            {rows.map((h) => (
              <tr key={h.household_ref}>
                {canWrite ? (
                  <td>
                    <input type="checkbox" aria-label={`Chọn ${labelOf(h)} để sao chép`} checked={picked.has(h.household_ref)} onChange={() => toggle(h)} />
                  </td>
                ) : null}
                <td>{h.label_unavailable ? <span className="mute">Không hiển thị được nhãn</span> : h.label}</td>
                <td style={{ fontFamily: "ui-monospace, Consolas, monospace" }}>{h.masked_phone || "—"}</td>
                <td>{communeName(h.commune_code)}</td>
                <td className="num">{formatDecimal(h.cultivated_area_ha, 2)}</td>
                <td>{formatDate(h.member_since)}</td>
                <td>
                  <ConsentBadges consents={h.consents} />
                </td>
                <td>{h.active ? <span className="chip c-ok">Thành viên</span> : <span className="chip c-mute">Đã rời</span>}</td>
                {canWrite ? (
                  <td style={{ minWidth: 150 }}>
                    <CallButton h={h} />
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {(search ? found.loading : list.loading) ? <Loading /> : null}
      {!search && !list.loading && list.items.length === 0 && !list.error ? <Empty>Chưa có nông hộ.</Empty> : null}
      {!search ? <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} /> : null}
      {copying ? (
        <CopyDialog
          coop={coop}
          sources={Array.from(picked.values())}
          onClose={() => {
            setCopying(false);
            setPicked(new Map());
          }}
          onDone={() => setVersion((v) => v + 1)}
        />
      ) : null}
    </>
  );
}
