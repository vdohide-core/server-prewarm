// ── State ──
const grid = document.getElementById('jobsGrid');
const emptyMsg = document.getElementById('jobsEmpty');
const cards = {};
let currentPage = 1;
let activeCount = 0;
let currentSort = { key: 'prewarmAt', dir: -1 };
let searchTerm = '';
let allLogs = [];

// ── Tabs ──
document.querySelectorAll('.nav-item').forEach(item => {
  item.addEventListener('click', e => {
    e.preventDefault();
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    item.classList.add('active');
    const tab = item.getAttribute('data-tab');
    document.getElementById('tab-' + tab).classList.add('active');
    document.getElementById('pageTitle').textContent = tab === 'dashboard' ? 'Dashboard' : 'History';
    if (tab === 'history') loadHistory(1);
  });
});

// ── WebSocket ──
function connectWS() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(proto + '//' + location.host + '/ws');
  ws.onopen = () => {
    document.getElementById('wsDot').classList.add('connected');
    document.getElementById('wsLabel').textContent = 'Live';
  };
  ws.onclose = () => {
    document.getElementById('wsDot').classList.remove('connected');
    document.getElementById('wsLabel').textContent = 'Offline';
    setTimeout(connectWS, 2000);
  };
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.type === 'url_result') onUrlResult(msg.data);
    else if (msg.type === 'job_start') onJobStart(msg.data);
    else if (msg.type === 'job_complete') { onJobComplete(msg.data); if (currentPage === 1) loadHistory(1); }
  };
}

// ── Active Jobs ──
function onJobStart(d) {
  if (cards[d.mediaSlug]) return;
  emptyMsg.style.display = 'none';
  activeCount++;
  document.getElementById('activeCount').textContent = activeCount;

  const card = document.createElement('div');
  card.className = 'job-card';
  card.innerHTML =
    '<div class="jc-top">' +
      '<span class="jc-indicator"></span>' +
      '<span class="jc-name">' + esc(d.fileSlug) + '</span>' +
      '<span class="jc-res">' + (d.resolution === 'original' ? 'Original' : d.resolution + 'p') + '</span>' +
      '<span class="jc-slug">' + d.mediaSlug + '</span>' +
      '<span class="jc-toggle" id="toggle-' + d.mediaSlug + '">▼</span>' +
    '</div>' +
    '<div class="jc-progress">' +
      '<div class="jc-bar-track"><div class="jc-bar-fill" id="bar-' + d.mediaSlug + '"></div></div>' +
      '<span class="jc-pct" id="pct-' + d.mediaSlug + '">0%</span>' +
    '</div>' +
    '<div class="jc-stats">' +
      '<span id="prog-' + d.mediaSlug + '">0 / 0</span>' +
      '<span class="green" id="hit-' + d.mediaSlug + '">HIT 0</span>' +
      '<span class="orange" id="miss-' + d.mediaSlug + '">MISS 0</span>' +
    '</div>' +
    '<div class="jc-urls" id="urls-' + d.mediaSlug + '"></div>';

  card.querySelector('.jc-top').addEventListener('click', () => {
    const urls = document.getElementById('urls-' + d.mediaSlug);
    const tog = document.getElementById('toggle-' + d.mediaSlug);
    if (urls) urls.classList.toggle('open');
    if (tog) tog.classList.toggle('open');
  });

  grid.insertBefore(card, grid.firstChild);
  cards[d.mediaSlug] = { el: card, hit: 0, miss: 0, startedAt: Date.now(), lastUpdate: Date.now() };
}

