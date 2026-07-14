// --- Status ---
function loadStatus() {
    if (typeof loadLatencyChart === 'function') loadLatencyChart();
    Promise.all([api('/status'), api('/upstreams/circuit-status'), api('/upstreams/rate-info')]).then(([d, circuitStatus, rateInfo]) => {
        const grid = document.getElementById('status-grid');
        const statCard = (label, value, color, liveKey) => '<div style="background:var(--bg);padding:16px;border-radius:var(--radius-sm);border:1px solid var(--border);text-align:center;"><div'+(liveKey?' data-live="'+liveKey+'"':'')+' style="font-size:1.5rem;font-weight:700;color:'+(color||'var(--text)')+';">'+value+'</div><div style="font-size:0.75rem;color:var(--text-dim);margin-top:4px;">'+label+'</div></div>';
        grid.innerHTML = statCard('版本', esc(d.version||'-'), 'var(--accent)') +
            statCard('运行时间', esc(d.uptime||'-'), 'var(--green)') +
            statCard('密钥数量', d.total_keys||0) +
            statCard('今日请求', d.today_requests||0, 'var(--orange)') +
            statCard('并发请求', d.active_requests||0, 'var(--accent)', 'active') +
            statCard('RPM', d.rpm||0, 'var(--green)', 'rpm') +
            statCard('RPS', d.rps||'0.0', 'var(--orange)', 'rps') +
            statCard('审计丢弃', d.audit_dropped||0, d.audit_dropped>0?'var(--red)':'var(--green)');

        // 熔断器状态
        const cbContainer = document.getElementById('status-circuit-breaker');
        const cs = circuitStatus || {};
        const cbEntries = Object.entries(cs);
        if (cbContainer) {
            if (cbEntries.length === 0) {
                cbContainer.innerHTML = '<div class="empty-state" style="padding:12px;">无熔断器数据</div>';
            } else {
                cbContainer.innerHTML = cbEntries.map(([uid, state]) => {
                    const color = state === 'open' ? 'var(--red)' : state === 'half_open' ? 'var(--orange)' : 'var(--green)';
                    const label = state === 'open' ? '熔断' : state === 'half_open' ? '恢复中' : '正常';
                    return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px;display:flex;align-items:center;justify-content:space-between;">'+
                        '<span>上游 #'+esc(uid)+'</span>'+
                        '<span class="badge" style="background:'+color+';color:#fff;">'+label+'</span></div>';
                }).join('');
            }
        }

        // 速率信息
        const riContainer = document.getElementById('status-rate-info');
        const ri = rateInfo || [];
        if (riContainer) {
            if (ri.length === 0) {
                riContainer.innerHTML = '<div class="empty-state" style="padding:12px;">无速率数据</div>';
            } else {
                riContainer.innerHTML = ri.map(r => {
                    const pct = r.rpm_limit > 0 ? Math.round(r.rpm_used / r.rpm_limit * 100) : 0;
                    return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px;">'+
                        '<div style="display:flex;justify-content:space-between;margin-bottom:4px;"><span>上游 #'+r.upstream_id+'</span>'+
                        (r.rpm_limit > 0 ? '<span style="font-size:0.8rem;color:var(--text-dim);">'+r.rpm_used+'/'+r.rpm_limit+' RPM ('+pct+'%)</span>' : '')+
                        '</div>'+
                        (r.total_429_count > 0 ? '<div style="font-size:0.78rem;color:var(--orange);">429 累计: '+r.total_429_count+'</div>' : '')+
                        '</div>';
                }).join('');
            }
        }

        const container = document.getElementById('status-upstreams');
        const ups = d.healthy_upstreams || [];
        if (ups.length === 0) {
            container.innerHTML = '<div class="empty-state">暂无健康上游</div>';
        } else {
            container.innerHTML = ups.map(u => {
                const mode = u.key_scheduling_mode || 'round-robin';
                const modeLabel = mode === 'fill' ? '填充' : '轮询';
                const modeColor = mode === 'fill' ? 'var(--orange)' : 'var(--accent)';
                const cbState = cs[String(u.id)];
                let cbBadge = '';
                if (cbState === 'open') cbBadge = '<span class="badge badge-red">熔断</span>';
                else if (cbState === 'half_open') cbBadge = '<span class="badge badge-orange">恢复中</span>';
                else cbBadge = '<span class="badge badge-green">健康</span>';
                return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">'+
                    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">'+
                    '<strong style="font-size:0.95rem;">'+esc(u.name)+'</strong>'+
                    cbBadge+'</div>'+
                    '<div style="font-size:0.82rem;color:var(--text-dim);margin-bottom:6px;"><code style="font-size:0.78rem;" title="'+esc(u.url)+'">'+esc(u.url)+'</code></div>'+
                    '<div style="display:flex;gap:12px;font-size:0.8rem;">'+
                    '<span>Keys: <strong>'+u.key_count+'</strong></span>'+
                    '<span>调度: <span style="color:'+modeColor+';font-weight:500;">'+modeLabel+'</span></span>'+
                    '</div></div>';
            }).join('');
        }
    });
}
