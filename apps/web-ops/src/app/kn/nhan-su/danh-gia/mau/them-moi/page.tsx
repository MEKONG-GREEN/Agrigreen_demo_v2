import { AppraisalTemplateEditorScreen } from "@/kn/hrm/AppraisalTemplateScreen";
export default async function Page({searchParams}:{searchParams:Promise<Record<string,string|string[]|undefined>>}){const q=await searchParams;return <AppraisalTemplateEditorScreen kind={q.kind==="summary"?"summary":"form"} summaryKind={q.summary==="MS06"?"MS06":"MS05"}/>;}
