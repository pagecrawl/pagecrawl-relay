package main

// The settings page. One self-contained file with no external requests, because
// the machine someone is diagnosing may be exactly the machine that cannot reach
// the internet.
//
// The key from the URL is kept in memory and sent as a header on every call, so it
// never has to survive a reload and never lands in browser history beyond the
// first load.
const settingsPage = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PageCrawl Relay</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f6f7f9; --card: #fff; --ink: #1a1d21; --muted: #6b7280;
    --line: #e5e7eb; --ok: #17803d; --bad: #b42318; --accent: #2563eb;
  }
  @media (prefers-color-scheme: dark) {
    :root { --bg:#15171a; --card:#1e2126; --ink:#e8eaed; --muted:#9aa1ab; --line:#2c3038; }
  }
  * { box-sizing: border-box; }
  body { margin:0; padding:32px 20px; background:var(--bg); color:var(--ink);
         font:14px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
  .wrap { max-width: 640px; margin: 0 auto; }
  h1 { font-size:20px; margin:0 0 4px; }
  .sub { color:var(--muted); margin:0 0 24px; }
  .card { background:var(--card); border:1px solid var(--line); border-radius:8px;
          padding:20px; margin-bottom:16px; }
  .row { display:flex; justify-content:space-between; gap:16px; padding:8px 0;
         border-bottom:1px solid var(--line); }
  .row:last-child { border-bottom:0; }
  .row span:first-child { color:var(--muted); }
  .row span:last-child { font-variant-numeric: tabular-nums; text-align:right; }
  .dot { display:inline-block; width:8px; height:8px; border-radius:50%; margin-right:8px; }
  .on { background:var(--ok); } .off { background:var(--bad); } .idle { background:var(--muted); }
  input { width:100%; padding:10px 12px; border:1px solid var(--line); border-radius:6px;
          background:var(--bg); color:var(--ink); font-family:ui-monospace, Menlo, monospace; }
  button { padding:10px 16px; border:0; border-radius:6px; background:var(--accent);
           color:#fff; font-size:14px; cursor:pointer; }
  button.ghost { background:transparent; color:var(--ink); border:1px solid var(--line); }
  button:disabled { opacity:.5; cursor:default; }
  .actions { display:flex; gap:8px; margin-top:12px; flex-wrap:wrap; }
  .msg { margin-top:10px; font-size:13px; }
  .msg.bad { color:var(--bad); } .msg.ok { color:var(--ok); }
  .check { display:flex; gap:10px; padding:8px 0; border-bottom:1px solid var(--line); }
  .check:last-child { border-bottom:0; }
  .check b { font-weight:600; }
  .check .d { color:var(--muted); font-size:13px; }
  .check .fix { color:var(--accent); font-size:13px; }
  code { font-family:ui-monospace, Menlo, monospace; font-size:13px; }
  .ev { display:flex; gap:10px; align-items:baseline; padding:5px 0;
        border-bottom:1px solid var(--line); font-size:13px; }
  .ev:last-child { border-bottom:0; }
  .ev time { color:var(--muted); font-variant-numeric:tabular-nums; }
  .ev .h { font-family:ui-monospace, Menlo, monospace; word-break:break-all; }
  .ev .why { color:var(--bad); }
  .empty { color:var(--muted); padding:8px 0; }
</style>
<div class="wrap">
  <h1>PageCrawl Relay</h1>
  <p class="sub">Your checks leave from this machine, so pages see your connection instead of ours.</p>

  <div class="card" id="setup" hidden>
    <b>Connect this machine</b>
    <p class="sub" style="margin:6px 0 12px">
      In PageCrawl, open <b>Settings &rarr; Relays</b>, choose <b>Add machine</b>, and paste the token here.
    </p>
    <input id="token" placeholder="Paste the token" autocomplete="off" spellcheck="false">
    <div class="actions"><button id="save">Connect</button></div>
    <div class="msg" id="saveMsg"></div>
  </div>

  <div class="card" id="status" hidden>
    <div class="row"><span>Status</span><span id="s-state"></span></div>
    <div class="row"><span>Connected for</span><span id="s-conn">-</span></div>
    <div class="row"><span>Running for</span><span id="s-up">-</span></div>
    <div class="row"><span>Data carried</span><span id="s-traffic">-</span></div>
    <div class="row"><span>Connections</span><span id="s-conns">-</span></div>
    <div class="row"><span>Your address</span><span id="s-ip">-</span></div>
    <div class="row"><span>Most recent site</span><span id="s-host">-</span></div>
    <div class="actions">
      <button class="ghost" id="pause">Pause</button>
      <button class="ghost" id="runCheck">Run a check</button>
    </div>
    <div class="msg" id="stateMsg"></div>
  </div>

  <div class="card" id="activity" hidden>
    <div style="display:flex;justify-content:space-between;align-items:baseline">
      <b>Recent activity</b>
      <span class="sub" style="font-size:12px;margin:0">newest first, last 40</span>
    </div>
    <p class="sub" style="margin:6px 0 4px;font-size:13px">
      Every destination this machine was asked to reach. Refusals are the guard
      protecting your own network.
    </p>
    <div id="log"></div>
  </div>

  <div class="card" id="checks" hidden><b>Self-check</b><div id="checkList"></div></div>

  <div class="card">
    <b>This machine</b>
    <div class="row"><span>Gateway</span><span id="s-gw">-</span></div>
    <div class="row"><span>Version</span><span id="s-ver">-</span></div>
    <div class="actions">
      <button class="ghost" id="forget">Disconnect this machine</button>
    </div>
    <p class="sub" style="margin:10px 0 0;font-size:13px">
      Forgets the token stored on this computer and stops relaying. The machine stays
      listed in PageCrawl until you remove it there.
    </p>
    <div class="msg" id="forgetMsg"></div>
  </div>

  <p class="sub" style="font-size:13px">
    This window is only a control panel. Closing it leaves the relay running;
    quit the app to stop it. <span id="ver"></span>
  </p>
</div>
<script>
  const key = new URLSearchParams(location.search).get('k') || '';
  // Drop the key from the address bar so it does not linger in history or get
  // copied into a screenshot.
  if (key) history.replaceState({}, '', location.pathname);

  const call = (path, opts = {}) =>
    fetch(path, { ...opts, headers: { 'Content-Type': 'application/json', 'X-Relay-Key': key } })
      .then(r => r.json());

  const $ = id => document.getElementById(id);

  // Hostnames arrive from the network. Never interpolate them as markup.
  const esc = v => String(v).replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

  function render(d) {
    const s = d.state;
    $('setup').hidden = d.configured;
    $('status').hidden = !d.configured;
    $('ver').textContent = d.version + ' · ' + d.platform;

    let cls = 'idle', label = 'Paused';
    if (!s.paused) {
      cls = s.connected ? 'on' : 'off';
      label = s.connected ? 'Connected' : 'Not connected';
    }
    $('s-state').innerHTML = '<span class="dot ' + cls + '"></span>' + label;
    $('s-conn').textContent = s.connected_for || '-';
    $('s-up').textContent = s.uptime;
    $('s-traffic').textContent = s.traffic;
    $('s-conns').textContent = s.connections;
    $('s-ip').textContent = s.exit_ip || 'Run a check to see it';
    $('s-host').textContent = s.last_host || 'Nothing yet';
    $('pause').textContent = s.paused ? 'Resume' : 'Pause';
    $('s-gw').textContent = d.gateway;
    $('s-ver').textContent = d.version + ' \u00b7 ' + d.platform;

    const rows = s.recent || [];
    $('activity').hidden = !d.configured;
    $('log').innerHTML = rows.length
      ? rows.map(e =>
          '<div class="ev"><time>' + e.at + '</time>' +
          '<span class="dot ' + (e.allowed ? 'on' : 'off') + '"></span>' +
          '<span class="h">' + esc(e.host) + (e.port && e.port !== 443 ? ':' + e.port : '') + '</span>' +
          (e.reason ? '<span class="why">' + esc(e.reason) + '</span>' : '') + '</div>'
        ).join('')
      : '<div class="empty">Nothing yet. Point a monitor at this machine and run a check.</div>';
    $('stateMsg').textContent = (!s.connected && !s.paused && s.last_error) ? s.last_error : '';
    $('stateMsg').className = 'msg bad';
  }

  const refresh = () => call('/api/state').then(render).catch(() => {});

  $('save').onclick = () => {
    const token = $('token').value.trim();
    $('save').disabled = true;
    call('/api/token', { method: 'POST', body: JSON.stringify({ token }) })
      .then(r => {
        $('saveMsg').textContent = r.ok ? 'Connected.' : r.error;
        $('saveMsg').className = 'msg ' + (r.ok ? 'ok' : 'bad');
        $('save').disabled = false;
        refresh();
      });
  };

  $('pause').onclick = () => call('/api/pause', { method: 'POST' }).then(refresh);

  $('forget').onclick = () => {
    if (!confirm('Stop relaying and forget the token stored on this computer?')) return;
    call('/api/forget', { method: 'POST' }).then(r => {
      $('forgetMsg').textContent = r.ok ? 'Disconnected. Paste a token to connect again.' : r.error;
      $('forgetMsg').className = 'msg ' + (r.ok ? 'ok' : 'bad');
      refresh();
    });
  };

  $('runCheck').onclick = () => {
    $('runCheck').disabled = true;
    $('checks').hidden = false;
    $('checkList').textContent = 'Checking...';
    call('/api/check').then(r => {
      $('checkList').innerHTML = r.checks.map(c =>
        '<div class="check"><span class="dot ' + (c.ok ? 'on' : 'off') + '" style="margin-top:6px"></span>' +
        '<div><b>' + c.name + '</b><div class="d">' + c.detail + '</div>' +
        (c.fix ? '<div class="fix">' + c.fix + '</div>' : '') + '</div></div>'
      ).join('');
      $('runCheck').disabled = false;
      refresh();
    });
  };

  refresh();
  setInterval(refresh, 2000);
</script>
`
