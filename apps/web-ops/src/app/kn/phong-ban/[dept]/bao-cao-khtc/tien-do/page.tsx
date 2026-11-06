import { PNProgressScreen } from "@/kn/khtc/pn-progress/PNProgressScreen";

export default async function Page({ params }: { params: Promise<{ dept: string }> }) {
  const { dept } = await params;
  return <PNProgressScreen department={dept} />;
}
