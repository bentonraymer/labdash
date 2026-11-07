import { lazy, Suspense, type ReactNode } from "react";
import { createBrowserRouter, RouterProvider, Outlet } from "react-router";
import { ApiError } from "@/lib/api";
import { useLiveEvents, useMe, useSetupStatus } from "@/lib/queries";
import { UIProvider } from "@/components/UIProvider";
import { LaunchpadPage } from "@/pages/Launchpad";
import { LoginPage, SetupPage } from "@/pages/Auth";
import { AdminLayout, ComingSoon } from "@/pages/admin/AdminLayout";
import { ServicesPage } from "@/pages/admin/ServicesPage";
import { SettingsPage } from "@/pages/admin/SettingsPage";
import { StatusPage } from "@/pages/admin/status/StatusPage";
import { HostsPage } from "@/pages/admin/hosts/HostsPage";
import { IPAddressesPage } from "@/pages/admin/network/IPAddressesPage";
import { IPDetailPage } from "@/pages/admin/network/IPDetailPage";
import { DevicesPage } from "@/pages/admin/network/DevicesPage";

// The docs editor (CodeMirror + markdown) is loaded on demand.
const DocsPage = lazy(() => import("@/pages/admin/docs/DocsPage").then((m) => ({ default: m.DocsPage })));
const TopologyPage = lazy(() => import("@/pages/admin/TopologyPage").then((m) => ({ default: m.TopologyPage })));
const Lazy = ({ children }: { children: ReactNode }) => <Suspense fallback={<div className="p-8 text-sm text-muted">Loading…</div>}>{children}</Suspense>;

function AuthGate() {
  const setup = useSetupStatus();
  const me = useMe();
  useLiveEvents(!!me.data);

  if (setup.isLoading || me.isLoading) return <div className="h-full bg-bg" />;
  if (setup.data?.needsSetup) return <SetupPage />;
  if (me.error instanceof ApiError && me.error.status === 401) return <LoginPage />;
  if (me.error) return <div className="p-8 text-down">Can't reach the LabDash server: {String(me.error.message)}</div>;
  return (
    <UIProvider>
      <Outlet />
    </UIProvider>
  );
}

const router = createBrowserRouter([
  {
    element: <AuthGate />,
    children: [
      { path: "/", element: <LaunchpadPage /> },
      {
        element: <AdminLayout />,
        children: [
          { path: "/services", element: <ServicesPage /> },
          { path: "/status", element: <StatusPage /> },
          { path: "/hosts", element: <HostsPage /> },
          { path: "/network/ips", element: <IPAddressesPage /> },
          { path: "/network/ips/:ip", element: <IPDetailPage /> },
          { path: "/network/devices", element: <DevicesPage /> },
          { path: "/docs", element: <Lazy><DocsPage /></Lazy> },
          { path: "/docs/:id", element: <Lazy><DocsPage /></Lazy> },
          { path: "/topology", element: <Lazy><TopologyPage /></Lazy> },
          { path: "/settings", element: <SettingsPage /> },
          {
            path: "*",
            element: (
              <ComingSoon title="Not found" phase={0}>
                That page doesn't exist.
              </ComingSoon>
            ),
          },
        ],
      },
    ],
  },
]);

export function App() {
  return <RouterProvider router={router} />;
}
