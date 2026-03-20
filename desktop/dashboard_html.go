package main

// dashboardHTML 内嵌的 Dashboard 前端页面（v2 — 差量更新，无闪烁）
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
  --bg-secondary:#111118;
  --bg-card:#15151e;
  --bg-card-hover:#1a1a26;
  --bg-card-alt:#131320;
  --border-color:#232336;
  --border-subtle:#1c1c2e;
  --border-glow:rgba(99,102,241,0.3);
  --text-primary:#e4e4ef;
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
  --radius:14px;
  --radius-sm:10px;
  --radius-xs:6px;
  --shadow-card:0 2px 12px rgba(0,0,0,0.25);
  --shadow-glow:0 0 24px rgba(99,102,241,0.08);
  --transition-fast:0.18s ease;
  --transition-normal:0.3s ease;
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
    linear-gradient(rgba(99,102,241,0.025) 1px,transparent 1px),
    linear-gradient(90deg,rgba(99,102,241,0.025) 1px,transparent 1px);
  background-size:64px 64px;
}
/* 渐变光晕 */
body::after{
  content:'';position:fixed;top:-300px;right:-200px;width:700px;height:700px;
  background:radial-gradient(circle,rgba(99,102,241,0.06) 0%,transparent 70%);
  z-index:0;pointer-events:none;
}

/* ===== 刷新倒计时环 ===== */
.refresh-ring{
  width:18px;height:18px;position:relative;flex-shrink:0;
}
.refresh-ring svg{width:100%;height:100%;transform:rotate(-90deg)}
.refresh-ring circle{
  fill:none;stroke-width:2;cx:9;cy:9;r:7;
}
.refresh-ring .ring-bg{stroke:var(--border-subtle)}
.refresh-ring .ring-fg{
  stroke:var(--accent-blue);
  stroke-dasharray:43.98;/* 2*PI*7 */
  stroke-dashoffset:0;
  stroke-linecap:round;
  transition:stroke-dashoffset 0.3s linear;
}
.countdown-text{
  font-family:var(--font-mono);font-size:0.65rem;color:var(--text-muted);
  min-width:18px;text-align:right;
}

.app{position:relative;z-index:1;max-width:1400px;margin:0 auto;padding:20px 32px 40px}

/* ===== Header ===== */
.header{
  display:flex;justify-content:space-between;align-items:center;
  padding:16px 0 20px;margin-bottom:20px;
  border-bottom:1px solid var(--border-subtle);
}
.header-left{display:flex;align-items:center;gap:14px}
.logo{
  width:38px;height:38px;border-radius:10px;
  background:linear-gradient(135deg,var(--accent-blue),var(--accent-purple));
  display:flex;align-items:center;justify-content:center;
  font-size:16px;font-weight:700;color:#fff;
  box-shadow:0 4px 16px rgba(99,102,241,0.25);
}
.header-title h1{font-size:1.15rem;font-weight:600;letter-spacing:-0.02em}
.header-title p{font-size:0.75rem;color:var(--text-muted);margin-top:1px}
.header-right{display:flex;align-items:center;gap:14px}
.refresh-indicator{
  display:flex;align-items:center;gap:8px;
  font-size:0.72rem;color:var(--text-muted);font-family:var(--font-mono);
}
.pulse-dot{
  width:7px;height:7px;border-radius:50%;background:var(--accent-green);
  box-shadow:0 0 8px rgba(16,185,129,0.5);
  animation:pulse 2s ease-in-out infinite;
}
.pulse-dot.error{background:var(--accent-red);box-shadow:0 0 8px rgba(239,68,68,0.5)}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:0.5;transform:scale(0.75)}}

