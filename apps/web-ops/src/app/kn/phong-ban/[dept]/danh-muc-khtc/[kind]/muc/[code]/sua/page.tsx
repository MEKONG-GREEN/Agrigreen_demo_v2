import { Suspense } from "react";
import { Loading } from "@/components/ui";
import { KhtcCatalogEditScreen } from "@/kn/khtc/CatalogItemScreens";

export default function Page() {
  return <Suspense fallback={<Loading />}><KhtcCatalogEditScreen /></Suspense>;
}
