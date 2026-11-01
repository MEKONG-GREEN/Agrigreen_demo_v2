import {KhtcPeriodsScreen} from "@/kn/khtc/periods/KhtcPeriodsScreen";
export default async function Page({params}:{params:Promise<{periodId:string}>}){const {periodId}=await params;return <KhtcPeriodsScreen initialPeriod={periodId}/>;}
