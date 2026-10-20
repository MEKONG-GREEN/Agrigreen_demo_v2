"use client";

// Hộ mô hình, điểm trình diễn (W11.47 — BRD CNTS v3.7 UR-XA-04, FN-10 phần hộ; prototype A "r-model_households"; FN-19
// "Cấp Xã: Dữ liệu › Điểm trình diễn"): danh sách điểm trong phạm vi (Cấp Xã: xã mình), chọn điểm để xem/thêm/sửa/rút hộ
// mô hình; danh sách mọi hộ theo trạng thái. Cấp Xã sửa được khi là cán bộ theo dõi điểm hoặc được cấp quyền (FN-14).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { createCntsModelApi, dmy } from "./api";
import { SitePanel } from "./parts";

export function ModelScreen() {
  const { api, kn } = useAuth();
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const role = kn.context?.role;
  const [tab, setTab] = useState<"sites" | "households">("sites");
  const [status, setStatus] = useState<"" | "active" | "withdrawn">("active");
  const sites = useLoad(mapi ? () => mapi.sites() : null, [mapi]);
  const households = useLoad(mapi && tab === "households" ? () => mapi.households(status ? { status } : {}) : null, [mapi, tab, status]);
  const officers = useLoad(mapi && (role === "kn_tinh" || role === "admin") ? () => mapi.candidates() : null, [mapi, role]);
  const communeName = useCommuneNames([...(sites.data ?? []).map((s) => s.commune_code), ...(households.data ?? []).map((h) => h.commune_code)]);
  const [open, setOpen] = useState<string | null>(null);
  if (!mapi) return null;
  return (
    <div className="page">
      <PageHeader title="Hộ mô hình, điểm trình diễn">
        <p className="mute">
          {role === "kn_xa"
            ? "Điểm trình diễn của xã phụ trách; cán bộ theo dõi điểm (hoặc được cấp quyền theo dõi mô hình) thêm, sửa hộ mô hình, người ghi nhật ký."
            : "Điểm trình diễn và hộ tham gia theo dự án; tạo điểm ở màn Chương trình, dự án."}
        </p>
      </PageHeader>
      <div className="row" role="tablist">
        <button role="tab" aria-selected={tab === "sites"} className={tab === "sites" ? "btn btn-primary" : "btn"} onClick={() => setTab("sites")}>
          Điểm trình diễn
        </button>
        <button role="tab" aria-selected={tab === "households"} className={tab === "households" ? "btn btn-primary" : "btn"} onClick={() => setTab("households")}>
          Hộ mô hình
        </button>
      </div>
      {tab === "sites" ? (
        <LoadState s={sites}>
          {(items) => (
            <table className="table">
              <thead>
                <tr>
                  <th>Điểm</th>
                  <th>Dự án</th>
                  <th>Xã</th>
                  <th>Loại mô hình</th>
                  <th>Cán bộ theo dõi</th>
                  <th>Mẫu nhật ký</th>
                  <th>Số hộ</th>
                </tr>
              </thead>
              <tbody>
                {items.length ? (
                  items.map((s) => (
                    <tr key={s.ref} className={open === s.ref ? "selected" : undefined}>
                      <td>
                        <button className="link" onClick={() => setOpen(open === s.ref ? null : s.ref)}>
                          {s.name}
                        </button>{" "}
                        {s.status === "closed" ? <StatusPill tone="neutral">Đã đóng</StatusPill> : null}
                      </td>
                      <td>{s.project_code}</td>
                      <td>{communeName(s.commune_code)}</td>
                      <td>{s.model_type_name ?? s.model_type_code}</td>
                      <td>{s.officer_display ?? "—"}</td>
                      <td>{s.diary_form ?? "—"}</td>
                      <td>{s.household_count}</td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td colSpan={7} className="empty">
                      Chưa có điểm trình diễn trong phạm vi.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
        </LoadState>
      ) : (
        <>
          <div className="row">
            <label>
              Trạng thái{" "}
              <select value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
                <option value="">Tất cả</option>
                <option value="active">Đang tham gia</option>
                <option value="withdrawn">Đã rút</option>
              </select>
            </label>
          </div>
          <LoadState s={households}>
            {(items) => (
              <table className="table">
                <thead>
                  <tr>
                    <th>Chủ hộ</th>
                    <th>Điểm</th>
                    <th>Xã</th>
                    <th>Quy mô</th>
                    <th>Người ghi nhật ký</th>
                    <th>Tham gia</th>
                    <th>Trạng thái</th>
                  </tr>
                </thead>
                <tbody>
                  {items.length ? (
                    items.map((h) => (
                      <tr key={h.ref}>
                        <td>{h.owner_label ?? "—"}</td>
                        <td>
                          <button className="link" onClick={() => (setTab("sites"), setOpen(h.site_ref))}>
                            {h.site_name}
                          </button>
                        </td>
                        <td>{communeName(h.commune_code)}</td>
                        <td>{h.scale ? `${h.scale} ${h.scale_unit ?? ""}` : "—"}</td>
                        <td>{h.keeper_kind === "self" ? "Hộ tự ghi" : (h.keeper_display ?? "Cán bộ")}</td>
                        <td>{dmy(h.joined_on)}</td>
                        <td>{h.status === "withdrawn" ? <StatusPill tone="neutral">Đã rút</StatusPill> : <StatusPill tone="ok">Đang tham gia</StatusPill>}</td>
                      </tr>
                    ))
                  ) : (
                    <tr>
                      <td colSpan={7} className="empty">
                        Chưa có hộ mô hình.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}
          </LoadState>
        </>
      )}
      {open && tab === "sites" ? (
        <SitePanel key={open} mapi={mapi} siteRef={open} officers={officers.data ?? []} communeName={communeName} onChanged={sites.reload} />
      ) : null}
    </div>
  );
}
