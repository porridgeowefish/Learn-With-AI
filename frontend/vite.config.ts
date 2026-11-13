/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// Vite config for the LLL frontend.
// In dev, Vite serves on :5173 and proxies /api, /files, /events to the Go
// backend on :8787. In production, `npm run build` emits a static bundle
// under dist/ that the Go binary serves via http.FileServer plus an SPA
// fallback (see backend-go/internal/transport/httpserver/router.go).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      '/api': {
        target: 'http://localhost:8787',
        changeOrigin: false,
      },
      '/files': {
        target: 'http://localhost:8787',
        changeOrigin: false,
      },
      '/events': {
        target: 'http://localhost:8787',
        changeOrigin: false,
        // SSE needs ws:false + long-lived; Vite proxy supports streaming.
        ws: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    rollupOptions: {
      input: {
        app: path.resolve(__dirname, 'index.html'),
        htmlPreview: path.resolve(__dirname, 'html-preview.html'),
      },
    },
    sourcemap: true,
    chunkSizeWarningLimit: 1024,
  },
  test: {
    globals: true,
    // Windows parallel workers can collide on temporary fixture cleanup.
    ...(process.platform === 'win32' ? { fileParallelism: false, maxWorkers: 2 } : {}),
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    css: false,
  },
});
