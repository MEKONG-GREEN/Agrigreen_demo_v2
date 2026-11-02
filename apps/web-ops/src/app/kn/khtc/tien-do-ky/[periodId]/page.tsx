import { redirect } from "next/navigation";
export default async function Page({ params }: { params: Promise<{ periodId: string }> }) {
  const { periodId } = await params;
  redirect(`/kn/khtc/ky-bao-cao/ky/${encodeURIComponent(periodId)}`);
}
