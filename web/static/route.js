// The route page: the line of stops is the navigation, the map shows the stories that have a point, and the list is the stories of each stop.
// Choosing a station flies the map to the stop and scrolls the list to it; choosing a marker scrolls to its story. The chosen stop and story
// are in the address (#stop-1, #stop-1/<story>), so the page can be shared and the back button works. Without this script the stations are
// plain links to the stops in the list.
(function () {
  'use strict';
  var dataEl = document.getElementById('rdata');
  var rp = document.getElementById('rp');
  if (!dataEl || !rp) { return; }
  var data = {};
  try { data = JSON.parse(dataEl.textContent); } catch (e) { return; }
  rp.classList.add('js');
  var line = document.getElementById('line');
  var ol = line.querySelector('.stations');
  var stations = [].slice.call(line.querySelectorAll('.stn'));
  var sections = [].slice.call(document.querySelectorAll('.stopsec'));
  var wrap = document.getElementById('rmapwrap');
  var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var wide = window.matchMedia('(min-width:900px)');
  var cur = data.cur || 0;
  var lock = 0;
  var map = null, cluster = null, circles = [], bounds = [], markers = {}, meta = {};
  var selKey = '';

  function measure() {
    var lh = line.offsetHeight;
    rp.style.setProperty('--lineH', lh + 'px');
    rp.style.setProperty('--stickyH', (wide.matches || !wrap || wrap.hidden ? lh + (wide.matches ? 10 : 0) : lh + wrap.offsetHeight) + 'px');
    if (map) { map.invalidateSize(); }
  }

  function setHash(h, replace) {
    var u = location.pathname + location.search + '#' + h;
    try { (replace ? history.replaceState : history.pushState).call(history, null, '', u); } catch (e) { /* the address is a convenience */ }
  }

  function setCurrent(i) {
    cur = i;
    stations.forEach(function (s, j) {
      if (j === i) { s.setAttribute('aria-current', 'location'); } else { s.removeAttribute('aria-current'); }
    });
    var li = stations[i] && stations[i].parentNode;
    if (li && ol.scrollTo) {
      ol.scrollTo({ left: li.offsetLeft - (ol.clientWidth - li.offsetWidth) / 2, behavior: reduce ? 'auto' : 'smooth' });
    }
    circles.forEach(function (c, j) {
      if (c) { c.setStyle(j === i ? { weight: 2.5, fillOpacity: 0.18 } : { weight: 1.25, fillOpacity: 0.06 }); }
    });
  }

  function flyToStop(i, instant) {
    var b = bounds[i];
    if (!map || !b) { return; }
    if (instant || reduce) { map.fitBounds(b, { padding: [16, 16], animate: false }); } else { map.flyToBounds(b, { padding: [16, 16], duration: 1.1 }); }
  }

  function selectStop(i, o) {
    o = o || {};
    clearSel();
    setCurrent(i);
    if (o.fly !== false) { flyToStop(i, o.instant); }
    if (o.scroll !== false && sections[i]) {
      lock = Date.now() + 1000;
      sections[i].scrollIntoView({ behavior: reduce || o.instant ? 'auto' : 'smooth', block: 'start' });
    }
    if (o.push) { setHash('stop-' + i); }
  }

  function clearSel() {
    var old = document.querySelector('.story.sel');
    if (old) { old.classList.remove('sel'); }
    if (selKey && markers[selKey]) { mark(selKey, 'mk-sel', false); }
    selKey = '';
  }

  function iconEl(key) {
    var m = markers[key];
    if (!m || !cluster) { return null; }
    var p = cluster.getVisibleParent(m);
    return p && p._icon ? p._icon : null;
  }
  function mark(key, cls, on) {
    var e = iconEl(key);
    if (e) { e.classList[on ? 'add' : 'remove'](cls); }
  }

  function selectItem(key, o) {
    o = o || {};
    var m = meta[key];
    if (!m || !sections[m.s]) { return; }
    var li = sections[m.s].querySelector('[data-item="' + key + '"]');
    if (!li) { return; }
    clearSel();
    var det = li.closest('details');
    if (det) { det.open = true; }
    li.classList.add('sel');
    selKey = key;
    setCurrent(m.s);
    var show = function () { mark(key, 'mk-sel', true); };
    if (o.pan && markers[key] && cluster) {
      cluster.zoomToShowLayer(markers[key], function () { map.panTo(markers[key].getLatLng(), { animate: !reduce }); show(); });
    } else { show(); }
    if (o.scroll !== false) {
      lock = Date.now() + 1000;
      li.scrollIntoView({ behavior: reduce || o.instant ? 'auto' : 'smooth', block: 'start' });
      li.focus({ preventScroll: true });
    }
    if (o.push) { setHash('stop-' + m.s + '/' + key); }
  }

  function applyHash(instant, push) {
    var h = /^#stop-(\d+)(?:\/([\w-]+))?$/.exec(location.hash);
    if (!h || !sections[+h[1]]) { return false; }
    var i = +h[1];
    selectStop(i, { instant: instant, scroll: !h[2] });
    if (h[2] && meta[h[2]]) { selectItem(h[2], { instant: instant }); }
    return true;
  }

  stations.forEach(function (a, i) {
    a.addEventListener('click', function (e) {
      if (e.metaKey || e.ctrlKey || e.shiftKey) { return; }
      e.preventDefault();
      selectStop(i, { push: true });
    });
  });

  // list: a story lights its marker; clicking the story (not its link) shows it on the map
  var listEl = document.getElementById('rlist');
  function keyOf(e) {
    var li = e.target.closest && e.target.closest('.story[data-item]');
    return li ? li.getAttribute('data-item') : '';
  }
  ['mouseover', 'focusin'].forEach(function (t) { listEl.addEventListener(t, function (e) { var k = keyOf(e); if (k) { mark(k, 'mk-hl', true); } }); });
  ['mouseout', 'focusout'].forEach(function (t) { listEl.addEventListener(t, function (e) { var k = keyOf(e); if (k) { mark(k, 'mk-hl', false); } }); });
  listEl.addEventListener('click', function (e) {
    if (e.target.closest('a, summary, button')) { return; }
    var k = keyOf(e);
    if (k && meta[k]) { selectItem(k, { pan: true, scroll: false, push: true }); }
  });

  // the stop in view is the current station (the map does not move while reading)
  var ticking = false;
  function spy() {
    ticking = false;
    if (Date.now() < lock) { return; }
    var probe = (parseFloat(getComputedStyle(rp).getPropertyValue('--stickyH')) || 100) + 48;
    var at = 0;
    sections.forEach(function (s, i) { if (s.getBoundingClientRect().top <= probe) { at = i; } });
    if (at !== cur) { setCurrent(at); setHash('stop-' + at, true); }
  }
  window.addEventListener('scroll', function () { if (!ticking) { ticking = true; requestAnimationFrame(spy); } }, { passive: true });
  window.addEventListener('popstate', function () { applyHash(false); });
  window.addEventListener('resize', measure);
  window.addEventListener('load', measure);
  if (document.fonts && document.fonts.ready) { document.fonts.ready.then(measure); }
  if (window.ResizeObserver) { new ResizeObserver(measure).observe(line); }

  // map
  var hasPoints = (data.stops || []).length > 0;
  if (!window.L || !wrap || !hasPoints) {
    if (wrap) { wrap.hidden = true; }
    rp.classList.add('nomap');
    measure();
    applyHash(true);
    return;
  }
  measure();
  map = L.map('rmap', { scrollWheelZoom: false, worldCopyJump: false });
  L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 18, attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors' }).addTo(map);
  var el = document.getElementById('rmap');
  map.on('click', function () { map.scrollWheelZoom.enable(); });
  el.addEventListener('mouseleave', function () { map.scrollWheelZoom.disable(); });

  var route = [];
  data.stops.forEach(function (s) {
    var p = [s.lat, s.lon];
    route[s.i] = p;
    bounds[s.i] = L.latLng(p).toBounds(s.r * 2000);
    circles[s.i] = L.circle(p, { radius: s.r * 1000, color: '#2F5BEA', weight: 1.25, fillColor: '#2F5BEA', fillOpacity: 0.06, interactive: false }).addTo(map);
  });
  var pts = route.filter(Boolean);
  if (pts.length > 1) { L.polyline(pts, { color: '#2F5BEA', weight: 3.5, opacity: 0.9, lineCap: 'round', interactive: false }).addTo(map); }
  data.stops.forEach(function (s) {
    var m = L.marker([s.lat, s.lon], {
      icon: L.divIcon({ className: 'mkw', html: '<div class="stm">' + (s.i + 1) + '</div>', iconSize: [34, 34], iconAnchor: [17, 17] }),
      title: s.n, alt: s.n, keyboard: false, zIndexOffset: -500
    }).addTo(map);
    m.on('click', function () { selectStop(s.i, { push: true }); });
  });

  cluster = L.markerClusterGroup({
    maxClusterRadius: 34, showCoverageOnHover: false, spiderfyOnMaxZoom: true, zoomToBoundsOnClick: true,
    iconCreateFunction: function (c) { return L.divIcon({ className: 'mkw', html: '<div class="mcl">' + c.getChildCount() + '</div>', iconSize: [34, 34], iconAnchor: [17, 17] }); }
  });
  (data.items || []).forEach(function (it) {
    meta[it.id] = it;
    var cls = 'mkw mk-' + it.b + ' mk-s' + (it.v || 0) + (it.o ? ' mk-off' : '') + (it.far ? ' mk-far' : '');
    var m = L.marker([it.lat, it.lon], {
      icon: L.divIcon({ className: cls, html: '<span class="mk"><i></i></span>', iconSize: [30, 30], iconAnchor: [15, 15] }),
      title: it.t + (it.w ? ' (' + it.w + ')' : ''), alt: '', keyboard: false, zIndexOffset: 1000
    });
    m.on('click', function () { selectItem(it.id, { push: true }); });
    markers[it.id] = m;
    cluster.addLayer(m);
  });
  map.addLayer(cluster);

  var all = L.latLngBounds([]);
  bounds.forEach(function (b) { if (b) { all.extend(b); } });
  map.fitBounds(all, { padding: [16, 16], animate: false });
  setCurrent(cur);
  if (!applyHash(true) && cur > 0) { selectStop(cur, { instant: true }); }

  // phone: the bar under the map sets its height (drag it, or use the arrow keys)
  var grip = document.getElementById('grip');
  function setMapH(vh) {
    vh = Math.max(20, Math.min(70, Math.round(vh)));
    rp.style.setProperty('--mapH', vh + 'vh');
    grip.setAttribute('aria-valuenow', String(vh));
    measure();
  }
  var dragging = false;
  grip.addEventListener('pointerdown', function (e) { dragging = true; grip.setPointerCapture(e.pointerId); e.preventDefault(); });
  grip.addEventListener('pointermove', function (e) {
    if (!dragging) { return; }
    setMapH(((e.clientY - line.offsetHeight + 12) / window.innerHeight) * 100);
  });
  ['pointerup', 'pointercancel'].forEach(function (t) { grip.addEventListener(t, function () { dragging = false; }); });
  grip.addEventListener('keydown', function (e) {
    var v = +grip.getAttribute('aria-valuenow');
    var d = { ArrowUp: -6, ArrowDown: 6 }[e.key];
    if (d) { e.preventDefault(); setMapH(v + d); }
    if (e.key === 'Home') { e.preventDefault(); setMapH(20); }
    if (e.key === 'End') { e.preventDefault(); setMapH(70); }
  });
  wide.addEventListener && wide.addEventListener('change', measure);
})();
