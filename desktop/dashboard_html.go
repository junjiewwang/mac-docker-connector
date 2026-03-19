package main

// dashboardHTML 内嵌的 Dashboard 前端页面
const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Docker Connector Dashboard</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600;700&family=Inter:wght@300;400;500;600;700&display=swap" rel="stylesheet">
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg-primary:#0a0a0f;
  --bg-secondary:#12121a;
  --bg-card:#16161f;
  --bg-card-hover:#1c1c28;
  --border-color:#2a2a3a;
  --border-glow:rgba(99,102,241,0.3);
  --text-primary:#e8e8ef;
  --text-secondary:#8888a0;
  --text-muted:#555568;
  --accent-blue:#6366f1;
  --accent-cyan:#22d3ee;
  --accent-green:#10b981;
  --accent-red:#ef4444;
  --accent-amber:#f59e0b;
  --accent-purple:#a78bfa;
  --font-mono:'JetBrains Mono',monospace;
  --font-sans:'Inter',system-ui,sans-serif;
  --radius:12px;
  --radius-sm:8px;
}
html{font-size:14px}
body{
  font-family:var(--font-sans);
  background:var(--bg-primary);
  color:var(--text-primary);
  min-height:100vh;
  overflow-x:hidden;
}
/* 背景网格 */
body::before{
  content:'';position:fixed;inset:0;z-index:0;pointer-events:none;
  background:
    linear-gradient(rgba(99,102,241,0.03) 1px,transparent 1px),
    linear-gradient(90deg,rgba(99,102,241,0.03) 1px,transparent 1px);
  background-size:60px 60px;
}
/* 渐变光晕 */
body::after{
  content:'';position:fixed;top:-200px;right:-200px;width:600px;height:600px;
  background:radial-gradient(circle,rgba(99,102,241,0.08) 0%,transparent 70%);
  z-index:0;pointer-events:none;
}

.app{position:relative;z-index:1;max-width:1400px;margin:0 auto;padding:24px 32px}

