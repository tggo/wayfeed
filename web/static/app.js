// Registers the service worker (offline copy of the last trip page on this device).
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(function () {});
}

// Forms that cannot be undone ask first, in the words of what they do.
document.addEventListener('submit', function (e) {
  var m = e.target && e.target.getAttribute && e.target.getAttribute('data-confirm');
  if (m && !window.confirm(m)) { e.preventDefault(); }
});
// Keep the open stop's tab in view when there are many stops.
var cur = document.querySelector('.tabs [aria-current]');
if (cur && cur.scrollIntoView) { cur.scrollIntoView({ inline: 'center', block: 'nearest' }); }