function onUrlResult(d) {
  if (!cards[d.mediaSlug]) onJobStart(d);
  const c = cards[d.mediaSlug];
  c.lastUpdate = Date.now();

  if (d.cache === 'HIT' || d.cache === 'REVALIDATED') c.hit++;
  else if (d.cache === 'MISS') c.miss++;

  if (d.total > 0) {
    const pct = ((d.progress / d.total) * 100).toFixed(0);
    const bar = document.getElementById('bar-' + d.mediaSlug);
    const pctEl = document.getElementById('pct-' + d.mediaSlug);
    const progEl = document.getElementById('prog-' + d.mediaSlug);
    if (bar) bar.style.width = pct + '%';
    if (pctEl) pctEl.textContent = pct + '%';
    if (progEl) progEl.textContent = d.progress + ' / ' + d.total;
  }
  const hitEl = document.getElementById('hit-' + d.mediaSlug);
  const missEl = document.getElementById('miss-' + d.mediaSlug);
  if (hitEl) hitEl.textContent = 'HIT ' + c.hit;
  if (missEl) missEl.textContent = 'MISS ' + c.miss;

  const urlsEl = document.getElementById('urls-' + d.mediaSlug);
  if (urlsEl) {
    const cache = d.cache || 'NONE';
    const sc = d.status || 0;
    const scClass = sc >= 200 && sc < 400 ? 'ok' : 'err';
    const line = document.createElement('div');
    line.className = 'url-line';
    line.innerHTML =
      '<span class="url-code ' + scClass + '">' + sc + '</span>' +
      '<span class="url-cache ' + cache + '">' + cache + '</span>' +
      '<span class="url-pop">' + (d.pop || '') + '</span>' +
      '<span class="url-dur">' + d.duration + '</span>' +
      '<span class="url-file">' + esc(d.url) + '</span>';
    urlsEl.appendChild(line);
    while (urlsEl.children.length > 50) urlsEl.removeChild(urlsEl.firstChild);
    urlsEl.scrollTop = urlsEl.scrollHeight;
  }
}

function onJobComplete(d) {
  const c = cards[d.mediaSlug];
  if (!c) return;
  c.el.classList.add('done');
  const ind = c.el.querySelector('.jc-indicator');
  if (ind) ind.classList.add('done');
  const bar = document.getElementById('bar-' + d.mediaSlug);
  const pctEl = document.getElementById('pct-' + d.mediaSlug);
  if (bar) bar.style.width = '100%';
  if (pctEl) pctEl.textContent = '100%';

  setTimeout(() => {
    c.el.classList.add('removing');
    setTimeout(() => {
      c.el.remove();
      delete cards[d.mediaSlug];
      activeCount--;
      document.getElementById('activeCount').textContent = activeCount;
      if (activeCount <= 0) { activeCount = 0; emptyMsg.style.display = ''; }
    }, 300);
  }, 3000);
}

// ── Status ──
function updateStatus() {
  fetch('/api/status').then(r => r.json()).then(s => {
    const b = document.getElementById('stateBadge');
    b.textContent = s.state.toUpperCase();
    b.className = 'badge badge-' + s.state;

    setText('totalMedia', fmt(s.totalMedia));
    setText('pending', fmt(s.pending));
    setText('processed', fmt(s.processed));
    setText('totalHit', fmt(s.totalHit));
    setText('totalMiss', fmt(s.totalMiss));

    const t = s.totalHit + s.totalMiss + s.totalFailed;
    setText('hitRate', t > 0 ? ((s.totalHit / t) * 100).toFixed(1) + '%' : '0%');

    const pct = s.totalMedia > 0 ? ((s.processed / s.totalMedia) * 100).toFixed(1) : 0;
    document.getElementById('progressBar').style.width = pct + '%';
    setText('progressText', fmt(s.processed) + ' / ' + fmt(s.totalMedia) + '  ·  ' + pct + '%');

    const el = document.getElementById('elapsedBadge');
    el.textContent = s.elapsed ? '⏱ ' + s.elapsed : '';

    // Server info
    const sName = s.storageName || 'All Storages';
    setText('serverName', sName + (s.pop ? ' · ' + s.pop.toUpperCase() : ''));
    let domain = '';
    if (s.domainContent) domain += s.domainContent;
    if (s.refDomain) domain += (domain ? ' · ' : '') + s.refDomain;
    setText('domainInfo', domain);

    cleanupStaleCards();
  }).catch(() => {});
}

