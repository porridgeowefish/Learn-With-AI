import { dispatch } from '/__design/runtime.mjs';
self.addEventListener('install', event => event.waitUntil(self.skipWaiting()));
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (url.origin === self.location.origin && /^\/(api|files|events)(\/|$)/.test(url.pathname)) {
    event.respondWith(dispatch(event.request, url).catch(error => {
      console.error('Design request failed', error);
      return Response.json({error: String(error.message || error)}, {status:500});
    }));
  }
});
