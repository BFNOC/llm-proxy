function parseQuotaJSON() {
    const input = document.getElementById('tools-json-input').value.trim();
    const container = document.getElementById('tools-result');
    if (!input) { container.innerHTML = '<div style="color:var(--text-dim);">请粘贴 JSON</div>'; return; }
    let parsed;
    try { parsed = JSON.parse(input); } catch(e) {
        container.innerHTML = '<div style="color:var(--red);">JSON 解析失败: ' + esc(e.message) + '</div>';
        return;
    }
    // 检测格式：sub2api 有 isValid 字段
    if (parsed.isValid !== undefined) {
        container.innerHTML = renderSub2apiDetails(parsed);
    } else {
        // new-api 格式
        const data = parsed.data || parsed;
        let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">';
        html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">';
        html += '<span style="font-weight:600;">📊 ' + esc(data.name || '未知') + '</span></div>';
        html += renderQuotaDetails(data);
        html += '</div>';
        container.innerHTML = html;
    }
}

function renderSub2apiDetails(d) {
    const toUSD = n => '$' + Number(n).toFixed(2);
    const fmtN = n => Number(n).toLocaleString();
    let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">';
    // 标题行
    html += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:12px;flex-wrap:wrap;">';
    html += '<span style="font-weight:600;font-size:1.05rem;">📊 ' + esc(d.planName || 'Sub2API') + '</span>';
    const modeMap = {quota_limited:'额度限制',unrestricted:'无限制'};
    const modeLabel = modeMap[d.mode] || d.mode || '未知';
    const modeColor = d.mode === 'unrestricted' ? 'badge-green' : 'badge-orange';
    html += '<span class="badge ' + modeColor + '">' + esc(modeLabel) + '</span>';
    if (d.status) html += '<span class="badge badge-green">' + esc(d.status) + '</span>';
    if (!d.isValid) html += '<span class="badge badge-red">无效</span>';
    html += '</div>';

    // 额度信息
    if (d.quota && d.quota.limit > 0) {
        const used = d.quota.used || 0, limit = d.quota.limit || 0, remain = d.quota.remaining || 0;
        const pct = limit > 0 ? (used / limit * 100).toFixed(1) : '0.0';
        const barColor = pct > 80 ? 'var(--red)' : pct > 50 ? 'var(--orange)' : 'var(--green)';
        html += '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:12px;">';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">剩余</div><div style="font-size:1.1rem;font-weight:700;color:var(--green);">' + toUSD(remain) + '</div></div>';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">已用</div><div style="font-size:1.1rem;font-weight:700;color:var(--orange);">' + toUSD(used) + '</div></div>';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">总额</div><div style="font-size:1.1rem;font-weight:700;">' + toUSD(limit) + '</div></div>';
        html += '</div>';
        html += '<div style="background:var(--bg-card);border-radius:4px;height:8px;overflow:hidden;">';
        html += '<div style="height:100%;width:' + pct + '%;background:' + barColor + ';border-radius:4px;transition:width 0.3s;"></div></div>';
        html += '<div style="text-align:right;font-size:0.75rem;color:var(--text-dim);margin-top:4px;">使用率 ' + pct + '%</div>';
    } else if (d.mode === 'unrestricted') {
        html += '<span class="badge badge-green">无限额度</span>';
        if (d.balance !== undefined) html += ' <span style="font-size:0.85rem;color:var(--text-dim);">余额: ' + toUSD(d.balance) + '</span>';
    }

    // 用量统计
    if (d.usage) {
        html += '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(120px,1fr));gap:8px;margin-top:12px;">';
        if (d.usage.rpm !== undefined) html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">RPM</div><div style="font-weight:600;">' + fmtN(d.usage.rpm) + '</div></div>';
        if (d.usage.tpm !== undefined) html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">TPM</div><div style="font-weight:600;">' + fmtN(d.usage.tpm) + '</div></div>';
        if (d.usage.average_duration_ms) html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">平均延迟</div><div style="font-weight:600;">' + (d.usage.average_duration_ms/1000).toFixed(1) + 's</div></div>';
        const today = d.usage.today;
        if (today) {
            html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">今日请求</div><div style="font-weight:600;">' + fmtN(today.requests) + '</div></div>';
            html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">今日花费</div><div style="font-weight:600;">' + toUSD(today.cost) + '</div></div>';
            html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">今日 Tokens</div><div style="font-weight:600;">' + fmtN(today.total_tokens) + '</div></div>';
        }
        html += '</div>';
    }

    // 模型用量明细
    if (d.model_stats && d.model_stats.length > 0) {
        const stats = d.model_stats.slice().sort((a,b) => (b.cost||0) - (a.cost||0));
        html += '<div style="margin-top:12px;"><div style="font-size:0.75rem;color:var(--text-dim);margin-bottom:6px;">模型用量明细 (' + stats.length + ')</div>';
        html += '<div class="table-container"><table style="font-size:0.8rem;"><thead><tr><th>模型</th><th>请求数</th><th>输入</th><th>输出</th><th>缓存读取</th><th>花费</th></tr></thead><tbody>';
        stats.forEach(m => {
            html += '<tr><td><span class="model-tag">' + esc(m.model) + '</span></td>';
            html += '<td>' + fmtN(m.requests) + '</td>';
            html += '<td>' + fmtN(m.input_tokens) + '</td>';
            html += '<td>' + fmtN(m.output_tokens) + '</td>';
            html += '<td>' + fmtN(m.cache_read_tokens) + '</td>';
            html += '<td>' + toUSD(m.cost) + '</td></tr>';
        });
        html += '</tbody></table></div></div>';
    }

    html += '</div>';
    return html;
}

// --- Settings ---
// --- Header Capture (Claude Code fingerprints) ---
