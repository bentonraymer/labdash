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
          {
            path: "/network/devices",
            element: (
              <ComingSoon title="Network devices" phase={5}>
                Scheduled nmap scans of your subnets with MAC vendor, OS detection, favicons for web ports, and Pushover alerts for new devices.
              </ComingSoon>
            ),
          },
          {
            path: "/docs",
            element: (
              <ComingSoon title="Docs" phase={6}>
                Markdown notes per service plus standalone notes, with autosave, version history, image paste, folders and tags.
              </ComingSoon>
            ),
          },
          {
            path: "/topology",
            element: (
              <ComingSoon title="Topology" phase={7}>
                An interactive map of node → VM/LXC → Docker → service, built from your hosting data.
              </ComingSoon>
            ),
          },
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