.btn{
  padding:7px 14px;border-radius:var(--radius-xs);border:1px solid var(--border-color);
  background:var(--bg-card);color:var(--text-primary);cursor:pointer;
  font-size:0.78rem;font-family:var(--font-sans);font-weight:500;
  transition:all var(--transition-fast);display:inline-flex;align-items:center;gap:5px;
}
.btn:hover{border-color:var(--accent-blue);background:var(--bg-card-hover)}
.btn:active{transform:scale(0.97)}
.btn-primary{
  background:var(--accent-blue);border-color:var(--accent-blue);color:#fff;
}
.btn-primary:hover{background:#5558e6;box-shadow:0 0 20px rgba(99,102,241,0.3)}
.btn:disabled{opacity:0.5;cursor:not-allowed;transform:none}

/* ===== 主布局 ===== */
.dashboard-grid{
  display:grid;
  grid-template-columns:1fr;
  gap:16px;
}

/* ===== Hero 状态区 — 突出关键指标 ===== */
.hero-stats{
  display:grid;
  grid-template-columns:repeat(4,1fr);
  gap:14px;
}
.hero-card{
  background:var(--bg-card);
  border:1px solid var(--border-color);
  border-radius:var(--radius);
  padding:20px 22px;
  position:relative;overflow:hidden;
  transition:border-color var(--transition-normal), background var(--transition-normal);
}
.hero-card:hover{border-color:rgba(99,102,241,0.2);background:var(--bg-card-hover)}
.hero-card::before{
  content:'';position:absolute;top:0;left:0;right:0;height:2px;
  border-radius:var(--radius) var(--radius) 0 0;
  opacity:0;transition:opacity var(--transition-normal);
}
.hero-card:hover::before{opacity:1}
.hero-card.blue::before{background:linear-gradient(90deg,var(--accent-blue),var(--accent-cyan))}
.hero-card.green::before{background:linear-gradient(90deg,var(--accent-green),var(--accent-cyan))}
.hero-card.purple::before{background:linear-gradient(90deg,var(--accent-purple),var(--accent-blue))}
.hero-card.amber::before{background:linear-gradient(90deg,var(--accent-amber),var(--accent-red))}

.hero-label{
  font-size:0.68rem;color:var(--text-muted);text-transform:uppercase;
  letter-spacing:0.08em;margin-bottom:10px;font-weight:500;
}
.hero-value{
  font-family:var(--font-mono);font-size:1.35rem;font-weight:700;
  color:var(--text-primary);transition:color var(--transition-fast);
  line-height:1.2;
}
.hero-value.connected{color:var(--accent-green)}
.hero-value.disconnected{color:var(--accent-red)}
.hero-sub{
  font-size:0.7rem;color:var(--text-muted);margin-top:6px;
  font-family:var(--font-mono);
}

/* ===== 次要信息栏 ===== */
.info-bar{
  display:grid;grid-template-columns:repeat(4,1fr);gap:14px;
}
.info-item{
  background:var(--bg-card-alt);
  border:1px solid var(--border-subtle);
  border-radius:var(--radius-sm);
  padding:14px 18px;
  transition:border-color var(--transition-fast);
}
.info-item:hover{border-color:var(--border-color)}
.info-label{
  font-size:0.66rem;color:var(--text-muted);text-transform:uppercase;
  letter-spacing:0.07em;margin-bottom:6px;font-weight:500;
}
.info-value{
  font-family:var(--font-mono);font-size:0.88rem;font-weight:500;
  color:var(--text-secondary);transition:color var(--transition-fast);
}

/* ===== 路由摘要 ===== */
.section-card{
  background:var(--bg-card);
  border:1px solid var(--border-color);
  border-radius:var(--radius);
  padding:20px 24px;
  transition:border-color var(--transition-normal);
}
.section-card:hover{border-color:rgba(99,102,241,0.15)}
.section-header{
  display:flex;justify-content:space-between;align-items:center;
  margin-bottom:18px;
}
.section-title{
  font-size:0.78rem;font-weight:600;color:var(--text-secondary);
  text-transform:uppercase;letter-spacing:0.05em;
  display:flex;align-items:center;gap:8px;
}
.section-icon{font-size:1rem;opacity:0.7}

.summary-pills{display:flex;gap:10px;flex-wrap:wrap}
.pill{
  display:flex;align-items:center;gap:8px;
  padding:10px 18px;border-radius:50px;
  background:var(--bg-secondary);border:1px solid var(--border-subtle);
  font-family:var(--font-mono);font-size:0.82rem;font-weight:500;
  transition:all var(--transition-fast);cursor:default;
}
.pill:hover{border-color:var(--border-color);transform:translateY(-1px)}
.pill .count{font-size:1.2rem;font-weight:700;transition:color var(--transition-fast)}
.pill .label{color:var(--text-secondary)}
.pill.ok .count{color:var(--accent-green)}
.pill.missing .count{color:var(--accent-red)}
.pill.conflict .count{color:var(--accent-amber)}
.pill.wrong .count{color:var(--accent-purple)}
.pill.extra .count{color:var(--accent-cyan)}

/* 全部 OK 庆祝态 */
.all-ok-banner{
  display:none;align-items:center;gap:10px;
  padding:12px 20px;border-radius:var(--radius-sm);
  background:rgba(16,185,129,0.06);border:1px solid rgba(16,185,129,0.15);
  color:var(--accent-green);font-size:0.85rem;font-weight:500;
  margin-top:14px;
}
.all-ok-banner.show{display:flex}
.all-ok-icon{font-size:1.3rem}

/* ===== 修复区域 ===== */
.fix-bar{
  display:none;justify-content:space-between;align-items:center;
  padding:14px 20px;border-radius:var(--radius-sm);
  background:rgba(239,68,68,0.05);border:1px solid rgba(239,68,68,0.12);
}
.fix-bar.show{display:flex}
.fix-msg{color:var(--accent-red);font-size:0.82rem;font-weight:500}
.fix-result-box{
  display:none;padding:14px 20px;border-radius:var(--radius-sm);
  background:var(--bg-card-alt);border:1px solid var(--border-subtle);
  font-family:var(--font-mono);font-size:0.78rem;
  white-space:pre-line;line-height:1.8;color:var(--text-secondary);
}
.fix-result-box.show{display:block}

/* ===== 路由表格 ===== */
.routes-table{width:100%;border-collapse:separate;border-spacing:0 3px}
.routes-table th{
  text-align:left;padding:8px 16px;font-size:0.68rem;font-weight:600;
  color:var(--text-muted);text-transform:uppercase;letter-spacing:0.06em;
  border-bottom:1px solid var(--border-subtle);
}
.routes-table td{
  padding:9px 16px;font-family:var(--font-mono);font-size:0.8rem;
  background:var(--bg-card-alt);
  transition:background var(--transition-fast);
}
.routes-table tr td:first-child{border-radius:var(--radius-xs) 0 0 var(--radius-xs)}
.routes-table tr td:last-child{border-radius:0 var(--radius-xs) var(--radius-xs) 0}
.routes-table tbody tr:hover td{background:var(--bg-card-hover)}

/* 行高亮动画已移除，仅保留值级别 value-flash */

/* 状态标记 */
.status-badge{
  display:inline-flex;align-items:center;gap:5px;
  padding:2px 9px;border-radius:50px;font-size:0.72rem;font-weight:600;
  font-family:var(--font-mono);
}
.status-badge.ok{background:rgba(16,185,129,0.1);color:var(--accent-green);border:1px solid rgba(16,185,129,0.18)}
.status-badge.missing{background:rgba(239,68,68,0.1);color:var(--accent-red);border:1px solid rgba(239,68,68,0.18)}
.status-badge.conflict{background:rgba(245,158,11,0.1);color:var(--accent-amber);border:1px solid rgba(245,158,11,0.18)}
.status-badge.wrong_gw{background:rgba(167,139,250,0.1);color:var(--accent-purple);border:1px solid rgba(167,139,250,0.18)}
.status-badge.extra{background:rgba(34,211,238,0.1);color:var(--accent-cyan);border:1px solid rgba(34,211,238,0.18)}

.status-dot{width:5px;height:5px;border-radius:50%;display:inline-block}
.status-dot.ok{background:var(--accent-green)}
.status-dot.missing{background:var(--accent-red);animation:blink 1.5s ease-in-out infinite}
.status-dot.conflict{background:var(--accent-amber)}
.status-dot.wrong_gw{background:var(--accent-purple)}
.status-dot.extra{background:var(--accent-cyan)}
@keyframes blink{0%,100%{opacity:1}50%{opacity:0.3}}

.muted{color:var(--text-muted)}

/* ===== 值变化高亮 ===== */
.value-flash{
  animation:valueFlash 0.5s ease-out;
}
@keyframes valueFlash{
  0%{color:var(--accent-cyan)}
  100%{color:inherit}
}

/* ===== 骨架屏 ===== */
.skeleton{
  position:relative;overflow:hidden;border-radius:var(--radius-xs);
  background:var(--bg-card-alt);
}
.skeleton::after{
  content:'';position:absolute;inset:0;
  background:linear-gradient(90deg,transparent,rgba(255,255,255,0.03),transparent);
  animation:shimmer 1.5s infinite;
}
@keyframes shimmer{0%{transform:translateX(-100%)}100%{transform:translateX(100%)}}
.skeleton-line{height:16px;margin-bottom:10px}
.skeleton-line.w60{width:60%}
.skeleton-line.w80{width:80%}
.skeleton-line.w40{width:40%}
.skeleton-block{height:120px}

/* ===== 空状态 ===== */
.empty-state{
  text-align:center;padding:36px;color:var(--text-muted);font-size:0.82rem;
}

/* ===== Toast ===== */
.toast{
  position:fixed;bottom:24px;right:24px;z-index:100;
  padding:10px 18px;border-radius:var(--radius-sm);
  background:var(--bg-card);border:1px solid var(--border-color);
  box-shadow:0 8px 32px rgba(0,0,0,0.4);
  font-size:0.78rem;opacity:0;transform:translateY(16px);
  transition:all var(--transition-normal);pointer-events:none;
}
.toast.show{opacity:1;transform:translateY(0);pointer-events:auto}
.toast.success{border-color:rgba(16,185,129,0.3);color:var(--accent-green)}
.toast.error{border-color:rgba(239,68,68,0.3);color:var(--accent-red)}

/* ===== Tab 栏 ===== */
.tab-bar{
  display:flex;gap:0;border-bottom:1px solid var(--border-subtle);
  margin-bottom:16px;
}
.tab-btn{
  padding:10px 22px;font-size:0.8rem;font-weight:600;color:var(--text-muted);
  cursor:pointer;border:none;background:none;
  border-bottom:2px solid transparent;
  transition:all var(--transition-fast);font-family:var(--font-sans);
  display:flex;align-items:center;gap:6px;
}
.tab-btn:hover{color:var(--text-secondary)}
.tab-btn.active{color:var(--accent-blue);border-bottom-color:var(--accent-blue)}
.tab-panel{display:none}
.tab-panel.active{display:block}

/* VM 状态指示 */
.vm-indicator{
  display:inline-flex;align-items:center;gap:5px;font-size:0.68rem;
  font-family:var(--font-mono);padding:2px 8px;border-radius:50px;
}
.vm-indicator.online{color:var(--accent-green);background:rgba(16,185,129,0.08);border:1px solid rgba(16,185,129,0.15)}
.vm-indicator.offline{color:var(--text-muted);background:var(--bg-card-alt);border:1px solid var(--border-subtle)}
.vm-dot{width:5px;height:5px;border-radius:50%;display:inline-block}
.vm-dot.online{background:var(--accent-green);box-shadow:0 0 6px rgba(16,185,129,0.5)}
.vm-dot.offline{background:var(--text-muted)}

/* ===== VM 链路卡片 ===== */
.vm-links-grid{
  display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));
  gap:14px;
}
.link-card{
  background:var(--bg-card);border:1px solid var(--border-color);
  border-radius:var(--radius);padding:18px 22px;
  transition:all var(--transition-normal);position:relative;overflow:hidden;
}
.link-card:hover{border-color:rgba(99,102,241,0.2);background:var(--bg-card-hover)}
.link-card::before{
  content:'';position:absolute;top:0;left:0;right:0;height:2px;
  border-radius:var(--radius) var(--radius) 0 0;
}
.link-card.active::before{background:linear-gradient(90deg,var(--accent-green),var(--accent-cyan))}
.link-card.partial::before{background:linear-gradient(90deg,var(--accent-amber),var(--accent-red))}
.link-card.inactive::before{background:var(--border-subtle)}

