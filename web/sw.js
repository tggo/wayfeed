// Wayfeed service worker. Keeps the static files and the last trip page so a traveller with no signal still reads
// the last briefing. Trip pages are fetched network-first; nothing else about the account is stored.
const STATIC = 'wf-static-6';
const PAGES = 'wf-pages-6';
self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(STATIC).then((c) => c.addAll(['/static/style.css?v=5', '/static/app.js?v=4', '/static/icon.svg'])).then(() => self.skipWaiting()));
});
self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== STATIC && k !== PAGES).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});
self.addEventListener('fetch', (e) => {
  const r = e.request;
  const u = new URL(r.url);
  if (r.method !== 'GET' || u.origin !== location.origin) return;
  if (u.pathname.startsWith('/static/')) {
    e.respondWith(caches.match(r).then((hit) => hit || fetch(r)));
    return;
  }
  if (u.pathname === '/trip') {
    e.respondWith(fetch(r).then((res) => {
      if (res.ok) { const copy = res.clone(); caches.open(PAGES).then((c) => c.put('/trip', copy)); }
      return res;
    }).catch(() => caches.match('/trip').then((hit) => hit || new Response('Offline, and no trip page was saved on this device yet.', { status: 503, headers: { 'Content-Type': 'text/plain; charset=utf-8' } }))));
  }
});

// A trip alert from feedql: {title, body, url, tag}. It says what a source published; the page it opens has the link and the disclaimer.
self.addEventListener('push', (e) => {
  let d = {};
  try { d = e.data ? e.data.json() : {}; } catch (_) { d = { body: e.data ? e.data.text() : '' }; }
  e.waitUntil(self.registration.showNotification(d.title || 'Wayfeed', { body: d.body || '', icon: '/static/icon.svg', tag: d.tag || 'wf-alert', data: { url: d.url || '/trip' } }));
});
self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = (e.notification.data && e.notification.data.url) || '/trip';
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((cs) => {
    for (const c of cs) { if (new URL(c.url).origin === location.origin && 'focus' in c) { c.navigate(url); return c.focus(); } }
    return self.clients.openWindow(url);
  }));
});
