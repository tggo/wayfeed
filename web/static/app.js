// Registers the service worker (offline copy of the last trip page on this device).
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(function () {});
}

// Forms that cannot be undone ask first, in the page and in the words of what they do (no browser dialog: it blocks
// assistive tools and looks foreign). The first press shows the question; "Yes" submits, "No, keep it" puts it away.
document.addEventListener('submit', function (e) {
  var f = e.target;
  var m = f && f.getAttribute && f.getAttribute('data-confirm');
  if (!m || f.getAttribute('data-confirmed') === '1') { return; }
  e.preventDefault();
  if (f.querySelector('.confirm')) { return; }
  var btn = f.querySelector('button[type=submit]');
  var box = document.createElement('div');
  box.className = 'confirm';
  box.setAttribute('role', 'alertdialog');
  var q = document.createElement('p');
  q.textContent = m;
  var yes = document.createElement('button');
  yes.type = 'button'; yes.className = 'btn danger'; yes.textContent = 'Yes, ' + (btn ? btn.textContent.replace(/ this trip$/i, '').toLowerCase() : 'continue');
  var no = document.createElement('button');
  no.type = 'button'; no.className = 'btn'; no.textContent = 'No, keep it';
  box.appendChild(q); box.appendChild(yes); box.appendChild(document.createTextNode(' ')); box.appendChild(no);
  f.appendChild(box);
  if (btn) { btn.hidden = true; }
  yes.focus();
  yes.addEventListener('click', function () { f.setAttribute('data-confirmed', '1'); f.submit(); });
  no.addEventListener('click', function () { box.remove(); if (btn) { btn.hidden = false; btn.focus(); } });
});
// Keep the open stop's tab in view when there are many stops.
var cur = document.querySelector('.tabs [aria-current]');
if (cur && cur.scrollIntoView) { cur.scrollIntoView({ inline: 'center', block: 'nearest' }); }