.link-card-header{
  display:flex;justify-content:space-between;align-items:center;
  margin-bottom:12px;
}
.link-name{
  font-family:var(--font-mono);font-size:0.92rem;font-weight:600;
  color:var(--text-primary);
}
.link-status-badge{
  display:inline-flex;align-items:center;gap:4px;padding:2px 10px;
  border-radius:50px;font-size:0.68rem;font-weight:600;font-family:var(--font-mono);
}
.link-status-badge.active{background:rgba(16,185,129,0.1);color:var(--accent-green);border:1px solid rgba(16,185,129,0.18)}
.link-status-badge.partial{background:rgba(245,158,11,0.1);color:var(--accent-amber);border:1px solid rgba(245,158,11,0.18)}
.link-status-badge.inactive{background:rgba(136,136,160,0.1);color:var(--text-muted);border:1px solid var(--border-subtle)}
.link-status-badge.error{background:rgba(239,68,68,0.1);color:var(--accent-red);border:1px solid rgba(239,68,68,0.18)}

.link-progress{
  height:3px;background:var(--bg-secondary);border-radius:2px;margin-bottom:12px;overflow:hidden;
}
.link-progress-fill{
  height:100%;border-radius:2px;transition:width 0.4s ease;
}
.link-progress-fill.active{background:var(--accent-green)}
.link-progress-fill.partial{background:var(--accent-amber)}
.link-progress-fill.inactive{background:var(--text-muted);width:0!important}

.link-stats{
  font-family:var(--font-mono);font-size:0.72rem;color:var(--text-muted);
  margin-bottom:14px;
}

.link-actions{
  display:flex;gap:8px;
}
.link-actions .btn{font-size:0.72rem;padding:5px 12px}
.btn-apply{border-color:rgba(16,185,129,0.3);color:var(--accent-green)}
.btn-apply:hover{background:rgba(16,185,129,0.1);border-color:var(--accent-green)}
.btn-revert{border-color:rgba(239,68,68,0.3);color:var(--accent-red)}
.btn-revert:hover{background:rgba(239,68,68,0.1);border-color:var(--accent-red)}

/* VM 链路详情表 */
.link-details-table{width:100%;border-collapse:separate;border-spacing:0 2px;margin-top:12px}
.link-details-table th{
  text-align:left;padding:6px 12px;font-size:0.64rem;font-weight:600;
  color:var(--text-muted);text-transform:uppercase;letter-spacing:0.06em;
  border-bottom:1px solid var(--border-subtle);
}
.link-details-table td{
  padding:6px 12px;font-family:var(--font-mono);font-size:0.74rem;
  background:var(--bg-card-alt);transition:background var(--transition-fast);
}
.link-details-table tr td:first-child{border-radius:var(--radius-xs) 0 0 var(--radius-xs)}
.link-details-table tr td:last-child{border-radius:0 var(--radius-xs) var(--radius-xs) 0}
.link-details-table tbody tr:hover td{background:var(--bg-card-hover)}

/* VM 离线遮罩 */
.vm-offline-overlay{
  display:flex;flex-direction:column;align-items:center;justify-content:center;
  padding:60px 20px;color:var(--text-muted);text-align:center;
}
.vm-offline-overlay .offline-icon{font-size:2.5rem;margin-bottom:16px;opacity:0.4}
.vm-offline-overlay .offline-title{font-size:1rem;font-weight:600;margin-bottom:6px}
.vm-offline-overlay .offline-desc{font-size:0.78rem;line-height:1.6;max-width:400px}

/* ===== 网络拓扑图 ===== */
.topo-card{
  background:var(--bg-card);border:1px solid var(--border-color);border-radius:var(--radius);
  padding:20px 24px;margin-bottom:14px;
  transition:border-color var(--transition-normal);
}
.topo-card:hover{border-color:rgba(99,102,241,0.15)}
.topo-svg{
  width:100%;max-width:900px;margin:0 auto;display:block;
}
.topo-zone{
  cursor:default;transition:all var(--transition-fast);
}
.topo-zone:hover .topo-zone-bg{filter:brightness(1.15)}
.topo-zone-bg{
  rx:14;ry:14;stroke-width:1.5;
  transition:all var(--transition-fast);
}
.topo-zone-icon{font-size:22px}
.topo-zone-label{
  font-family:var(--font-sans);font-size:13px;font-weight:600;
  fill:var(--text-primary);
}
.topo-zone-sub{
  font-family:var(--font-mono);font-size:10px;
  fill:var(--text-muted);
}
.topo-link{
  stroke-width:2.5;fill:none;stroke-linecap:round;
  transition:stroke 0.4s ease, opacity 0.4s ease;
}
.topo-link.active{stroke:var(--accent-green);opacity:1}
.topo-link.partial{stroke:var(--accent-amber);opacity:0.85;stroke-dasharray:8 4}
.topo-link.inactive{stroke:var(--text-muted);opacity:0.3;stroke-dasharray:4 4}
.topo-link-label{
  font-family:var(--font-mono);font-size:9.5px;font-weight:600;
  transition:fill 0.4s ease;
}
.topo-link-label.active{fill:var(--accent-green)}
.topo-link-label.partial{fill:var(--accent-amber)}
.topo-link-label.inactive{fill:var(--text-muted)}
.topo-link-hit{
  stroke:transparent;stroke-width:16;fill:none;cursor:pointer;
}
.topo-legend{
  display:flex;gap:16px;justify-content:center;margin-top:12px;
}
.topo-legend-item{
  display:flex;align-items:center;gap:5px;
  font-family:var(--font-mono);font-size:0.68rem;color:var(--text-muted);
}
.topo-legend-dot{
  width:10px;height:3px;border-radius:2px;
}
.topo-legend-dot.active{background:var(--accent-green)}
.topo-legend-dot.partial{background:var(--accent-amber)}
.topo-legend-dot.inactive{background:var(--text-muted)}