// ── History ──
function loadHistory(page) {
  if (page) currentPage = page;
  const q = searchTerm ? '&q=' + encodeURIComponent(searchTerm) : '';
  const s = '&sort=' + currentSort.key + '&dir=' + (currentSort.dir === 1 ? '1' : '-1');
  fetch('/api/logs?page=' + currentPage + q + s).then(r => r.json()).then(data => {
    allLogs = data.logs || [];
    setText('logCount', fmt(data.total) + ' results');
    renderHistory(allLogs, data);
  }).catch(() => {});
}

function renderHistory(logs, data) {
  const body = document.getElementById('historyBody');

  if (!logs.length) {
    body.innerHTML = '<tr><td colspan="7" class="empty-msg">No history yet</td></tr>';
    document.getElementById('pageInfo').innerHTML = '';
    return;
  }

  let html = '';
  logs.forEach(l => {
    const pctNum = parseFloat(l.hitRate) || 0;
    const pctClass = pctNum >= 90 ? 'high' : pctNum >= 50 ? 'mid' : 'low';
    const pctColor = pctNum >= 90 ? 'var(--green)' : pctNum >= 50 ? 'var(--yellow)' : 'var(--orange)';
    const time = l.prewarmAt ? timeAgo(new Date(l.prewarmAt)) : '';
    html +=
      '<tr onclick="showDetail(\'' + esc(l.slug) + '\',' + JSON.stringify(l).replace(/"/g, '&quot;') + ')">' +
        '<td><div class="td-name">' + esc(l.fileSlug) + '<span class="name-sub">' + esc(l.slug) + '</span></div></td>' +
        '<td class="td-res">' + (l.resolution === 'original' ? 'Original' : l.resolution + 'p') + '</td>' +
        '<td>' + fmt(l.total) + '</td>' +
        '<td class="td-hit">' + fmt(l.hit) + '</td>' +
        '<td class="td-miss">' + fmt(l.miss) + '</td>' +
        '<td><div class="pct-bar"><div class="pct-track"><div class="pct-fill" style="width:' + pctNum + '%;background:' + pctColor + '"></div></div><span class="td-pct ' + pctClass + '">' + l.hitRate + '</span></div></td>' +
        '<td class="td-time">' + time + '</td>' +
      '</tr>';
  });
  body.innerHTML = html;

  // Pagination
  let pg = '';
  if (data && data.totalPages > 1) {
    pg += '<button class="pg-btn" ' + (currentPage <= 1 ? 'disabled' : '') + ' onclick="loadHistory(' + (currentPage - 1) + ')">←</button>';
    pg += '<span class="pg-text">' + currentPage + '</span>';
    pg += '<span class="pg-text">/</span>';
    pg += '<input class="pg-input" type="number" min="1" max="' + data.totalPages + '" value="' + currentPage + '" onkeydown="if(event.key===\'Enter\')loadHistory(+this.value)" />';
    pg += '<span class="pg-text">of ' + data.totalPages + '</span>';
    pg += '<button class="pg-btn" ' + (currentPage >= data.totalPages ? 'disabled' : '') + ' onclick="loadHistory(' + (currentPage + 1) + ')">→</button>';
  }
  document.getElementById('pageInfo').innerHTML = pg;
}

// ── Sort ──
document.querySelectorAll('.sortable').forEach(th => {
  th.innerHTML += ' <span class="sort-icon">▲</span>';
  th.addEventListener('click', () => {
    const key = th.getAttribute('data-sort');
    if (currentSort.key === key) {
      currentSort.dir *= -1;
    } else {
      currentSort = { key, dir: -1 };
    }
    document.querySelectorAll('.sortable').forEach(h => {
      h.classList.remove('sort-active');
      h.querySelector('.sort-icon').textContent = '▲';
    });
    th.classList.add('sort-active');
    th.querySelector('.sort-icon').textContent = currentSort.dir === 1 ? '▲' : '▼';
    loadHistory(1);
  });
});

// ── Search ──
let searchTimeout;
document.getElementById('searchInput').addEventListener('input', e => {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => {
    searchTerm = e.target.value.trim();
    loadHistory(1);
  }, 300);
});

