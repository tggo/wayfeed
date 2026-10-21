// The trip planner: stops are picked on an OpenStreetMap map (Leaflet), named by OpenStreetMap's Nominatim, and sent to
// Wayfeed's own /new/preview and /new/create, which call the public feedql API. Nothing here talks to feedql directly.
(function () {
  'use strict';
  var mapEl = document.getElementById('map');
  if (!mapEl || !window.L) { return; }
  var $ = function (id) { return document.getElementById(id); };
  var map = L.map(mapEl, { worldCopyJump: true }).setView([22, 40], 2);
  L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 18, attribution: '&copy; OpenStreetMap contributors' }).addTo(map);
  var stops = [];
  var layer = L.layerGroup().addTo(map);
  var previewed = false;
  var NOMINATIM = 'https://nominatim.openstreetmap.org';

  function iso(d) { return d.toISOString().slice(0, 10); }
  function addDays(s, n) { var d = new Date(s + 'T00:00:00Z'); d.setUTCDate(d.getUTCDate() + n); return iso(d); }
  function el(tag, attrs, text) {
    var e = document.createElement(tag);
    Object.keys(attrs || {}).forEach(function (k) { e.setAttribute(k, attrs[k]); });
    if (text != null) { e.textContent = text; }
    return e;
  }
  function showError(msg) { var e = $('err'); e.textContent = msg || ''; e.hidden = !msg; }
  function invalidate() {
    previewed = false;
    $('previewbox').hidden = true;
    $('create').hidden = true;
    $('preview').disabled = stops.length === 0;
  }

  function draw() {
    layer.clearLayers();
    var pts = [];
    stops.forEach(function (s, i) {
      var p = [s.lat, s.lon];
      pts.push(p);
      L.circle(p, { radius: s.radius_km * 1000, color: '#1f5bff', weight: 1, fillOpacity: 0.08, interactive: false }).addTo(layer);
      L.marker(p, { keyboard: false, icon: L.divIcon({ className: 'pin', html: '<span>' + (i + 1) + '</span>', iconSize: [28, 28], iconAnchor: [14, 14] }) }).addTo(layer);
    });
    if (pts.length > 1) { L.polyline(pts, { color: '#1f5bff', weight: 3, dashArray: '2 8', lineCap: 'round' }).addTo(layer); }
  }
  function fit() {
    if (stops.length === 1) { map.setView([stops[0].lat, stops[0].lon], 8); }
    else if (stops.length > 1) { map.fitBounds(L.latLngBounds(stops.map(function (s) { return [s.lat, s.lon]; })).pad(0.3)); }
  }

  function render() {
    var ol = $('stops');
    ol.textContent = '';
    stops.forEach(function (s, i) {
      var li = el('li', { 'class': 'stopcard' });
      li.appendChild(el('span', { 'class': 'num', 'aria-hidden': 'true' }, String(i + 1)));
      var body = el('div', { 'class': 'sbody' });
      var name = el('input', { type: 'text', 'aria-label': 'Name of stop ' + (i + 1), maxlength: '80', value: s.name });
      name.addEventListener('input', function () { s.name = name.value; invalidate(); });
      body.appendChild(name);
      var row = el('div', { 'class': 'frow' });
      function field(label, type, val, onchange, attrs) {
        var w = el('label', {}, label);
        var inp = el('input', Object.assign({ type: type, value: val }, attrs || {}));
        inp.addEventListener('change', function () { onchange(inp.value); invalidate(); draw(); });
        w.appendChild(inp);
        row.appendChild(w);
      }
      field('Arrive', 'date', s.from, function (v) { s.from = v; });
      field('Leave', 'date', s.to, function (v) { s.to = v; });
      field('Radius km', 'number', String(s.radius_km), function (v) { s.radius_km = Math.max(1, Math.min(300, parseInt(v, 10) || 25)); }, { min: '1', max: '300', inputmode: 'numeric' });
      body.appendChild(row);
      var tools = el('div', { 'class': 'tools' });
      function tool(label, fn, disabled) {
        var b = el('button', { type: 'button', 'class': 'mini', 'aria-label': label + ' stop ' + (i + 1) }, label);
        b.disabled = !!disabled;
        b.addEventListener('click', fn);
        tools.appendChild(b);
      }
      tool('Up', function () { stops.splice(i - 1, 0, stops.splice(i, 1)[0]); changed(); }, i === 0);
      tool('Down', function () { stops.splice(i + 1, 0, stops.splice(i, 1)[0]); changed(); }, i === stops.length - 1);
      tool('Remove', function () { stops.splice(i, 1); changed(); });
      body.appendChild(tools);
      li.appendChild(body);
      ol.appendChild(li);
    });
    $('empty').hidden = stops.length > 0;
  }
  function changed() { showError(''); invalidate(); render(); draw(); }

  function addStop(name, country, admin, lat, lon) {
    if (stops.length >= 8) { showError('A trip has at most 8 stops.'); return; }
    var last = stops[stops.length - 1];
    var from = last ? addDays(last.to, 1) : addDays(iso(new Date()), 7);
    stops.push({ name: name || 'Stop ' + (stops.length + 1), country: country || '', admin: admin || '', lat: +lat.toFixed(5), lon: +lon.toFixed(5), radius_km: 25, from: from, to: addDays(from, 2) });
    changed();
    fit();
  }

  function placeName(a, fallback) {
    return a.city || a.town || a.village || a.municipality || a.county || a.state || (fallback || '').split(',')[0];
  }
  function adminOf(a, cc) { return cc === 'US' && a['ISO3166-2-lvl4'] ? a['ISO3166-2-lvl4'].split('-')[1] || '' : ''; }

  map.on('click', function (ev) {
    var lat = ev.latlng.lat, lon = ev.latlng.wrap().lng;
    fetch(NOMINATIM + '/reverse?format=jsonv2&zoom=10&addressdetails=1&accept-language=en&lat=' + lat + '&lon=' + lon)
      .then(function (r) { return r.json(); })
      .then(function (j) {
        var a = j.address || {}; var cc = (a.country_code || '').toUpperCase();
        addStop(placeName(a, j.display_name), cc, adminOf(a, cc), lat, lon);
      })
      .catch(function () { addStop('', '', '', lat, lon); });
  });

  $('search').addEventListener('submit', function (ev) {
    ev.preventDefault();
    var q = $('q').value.trim();
    var ul = $('results');
    ul.textContent = '';
    if (q.length < 2) { return; }
    fetch(NOMINATIM + '/search?format=jsonv2&addressdetails=1&limit=5&accept-language=en&q=' + encodeURIComponent(q))
      .then(function (r) { return r.json(); })
      .then(function (list) {
        if (!list.length) { ul.appendChild(el('li', { 'class': 'fine' }, 'No place found. Try another spelling.')); return; }
        list.forEach(function (p) {
          var li = el('li');
          var b = el('button', { type: 'button', 'class': 'result' }, p.display_name);
          b.addEventListener('click', function () {
            var a = p.address || {}; var cc = (a.country_code || '').toUpperCase();
            addStop(p.name || placeName(a, p.display_name), cc, adminOf(a, cc), parseFloat(p.lat), parseFloat(p.lon));
            ul.textContent = '';
            $('q').value = '';
          });
          li.appendChild(b);
          ul.appendChild(li);
        });
      })
      .catch(function () { ul.appendChild(el('li', { 'class': 'fine' }, 'Search is not available right now. Click the map instead.')); });
  });

  function post(path) {
    return fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ stops: stops }), credentials: 'same-origin' })
      .then(function (r) {
        if (r.status === 401) { window.location = '/login'; return Promise.reject(new Error('signin')); }
        return r.json().then(function (j) { return { ok: r.ok, status: r.status, body: j }; });
      });
  }

  $('preview').addEventListener('click', function () {
    showError('');
    $('preview').disabled = true;
    post('/new/preview').then(function (res) {
      $('preview').disabled = false;
      if (!res.ok) { showError(res.body.error || 'The trip could not be checked.'); return; }
      var p = res.body, box = $('previewbox');
      box.textContent = '';
      box.appendChild(el('h3', { 'class': 'h4' }, p.name));
      [['What is collected', p.what_is_collected], ['Refresh', p.refresh], ['Who sees it', p.who_sees_it], ['When it is deleted', p.when_it_is_deleted], ['Can it be undone', p.can_it_be_undone]].forEach(function (kv) {
        var d = el('div', { 'class': 'prow' });
        d.appendChild(el('b', {}, kv[0]));
        d.appendChild(el('span', {}, kv[1]));
        box.appendChild(d);
      });
      box.hidden = false;
      $('create').hidden = false;
      previewed = true;
    }).catch(function () { $('preview').disabled = false; });
  });

  $('create').addEventListener('click', function () {
    if (!previewed) { return; }
    showError('');
    $('create').disabled = true;
    post('/new/create').then(function (res) {
      if (res.ok) { window.location = res.body.redirect || '/trip'; return; }
      $('create').disabled = false;
      showError(res.body.error || 'The trip could not be created.');
      if (res.body.code === 'trip_active') { var a = el('a', { href: '/trip' }, ' Open your trip.'); $('err').appendChild(a); }
    }).catch(function () { $('create').disabled = false; });
  });

  render();
  invalidate();
})();
