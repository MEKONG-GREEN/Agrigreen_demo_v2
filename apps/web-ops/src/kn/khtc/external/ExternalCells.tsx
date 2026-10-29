"use client";
// W11.67 — ô "Nhập ngoài" trên màn soạn báo cáo (BR-DL-19): khóa như ô số, nền tím; "Nguồn dữ liệu" hiện cơ quan, văn bản
// hoặc kênh, ngày, tệp minh chứng; sửa tại Dữ liệu › Số liệu nguồn ngoài.
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { useAction } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { createExternalApi, fmtVn, sourceText, type ReportExternal } from "./api";

export function ExternalCells({ items }: { items: ReportExternal[] | undefined }) {
    const { api } = useAuth();
    const ext = useMemo(() => (api ? createExternalApi(api) : null), [api]);
    const [open, setOpen] = useState<string | null>(null);
    const act = useAction();
    if (!items?.length)
        return null;
    return <section aria-label="Số liệu nguồn ngoài">
  <h3>Số liệu nguồn ngoài (Nhập ngoài)</h3>
  <p>Chỉ đọc · Sửa tại <Link href="/kn/khtc/so-lieu-ngoai">Số liệu nguồn ngoài</Link> đến khi kỳ đóng, rồi bấm “Cập nhật số liệu”.</p>
  {act.view}
  <div className="table-wrap"><table><thead><tr>{["Chỉ tiêu", "Giá trị", "ĐVT", ""].map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>{items.map(x => <tr key={x.entry_id}>
   <td>{x.name}</td>
   <td className="num c-violet" title="Nhập ngoài — khóa trên bản soạn" aria-readonly="true"><b>{fmtVn(x.value)}</b></td>
   <td>{x.unit}</td>
   <td><button className="btn btn-sm btn-o" aria-expanded={open === x.entry_id} onClick={() => setOpen(open === x.entry_id ? null : x.entry_id)}>Nguồn dữ liệu</button>
    {open === x.entry_id && <dl className="grid2">
     <dt>Nguồn</dt><dd>{sourceText(x)}</dd>
     <dt>Cách tính</dt><dd>Nhập tại Số liệu nguồn ngoài (FN-TI-02.11), phiên bản {x.version}</dd>
     <dt>Minh chứng</dt><dd>{x.evidence.length ? x.evidence.map(ev => <button key={ev.id} className="btn btn-sm btn-o" onClick={() => ext && act.run(async () => { const f = await ext.evidence(ev.id, ev.file_name); saveBlob(f.blob, f.name); })}>{ev.file_name}</button>) : "—"}</dd>
    </dl>}
   </td>
  </tr>)}</tbody></table></div>
 </section>;
}
