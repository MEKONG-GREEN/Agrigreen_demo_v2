import { Suspense } from "react";
import { Loading } from "@/components/ui";
import { KhtcCatalogDetailScreen } from "@/kn/khtc/CatalogItemScreens";

export default function Page() {
  return <Suspense fallback={<Loading />}><KhtcCatalogDetailScreen /></Suspense>;
}
