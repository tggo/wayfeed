// After sign-out: forget the pages this device kept for offline reading.
if (window.caches) {
  caches.keys().then(function (ks) { ks.forEach(function (k) { caches.delete(k); }); });
}
