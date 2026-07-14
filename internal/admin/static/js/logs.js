// --- Logs ---
let slowRequestThreshold = 30000;
function loadSlowThreshold() {
    api('/settings').then(data => {
        if (data && data.slow_request_threshold_ms != null) {
            slowRequestThreshold = data.slow_request_threshold_ms;
        }
    }).catch(() => {});
}
function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '-';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1048576).toFixed(1) + ' MB';
}
function loadLogs(e) {
    if(e) e.preventDefault();
    const f = e ? new FormData(e.target) : new FormData();
    let q = '?limit='+(f.get('limit')||50);
    if(f.get('key_id')) q += '&key_id='+f.get('key_id');
    loadSlowThreshold();
    api('/logs'+q).then(data => {
        const tbody = document.getElementById('logs-table');
        if (!data || data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="15" class="empty-state">暂无日志</td></tr>';
            return;
        }
        tbody.innerHTML = (data||[]).map(l => {
            const keyIdx = l.UpstreamKeyIdx;
            const keyIdxText = keyIdx >= 0 ? '#' + (keyIdx + 1) : '-';
            const modelText = l.Model || '-';
            const proxyText = l.UsedProxy ? esc(l.UsedProxy) : '<span style="color:var(--text-secondary)">直连</span>';
            const isSlow = slowRequestThreshold > 0 && l.LatencyMs > slowRequestThreshold;
            const latencyHtml = isSlow
                ? '<span class="badge badge-red" title="慢请求">'+l.LatencyMs+'ms</span>'
                : l.LatencyMs+'ms';
            const rowStyle = isSlow ? ' style="background:rgba(239,68,68,0.06);"' : '';
            return '<tr'+rowStyle+'><td class="hide-on-mobile">'+l.ID+'</td><td>'+l.DownstreamKeyID+'</td><td>'+esc(l.UpstreamName||'-')+'</td><td class="hide-on-mobile"><span class="badge badge-purple" style="font-size:0.7rem">'+keyIdxText+'</span></td><td class="hide-on-mobile"><code style="font-size:0.78rem">'+esc(modelText)+'</code></td><td class="hide-on-mobile" style="font-size:0.78rem;max-width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(l.UsedProxy||'')+'">'+proxyText+'</td><td class="hide-on-mobile">'+esc(l.ClientIP||'-')+'</td><td>'+esc(l.IPRegion||'-')+'</td><td class="hide-on-mobile">'+esc(l.ProviderStyle)+'</td><td class="hide-on-mobile">'+esc(l.Path)+'</td><td><span class="badge '+(l.StatusCode<400?'badge-green':'badge-red')+'">'+l.StatusCode+'</span></td><td class="hide-on-mobile">'+latencyHtml+'</td><td class="hide-on-mobile">'+formatBytes(l.RequestSize)+'</td><td class="hide-on-mobile">'+formatBytes(l.ResponseSize)+'</td><td>'+fmtTime(l.CreatedAt)+'</td></tr>';
        }).join('');
    });
}

