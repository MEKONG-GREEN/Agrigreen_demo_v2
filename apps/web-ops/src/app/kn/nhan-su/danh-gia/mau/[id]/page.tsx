import { notFound } from "next/navigation";
import { AppraisalTemplateEditorScreen } from "@/kn/hrm/AppraisalTemplateScreen";
export default async function Page({params}:{params:Promise<{id:string}>}){const{id}=await params;if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id))notFound();return <AppraisalTemplateEditorScreen id={id}/>;}