// ── Detail Modal ──
function showDetail(slug, data) {
  let overlay = document.getElementById('detailOverlay');
  if (!overlay) {
    overlay = document.createElement('div');
    overlay.id = 'detailOverlay';
    overlay.className = 'detail-overlay';
    overlay.addEventListener('click', e => { if (e.target === overlay) overlay.classList.remove('open'); });
    document.body.appendChild(overlay);
  }

  const pct = data.hitRate || '0%';
  overlay.innerHTML =
    '<div class="detail-panel">' +
      '<div class="detail-head">' +
        '<h3>' + esc(data.fileSlug) + '</h3>' +
        '<button class="detail-close" onclick="document.getElementById(\'detailOverlay\').classList.remove(\'open\')">&times;</button>' +
      '</div>' +
      '<div class="detail-body">' +
        '<div class="detail-grid">' +
          '<div class="detail-item"><span class="d-label">Slug</span><span class="d-value">' + esc(data.slug) + '</span></div>' +
          '<div class="detail-item"><span class="d-label">Resolution</span><span class="d-value td-res">' + (data.resolution === 'original' ? 'Original' : data.resolution + 'p') + '</span></div>' +
          '<div class="detail-item"><span class="d-label">Total URLs</span><span class="d-value">' + fmt(data.total) + '</span></div>' +
          '<div class="detail-item"><span class="d-label">Hit Rate</span><span class="d-value purple">' + pct + '</span></div>' +
          '<div class="detail-item"><span class="d-label">HIT</span><span class="d-value green">' + fmt(data.hit) + '</span></div>' +
          '<div class="detail-item"><span class="d-label">MISS</span><span class="d-value orange">' + fmt(data.miss) + '</span></div>' +
          '<div class="detail-item"><span class="d-label">Expired</span><span class="d-value">' + fmt(data.expired) + '</span></div>' +
          '<div class="detail-item"><span class="d-label">Failed</span><span class="d-value" style="color:var(--red)">' + fmt(data.failed) + '</span></div>' +
          '<div class="detail-item" style="grid-column:1/-1"><span class="d-label">Prewarmed At</span><span class="d-value">' + (data.prewarmAt ? new Date(data.prewarmAt).toLocaleString() : '-') + '</span></div>' +
        '</div>' +
      '</div>' +
    '</div>';
  overlay.classList.add('open');
}

// ── Helpers ──
function setText(id, v) { document.getElementById(id).textContent = v; }
function fmt(n) { return (n || 0).toLocaleString(); }
function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
function timeAgo(d) {
  const s = Math.floor((Date.now() - d.getTime()) / 1000);
  if (s < 60) return s + 's ago';
  const m = Math.floor(s / 60);
  if (m < 60) return m + 'm ago';
  const h = Math.floor(m / 60);
  if (h < 24) return h + 'h ago';
  return Math.floor(h / 24) + 'd ago';
}
function cleanupStaleCards() {
  const staleMs = 5 * 60 * 1000;
  const now = Date.now();
  Object.keys(cards).forEach(slug => {
    const c = cards[slug];
    if (c.el.classList.contains('done')) return;
    if (now - c.lastUpdate > staleMs) onJobComplete({ mediaSlug: slug });
  });
}

// ── Init ──
connectWS();
updateStatus();
loadHistory(1);
setInterval(updateStatus, 3000);
