// --- Status ---
function loadStatus() {
    api('/status').then(d => {
        const grid = document.getElementById('status-grid');
        const statCard = (label, value, color) => '<div style="background:var(--bg);padding:16px;border-radius:var(--radius-sm);border:1px solid var(--border);text-align:center;"><div style="font-size:1.5rem;font-weight:700;color:'+(color||'var(--text)')+';">'+value+'</div><div style="font-size:0.75rem;color:var(--text-dim);margin-top:4px;">'+label+'</div></div>';
        grid.innerHTML = statCard('版本', esc(d.version||'-'), 'var(--accent)') +
            statCard('运行时间', esc(d.uptime||'-'), 'var(--green)') +
            statCard('密钥数量', d.total_keys||0) +
            statCard('今日请求', d.today_requests||0, 'var(--orange)') +
            statCard('并发请求', d.active_requests||0, 'var(--accent)') +
            statCard('RPM', d.rpm||0, 'var(--green)') +
            statCard('RPS', d.rps||'0.0', 'var(--orange)') +
            statCard('审计丢弃', d.audit_dropped||0, d.audit_dropped>0?'var(--red)':'var(--green)');

        const container = document.getElementById('status-upstreams');
        const ups = d.healthy_upstreams || [];
        if (ups.length === 0) {
            container.innerHTML = '<div class="empty-state">暂无健康上游</div>';
        } else {
            container.innerHTML = ups.map(u => {
                const mode = u.key_scheduling_mode || 'round-robin';
                const modeLabel = mode === 'fill' ? '填充' : '轮询';
                const modeColor = mode === 'fill' ? 'var(--orange)' : 'var(--accent)';
                return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">'+
                    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">'+
                    '<strong style="font-size:0.95rem;">'+esc(u.name)+'</strong>'+
                    '<span class="badge badge-green">健康</span></div>'+
                    '<div style="font-size:0.82rem;color:var(--text-dim);margin-bottom:6px;"><code style="font-size:0.78rem;" title="'+esc(u.url)+'">'+esc(u.url)+'</code></div>'+
                    '<div style="display:flex;gap:12px;font-size:0.8rem;">'+
                    '<span>Keys: <strong>'+u.key_count+'</strong></span>'+
                    '<span>调度: <span style="color:'+modeColor+';font-weight:500;">'+modeLabel+'</span></span>'+
                    '</div></div>';
            }).join('');
        }
    });
}

