import { createBrowserRouter, type RouteObject } from "react-router";

import { RouteError, RouteLoading } from "@/app/RouteError";
import { AppLayout } from "@/components/layout/AppLayout";
import { HomePage } from "@/pages/HomePage";

// Home ships in the main bundle; other pages load on first visit.
export const routeObjects: RouteObject[] = [
  {
    path: "/",
    element: <AppLayout />,
    errorElement: <RouteError />,
    hydrateFallbackElement: <RouteLoading />,
    children: [
      { index: true, element: <HomePage /> },
      { path: "search", lazy: () => import("@/pages/SearchPage").then((m) => ({ Component: m.SearchPage })) },
      { path: "host/:hostname", lazy: () => import("@/pages/HostPage").then((m) => ({ Component: m.HostPage })) },
      { path: "subdomain/:root_domain", lazy: () => import("@/pages/SubdomainPage").then((m) => ({ Component: m.SubdomainPage })) },
      { path: "http/:id", lazy: () => import("@/pages/ArtifactDetailPage").then((m) => ({ Component: m.ArtifactDetailPage })) },
      { path: "*", lazy: () => import("@/pages/NotFoundPage").then((m) => ({ Component: m.NotFoundPage })) },
    ],
  },
];

export const router = createBrowserRouter(routeObjects);
