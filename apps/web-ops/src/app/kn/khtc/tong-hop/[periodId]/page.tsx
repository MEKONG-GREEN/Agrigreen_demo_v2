import {KhtcReportsScreen} from "@/kn/khtc/reports/KhtcReportsScreen";
export default async function Page({params}:{params:Promise<{periodId:string}>}){const {periodId}=await params;return <KhtcReportsScreen mode="progress" initialPeriod={periodId}/>;}
