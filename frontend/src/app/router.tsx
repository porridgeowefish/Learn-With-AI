import { lazy, Suspense } from 'react';
import { Navigate, RouteObject, useRoutes } from 'react-router-dom';

import { AppShell } from '@/app/layout/AppShell';

// Routes are lazy-loaded so each page lands in its own chunk. AppShell
// wraps every page; it owns the topbar, sidebar, SSE connection, and
// the terminal drawer.
const HomePage = lazy(() => import('@/features/projects/HomePage').then((m) => ({ default: m.HomePage })));
const ProjectPage = lazy(() => import('@/features/projects/ProjectPage').then((m) => ({ default: m.ProjectPage })));
const UsagePage = lazy(() => import('@/features/usage/UsagePage').then((m) => ({ default: m.UsagePage })));
const PreferencesPage = lazy(() => import('@/features/preferences/PreferencesPage').then((m) => ({ default: m.PreferencesPage })));
const SettingsPage = lazy(() => import('@/features/settings/SettingsPage').then((m) => ({ default: m.SettingsPage })));


const CollectionPage=lazy(()=>import('@/features/treasures').then(m=>({default:m.CollectionPage})));
const TreasureDisplay=lazy(()=>import('@/features/treasures').then(m=>({default:m.TreasureDisplay})));
const TreasureFAQ=lazy(()=>import('@/features/treasures').then(m=>({default:m.TreasureFAQ})));

function PageFallback() {
  return <div style={{ padding: 24, color: 'var(--muted)' }}>加载中…</div>;
}

function withShell(el: React.ReactNode) {
  return (
    <Suspense fallback={<PageFallback />}>{el}</Suspense>
  );
}

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppShell />,
    children: [
      {
        index: true,
        element: withShell(<HomePage />),
      },
      {
        path: 'project/:id/:zone?',
        element: withShell(<ProjectPage />),
      },
      {path:'collection',element:withShell(<CollectionPage/>)},
      {path:'collection/faq',element:withShell(<TreasureFAQ/>)},
      {path:'collection/:itemId',element:withShell(<TreasureDisplay/>)},
      { path: 'usage', element: withShell(<UsagePage />) },
      {
        path: 'preferences',
        element: withShell(<PreferencesPage />),
      },
      {
        path: 'memory',
        element: <Navigate to="/preferences" replace />,
      },
      {
        path: 'settings',
        element: withShell(<SettingsPage />),
      },
      {
        path: 'models',
        element: <Navigate to="/settings" replace />,
      },
      {
        path: '*',
        element: <Navigate to="/" replace />,
      },
    ],
  },
];

// AppRoutes — helper component so App.tsx stays boring. useRoutes needs
// to be called from inside a <BrowserRouter>, which is mounted in main.
export function AppRoutes() {
  return useRoutes(routes);
}
