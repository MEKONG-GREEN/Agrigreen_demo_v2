"use client";

// Hộ mô hình, điểm trình diễn (W11.47 — BRD CNTS v3.7 UR-XA-04, FN-10 phần hộ; prototype A "r-model_households"; FN-19
// "Cấp Xã: Dữ liệu › Điểm trình diễn"): danh sách điểm trong phạm vi (Cấp Xã: xã mình), chọn điểm để xem/thêm/sửa/rút hộ
// mô hình; danh sách mọi hộ theo trạng thái. Cấp Xã sửa được khi là cán bộ theo dõi điểm hoặc được cấp quyền (FN-14).
// BF-SWEEP-CNTS (#101): bảng có STT (DataTable).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
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
            <DataTable
              caption="Điểm trình diễn"
              rows={items}
              rowKey={(s) => s.ref}
              empty="Chưa có điểm trình diễn trong phạm vi."
              columns={[
                {
                  key: "name",
                  header: "Điểm",
                  cell: (s) => (
                    <>
                      <button className="link" aria-expanded={open === s.ref} onClick={() => setOpen(open === s.ref ? null : s.ref)}>
                        {s.name}
                      </button>{" "}
                      {s.status === "closed" ? <StatusPill tone="neutral">Đã đóng</StatusPill> : null}
                    </>
                  ),
                },
                { key: "project", header: "Dự án", cell: (s) => s.project_code },
                { key: "commune", header: "Xã", cell: (s) => communeName(s.commune_code) },
                { key: "model", header: "Loại mô hình", cell: (s) => s.model_type_name ?? s.model_type_code },
                { key: "officer", header: "Cán bộ theo dõi", cell: (s) => s.officer_display ?? "—" },
                { key: "diary", header: "Mẫu nhật ký", cell: (s) => s.diary_form ?? "—" },
                { key: "hh", header: "Số hộ", cell: (s) => s.household_count },
              ]}
            />
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
              <DataTable
                caption="Hộ mô hình"
                rows={items}
                rowKey={(h) => h.ref}
                empty="Chưa có hộ mô hình."
                columns={[
                  { key: "owner", header: "Chủ hộ", cell: (h) => h.owner_label ?? "—" },
                  {
                    key: "site",
                    header: "Điểm",
                    cell: (h) => (
                      <button className="link" onClick={() => (setTab("sites"), setOpen(h.site_ref))}>
                        {h.site_name}
                      </button>
                    ),
                  },
                  { key: "commune", header: "Xã", cell: (h) => communeName(h.commune_code) },
                  { key: "scale", header: "Quy mô", cell: (h) => (h.scale ? `${h.scale} ${h.scale_unit ?? ""}` : "—") },
                  { key: "keeper", header: "Người ghi nhật ký", cell: (h) => (h.keeper_kind === "self" ? "Hộ tự ghi" : (h.keeper_display ?? "Cán bộ")) },
                  { key: "joined", header: "Tham gia", cell: (h) => dmy(h.joined_on) },
                  { key: "st", header: "Trạng thái", cell: (h) => (h.status === "withdrawn" ? <StatusPill tone="neutral">Đã rút</StatusPill> : <StatusPill tone="ok">Đang tham gia</StatusPill>) },
                ]}
              />
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
