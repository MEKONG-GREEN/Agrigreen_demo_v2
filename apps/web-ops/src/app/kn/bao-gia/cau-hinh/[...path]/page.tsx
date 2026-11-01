import { notFound } from "next/navigation";
import { PriceConfigDetailScreen } from "@/kn/price/CalendarDetail";
import { parseConfigRoute } from "@/kn/price/calendar-routes";

export default async function Page({
  params,
}: {
  params: Promise<{ path: string[] }>;
}) {
  const { path } = await params;
  const route = parseConfigRoute(path);
  if (!route) notFound();
  return <PriceConfigDetailScreen route={route} />;
}
