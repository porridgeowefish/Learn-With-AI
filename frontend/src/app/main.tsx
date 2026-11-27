import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { App } from './App';
import 'katex/dist/katex.min.css';
import '@/shared/styles/tokens.css';
import '@/shared/styles/globals.css';
import '@/shared/lib/fontSize';

// Single QueryClient for the app. Default options keep data fresh enough
// for a local-tool UX without hammering the backend.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000, // 30s — most data is project files on local disk
      gcTime: 5 * 60_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: 0,
    },
  },
});

const container = document.getElementById('root');
if (!container) {
  throw new Error('Learn-With-AI: #root element not found in index.html');
}

async function mount() {
  if (import.meta.env.DEV &&
    !document.querySelector('meta[name="lll-design-revision"]') && navigator.serviceWorker) {
    const worker = await navigator.serviceWorker.getRegistration().catch(() => undefined);
    if (worker?.active && new URL(worker.active.scriptURL).pathname === '/design-worker.mjs') {
      if (await worker.unregister() && navigator.serviceWorker.controller) {
        window.location.reload();
        return;
      }
    }
  }
  createRoot(container!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </QueryClientProvider>
    </StrictMode>,
  );
}

void mount();
