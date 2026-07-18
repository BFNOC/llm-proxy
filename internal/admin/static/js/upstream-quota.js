function checkQuota(e, id) {
    const btn = e.target;
    const row = btn.closest('tr');
    // 如果已有展开的额度行，则收起
    const existingRow = document.getElementById('quota-row-'+id);
    if (existingRow) { existingRow.remove(); return; }
    // 移除其他已展开的额度行
    document.querySelectorAll('[id^="quota-row-"]').forEach(r => r.remove());

    const origText = btn.textContent;
    btn.textContent = '查询中...';
    btn.disabled = true;
    const cfBody = getCFConfig(id);
    api('/upstreams/'+id+'/check-quota', {method:'POST', body: JSON.stringify(cfBody||{})}).then(d => {
        btn.textContent = origText;
        btn.disabled = false;
        const tr = document.createElement('tr');
        tr.id = 'quota-row-'+id;
        const td = document.createElement('td');
        td.colSpan = 11;
        td.style.cssText = 'padding:0;border:none;';

        if (d.success) {
            const data = d.data;
            let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">';
            html += '<span style="font-weight:600;">📊 ' + esc(data.name) + '</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            html += renderQuotaDetails(data);
            html += '</div>';
            td.innerHTML = html;
        } else {
            let msg = d.message || '未知错误';
            let html = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">';
            html += '<span style="color:var(--red);font-weight:600;">❌ 查询失败</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            html += '<div style="color:var(--text-dim);font-size:0.85rem;">' + esc(msg) + '</div>';
            if (d.origin_content) {
                html += '<pre style="margin-top:8px;padding:8px;background:var(--bg);border-radius:4px;font-size:0.75rem;color:var(--text-dim);white-space:pre-wrap;word-break:break-all;">' + esc(d.origin_content) + '</pre>';
            }
            if (d.origin_content || (msg && msg.indexOf('403') !== -1)) {
                html += '<div style="margin-top:8px;"><button class="btn btn-ghost btn-sm" style="color:var(--orange)" onclick="this.closest(\'tr\').remove();openCFDialog('+id+')">🔧 可能需要配置 CF 绕过</button></div>';
            }
            html += '</div>';
            td.innerHTML = html;
        }
        tr.appendChild(td);
        row.after(tr);
    }).catch(err => {
        btn.textContent = origText;
        btn.disabled = false;
        let tr = document.createElement('tr');
        tr.id = 'quota-row-'+id;
        let td = document.createElement('td');
        td.colSpan = 11;
        td.innerHTML = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;color:var(--red);">请求失败: '+esc(err.message)+'</div>';
        tr.appendChild(td);
        row.after(tr);
    });
}

// --- Quota details (used by checkQuota) ---
function renderQuotaDetails(data) {
    const fmt = n => n.toLocaleString();
    const toUSD = n => '$' + (n / 500000).toFixed(2);
    let html = '';
    if (data.unlimited_quota) {
        html += '<span class="badge badge-green">无限额度</span>';
    } else {
        const pct = data.total_granted > 0 ? (data.total_used / data.total_granted * 100).toFixed(1) : '0.0';
        const barColor = pct > 80 ? 'var(--red)' : pct > 50 ? 'var(--orange)' : 'var(--green)';
        html += '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:12px;">';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">可用</div><div style="font-size:1.1rem;font-weight:700;color:var(--green);">' + toUSD(data.total_available) + '</div><div style="font-size:0.7rem;color:var(--text-dim);">' + fmt(data.total_available) + '</div></div>';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">已用</div><div style="font-size:1.1rem;font-weight:700;color:var(--orange);">' + toUSD(data.total_used) + '</div><div style="font-size:0.7rem;color:var(--text-dim);">' + fmt(data.total_used) + '</div></div>';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">总额</div><div style="font-size:1.1rem;font-weight:700;">' + toUSD(data.total_granted) + '</div><div style="font-size:0.7rem;color:var(--text-dim);">' + fmt(data.total_granted) + '</div></div>';
        html += '</div>';
        html += '<div style="background:var(--bg-card);border-radius:4px;height:8px;overflow:hidden;">';
        html += '<div style="height:100%;width:' + pct + '%;background:' + barColor + ';border-radius:4px;transition:width 0.3s;"></div></div>';
        html += '<div style="text-align:right;font-size:0.75rem;color:var(--text-dim);margin-top:4px;">使用率 ' + pct + '%</div>';
    }
    if (data.expires_at > 0) {
        const expDate = new Date(data.expires_at * 1000);
        const remain = data.expires_at * 1000 - Date.now();
        let remainStr = '', remainColor = 'var(--text-dim)';
        if (remain <= 0) { remainStr = '已过期'; remainColor = 'var(--red)'; }
        else {
            const days = Math.floor(remain / 86400000);
            const hrs = Math.floor((remain % 86400000) / 3600000);
            const mins = Math.floor((remain % 3600000) / 60000);
            if (days > 0) remainStr = days + '天' + hrs + '小时';
            else if (hrs > 0) remainStr = hrs + '小时' + mins + '分';
            else remainStr = mins + '分钟';
            remainStr = '剩余 ' + remainStr;
            if (remain < 86400000) remainColor = 'var(--red)';
            else if (remain < 86400000 * 3) remainColor = 'var(--orange)';
        }
        html += '<div style="font-size:0.8rem;margin-top:8px;">过期时间: ' + fmtTime(expDate.toISOString()) + ' <span style="color:' + remainColor + ';font-weight:600;">(' + remainStr + ')</span></div>';
    }
    if (data.model_limits_enabled) {
        html += '<div style="font-size:0.8rem;margin-top:8px;color:var(--text-dim);">模型限制: <span class="badge badge-green">已启用</span></div>';
    }
    if (data.model_limits && typeof data.model_limits === 'object') {
        const models = Object.keys(data.model_limits).filter(k => data.model_limits[k]);
        if (models.length > 0) {
            html += '<div style="margin-top:8px;"><div style="font-size:0.75rem;color:var(--text-dim);margin-bottom:4px;">可用模型 (' + models.length + ')</div>';
            html += '<div class="model-tags">' + models.map(m => '<span class="model-tag">' + esc(m) + '</span>').join('') + '</div></div>';
        }
    }
    return html;
}
