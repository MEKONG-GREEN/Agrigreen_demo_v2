import { notFound } from "next/navigation";
import { AppraisalTemplateEditorScreen } from "@/kn/hrm/AppraisalTemplateScreen";
export default async function Page({searchParams}:{searchParams:Promise<Record<string,string|string[]|undefined>>}) {
  const q=await searchParams;
  if(q.kind && q.kind!=="summary")notFound();
  return <AppraisalTemplateEditorScreen kind="summary" summaryKind={q.summary==="MS06"?"MS06":"MS05"}/>;
}
