"use client";

// Số liệu cấp xã › Nhật ký sản xuất — Cấp Tỉnh Phòng CNTS (BF-CN-XA-DATA #175, BRD CNTS v3.7 FN-10 phần Tỉnh đọc, FN-13):
// tra cứu mọi lần ghi nhật ký BM-08 (gà), BM-09 (vịt xiêm), BM-19 (thủy sản) của hộ mô hình các xã trong phạm vi; lọc xã,
// chương trình, hộ, mẫu, trạng thái, khoảng ngày ghi, cảnh báo — lọc và phân trang ở máy chủ (GET diary-entries, con trỏ
// next_cursor). Chi tiết CHỈ ĐỌC dùng lại khối của màn Rà soát (số liệu, bảng phụ, kiểm tra tự động, vị trí, ảnh, lịch sử);
// lần ghi chờ xác thực có liên kết sang Rà soát nhật ký. Tỉnh không sửa số liệu xã — màn không có nút ghi.
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader } from "@/components/patterns/list";
import { LoadState, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { ProblemAlert } from "@/components/ProblemAlert";
import { ApiProblem } from "@/api/problem";
import { createCntsModelApi } from "@/kn/cnts/model/api";
import { createCntsDiaryApi, dmy, ENTRY_PAGE, FORM_LABEL, KIND_LABEL, STATUS_LABEL, type CntsDiaryApi, type Entry, type EntryPageQuery, type EntryStatus, type Form } from "./api";
import { Checks, flagged, LocationInfo, MetricsTable, Photos, StatusBadge, TablesView, WeightChart } from "./parts";

const FORMS: Form[] = ["BM-08", "BM-09", "BM-19"];
const EVENT_LABEL: Record<string, string> = { created: "Tạo", updated: "Sửa", submitted: "Gửi", returned: "Yêu cầu bổ sung", verified: "Xác thực", corrected: "Điều chỉnh" };
const STATUSES: EntryStatus[] = ["submitted", "returned", "verified"];

interface Filter {
  commune: string;
  project: string;
  household: string;
  form: Form | "";
  status: EntryStatus | "";
  from: string;
  to: string;
  flagged: boolean;
}

const EMPTY: Filter = { commune: "", project: "", household: "", form: "", status: "", from: "", to: "", flagged: false };

/** Tham số gửi máy chủ (bỏ ô trống). */
export function pageQuery(f: Filter, cursor?: string): EntryPageQuery {
  return {
    ...(f.commune ? { commune: f.commune } : {}),
    ...(f.project ? { project_ref: f.project } : {}),
    ...(f.household ? { household_ref: f.household } : {}),
    ...(f.form ? { form: f.form } : {}),
    ...(f.status ? { status: f.status } : {}),
    ...(f.from ? { from: f.from } : {}),
    ...(f.to ? { to: f.to } : {}),
    ...(f.flagged ? { flagged: true } : {}),
    limit: ENTRY_PAGE,
    ...(cursor ? { cursor } : {}),
  };
}

export function ProvinceDiaryScreen() {
  const { api } = useAuth();
  const dapi = useMemo(() => (api ? createCntsDiaryApi(api) : null), [api]);
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const projects = useLoad(mapi ? async () => (await mapi.projects()).items : null, [mapi]);
  const households = useLoad(mapi ? () => mapi.households() : null, [mapi]);
  const [f, setF] = useState<Filter>(EMPTY);
  const [open, setOpen] = useState<string | null>(null);
  const hs = households.data ?? [];
  const communes = [...new Set(hs.map((h) => h.commune_code))].sort();
  const communeName = useCommuneNames(communes);
  const shownHouseholds = hs.filter((h) => (!f.commune || h.commune_code === f.commune) && (!f.project || h.project_ref === f.project));
  const set = (patch: Partial<Filter>) => {
    setF((x) => {
      const n = { ...x, ...patch };
      // Đổi xã / chương trình → bỏ hộ không còn khớp.
      if (n.household && !hs.some((h) => h.ref === n.household && (!n.commune || h.commune_code === n.commune) && (!n.project || h.project_ref === n.project))) n.household = "";
      return n;
    });
    setOpen(null);
  };
  const dateErr = f.from && f.to && f.to < f.from ? "Đến ngày phải không trước Từ ngày." : null;
  if (!dapi) return null;
  return (
    <div className="page">
      <PageHeader title="Nhật ký sản xuất — số liệu cấp xã">
        <p className="mute">
          Lần ghi nhật ký BM-08, BM-09, BM-19 của hộ mô hình các xã trong tỉnh (chỉ xem). Xác thực, yêu cầu bổ sung ở{" "}
          <Link href="/kn/cnts/ra-soat-nhat-ky">Rà soát nhật ký</Link>.
        </p>
      </PageHeader>
      <div className="toolbar" role="group" aria-label="Bộ lọc">
        <select className="select" aria-label="Xã" value={f.commune} onChange={(e) => set({ commune: e.target.value })}>
          <option value="">Mọi xã có mô hình</option>
          {communes.map((c) => (
            <option key={c} value={c}>
              {communeName(c)}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Chương trình" value={f.project} onChange={(e) => set({ project: e.target.value })}>
          <option value="">Mọi chương trình</option>
          {(projects.data ?? []).map((p) => (
            <option key={p.ref} value={p.ref}>
              {p.code} — {p.name}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Hộ" value={f.household} onChange={(e) => set({ household: e.target.value })}>
          <option value="">Mọi hộ</option>
          {shownHouseholds.map((h) => (
            <option key={h.ref} value={h.ref}>
              {h.owner_label ?? h.ref.slice(0, 8)} · {h.site_name}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Mẫu nhật ký" value={f.form} onChange={(e) => set({ form: e.target.value as Form | "" })}>
          <option value="">Mọi mẫu</option>
          {FORMS.map((x) => (
            <option key={x} value={x}>
              {x} · {FORM_LABEL[x]}
            </option>
          ))}
        </select>
        <select className="select" aria-label="Trạng thái" value={f.status} onChange={(e) => set({ status: e.target.value as EntryStatus | "" })}>
          <option value="">Mọi trạng thái</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <label>
          Từ ngày <input className="input" type="date" aria-label="Từ ngày" value={f.from} onChange={(e) => set({ from: e.target.value })} />
        </label>
        <label>
          Đến ngày <input className="input" type="date" aria-label="Đến ngày" value={f.to} onChange={(e) => set({ to: e.target.value })} />
        </label>
        <label>
          <input type="checkbox" checked={f.flagged} onChange={(e) => set({ flagged: e.target.checked })} /> Chỉ lần ghi có cảnh báo
        </label>
      </div>
      {dateErr ? (
        <p className="err" role="alert">
          {dateErr}
        </p>
      ) : (
        <EntryList key={JSON.stringify(f)} dapi={dapi} filter={f} open={open} setOpen={setOpen} />
      )}
      {open ? <EntryDetailView key={open} dapi={dapi} entryRef={open} /> : null}
    </div>
  );
}

function EntryList({ dapi, filter, open, setOpen }: { dapi: CntsDiaryApi; filter: Filter; open: string | null; setOpen: (r: string | null) => void }) {
  // Trang đầu theo bộ lọc (thành phần được khóa theo bộ lọc — đổi lọc là dựng lại); trang sau nối thêm khi bấm "Tải thêm".
  const first = useLoad(() => dapi.entryPage(pageQuery(filter)), [dapi]);
  const [more, setMore] = useState<{ items: Entry[]; cursor: string | null } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const items = [...(first.data?.items ?? []), ...(more?.items ?? [])];
  const cursor = more ? more.cursor : (first.data?.next_cursor ?? null);
  const loading = first.loading || busy;
  const communeName = useCommuneNames(items.map((e) => e.commune_code));
  const loadMore = async () => {
    if (!cursor) return;
    setBusy(true);
    setProblem(null);
    try {
      const p = await dapi.entryPage(pageQuery(filter, cursor));
      setMore((m) => ({ items: [...(m?.items ?? []), ...p.items], cursor: p.next_cursor }));
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    } finally {
      setBusy(false);
    }
  };
  const err = first.problem ?? problem;
  return (
    <section className="card sec">
      {err ? (
        <ProblemAlert
          problem={err}
          onRetry={() => {
            if (first.problem) {
              setMore(null);
              first.reload();
            } else void loadMore();
          }}
        />
      ) : null}
      <div className="table-scroll">
        <table className="dt" aria-label="Lần ghi nhật ký">
          <thead>
            <tr>
              <th>STT</th>
              <th>Ngày ghi</th>
              <th>Mẫu</th>
              <th>Chương trình</th>
              <th>Điểm / hộ</th>
              <th>Xã</th>
              <th>Người ghi</th>
              <th>Cảnh báo</th>
              <th>Trạng thái</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.length ? (
              items.map((e, i) => (
                <tr key={e.ref} className={open === e.ref ? "selected" : undefined}>
                  <td>{i + 1}</td>
                  <td>{dmy(e.entry_date)}</td>
                  <td>
                    {e.form} · {KIND_LABEL[e.entry_kind]}
                  </td>
                  <td>{e.project_code}</td>
                  <td>
                    {e.site_name}
                    {e.owner_label ? ` · ${e.owner_label}` : ""}
                  </td>
                  <td>{communeName(e.commune_code)}</td>
                  <td>{e.author_display ?? "—"}</td>
                  <td>{flagged(e) ? `${e.checks.filter((c) => c.level === "warn").length} cảnh báo` : "—"}</td>
                  <td>
                    <StatusBadge status={e.status} />
                  </td>
                  <td>
                    <button className="btn btn-sm" aria-label={`Xem lần ghi ${dmy(e.entry_date)} ${e.site_name}`} onClick={() => setOpen(open === e.ref ? null : e.ref)}>
                      {open === e.ref ? "Đóng" : "Xem"}
                    </button>
                  </td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={10} className="empty">
                  {loading ? "Đang tải…" : "Không có lần ghi khớp bộ lọc."}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {cursor ? (
        <button className="btn" disabled={loading} onClick={() => void loadMore()}>
          {loading ? "Đang tải…" : "Tải thêm"}
        </button>
      ) : items.length ? (
        <p className="mute">Đã hiện hết {items.length} lần ghi.</p>
      ) : null}
    </section>
  );
}

function EntryDetailView({ dapi, entryRef }: { dapi: CntsDiaryApi; entryRef: string }) {
  const d = useLoad(() => dapi.entry(entryRef), [dapi, entryRef]);
  return (
    <LoadState s={d}>
      {(e) => (
        <section className="card sec" aria-label="Chi tiết lần ghi">
          <h3>
            {FORM_LABEL[e.form]} · {KIND_LABEL[e.entry_kind]} · {dmy(e.entry_date)} <StatusBadge status={e.status} />
          </h3>
          <p className="mute">
            {e.project_code} · {e.site_name}
            {e.owner_label ? ` · ${e.owner_label}` : ""}
            {e.breed ? ` · ${e.breed}` : ""} · ghi bởi {e.author_display ?? "—"} lúc {new Date(e.captured_at).toLocaleString("vi-VN")}
          </p>
          {e.status_reason ? <p>Lý do: {e.status_reason}</p> : null}
          {e.status === "submitted" ? (
            <p>
              Lần ghi chờ xác thực — <Link href="/kn/cnts/ra-soat-nhat-ky">mở Rà soát nhật ký</Link> (chủ nhiệm chương trình hoặc lãnh đạo phòng).
            </p>
          ) : null}
          <h4>Số liệu</h4>
          <MetricsTable form={e.form} entry={e} previous={e.previous?.metrics} />
          <TablesView form={e.form} tables={e.tables} />
          <h4>Kiểm tra tự động</h4>
          <Checks checks={e.checks} />
          <h4>Vị trí</h4>
          <LocationInfo e={e} />
          <h4>Tăng trọng theo lần cân</h4>
          <WeightChart points={e.weights} />
          <h4>Ảnh</h4>
          <Photos dapi={dapi} photos={e.photos} />
          <details>
            <summary>Lịch sử ({e.versions.length} phiên bản)</summary>
            <ul className="list-plain">
              {e.versions.map((v) => (
                <li key={v.version}>
                  #{v.version} · {EVENT_LABEL[v.event] ?? v.event} · {v.updated_by_display ?? v.updated_by} · {new Date(v.updated_at).toLocaleString("vi-VN")}
                  {v.status_reason ? ` · ${v.status_reason}` : ""}
                </li>
              ))}
            </ul>
          </details>
        </section>
      )}
    </LoadState>
  );
}
