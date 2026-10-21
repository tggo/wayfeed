// Registers the service worker (offline copy of the last trip page on this device).
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(function () {});
}