/* Header */
.header{
  display:flex;justify-content:space-between;align-items:center;
  padding:20px 0;margin-bottom:24px;
  border-bottom:1px solid var(--border-color);
}
.header-left{display:flex;align-items:center;gap:16px}
.logo{
  width:40px;height:40px;border-radius:10px;
  background:linear-gradient(135deg,var(--accent-blue),var(--accent-purple));
  display:flex;align-items:center;justify-content:center;
  font-size:20px;font-weight:700;color:#fff;
}
.header-title h1{font-size:1.25rem;font-weight:600;letter-spacing:-0.02em}
.header-title p{font-size:0.8rem;color:var(--text-secondary);margin-top:2px}
.header-right{display:flex;align-items:center;gap:16px}
.refresh-indicator{
  display:flex;align-items:center;gap:8px;
  font-size:0.75rem;color:var(--text-muted);font-family:var(--font-mono);
}
.pulse-dot{
  width:8px;height:8px;border-radius:50%;background:var(--accent-green);
  animation:pulse 2s ease-in-out infinite;
}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:0.5;transform:scale(0.8)}}
.btn{
  padding:8px 16px;border-radius:var(--radius-sm);border:1px solid var(--border-color);
  background:var(--bg-card);color:var(--text-primary);cursor:pointer;
  font-size:0.8rem;font-family:var(--font-sans);font-weight:500;
  transition:all 0.2s ease;display:inline-flex;align-items:center;gap:6px;
}
.btn:hover{border-color:var(--accent-blue);background:var(--bg-card-hover)}
.btn-primary{
  background:var(--accent-blue);border-color:var(--accent-blue);color:#fff;
}
.btn-primary:hover{background:#5558e6;box-shadow:0 0 20px rgba(99,102,241,0.3)}
.btn-danger{background:var(--accent-red);border-color:var(--accent-red);color:#fff}
.btn-danger:hover{background:#dc2626}
.btn:disabled{opacity:0.5;cursor:not-allowed}

/* Bento Grid */
.bento-grid{
  display:grid;
  grid-template-columns:repeat(4,1fr);
  grid-template-rows:auto;
  gap:16px;
}
.card{
  background:var(--bg-card);
  border:1px solid var(--border-color);
  border-radius:var(--radius);
  padding:20px;
  transition:all 0.3s ease;
  position:relative;overflow:hidden;
}
.card:hover{border-color:rgba(99,102,241,0.2);background:var(--bg-card-hover)}
.card-header{
  display:flex;justify-content:space-between;align-items:center;
  margin-bottom:16px;
}
.card-title{
  font-size:0.8rem;font-weight:600;color:var(--text-secondary);
  text-transform:uppercase;letter-spacing:0.05em;
}
.card-icon{font-size:1.2rem}

/* 状态卡片 (跨越 4 列) */
.card-status{grid-column:span 4}
.status-grid{
  display:grid;grid-template-columns:repeat(4,1fr);gap:16px;
}
.stat-item{
  background:var(--bg-secondary);border-radius:var(--radius-sm);padding:16px;
  border:1px solid transparent;transition:border-color 0.2s ease;
}
.stat-item:hover{border-color:var(--border-color)}
.stat-label{font-size:0.7rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.06em;margin-bottom:8px}
.stat-value{font-family:var(--font-mono);font-size:1.1rem;font-weight:600;color:var(--text-primary)}
.stat-value.connected{color:var(--accent-green)}
.stat-value.disconnected{color:var(--accent-red)}

/* 路由摘要 (跨越 4 列) */
.card-summary{grid-column:span 4}
.summary-pills{display:flex;gap:12px;flex-wrap:wrap}
.pill{
  display:flex;align-items:center;gap:8px;
  padding:10px 20px;border-radius:50px;
  background:var(--bg-secondary);border:1px solid var(--border-color);
  font-family:var(--font-mono);font-size:0.85rem;font-weight:500;
  transition:all 0.2s ease;
}
.pill .count{font-size:1.3rem;font-weight:700}
.pill.ok .count{color:var(--accent-green)}
.pill.missing .count{color:var(--accent-red)}
.pill.conflict .count{color:var(--accent-amber)}
.pill.wrong .count{color:var(--accent-purple)}
.pill.extra .count{color:var(--accent-cyan)}

/* 路由表格 (跨越 4 列) */
.card-routes{grid-column:span 4}
.routes-table{width:100%;border-collapse:separate;border-spacing:0}
.routes-table th{
  text-align:left;padding:10px 16px;font-size:0.7rem;font-weight:600;
  color:var(--text-muted);text-transform:uppercase;letter-spacing:0.06em;
  border-bottom:1px solid var(--border-color);
}
.routes-table td{
  padding:10px 16px;font-family:var(--font-mono);font-size:0.82rem;
  border-bottom:1px solid rgba(42,42,58,0.5);
  transition:background 0.15s ease;
}
.routes-table tr:hover td{background:var(--bg-card-hover)}
.routes-table tr:last-child td{border-bottom:none}

/* 状态标记 */
.status-badge{
  display:inline-flex;align-items:center;gap:6px;
  padding:3px 10px;border-radius:50px;font-size:0.75rem;font-weight:600;
  font-family:var(--font-mono);
}
.status-badge.ok{background:rgba(16,185,129,0.12);color:var(--accent-green);border:1px solid rgba(16,185,129,0.2)}
.status-badge.missing{background:rgba(239,68,68,0.12);color:var(--accent-red);border:1px solid rgba(239,68,68,0.2)}
.status-badge.conflict{background:rgba(245,158,11,0.12);color:var(--accent-amber);border:1px solid rgba(245,158,11,0.2)}
.status-badge.wrong_gw{background:rgba(167,139,250,0.12);color:var(--accent-purple);border:1px solid rgba(167,139,250,0.2)}
.status-badge.extra{background:rgba(34,211,238,0.12);color:var(--accent-cyan);border:1px solid rgba(34,211,238,0.2)}

/* 状态点 */
.status-dot{width:6px;height:6px;border-radius:50%;display:inline-block}
.status-dot.ok{background:var(--accent-green)}
.status-dot.missing{background:var(--accent-red);animation:blink 1.5s ease-in-out infinite}
.status-dot.conflict{background:var(--accent-amber)}
.status-dot.wrong_gw{background:var(--accent-purple)}
.status-dot.extra{background:var(--accent-cyan)}
@keyframes blink{0%,100%{opacity:1}50%{opacity:0.3}}

/* 修复区域 */
.fix-bar{
  grid-column:span 4;
  display:flex;justify-content:space-between;align-items:center;
  padding:16px 20px;border-radius:var(--radius);
  background:rgba(239,68,68,0.06);border:1px solid rgba(239,68,68,0.15);
}
.fix-bar.hidden{display:none}
.fix-msg{color:var(--accent-red);font-size:0.85rem;font-weight:500}
.fix-result{
  grid-column:span 4;padding:16px 20px;border-radius:var(--radius);
  background:var(--bg-card);border:1px solid var(--border-color);
  font-family:var(--font-mono);font-size:0.8rem;
  white-space:pre-line;line-height:1.8;
}
.fix-result.hidden{display:none}

/* 加载状态 */
.loading{
  display:flex;align-items:center;justify-content:center;
  padding:60px;color:var(--text-muted);
}
.spinner{
  width:24px;height:24px;border:2px solid var(--border-color);
  border-top-color:var(--accent-blue);border-radius:50%;
  animation:spin 0.8s linear infinite;margin-right:12px;
}
@keyframes spin{to{transform:rotate(360deg)}}

/* 淡入动画 */
.fade-in{animation:fadeIn 0.4s ease-out forwards;opacity:0}
.fade-in:nth-child(1){animation-delay:0.05s}
.fade-in:nth-child(2){animation-delay:0.1s}
.fade-in:nth-child(3){animation-delay:0.15s}
.fade-in:nth-child(4){animation-delay:0.2s}
@keyframes fadeIn{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:translateY(0)}}

/* 空状态 */
.empty-state{
  text-align:center;padding:40px;color:var(--text-muted);
  font-size:0.85rem;
}

/* Toast 通知 */
.toast{
  position:fixed;bottom:24px;right:24px;z-index:100;
  padding:12px 20px;border-radius:var(--radius-sm);
  background:var(--bg-card);border:1px solid var(--border-color);
  box-shadow:0 8px 32px rgba(0,0,0,0.3);
  font-size:0.8rem;opacity:0;transform:translateY(20px);
  transition:all 0.3s ease;pointer-events:none;
}
.toast.show{opacity:1;transform:translateY(0);pointer-events:auto}
.toast.success{border-color:rgba(16,185,129,0.3);color:var(--accent-green)}
.toast.error{border-color:rgba(239,68,68,0.3);color:var(--accent-red)}

/* 响应式 */
@media(max-width:1024px){
  .bento-grid{grid-template-columns:1fr}
  .card-status,.card-summary,.card-routes,.fix-bar,.fix-result{grid-column:span 1}
  .status-grid{grid-template-columns:repeat(2,1fr)}
}
@media(max-width:640px){
  .app{padding:16px}
  .status-grid{grid-template-columns:1fr}
  .summary-pills{flex-direction:column}
  .routes-table{font-size:0.7rem}
  .routes-table th,.routes-table td{padding:8px}
}
</style>
</head>
<body>
<div class="app">
  <!-- Header -->
  <header class="header">
    <div class="header-left">
      <div class="logo">DC</div>
      <div class="header-title">
        <h1>Docker Connector</h1>
        <p>Route Verification Dashboard</p>
      </div>
    </div>
    <div class="header-right">
      <div class="refresh-indicator">
        <span class="pulse-dot"></span>
        <span id="lastUpdate">--</span>
      </div>
      <button class="btn" onclick="fetchStatus()" id="refreshBtn">&#x21bb; Refresh</button>
    </div>
  </header>

  <!-- Content -->
  <div class="bento-grid" id="content">
    <div class="loading" style="grid-column:span 4">
      <div class="spinner"></div>Loading...
    </div>
  </div>
</div>

<!-- Toast -->
<div class="toast" id="toast"></div>

<script>
let autoRefreshTimer = null;
const AUTO_REFRESH_INTERVAL = 5000;

async function fetchStatus() {
  try {
    const res = await fetch('/api/status');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const data = await res.json();
    render(data);
    document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
  } catch(e) {
    showToast('获取状态失败: ' + e.message, 'error');
  }
}

function render(data) {
  const c = data.connector;
  const r = data.routes;
  const s = r.summary;
  const hasMissing = s.missing > 0;

  let html = '';

  // 状态卡片
  html += '<div class="card card-status fade-in"><div class="card-header"><span class="card-title">Connector Status</span><span class="card-icon">&#x1f50c;</span></div>';
  html += '<div class="status-grid">';
  html += statItem('运行时间', c.uptime || '--');
  html += statItem('客户端', c.client_connected ? c.client_addr : '未连接', c.client_connected ? 'connected' : 'disconnected');
  html += statItem('TUN 接口', c.tun_interface || '--');
  html += statItem('Peer IP', c.peer_ip || '--');
  html += statItem('Local IP', c.local_ip || '--');
  html += statItem('UDP 端口', c.udp_port || '--');
  html += statItem('配置文件', shortPath(c.config_file));
  html += statItem('路由总数', s.total);
  html += '</div></div>';

  // 路由摘要
  html += '<div class="card card-summary fade-in"><div class="card-header"><span class="card-title">Route Verification Summary</span><span class="card-icon">&#x1f6e1;</span></div>';
  html += '<div class="summary-pills">';
  html += pill('ok', '✓ OK', s.ok);
  html += pill('missing', '✗ Missing', s.missing);
  html += pill('conflict', '⚠ Conflict', s.conflict);
  html += pill('wrong', '? Wrong GW', s.wrong_gw);
  html += pill('extra', '+ Extra', s.extra);
  html += '</div></div>';

  // 修复条
  if (hasMissing) {
    html += '<div class="fix-bar fade-in" id="fixBar">';
    html += '<span class="fix-msg">&#x26a0;&#xfe0f; 检测到 ' + s.missing + ' 条路由缺失，需要修复</span>';
    html += '<button class="btn btn-primary" onclick="fixRoutes()" id="fixBtn">&#x1f527; 一键修复</button>';
    html += '</div>';
  }

  // 修复结果（初始隐藏）
  html += '<div class="fix-result hidden" id="fixResult"></div>';

  // 路由表
  html += '<div class="card card-routes fade-in"><div class="card-header"><span class="card-title">Route Details</span><span class="card-icon">&#x1f6e3;</span></div>';
  if (r.routes && r.routes.length > 0) {
    html += '<table class="routes-table"><thead><tr>';
    html += '<th>Network</th><th>Status</th><th>System Gateway</th><th>System Interface</th><th>Expected Gateway</th>';
    html += '</tr></thead><tbody>';
    // 排序：missing 优先，然后 conflict，最后 ok
    const order = {missing:0, conflict:1, wrong_gw:2, extra:3, ok:4};
    const sorted = [...r.routes].sort((a,b) => (order[a.status]||99) - (order[b.status]||99));
    for (const rt of sorted) {
      html += '<tr>';
      html += '<td>' + rt.network + '</td>';
      html += '<td>' + statusBadge(rt.status) + '</td>';
      html += '<td>' + (rt.system_gateway || '<span style="color:var(--text-muted)">—</span>') + '</td>';
      html += '<td>' + (rt.system_interface || '<span style="color:var(--text-muted)">—</span>') + '</td>';
      html += '<td>' + (rt.expected_gateway || '<span style="color:var(--text-muted)">—</span>') + '</td>';
      html += '</tr>';
    }
    html += '</tbody></table>';
  } else {
    html += '<div class="empty-state">暂无路由数据</div>';
  }
  html += '</div>';

  document.getElementById('content').innerHTML = html;
}

function statItem(label, value, cls) {
  const valCls = cls ? ' ' + cls : '';
  return '<div class="stat-item"><div class="stat-label">' + label + '</div><div class="stat-value' + valCls + '">' + escapeHtml(String(value)) + '</div></div>';
}

function pill(type, label, count) {
  return '<div class="pill ' + type + '"><span class="count">' + count + '</span><span>' + label + '</span></div>';
}

function statusBadge(status) {
  const labels = {ok:'OK', missing:'MISSING', conflict:'CONFLICT', wrong_gw:'WRONG GW', extra:'EXTRA'};
  return '<span class="status-badge ' + status + '"><span class="status-dot ' + status + '"></span>' + (labels[status] || status) + '</span>';
}

function shortPath(p) {
  if (!p) return '--';
  const parts = p.split('/');
  if (parts.length > 3) return '.../' + parts.slice(-2).join('/');
  return p;
}

function escapeHtml(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

async function fixRoutes() {
  const btn = document.getElementById('fixBtn');
  if (btn) { btn.disabled = true; btn.textContent = '修复中...'; }

  try {
    const res = await fetch('/api/routes/fix', {method:'POST'});
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const data = await res.json();

    // 显示结果
    const el = document.getElementById('fixResult');
    if (el) {
      el.classList.remove('hidden');
      let msg = '修复结果: ✅ 成功 ' + data.fixed + ' 条, ❌ 失败 ' + data.failed + ' 条\n\n';
      if (data.details) msg += data.details.join('\n');
      el.textContent = msg;
    }

    showToast('修复完成: ' + data.fixed + ' 条路由已添加', 'success');

    // 刷新状态
    setTimeout(fetchStatus, 1000);
  } catch(e) {
    showToast('修复失败: ' + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = '🔧 一键修复'; }
  }
}

function showToast(msg, type) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast show ' + (type || '');
  setTimeout(() => { t.className = 'toast'; }, 3000);
}

// 自动刷新
function startAutoRefresh() {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer);
  autoRefreshTimer = setInterval(fetchStatus, AUTO_REFRESH_INTERVAL);
}

// 初始化
fetchStatus();
startAutoRefresh();
</script>
</body>
</html>`
