// Web Push on the trip page: subscribe this browser, tell feedql (through Wayfeed's own /push/* routes), show the state.
(function () {
  'use strict';
  var box = document.getElementById('pushbox');
  if (!box) { return; }
  var $ = function (id) { return document.getElementById(id); };
  var text = $('pushtext'), on = $('pushon'), off = $('pushoff'), test = $('pushtest'), err = $('pusherr');
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    box.hidden = false;
    text.textContent = 'This browser cannot receive push alerts. On an iPhone, add Wayfeed to the Home Screen first, then open it from there.';
    return;
  }
  function b64(s) {
    var p = '='.repeat((4 - s.length % 4) % 4), r = (s + p).replace(/-/g, '+').replace(/_/g, '/'), raw = atob(r), a = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) { a[i] = raw.charCodeAt(i); }
    return a;
  }
  function show(msg) { err.textContent = msg || ''; err.hidden = !msg; }
  function post(path, body) {
    return fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), credentials: 'same-origin' })
      .then(function (r) { return r.json().then(function (j) { if (!r.ok) { throw new Error(j.error || 'feedql could not be reached.'); } return j; }); });
  }
  function state(reg) {
    return reg.pushManager.getSubscription().then(function (sub) {
      var q = sub ? '?endpoint=' + encodeURIComponent(sub.endpoint) : '';
      return fetch('/push/state' + q, { credentials: 'same-origin' }).then(function (r) { return r.json(); }).then(function (st) { return { sub: sub, st: st }; });
    });
  }
  function paint(reg) {
    return state(reg).then(function (x) {
      box.hidden = false;
      var perm = Notification.permission;
      on.hidden = off.hidden = test.hidden = true;
      if (perm === 'denied') { text.textContent = 'Notifications are blocked for this site in your browser settings. Allow them there to get alerts here.'; return; }
      if (x.sub && x.st.subscribed) {
        text.textContent = 'Trip alerts are on for this device.';
        off.hidden = false; test.hidden = false;
      } else {
        text.textContent = 'Trip alerts are off on this device.';
        on.hidden = false;
        if (!x.st.public_key) { on.disabled = true; text.textContent = 'Push alerts are not available from feedql right now.'; }
      }
      window.wfPush = x;
    }).catch(function () { box.hidden = false; text.textContent = 'Could not check the alert settings. Reload to try again.'; });
  }
  navigator.serviceWorker.ready.then(function (reg) {
    paint(reg);
    on.addEventListener('click', function () {
      show('');
      on.disabled = true;
      Notification.requestPermission().then(function (perm) {
        if (perm !== 'granted') { throw new Error('Permission was not given, so alerts stay off.'); }
        return state(reg);
      }).then(function (x) {
        return reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64(x.st.public_key) });
      }).then(function (sub) {
        return post('/push/subscribe', sub.toJSON());
      }).then(function () { return paint(reg); })
        .catch(function (e) { show(e.message || 'Alerts could not be turned on.'); })
        .then(function () { on.disabled = false; });
    });
    off.addEventListener('click', function () {
      show('');
      off.disabled = true;
      reg.pushManager.getSubscription().then(function (sub) {
        if (!sub) { return null; }
        return post('/push/unsubscribe', { endpoint: sub.endpoint }).then(function () { return sub.unsubscribe(); });
      }).then(function () { return paint(reg); })
        .catch(function (e) { show(e.message || 'Alerts could not be turned off.'); })
        .then(function () { off.disabled = false; });
    });
    test.addEventListener('click', function () {
      reg.showNotification('Wayfeed test', { body: 'This is how a trip alert will look on this device.', icon: '/static/icon.svg', tag: 'wf-test', data: { url: '/trip' } });
    });
  });
})();
