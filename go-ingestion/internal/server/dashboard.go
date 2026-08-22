package server

import (
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DataGuard Rail — Dashboard</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: system-ui, sans-serif; background: #f5f7fa; color: #222; }
  header { background: #1a1a2e; color: #fff; padding: 1rem 2rem; display: flex; align-items: center; gap: 1rem; }
  header h1 { font-size: 1.25rem; font-weight: 600; }
  header .badge { background: #e94560; border-radius: 9999px; padding: 2px 10px; font-size: .8rem; }
  main { max-width: 1200px; margin: 2rem auto; padding: 0 1rem; }
  .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; margin-bottom: 2rem; }
  .card { background: #fff; border-radius: 8px; padding: 1.25rem 1.5rem; box-shadow: 0 1px 4px rgba(0,0,0,.08); }
  .card .label { font-size: .8rem; color: #666; margin-bottom: .25rem; }
  .card .value { font-size: 2rem; font-weight: 700; }
  .card.danger .value { color: #e94560; }
  .card.ok .value { color: #22c55e; }
  section { background: #fff; border-radius: 8px; padding: 1.25rem 1.5rem; box-shadow: 0 1px 4px rgba(0,0,0,.08); margin-bottom: 1.5rem; }
  section h2 { font-size: 1rem; font-weight: 600; margin-bottom: 1rem; display: flex; align-items: center; gap: .5rem; }
  .controls { display: flex; gap: .5rem; margin-bottom: .75rem; }
  .controls select, .controls input { padding: .4rem .75rem; border: 1px solid #ddd; border-radius: 6px; font-size: .9rem; }
  .controls button { padding: .4rem .75rem; background: #1a1a2e; color: #fff; border: none; border-radius: 6px; cursor: pointer; font-size: .9rem; }
  .controls button:hover { background: #e94560; }
  table { width: 100%; border-collapse: collapse; font-size: .875rem; }
  th { text-align: left; padding: .5rem .75rem; background: #f5f7fa; font-weight: 600; border-bottom: 2px solid #e5e7eb; }
  td { padding: .5rem .75rem; border-bottom: 1px solid #f0f0f0; }
  tr:hover td { background: #fafafa; }
  .tag { display: inline-block; padding: 1px 8px; border-radius: 9999px; font-size: .75rem; font-weight: 600; }
  .tag-rule { background: #dbeafe; color: #1d4ed8; }
  .tag-diff { background: #fef3c7; color: #92400e; }
  .empty { color: #999; font-style: italic; padding: 1rem 0; text-align: center; }
  #status { font-size: .8rem; color: #666; margin-left: auto; }
</style>
</head>
<body>
<header>
  <h1>DataGuard Rail</h1>
  <span class="badge">Dashboard</span>
  <span id="status">読み込み中…</span>
</header>
<main>
  <div class="cards">
    <div class="card" id="card-total">
      <div class="label">Total Violations</div>
      <div class="value" id="cnt-total">—</div>
    </div>
    <div class="card" id="card-tables">
      <div class="label">Tables Affected</div>
      <div class="value" id="cnt-tables">—</div>
    </div>
    <div class="card" id="card-diffs">
      <div class="label">Schema Diffs</div>
      <div class="value" id="cnt-diffs">—</div>
    </div>
  </div>

  <section>
    <h2>Violations
      <span id="viol-count" style="font-weight:400;color:#666;font-size:.85rem"></span>
    </h2>
    <div class="controls">
      <select id="table-filter"><option value="">すべてのテーブル</option></select>
      <input id="rule-filter" placeholder="ルール名で絞り込み" style="flex:1">
      <button onclick="clearFilters()">クリア</button>
    </div>
    <table>
      <thead><tr>
        <th>ID</th><th>Rule</th><th>Table</th><th>Row</th><th>Column</th><th>Value</th><th>Detected At</th>
      </tr></thead>
      <tbody id="viol-body"></tbody>
    </table>
  </section>

  <section>
    <h2>Schema Diffs</h2>
    <table>
      <thead><tr>
        <th>Table</th><th>Added</th><th>Dropped</th><th>Changed</th><th>Detected At</th>
      </tr></thead>
      <tbody id="diff-body"></tbody>
    </table>
  </section>
</main>

<script>
let allViolations = [];

async function load() {
  const [violRes, diffRes] = await Promise.all([
    fetch('/api/violations'),
    fetch('/api/schema-diff'),
  ]);
  allViolations = violRes.ok ? await violRes.json() : [];
  const diffs = diffRes.ok ? await diffRes.json() : [];

  // summary cards
  const tables = new Set(allViolations.map(v => v.table));
  document.getElementById('cnt-total').textContent = allViolations.length;
  document.getElementById('cnt-tables').textContent = tables.size;
  document.getElementById('cnt-diffs').textContent = Array.isArray(diffs) ? diffs.length : 0;
  document.getElementById('card-total').className = 'card ' + (allViolations.length > 0 ? 'danger' : 'ok');

  // table filter options
  const sel = document.getElementById('table-filter');
  [...tables].sort().forEach(t => {
    const o = document.createElement('option');
    o.value = t; o.textContent = t;
    sel.appendChild(o);
  });
  sel.addEventListener('change', renderViolations);
  document.getElementById('rule-filter').addEventListener('input', renderViolations);

  renderViolations();
  renderDiffs(diffs);
  document.getElementById('status').textContent = '最終更新: ' + new Date().toLocaleTimeString('ja-JP');
}

function renderViolations() {
  const tableF = document.getElementById('table-filter').value;
  const ruleF  = document.getElementById('rule-filter').value.toLowerCase();
  const filtered = allViolations.filter(v =>
    (!tableF || v.table === tableF) &&
    (!ruleF  || v.rule.toLowerCase().includes(ruleF))
  );
  const tbody = document.getElementById('viol-body');
  document.getElementById('viol-count').textContent = filtered.length + ' 件';
  if (!filtered.length) {
    tbody.innerHTML = '<tr><td colspan="7" class="empty">違反なし</td></tr>';
    return;
  }
  tbody.innerHTML = filtered.map(v => ` + "`" + `
    <tr>
      <td>${esc(v.id)}</td>
      <td><span class="tag tag-rule">${esc(v.rule)}</span></td>
      <td>${esc(v.table)}</td>
      <td>${v.row}</td>
      <td>${esc(v.column)}</td>
      <td><code>${esc(v.value)}</code></td>
      <td>${esc((v.detected_at||'').replace('T',' ').slice(0,19))}</td>
    </tr>` + "`" + `).join('');
}

function renderDiffs(diffs) {
  const tbody = document.getElementById('diff-body');
  if (!Array.isArray(diffs) || !diffs.length) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">差分なし</td></tr>';
    return;
  }
  tbody.innerHTML = diffs.map(d => ` + "`" + `
    <tr>
      <td>${esc(d.table)}</td>
      <td>${(d.added||[]).map(c=>'<span class="tag tag-diff">+'+esc(c)+'</span>').join(' ')}</td>
      <td>${(d.dropped||[]).map(c=>'<span class="tag tag-diff">-'+esc(c)+'</span>').join(' ')}</td>
      <td>${(d.changed||[]).map(c=>'<span class="tag tag-diff">~'+esc(c.column||c)+'</span>').join(' ')}</td>
      <td>${esc((d.detected_at||'').replace('T',' ').slice(0,19))}</td>
    </tr>` + "`" + `).join('');
}

function clearFilters() {
  document.getElementById('table-filter').value = '';
  document.getElementById('rule-filter').value = '';
  renderViolations();
}

function esc(s) {
  return String(s ?? '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

load();
</script>
</body>
</html>
`))

// handleDashboard は GET / でダッシュボード HTML を返す。
func (s *Server) handleDashboard(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTmpl.Execute(c.Writer, nil); err != nil {
		c.Status(http.StatusInternalServerError)
	}
}
