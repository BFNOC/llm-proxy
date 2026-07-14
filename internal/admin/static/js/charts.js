// --- Charts (Chart.js) ---

// 延迟统计图表（状态页）
let latencyChartInstance = null;
function loadLatencyChart() {
    if (typeof Chart === 'undefined') return;
    api('/stats/latency?hours=24').then(data => {
        const canvas = document.getElementById('latency-chart');
        if (!canvas) return;
        if (!data || !Array.isArray(data) || data.length === 0) {
            canvas.parentElement.style.display = 'none';
            return;
        }
        canvas.parentElement.style.display = '';
        const labels = data.map(d => d.upstream_name || ('ID:' + d.upstream_id));
        const avgData = data.map(d => d.avg_ms || 0);
        const p95Data = data.map(d => d.p95_ms || 0);
        const maxData = data.map(d => d.max_ms || 0);
        if (latencyChartInstance) latencyChartInstance.destroy();
        latencyChartInstance = new Chart(canvas, {
            type: 'bar',
            data: {
                labels: labels,
                datasets: [
                    { label: '平均', data: avgData, backgroundColor: 'rgba(16,185,129,0.7)', borderColor: 'rgba(16,185,129,1)', borderWidth: 1 },
                    { label: 'P95', data: p95Data, backgroundColor: 'rgba(245,158,11,0.7)', borderColor: 'rgba(245,158,11,1)', borderWidth: 1 },
                    { label: '最大', data: maxData, backgroundColor: 'rgba(239,68,68,0.7)', borderColor: 'rgba(239,68,68,1)', borderWidth: 1 }
                ]
            },
            options: {
                indexAxis: 'y',
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: { position: 'top', labels: { font: { size: 12 }, usePointStyle: true, pointStyle: 'rect' } },
                    tooltip: { callbacks: { label: function(ctx) { return ctx.dataset.label + ': ' + ctx.raw.toFixed(1) + ' ms'; } } }
                },
                scales: {
                    x: { title: { display: true, text: '延迟 (ms)' }, beginAtZero: true },
                    y: { ticks: { font: { size: 11 } } }
                }
            }
        });
    }).catch(() => {
        const canvas = document.getElementById('latency-chart');
        if (canvas) canvas.parentElement.style.display = 'none';
    });
}

// 健康历史（上游操作菜单）
function showHealthHistory(upstreamId) {
    const existingRow = document.getElementById('hh-row-' + upstreamId);
    if (existingRow) { existingRow.remove(); return; }
    // 移除其他展开的健康历史行
    document.querySelectorAll('[id^="hh-row-"]').forEach(r => r.remove());

    const btn = event.target;
    const row = btn.closest('tr');
    if (!row) return;

    const tr = document.createElement('tr');
    tr.id = 'hh-row-' + upstreamId;
    const td = document.createElement('td');
    td.colSpan = 12;
    td.style.cssText = 'padding:0;border:none;';
    td.innerHTML = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px 18px;margin:8px 16px;"><div style="text-align:center;color:var(--text-dim);padding:12px;">加载中...</div></div>';
    tr.appendChild(td);
    row.after(tr);

    api('/upstreams/' + upstreamId + '/health-history?hours=24&limit=100').then(data => {
        if (!data || !Array.isArray(data) || data.length === 0) {
            td.innerHTML = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px 18px;margin:8px 16px;display:flex;align-items:center;justify-content:space-between;">' +
                '<span style="color:var(--text-dim);">暂无健康探测记录</span>' +
                '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()">✕</button></div>';
            return;
        }

        const chartId = 'hh-chart-' + upstreamId;
        let errorsHtml = '';
        const failures = data.filter(p => !p.healthy);
        if (failures.length > 0) {
            errorsHtml = '<div style="margin-top:12px;"><div style="font-size:0.75rem;color:var(--text-dim);margin-bottom:6px;">失败记录 (' + failures.length + ')</div>' +
                '<div style="max-height:120px;overflow:auto;">' +
                failures.slice(0, 20).map(f => {
                    const t = f.checked_at ? fmtTime(f.checked_at) : '-';
                    return '<div style="display:flex;gap:8px;align-items:center;padding:4px 0;font-size:0.78rem;border-bottom:1px solid var(--border);">' +
                        '<span class="badge badge-red" style="font-size:0.65rem;">失败</span>' +
                        '<span style="color:var(--text-dim);min-width:120px;">' + esc(t) + '</span>' +
                        '<span>' + esc(f.error || '-') + '</span>' +
                        '</div>';
                }).join('') +
                '</div></div>';
        }

        td.innerHTML = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px 18px;margin:8px 16px;">' +
            '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">' +
            '<span style="font-weight:600;font-size:0.9rem;">健康历史（24h）</span>' +
            '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()">✕</button></div>' +
            '<div style="height:180px;"><canvas id="' + chartId + '"></canvas></div>' +
            errorsHtml +
            '</div>';

        if (typeof Chart === 'undefined') return;
        const canvas = document.getElementById(chartId);
        if (!canvas) return;

        const labels = data.map(d => {
            if (!d.checked_at) return '';
            const dt = new Date(d.checked_at);
            return dt.getHours().toString().padStart(2, '0') + ':' + dt.getMinutes().toString().padStart(2, '0');
        });
        const latencies = data.map(d => d.latency_ms || 0);
        const pointColors = data.map(d => d.healthy ? 'rgba(16,185,129,0.8)' : 'rgba(239,68,68,0.8)');
        const bgColors = data.map(d => d.healthy ? 'rgba(16,185,129,0.15)' : 'rgba(239,68,68,0.15)');

        new Chart(canvas, {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: '延迟 (ms)',
                    data: latencies,
                    borderColor: 'rgba(99,102,241,0.6)',
                    backgroundColor: 'rgba(99,102,241,0.08)',
                    pointBackgroundColor: pointColors,
                    pointBorderColor: pointColors,
                    pointRadius: 4,
                    pointHoverRadius: 6,
                    fill: true,
                    tension: 0.3
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        callbacks: {
                            label: function(ctx) {
                                const d = data[ctx.dataIndex];
                                let s = '延迟: ' + ctx.raw + ' ms';
                                if (d && !d.healthy) s += ' (失败: ' + (d.error || '未知') + ')';
                                return s;
                            }
                        }
                    }
                },
                scales: {
                    x: { ticks: { maxTicksLimit: 12, font: { size: 10 } } },
                    y: { title: { display: true, text: 'ms' }, beginAtZero: true }
                }
            }
        });
    }).catch(() => {
        td.innerHTML = '<div style="background:var(--bg);border:1px solid rgba(239,68,68,0.25);border-radius:var(--radius-sm);padding:14px 18px;margin:8px 16px;display:flex;align-items:center;justify-content:space-between;">' +
            '<span style="color:var(--red);">加载健康历史失败</span>' +
            '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()">✕</button></div>';
    });
}
