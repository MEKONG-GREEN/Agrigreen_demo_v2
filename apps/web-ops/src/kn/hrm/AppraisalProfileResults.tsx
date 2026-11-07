"use client";
import { DataTable } from "@/components/patterns/list";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { useAppraisalApi } from "./appraisal-ui";
import { APPRAISAL_RATING_LABEL } from "./appraisal-form";
import { vnDate } from "./staff-api";
export function AppraisalProfileResults({staff}:{staff:string}) {
  const api=useAppraisalApi(),results=useLoad(()=>api.results(staff),[staff]);
  return <section><h2>Kết quả đánh giá</h2><p>Kết luận quý, năm đã chốt được lưu trong hồ sơ.</p><LoadState s={results}>{v=><DataTable caption="Kết quả xếp loại trong hồ sơ" rows={v.items} rowKey={x=>x.entry_version} empty="Chưa có kết quả đã chốt." columns={[
    {key:"period",header:"Kỳ",cell:x=>x.label},{key:"rating",header:"Kết quả chính thức",cell:x=>APPRAISAL_RATING_LABEL[x.rating]},{key:"at",header:"Ngày ghi hồ sơ",cell:x=>vnDate(x.recorded_at)}
  ]}/>}</LoadState></section>;
}
