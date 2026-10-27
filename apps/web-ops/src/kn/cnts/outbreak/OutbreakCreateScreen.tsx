"use client";

// Tạo báo cáo ổ dịch (BF-CN-OUTBREAK, PRD UP-08; BRD CNTS UR-XA-10, BM-10) trên TRANG RIÊNG (R-A2): ba phần — tình hình dịch;
// xét nghiệm, xử lý, khoanh vùng; phòng chống, kiến nghị. Trường có dấu * bắt buộc, lấy vị trí GPS, ô số mặc định 0 và
// không âm. Gửi → máy chủ phát pf.extension.outbreak.reported: cảnh báo "Khẩn" cho Phòng CNTS (cntsalert, BF-NOTIFY-CNTS)
// và ổ dịch có tọa độ hiện trên Bản đồ vùng dịch (#28). Sau khi gửi: Xuất Word theo mẫu BM-10 (pf/export).
import Link from "next/link";
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { BackLink } from "@/components/patterns/detail";
import { PageHeader } from "@/components/patterns/list";
import { useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import { CREATE_ROUTE, MAP_ROUTE, OutbreakEditor } from "./OutbreakScreen";
import { createOutbreakApi, createOutbreakWordApi, outbreakCode } from "./api";

const LIST_ROUTE = "/kn/cnts/dich-benh";

export function OutbreakCreateScreen() {
  const { api, kn } = useAuth();
  const oapi = useMemo(() => (api ? createOutbreakApi(api) : null), [api]);
  const wapi = useMemo(() => (api ? createOutbreakWordApi(api) : null), [api]);
  const communes = (kn.context?.communes ?? []).map((c) => c.code);
  const communeName = useCommuneNames(communes);
  const catalog = (name: "cnts_disease" | "cnts_species") => async () =>
    (await unwrap(api!.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: name }, query: {} } }))).items as CatalogItem[];
  const diseases = useLoad(api ? catalog("cnts_disease") : null, [api]);
  const species = useLoad(api ? catalog("cnts_species") : null, [api]);
  const [sent, setSent] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  const act = useAction();
  const canCreate = kn.context?.role === "kn_xa" && !kn.context?.read_only;

  if (!oapi || !wapi) return null;
  const exportWord = async (ref: string) => {
    const r = await act.run(async () => {
      const run = await wapi.export(ref);
      const blob = await wapi.download(ref, run.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = run.file_name;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      return run;
    });
    if (r) act.setNote(`Đã xuất Word BM-10 — số ${r.doc_number || "(chưa cấp)"}.`);
  };
  return (
    <div className="page">
      <BackLink href={LIST_ROUTE}>Danh sách báo cáo dịch bệnh</BackLink>
      <PageHeader title="Tạo báo cáo ổ dịch">
        <p className="mute">BM-10 — báo cáo ngay khi phát hiện. Trường có dấu * là bắt buộc.</p>
      </PageHeader>
      {!canCreate ? <p className="err">Chỉ cán bộ xã Phòng Chăn nuôi – Thủy sản lập báo cáo dịch bệnh.</p> : null}
      {sent ? (
        <section className="card sec" aria-label="Đã gửi báo cáo">
          <div className="alert alert-info" role="status">
            <span>
              Đã gửi báo cáo ổ dịch {outbreakCode(sent)}. Hệ thống đã tạo cảnh báo <b>Khẩn</b> cho Phòng Chăn nuôi – Thủy sản và đánh dấu ổ dịch trên bản đồ.
            </span>
          </div>
          {act.view}
          <div className="row">
            <button className="btn btn-primary" disabled={act.busy} onClick={() => void exportWord(sent)}>
              Xuất Word (BM-10)
            </button>
            <Link className="btn" href={MAP_ROUTE}>
              Xem trên bản đồ
            </Link>
            <Link className="btn" href={LIST_ROUTE}>
              Về danh sách
            </Link>
            <button
              className="btn"
              onClick={() => {
                setSent(null);
                setNonce((n) => n + 1);
              }}
            >
              Lập báo cáo khác
            </button>
          </div>
        </section>
      ) : canCreate ? (
        <OutbreakEditor
          key={`${CREATE_ROUTE}:${nonce}`}
          mode={{ kind: "new" }}
          oapi={oapi}
          communes={communes}
          communeName={communeName}
          diseases={diseases.data ?? []}
          species={species.data ?? []}
          onDone={(ref) => {
            if (ref) setSent(ref);
            else window.history.back();
          }}
        />
      ) : null}
    </div>
  );
}
