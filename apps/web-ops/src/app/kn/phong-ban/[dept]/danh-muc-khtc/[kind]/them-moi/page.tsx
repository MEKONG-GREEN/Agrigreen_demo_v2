import { Suspense } from "react";
import { Loading } from "@/components/ui";
import { KhtcCatalogCreateScreen } from "@/kn/khtc/CatalogItemScreens";

export default function Page() {
  return <Suspense fallback={<Loading />}><KhtcCatalogCreateScreen /></Suspense>;
}