/* ===== 响应式 ===== */
@media(max-width:1024px){
  .hero-stats{grid-template-columns:repeat(2,1fr)}
  .info-bar{grid-template-columns:repeat(2,1fr)}
  .vm-links-grid{grid-template-columns:1fr}
}
@media(max-width:640px){
  .app{padding:12px 16px 32px}
  .hero-stats{grid-template-columns:1fr}
  .info-bar{grid-template-columns:1fr}
  .summary-pills{flex-direction:column}
  .routes-table th,.routes-table td{padding:7px 10px;font-size:0.7rem}
  .header{flex-direction:column;gap:12px;align-items:flex-start}
  .vm-links-grid{grid-template-columns:1fr}
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
        <span class="pulse-dot" id="pulseDot"></span>
        <span id="lastUpdate">--</span>
        <span class="countdown-text" id="countdownText"></span>
      </div>
      <button class="btn" onclick="manualRefresh()" id="refreshBtn">
        <div class="refresh-ring" id="refreshRing">
          <svg viewBox="0 0 18 18"><circle class="ring-bg"/><circle class="ring-fg" id="ringFg"/></svg>
        </div>
        Refresh
      </button>
    </div>
  </header>

  <!-- Dashboard 主内容 -->
  <div class="dashboard-grid" id="dashboardGrid">

    <!-- === 骨架屏（首次加载显示，数据到达后隐藏） === -->
    <div id="skeletonView">
      <div class="hero-stats" style="margin-bottom:16px">
        <div class="hero-card"><div class="skeleton skeleton-line w40" style="margin-bottom:14px"></div><div class="skeleton skeleton-line w60" style="height:24px"></div></div>
        <div class="hero-card"><div class="skeleton skeleton-line w40" style="margin-bottom:14px"></div><div class="skeleton skeleton-line w60" style="height:24px"></div></div>
        <div class="hero-card"><div class="skeleton skeleton-line w40" style="margin-bottom:14px"></div><div class="skeleton skeleton-line w60" style="height:24px"></div></div>
        <div class="hero-card"><div class="skeleton skeleton-line w40" style="margin-bottom:14px"></div><div class="skeleton skeleton-line w60" style="height:24px"></div></div>
      </div>
      <div class="section-card"><div class="skeleton skeleton-block"></div></div>
    </div>

    <!-- === 真实内容（初始隐藏） === -->
    <div id="realContent" style="display:none">

      <!-- Hero 状态卡片 — 4 个关键指标 -->
      <div class="hero-stats">
        <div class="hero-card blue">
          <div class="hero-label">运行时间</div>
          <div class="hero-value" id="valUptime">--</div>
          <div class="hero-sub" id="valUdpPort">--</div>
        </div>
        <div class="hero-card green">
          <div class="hero-label">客户端连接</div>
          <div class="hero-value" id="valClient">--</div>
          <div class="hero-sub" id="valClientAddr">&nbsp;</div>
        </div>
        <div class="hero-card purple">
          <div class="hero-label">TUN 接口</div>
          <div class="hero-value" id="valTun">--</div>
          <div class="hero-sub" id="valPeerIp">--</div>
        </div>
        <div class="hero-card amber">
          <div class="hero-label">路由总数</div>
          <div class="hero-value" id="valRouteTotal">--</div>
          <div class="hero-sub" id="valRouteHealth">--</div>
        </div>
      </div>

      <!-- 次要信息栏 -->
      <div class="info-bar">
        <div class="info-item">
          <div class="info-label">Local IP</div>
          <div class="info-value" id="valLocalIp">--</div>
        </div>
        <div class="info-item">
          <div class="info-label">Peer IP</div>
          <div class="info-value" id="valPeerIpFull">--</div>
        </div>
        <div class="info-item">
          <div class="info-label">配置文件</div>
          <div class="info-value" id="valConfigFile">--</div>
        </div>
        <div class="info-item">
          <div class="info-label">接口名称</div>
          <div class="info-value" id="valTunFull">--</div>
        </div>
      </div>

      <!-- Tab 栏 -->
      <div class="tab-bar">
        <button class="tab-btn active" data-tab="routes" onclick="switchTab('routes')">
          &#x1f6e3; Routes
        </button>
        <button class="tab-btn" data-tab="vmlinks" onclick="switchTab('vmlinks')">
          &#x1f517; VM Links
          <span class="vm-indicator offline" id="vmTabIndicator"><span class="vm-dot offline"></span>offline</span>
        </button>
      </div>

      <!-- ===== Routes 面板 ===== -->
      <div class="tab-panel active" id="panelRoutes">

      <!-- 路由摘要 -->
      <div class="section-card">
        <div class="section-header">
          <span class="section-title"><span class="section-icon">&#x1f6e1;</span> Route Verification</span>
        </div>
        <div class="summary-pills" id="summaryPills">
          <div class="pill ok"><span class="count" id="cntOk">0</span><span class="label">&#x2713; OK</span></div>
          <div class="pill missing"><span class="count" id="cntMissing">0</span><span class="label">&#x2717; Missing</span></div>
          <div class="pill conflict"><span class="count" id="cntConflict">0</span><span class="label">&#x26a0; Conflict</span></div>
          <div class="pill wrong"><span class="count" id="cntWrong">0</span><span class="label">&#x2753; Wrong GW</span></div>
          <div class="pill extra"><span class="count" id="cntExtra">0</span><span class="label">&#x2b; Extra</span></div>
        </div>
        <div class="all-ok-banner" id="allOkBanner">
          <span class="all-ok-icon">&#x2728;</span>
          <span>所有路由均已正确配置，一切正常！</span>
        </div>
      </div>

      <!-- 修复区域 -->
      <div class="fix-bar" id="fixBar">
        <span class="fix-msg" id="fixMsg">&#x26a0;&#xfe0f; 检测到路由缺失</span>
        <button class="btn btn-primary" onclick="fixRoutes()" id="fixBtn">&#x1f527; 一键修复</button>
      </div>
      <div class="fix-result-box" id="fixResult"></div>

      <!-- 路由详情表 -->
      <div class="section-card">
        <div class="section-header">
          <span class="section-title"><span class="section-icon">&#x1f6e3;</span> Route Details</span>
        </div>
        <table class="routes-table" id="routesTable">
          <thead>
            <tr>
              <th>Network</th>
              <th>Status</th>
              <th>System Gateway</th>
              <th>System Interface</th>
              <th>Expected Gateway</th>
            </tr>
          </thead>
          <tbody id="routesTbody"></tbody>
        </table>
        <div class="empty-state" id="routesEmpty" style="display:none">暂无路由数据</div>
      </div>

      </div><!-- /panelRoutes -->

      <!-- ===== VM Links 面板 ===== -->
      <div class="tab-panel" id="panelVmlinks">
        <!-- VM 离线时显示 -->
        <div class="vm-offline-overlay" id="vmOfflineOverlay">
          <div class="offline-icon">&#x1f50c;</div>
          <div class="offline-title">VM 端未连接</div>
          <div class="offline-desc">
            请确认 Lima VM 中的 docker-connector 以 <code>-mode=service</code> 运行。<br>
            服务启动后将自动连接。
          </div>
        </div>
        <!-- VM 在线时显示 -->
        <div id="vmOnlineContent" style="display:none">
          <!-- 网络拓扑图 -->
          <div class="topo-card">
            <div class="section-header">
              <span class="section-title"><span class="section-icon">&#x1f5fa;</span> Network Topology</span>
              <span class="vm-indicator online" id="vmStatusBadge"><span class="vm-dot online"></span>connected</span>
            </div>
            <svg class="topo-svg" viewBox="0 0 800 400" id="topoSvg">
              <defs>
                <filter id="glow"><feGaussianBlur stdDeviation="3" result="g"/><feMerge><feMergeNode in="g"/><feMergeNode in="SourceGraphic"/></feMerge></filter>
                <marker id="arrowGreen" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto"><path d="M0,0 L8,3 L0,6" fill="var(--accent-green)" opacity="0.6"/></marker>
                <marker id="arrowAmber" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto"><path d="M0,0 L8,3 L0,6" fill="var(--accent-amber)" opacity="0.6"/></marker>
                <marker id="arrowGray" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto"><path d="M0,0 L8,3 L0,6" fill="var(--text-muted)" opacity="0.4"/></marker>
              </defs>
              <!-- 链路连线（底层） -->
              <g id="topoLinks">
                <!-- internet: Docker ↔ Internet -->
                <path id="topo-link-internet" class="topo-link inactive" d="M540,120 Q680,60 700,200"/>
                <path class="topo-link-hit" d="M540,120 Q680,60 700,200" onclick="vmLinkDetail('internet')"/>
                <text id="topo-label-internet" class="topo-link-label inactive" x="650" y="100" text-anchor="middle">internet</text>
                <!-- host-docker: Host ↔ Docker -->
                <path id="topo-link-host-docker" class="topo-link inactive" d="M250,200 L420,120"/>
                <path class="topo-link-hit" d="M250,200 L420,120" onclick="vmLinkDetail('host-docker')"/>
                <text id="topo-label-host-docker" class="topo-link-label inactive" x="325" y="145" text-anchor="middle">host-docker</text>
                <!-- host-k8s: Host ↔ K8s -->
                <path id="topo-link-host-k8s" class="topo-link inactive" d="M250,240 L420,300"/>
                <path class="topo-link-hit" d="M250,240 L420,300" onclick="vmLinkDetail('host-k8s.service')"/>
                <text id="topo-label-host-k8s" class="topo-link-label inactive" x="320" y="285" text-anchor="middle">host-k8s</text>
                <!-- docker-k8s: Docker ↔ K8s -->
                <path id="topo-link-docker-k8s" class="topo-link inactive" d="M500,160 L500,260"/>
                <path class="topo-link-hit" d="M500,160 L500,260" onclick="vmLinkDetail('docker-k8s.service')"/>
                <text id="topo-label-docker-k8s" class="topo-link-label inactive" x="528" y="215" text-anchor="start">docker-k8s</text>
                <!-- docker-docker: Docker ↔ Docker -->
                <path id="topo-link-docker-docker" class="topo-link inactive" d="M520,90 C580,50 620,50 570,110"/>
                <path class="topo-link-hit" d="M520,90 C580,50 620,50 570,110" onclick="vmLinkDetail('docker-docker')"/>
                <text id="topo-label-docker-docker" class="topo-link-label inactive" x="590" y="62" text-anchor="start">docker-docker</text>
              </g>
              <!-- 域节点 -->
              <g class="topo-zone" id="topoZoneHost">
                <rect class="topo-zone-bg" x="80" y="160" width="170" height="100" fill="#15151e" stroke="var(--accent-blue)" stroke-opacity="0.4"/>
                <text class="topo-zone-icon" x="165" y="200" text-anchor="middle" fill="var(--text-primary)">&#x1f5a5;</text>
                <text class="topo-zone-label" x="165" y="224" text-anchor="middle">Host (macOS)</text>
                <text class="topo-zone-sub" x="165" y="242" text-anchor="middle" id="topoHostSub">via tun0 tunnel</text>
              </g>
              <g class="topo-zone" id="topoZoneDocker">
                <rect class="topo-zone-bg" x="400" y="70" width="170" height="100" fill="#15151e" stroke="var(--accent-cyan)" stroke-opacity="0.4"/>
                <text class="topo-zone-icon" x="485" y="108" text-anchor="middle" fill="var(--text-primary)">&#x1f433;</text>
                <text class="topo-zone-label" x="485" y="132" text-anchor="middle">Docker</text>
                <text class="topo-zone-sub" x="485" y="150" text-anchor="middle" id="topoDockerSub">bridge networks</text>
              </g>
              <g class="topo-zone" id="topoZoneK8s">
                <rect class="topo-zone-bg" x="400" y="250" width="170" height="100" fill="#15151e" stroke="var(--accent-purple)" stroke-opacity="0.4"/>
                <text class="topo-zone-icon" x="485" y="290" text-anchor="middle" fill="var(--text-primary)">&#x2638;</text>
                <text class="topo-zone-label" x="485" y="314" text-anchor="middle">Kubernetes</text>
                <text class="topo-zone-sub" x="485" y="332" text-anchor="middle" id="topoK8sSub">minikube</text>
              </g>
              <g class="topo-zone" id="topoZoneInternet">
                <rect class="topo-zone-bg" x="660" y="160" width="120" height="100" fill="#15151e" stroke="var(--accent-green)" stroke-opacity="0.4"/>
                <text class="topo-zone-icon" x="720" y="200" text-anchor="middle" fill="var(--text-primary)">&#x1f310;</text>
                <text class="topo-zone-label" x="720" y="224" text-anchor="middle">Internet</text>
                <text class="topo-zone-sub" x="720" y="242" text-anchor="middle" id="topoInternetSub">NAT masquerade</text>
              </g>
            </svg>
            <div class="topo-legend">
              <div class="topo-legend-item"><div class="topo-legend-dot active"></div>Active</div>
              <div class="topo-legend-item"><div class="topo-legend-dot partial"></div>Partial</div>
              <div class="topo-legend-item"><div class="topo-legend-dot inactive"></div>Inactive</div>
            </div>
          </div>

          <div class="section-card" style="margin-bottom:14px">
            <div class="section-header">
              <span class="section-title"><span class="section-icon">&#x1f517;</span> VM Link Status</span>
            </div>
            <div class="vm-links-grid" id="vmLinksGrid">
              <!-- JS 动态填充 -->
            </div>
          </div>

          <!-- 链路规则详情 -->
          <div class="section-card" id="vmLinkDetailCard" style="display:none">
            <div class="section-header">
              <span class="section-title" id="vmDetailTitle"><span class="section-icon">&#x1f4cb;</span> Link Details</span>
              <button class="btn" onclick="closeVMLinkDetail()">&#x2715; Close</button>
            </div>
            <table class="link-details-table">
              <thead>
                <tr>
                  <th>Rule</th>
                  <th>Status</th>
                  <th>Type</th>
                </tr>
              </thead>
              <tbody id="vmDetailTbody"></tbody>
            </table>
          </div>
        </div>
      </div><!-- /panelVmlinks -->

    </div><!-- /realContent -->
  </div>
</div>

<!-- Toast -->
<div class="toast" id="toast"></div>

<script>
(function(){
  'use strict';

  // ========== 状态管理 ==========
  const REFRESH_INTERVAL = 5000;
  let refreshTimer = null;
  let progressTimer = null;
  let prevData = null;          // 上一次数据快照
  let fixResultText = '';       // 修复结果持久化
  let firstLoad = true;
  let progressStart = 0;

  // ========== DOM 引用缓存 ==========
  const $ = id => document.getElementById(id);

  // ========== 差量更新核心 ==========
  function updateText(id, newVal) {
    const el = $(id);
    if (!el) return;
    const s = String(newVal);
    if (el.textContent !== s) {
      el.textContent = s;
      // 非首次加载时才播放高亮动画
      if (!firstLoad) {
        el.classList.remove('value-flash');
        void el.offsetWidth; // 强制 reflow 重播动画
        el.classList.add('value-flash');
      }
    }
  }

  function updateClass(id, cls) {
    const el = $(id);
    if (el && el.className !== cls) el.className = cls;
  }

  // ========== 数据获取 ==========
  async function fetchData() {
    try {
      const res = await fetch('/api/status');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      applyData(data);
      $('lastUpdate').textContent = new Date().toLocaleTimeString();
      updateClass('pulseDot', 'pulse-dot');
    } catch(e) {
      updateClass('pulseDot', 'pulse-dot error');
      showToast('获取状态失败: ' + e.message, 'error');
    }
  }

  // ========== 数据应用（差量更新） ==========
  function applyData(data) {
    const c = data.connector;
    const r = data.routes;
    const s = r.summary;

    // 首次加载：隐藏骨架，显示内容
    if (firstLoad) {
      $('skeletonView').style.display = 'none';
      $('realContent').style.display = '';
      firstLoad = false;
    }

    // --- Hero 卡片 ---
    updateText('valUptime', c.uptime || '0m');
    updateText('valUdpPort', 'UDP :' + (c.udp_port || '--'));

    if (c.client_connected) {
      updateText('valClient', '已连接');
      updateClass('valClient', 'hero-value connected');
      updateText('valClientAddr', c.client_addr || '');
    } else {
      updateText('valClient', '未连接');
      updateClass('valClient', 'hero-value disconnected');
      updateText('valClientAddr', '\u00a0');
    }

    updateText('valTun', c.tun_interface || '--');
    updateText('valPeerIp', c.peer_ip ? 'peer ' + c.peer_ip : '--');

    updateText('valRouteTotal', String(s.total));
    if (s.total > 0 && s.missing === 0 && s.conflict === 0 && s.wrong_gw === 0) {
      updateText('valRouteHealth', '100% healthy');
    } else if (s.total > 0) {
      const pct = Math.round((s.ok / s.total) * 100);
      updateText('valRouteHealth', pct + '% healthy');
    } else {
      updateText('valRouteHealth', '--');
    }

    // --- 次要信息 ---
    updateText('valLocalIp', c.local_ip || '--');
    updateText('valPeerIpFull', c.peer_ip || '--');
    updateText('valConfigFile', shortPath(c.config_file));
    updateText('valTunFull', c.tun_interface || '--');

    // --- 路由摘要 ---
    updateText('cntOk', String(s.ok));
    updateText('cntMissing', String(s.missing));
    updateText('cntConflict', String(s.conflict));
    updateText('cntWrong', String(s.wrong_gw));
    updateText('cntExtra', String(s.extra));

    // 全部 OK 庆祝
    const allOk = s.total > 0 && s.missing === 0 && s.conflict === 0 && s.wrong_gw === 0;
    $('allOkBanner').className = 'all-ok-banner' + (allOk ? ' show' : '');

    // --- 修复栏 ---
    if (s.missing > 0) {
      $('fixBar').className = 'fix-bar show';
      updateText('fixMsg', '\u26a0\ufe0f 检测到 ' + s.missing + ' 条路由缺失，需要修复');
    } else {
      $('fixBar').className = 'fix-bar';
    }

    // 修复结果持久化
    if (fixResultText) {
      $('fixResult').className = 'fix-result-box show';
      $('fixResult').textContent = fixResultText;
    }

    // --- 路由表格差量更新 ---
    updateRouteTable(r.routes || []);

    prevData = data;
  }

  // ========== 路由表格差量更新 ==========
  function updateRouteTable(routes) {
    const tbody = $('routesTbody');
    const empty = $('routesEmpty');

    if (!routes || routes.length === 0) {
      tbody.innerHTML = '';
      empty.style.display = '';
      return;
    }
    empty.style.display = 'none';

    // 排序
    const order = {missing:0, conflict:1, wrong_gw:2, extra:3, ok:4};
    const sorted = [...routes].sort((a,b) => (order[a.status]||99) - (order[b.status]||99));

    // 构建新旧映射
    const oldRows = {};
    const existingTrs = tbody.querySelectorAll('tr[data-net]');
    existingTrs.forEach(tr => { oldRows[tr.dataset.net] = tr; });

    const newKeys = new Set();
    const fragment = document.createDocumentFragment();

    for (const rt of sorted) {
      newKeys.add(rt.network);
      const existing = oldRows[rt.network];

      if (existing) {
        // 更新已存在的行（仅差量更新单元格文本，无行级动画）
        updateRowCells(existing, rt);
        fragment.appendChild(existing);
      } else {
        // 新增行
        const tr = createRow(rt);
        fragment.appendChild(tr);
      }
    }

    // 清空并按新顺序添加
    tbody.innerHTML = '';
    tbody.appendChild(fragment);
  }

  function createRow(rt) {
    const tr = document.createElement('tr');
    tr.dataset.net = rt.network;
    tr.dataset.status = rt.status;
    tr.innerHTML =
      '<td>' + esc(rt.network) + '</td>' +
      '<td>' + badgeHTML(rt.status) + '</td>' +
      '<td>' + (rt.system_gateway || '<span class="muted">\u2014</span>') + '</td>' +
      '<td>' + (rt.system_interface || '<span class="muted">\u2014</span>') + '</td>' +
      '<td>' + (rt.expected_gateway || '<span class="muted">\u2014</span>') + '</td>';
    return tr;
  }

  function updateRowCells(tr, rt) {
    let changed = false;
    const tds = tr.querySelectorAll('td');
    if (tds.length < 5) return false;

    // 状态变化
    if (tr.dataset.status !== rt.status) {
      tr.dataset.status = rt.status;
      tds[1].innerHTML = badgeHTML(rt.status);
      changed = true;
    }
    // 网关变化
    const gw = rt.system_gateway || '\u2014';
    if (tds[2].textContent !== gw) {
      tds[2].innerHTML = rt.system_gateway || '<span class="muted">\u2014</span>';
      changed = true;
    }
    // 接口变化
    const iface = rt.system_interface || '\u2014';
    if (tds[3].textContent !== iface) {
      tds[3].innerHTML = rt.system_interface || '<span class="muted">\u2014</span>';
      changed = true;
    }
    return changed;
  }

  function badgeHTML(status) {
    const labels = {ok:'OK', missing:'MISSING', conflict:'CONFLICT', wrong_gw:'WRONG GW', extra:'EXTRA'};
    return '<span class="status-badge ' + status + '"><span class="status-dot ' + status + '"></span>' + (labels[status] || status) + '</span>';
  }

  // ========== 修复路由 ==========
  async function fixRoutes() {
    const btn = $('fixBtn');
    if (btn) { btn.disabled = true; btn.textContent = '修复中...'; }

    try {
      const res = await fetch('/api/routes/fix', {method:'POST'});
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();

      fixResultText = '修复结果: \u2705 成功 ' + data.fixed + ' 条, \u274c 失败 ' + data.failed + ' 条\n\n';
      if (data.details) fixResultText += data.details.join('\n');

      $('fixResult').className = 'fix-result-box show';
      $('fixResult').textContent = fixResultText;

      showToast('修复完成: ' + data.fixed + ' 条路由已添加', 'success');
      setTimeout(fetchData, 800);
    } catch(e) {
      showToast('修复失败: ' + e.message, 'error');
    } finally {
      if (btn) { btn.disabled = false; btn.textContent = '\ud83d\udd27 一键修复'; }
    }
  }
  // 挂载到 window 供 onclick 使用
  window.fixRoutes = fixRoutes;

  // ========== 环形倒计时 ==========
  const RING_CIRCUMFERENCE = 43.98; // 2*PI*7
  function startProgress() {
    progressStart = Date.now();
    if (progressTimer) cancelAnimationFrame(progressTimer);
    tickProgress();
  }

  function tickProgress() {
    const elapsed = Date.now() - progressStart;
    const ratio = Math.min(elapsed / REFRESH_INTERVAL, 1);
    // 环形：从满到空
    const offset = RING_CIRCUMFERENCE * ratio;
    const fg = $('ringFg');
    if (fg) fg.style.strokeDashoffset = String(offset);
    // 倒计时秒数
    const remaining = Math.max(0, Math.ceil((REFRESH_INTERVAL - elapsed) / 1000));
    const ct = $('countdownText');
    if (ct) ct.textContent = remaining > 0 ? remaining + 's' : '';
    if (ratio < 1) {
      progressTimer = requestAnimationFrame(tickProgress);
    }
  }

  // ========== 自动刷新 ==========
  function scheduleRefresh() {
    if (refreshTimer) clearTimeout(refreshTimer);
    startProgress();
    refreshTimer = setTimeout(async () => {
      await fetchData();
      scheduleRefresh();
    }, REFRESH_INTERVAL);
  }

  function manualRefresh() {
    if (refreshTimer) clearTimeout(refreshTimer);
    const fg = $('ringFg');
    if (fg) fg.style.strokeDashoffset = String(RING_CIRCUMFERENCE);
    const ct = $('countdownText');
    if (ct) ct.textContent = '';
    fetchData().then(() => scheduleRefresh());
  }
  window.manualRefresh = manualRefresh;

  // ========== 工具函数 ==========
  function shortPath(p) {
    if (!p) return '--';
    const parts = p.split('/');
    if (parts.length > 3) return '.../' + parts.slice(-2).join('/');
    return p;
  }

  function esc(s) {
    return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function showToast(msg, type) {
    const t = $('toast');
    t.textContent = msg;
    t.className = 'toast show ' + (type || '');
    setTimeout(() => { t.className = 'toast'; }, 3000);
  }

  // ========== VM Links 模块 ==========
  let vmSSE = null;
  let vmLinksData = null;
  let vmReachable = false;
  let currentTab = 'routes';

  // Tab 切换
  function switchTab(tab) {
    currentTab = tab;
    document.querySelectorAll('.tab-btn').forEach(b => {
      b.classList.toggle('active', b.dataset.tab === tab);
    });
    document.querySelectorAll('.tab-panel').forEach(p => {
      p.classList.toggle('active', p.id === 'panel' + tab.charAt(0).toUpperCase() + tab.slice(1));
    });
    // 首次打开 VM Links 面板时检测并连接 SSE
    if (tab === 'vmlinks') {
      checkVMAndConnect();
    }
  }
  window.switchTab = switchTab;

  // 检测 VM 状态并连接 SSE
  async function checkVMAndConnect() {
    try {
      const res = await fetch('/api/vm/status');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      vmReachable = data.vm_reachable;
      updateVMIndicator(vmReachable);
      if (vmReachable) {
        $('vmOfflineOverlay').style.display = 'none';
        $('vmOnlineContent').style.display = '';
        fetchVMLinks();
        connectVMSSE();
      } else {
        $('vmOfflineOverlay').style.display = '';
        $('vmOnlineContent').style.display = 'none';
        disconnectVMSSE();
      }
    } catch(e) {
      vmReachable = false;
      updateVMIndicator(false);
      $('vmOfflineOverlay').style.display = '';
      $('vmOnlineContent').style.display = 'none';
    }
  }

  // 更新 Tab 上的 VM 指示器
  function updateVMIndicator(online) {
    const ind = $('vmTabIndicator');
    if (!ind) return;
    if (online) {
      ind.className = 'vm-indicator online';
      ind.innerHTML = '<span class="vm-dot online"></span>online';
    } else {
      ind.className = 'vm-indicator offline';
      ind.innerHTML = '<span class="vm-dot offline"></span>offline';
    }
  }

  // 获取 VM 链路数据
  async function fetchVMLinks() {
    try {
      const res = await fetch('/api/vm/links');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      vmLinksData = data;
      renderVMLinks(data);
    } catch(e) {
      showToast('获取 VM 链路失败: ' + e.message, 'error');
    }
  }

  // 更新拓扑图链路状态
  function updateTopoLinks(links) {
    if (!links || links.length === 0) return;
    // 构建 name → status 映射，合并子链路状态
    const statusMap = {};
    for (const link of links) {
      // 解析名称："host-k8s.service" → base = "host-k8s"
      const dot = link.name.indexOf('.');
      const base = dot >= 0 ? link.name.substring(0, dot) : link.name;
      const s = link.status || 'inactive';
      if (!statusMap[base]) {
        statusMap[base] = s;
      } else {
        // 合并：active+active=active, 否则取最差状态
        const prio = {active:0, partial:1, inactive:2};
        const cur = prio[statusMap[base]] || 2;
        const nxt = prio[s] || 2;
        if (nxt > cur) statusMap[base] = s;
      }
    }
    // 更新 SVG 链路元素
    const linkNames = ['internet','host-docker','host-k8s','docker-k8s','docker-docker'];
    for (const name of linkNames) {
      const st = statusMap[name] || 'inactive';
      const pathEl = $('topo-link-' + name);
      const labelEl = $('topo-label-' + name);
      if (pathEl) {
        pathEl.className.baseVal = 'topo-link ' + st;
      }
      if (labelEl) {
        labelEl.className.baseVal = 'topo-link-label ' + st;
      }
    }
  }

  // 渲染 VM 链路卡片
  function renderVMLinks(data) {
    const grid = $('vmLinksGrid');
    if (!grid) return;
    const links = data.links || [];
    // 更新拓扑图
    updateTopoLinks(links);
    if (links.length === 0) {
      grid.innerHTML = '<div class="empty-state">暂无链路数据</div>';
      return;
    }
    grid.innerHTML = '';
    for (const link of links) {
      const card = document.createElement('div');
      card.className = 'link-card ' + (link.status || 'inactive');
      const pct = link.rules_total > 0 ? Math.round((link.rules_active / link.rules_total) * 100) : 0;
      const statusCls = link.status || 'inactive';
      card.innerHTML =
        '<div class="link-card-header">' +
          '<span class="link-name">' + esc(link.name) + '</span>' +
          '<span class="link-status-badge ' + statusCls + '">' + statusCls.toUpperCase() + '</span>' +
        '</div>' +
        '<div class="link-progress"><div class="link-progress-fill ' + statusCls + '" style="width:' + pct + '%"></div></div>' +
        '<div class="link-stats">' + link.rules_active + ' / ' + link.rules_total + ' rules active</div>' +
        '<div class="link-actions">' +
          '<button class="btn btn-apply" onclick="vmLinkApply(\'' + esc(link.name) + '\')">&#x25b6; Apply</button>' +
          '<button class="btn btn-revert" onclick="vmLinkRevert(\'' + esc(link.name) + '\')">&#x23f9; Revert</button>' +
          '<button class="btn" onclick="vmLinkDetail(\'' + esc(link.name) + '\')">&#x1f4cb; Details</button>' +
        '</div>';
      grid.appendChild(card);
    }
  }

  // Apply 链路
  async function vmLinkApply(linkName) {
    try {
      const res = await fetch('/api/vm/apply', {
        method: 'POST',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({link: linkName})
      });
      const data = await res.json();
      if (data.ok) {
        showToast('链路 ' + linkName + ' 已应用', 'success');
      } else {
        showToast('应用失败: ' + (data.message || '未知错误'), 'error');
      }
      fetchVMLinks();
    } catch(e) {
      showToast('Apply 失败: ' + e.message, 'error');
    }
  }
  window.vmLinkApply = vmLinkApply;

  // Revert 链路
  async function vmLinkRevert(linkName) {
    try {
      const res = await fetch('/api/vm/revert', {
        method: 'POST',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({link: linkName})
      });
      const data = await res.json();
      if (data.ok) {
        showToast('链路 ' + linkName + ' 已还原', 'success');
      } else {
        showToast('还原失败: ' + (data.message || '未知错误'), 'error');
      }
      fetchVMLinks();
    } catch(e) {
      showToast('Revert 失败: ' + e.message, 'error');
    }
  }
  window.vmLinkRevert = vmLinkRevert;

  // 链路详情
  async function vmLinkDetail(linkName) {
    const card = $('vmLinkDetailCard');
    const title = $('vmDetailTitle');
    const tbody = $('vmDetailTbody');
    if (!card || !tbody) return;

    title.innerHTML = '<span class="section-icon">&#x1f4cb;</span> ' + esc(linkName) + ' Details';
    tbody.innerHTML = '<tr><td colspan="3" class="muted">Loading...</td></tr>';
    card.style.display = '';

    // 从已有数据中查找
    if (vmLinksData && vmLinksData.links) {
      const link = vmLinksData.links.find(l => l.name === linkName);
      if (link && link.details) {
        renderVMLinkDetails(link.details);
        return;
      }
    }

    // 否则重新获取
    try {
      const res = await fetch('/api/vm/links');
      const data = await res.json();
      const link = (data.links || []).find(l => l.name === linkName);
      if (link && link.details) {
        renderVMLinkDetails(link.details);
      } else {
        tbody.innerHTML = '<tr><td colspan="3" class="muted">无详情数据</td></tr>';
      }
    } catch(e) {
      tbody.innerHTML = '<tr><td colspan="3" class="muted">获取详情失败</td></tr>';
    }
  }
  window.vmLinkDetail = vmLinkDetail;

  function renderVMLinkDetails(details) {
    const tbody = $('vmDetailTbody');
    if (!tbody || !details) return;
    tbody.innerHTML = '';
    for (const d of details) {
      const tr = document.createElement('tr');
      const statusCls = d.active ? 'ok' : 'missing';
      const statusText = d.active ? 'ACTIVE' : 'MISSING';
      tr.innerHTML =
        '<td>' + esc(d.label || '--') + '</td>' +
        '<td><span class="status-badge ' + statusCls + '"><span class="status-dot ' + statusCls + '"></span>' + statusText + '</span></td>' +
        '<td>' + esc(d.type || 'iptables') + '</td>';
      tbody.appendChild(tr);
    }
  }

  function closeVMLinkDetail() {
    const card = $('vmLinkDetailCard');
    if (card) card.style.display = 'none';
  }
  window.closeVMLinkDetail = closeVMLinkDetail;

  // SSE 连接
  function connectVMSSE() {
    if (vmSSE) return; // 已连接
    try {
      vmSSE = new EventSource('/api/vm/links/stream');
      vmSSE.onmessage = function(event) {
        try {
          const data = JSON.parse(event.data);
          vmLinksData = data;
          if (currentTab === 'vmlinks') {
            renderVMLinks(data);
          }
        } catch(e) { /* ignore parse error */ }
      };
      vmSSE.onerror = function() {
        disconnectVMSSE();
        // 5 秒后重试
        setTimeout(() => {
          if (currentTab === 'vmlinks') checkVMAndConnect();
        }, 5000);
      };
    } catch(e) { /* ignore */ }
  }

  function disconnectVMSSE() {
    if (vmSSE) {
      vmSSE.close();
      vmSSE = null;
    }
  }

  // 定期检测 VM 状态（每 15 秒）
  setInterval(async () => {
    try {
      const res = await fetch('/api/vm/status');
      const data = await res.json();
      const wasReachable = vmReachable;
      vmReachable = data.vm_reachable;
      updateVMIndicator(vmReachable);
      // 状态变化时自动处理
      if (vmReachable && !wasReachable && currentTab === 'vmlinks') {
        checkVMAndConnect();
      }
      if (!vmReachable && wasReachable) {
        disconnectVMSSE();
        if (currentTab === 'vmlinks') {
          $('vmOfflineOverlay').style.display = '';
          $('vmOnlineContent').style.display = 'none';
        }
      }
    } catch(e) { /* ignore */ }
  }, 15000);

  // ========== 启动 ==========
  fetchData().then(() => scheduleRefresh());
  // 首次检测 VM 状态（更新 Tab 指示器）
  checkVMAndConnect();

})();
</script>
</body>
</html>`
