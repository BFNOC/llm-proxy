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
function loadHeaderCapture() {
    const baseEl = document.getElementById('hc-base-url');
    if (baseEl) baseEl.textContent = location.origin + '/v1';
    api('/header-capture').then(d => {
        if (d.error) { toastErr(d.error); return; }
        const on = !!d.enabled;
        const st = document.getElementById('hc-status');
        const btn = document.getElementById('hc-toggle');
        if (st) {
            st.textContent = on ? '抓取中' : '已关闭';
            st.style.color = on ? 'var(--green)' : '';
        }
        if (btn) btn.textContent = on ? '停止抓取' : '开启抓取';
        renderHeaderCaptures(d.captures || []);
    }).catch(() => toastErr('加载 Header 抓取失败'));
}
function toggleHeaderCapture() {
    api('/header-capture').then(d => {
        const next = !d.enabled;
        return api('/header-capture', {method:'PUT', body: JSON.stringify({enabled: next})});
    }).then(d => {
        if (d.error) { toastErr(d.error); return; }
        toastOk(d.enabled ? '已开启抓取，请用 Claude Code 或 Codex 发一条消息' : '已停止抓取');
        loadHeaderCapture();
    });
}
function clearHeaderCapture() {
    api('/header-capture', {method:'DELETE'}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        toastOk('已清空');
        loadHeaderCapture();
    });
}
function hcFamilyBadge(family) {
    if (family === 'claude_code') return '<span class="badge badge-purple">Claude Code</span>';
    if (family === 'codex') return '<span class="badge" style="background:rgba(59,130,246,0.12);color:#2563eb;border:1px solid rgba(59,130,246,0.25);">Codex</span>';
    return '<span class="badge badge-orange">Other</span>';
}
function renderHeaderCaptures(list) {
    const box = document.getElementById('hc-list');
    if (!box) return;
    if (!list.length) {
        box.className = 'empty-state';
        box.style.padding = '20px';
        box.innerHTML = '尚未抓取到请求。先开启抓取，再从 Claude Code 或 Codex 发一条消息。';
        return;
    }
    box.className = '';
    box.style.padding = '0';
    // Keep raw list for full-dump copy (includes secrets + body).
    window.__hcCaptures = list;
    box.innerHTML = list.map((c, idx) => {
        const flat = c.flat || {};
        const keys = Object.keys(flat).sort((a,b) => a.localeCompare(b));
        const interesting = keys.filter(k => {
            const lk = k.toLowerCase();
            return lk.indexOf('anthropic') >= 0 || lk === 'user-agent' || lk === 'x-app' ||
                lk.indexOf('claude') >= 0 || lk === 'content-type' || lk === 'accept' ||
                lk.indexOf('stainless') >= 0 || lk === 'authorization' || lk === 'x-api-key' ||
                lk === 'originator' || lk.indexOf('openai') >= 0 || lk.indexOf('codex') >= 0 ||
                lk === 'session_id' || lk === 'session-id' || lk === 'conversation_id' ||
                lk === 'chatgpt-account-id' || lk.indexOf('x-client-request') >= 0;
        });
        const interestingObj = {};
        interesting.forEach(k => { interestingObj[k] = flat[k]; });
        const fullJson = JSON.stringify(flat, null, 2);
        const multiJson = JSON.stringify(c.headers || {}, null, 2);
        const interestingJson = JSON.stringify(interestingObj, null, 2);
        let bodyText = c.body || '';
        let bodyPretty = bodyText;
        try { bodyPretty = JSON.stringify(JSON.parse(bodyText), null, 2); } catch (_) {}
        const time = c.time ? fmtTime(c.time) : '-';
        const trunc = c.body_truncated ? ' <span class="badge badge-orange">body 已截断</span>' : '';
        const family = c.client_family || 'other';
        const meta = [
            c.host ? 'Host '+c.host : '',
            c.proto || '',
            c.content_length != null ? 'CL '+c.content_length : '',
            c.body_bytes != null ? 'captured '+c.body_bytes+'B' : '',
            c.remote_addr || ''
        ].filter(Boolean).join(' · ');
        return '<div style="border:1px solid var(--line);border-radius:var(--radius);padding:14px 16px;margin-bottom:12px;background:var(--paper);">'+
            '<div style="display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-bottom:10px;">'+
            '<span class="badge badge-purple">#'+esc(String(c.id||idx+1))+'</span>'+
            hcFamilyBadge(family)+
            '<code style="font-size:0.8rem;">'+esc(c.method||'')+' '+esc(c.path||'')+(c.query?'?'+esc(c.query):'')+'</code>'+
            '<span class="count-chip">'+esc(time)+'</span>'+trunc+
            '<span class="spacer" style="flex:1"></span>'+
            '<button type="button" class="btn btn-ghost btn-sm" data-copy-hc="'+idx+'-i">复制关键头</button>'+
            '<button type="button" class="btn btn-ghost btn-sm" data-copy-hc="'+idx+'-f">复制全部头</button>'+
            '<button type="button" class="btn btn-ghost btn-sm" data-copy-hc="'+idx+'-b">复制 Body</button>'+
            '<button type="button" class="btn btn-primary btn-sm" data-copy-hc="'+idx+'-a">复制完整 Dump</button>'+
            '</div>'+
            (meta ? '<div style="font-size:0.75rem;color:var(--text-dim);margin:-4px 0 10px;">'+esc(meta)+'</div>' : '')+
            '<div style="font-size:0.72rem;font-weight:600;color:var(--text-dim);margin-bottom:6px;">关键头（含 Authorization 明文）</div>'+
            '<pre id="hc-pre-i-'+idx+'" style="margin:0 0 10px;font-size:0.75rem;line-height:1.45;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-xs);max-height:220px;overflow:auto;">'+esc(interestingJson)+'</pre>'+
            '<details open><summary style="cursor:pointer;font-size:0.8rem;color:var(--text-dim);margin-bottom:6px;">全部 Header（Flat）</summary>'+
            '<pre id="hc-pre-f-'+idx+'" style="margin:0 0 10px;font-size:0.72rem;line-height:1.45;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-xs);max-height:280px;overflow:auto;">'+esc(fullJson)+'</pre>'+
            '</details>'+
            '<details><summary style="cursor:pointer;font-size:0.8rem;color:var(--text-dim);margin-bottom:6px;">Header 多值原始</summary>'+
            '<pre id="hc-pre-m-'+idx+'" style="margin:0 0 10px;font-size:0.72rem;line-height:1.45;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-xs);max-height:200px;overflow:auto;">'+esc(multiJson)+'</pre>'+
            '</details>'+
            '<details open><summary style="cursor:pointer;font-size:0.8rem;color:var(--text-dim);margin-bottom:6px;">Body'+(c.body_truncated?'（已截断）':'')+' · '+esc(String(c.body_bytes||0))+' bytes</summary>'+
            '<pre id="hc-pre-b-'+idx+'" style="margin:0;font-size:0.72rem;line-height:1.45;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-xs);max-height:420px;overflow:auto;">'+esc(bodyPretty||'(empty)')+'</pre>'+
            '</details></div>';
    }).join('');
    box.querySelectorAll('[data-copy-hc]').forEach(btn => {
        btn.onclick = function() {
            const id = btn.getAttribute('data-copy-hc');
            const parts = id.split('-');
            const idx = parseInt(parts[0], 10), kind = parts[1];
            if (kind === 'a') {
                const raw = (window.__hcCaptures || [])[idx];
                if (raw) copyTextToClipboard(JSON.stringify(raw, null, 2), btn);
                return;
            }
            const map = {i:'hc-pre-i-', f:'hc-pre-f-', b:'hc-pre-b-', m:'hc-pre-m-'};
            const pre = document.getElementById((map[kind]||'hc-pre-f-')+idx);
            if (pre) copyTextToClipboard(pre.textContent, btn);
        };
    });
}

function loadSettings() {
	const keysPromise = keysCache.length > 0 ? Promise.resolve(keysCache) : api('/keys').then(data => {
		if (Array.isArray(data)) keysCache = data;
		return keysCache;
	});
	Promise.all([api('/settings'), keysPromise]).then(([data, keys]) => {
		if (!data || data.error) {
			if (data && data.error) toastErr(data.error);
			return;
		}
		document.getElementById('setting-threshold').value = data.auto_disable_threshold ?? 2;
		document.getElementById('setting-slow-threshold').value = data.slow_request_threshold_ms ?? 30000;
		document.getElementById('setting-full-recording-enabled').checked = !!data.full_recording_enabled;
		const selectedIDs = Array.isArray(data.full_recording_key_ids) ? data.full_recording_key_ids.map(Number) : [];
		const allKeys = data.full_recording_all_keys !== false;
		document.getElementById('setting-full-recording-all-keys').checked = allKeys;
		const selectedMode = document.querySelector('input[name="full-recording-mode"][value="selected"]');
		selectedMode.checked = !allKeys;
		renderFullRecordingKeys(keys || [], selectedIDs);
		updateFullRecordingControls();
	}).catch(() => toastErr('加载设置失败'));
}

function renderFullRecordingKeys(keys, selectedIDs) {
	const box = document.getElementById('setting-full-recording-keys');
	const selected = new Set((selectedIDs || []).map(Number));
	if (!keys || keys.length === 0) {
		box.innerHTML = '<div class="empty-state">暂无下游密钥</div>';
		return;
	}
	box.innerHTML = keys.map(key => {
		const id = Number(key.id);
		return '<label class="recording-key-option" data-testid="full-recording-key"><input type="checkbox" value="' + id + '" ' + (selected.has(id) ? 'checked' : '') + '><span><strong>#' + id + ' ' + esc(key.name || '') + '</strong><code>' + esc(key.key_prefix || '') + '</code></span></label>';
	}).join('');
}

function updateFullRecordingControls() {
	const enabled = document.getElementById('setting-full-recording-enabled').checked;
	const scope = document.getElementById('setting-full-recording-scope');
	const selectedMode = document.querySelector('input[name="full-recording-mode"]:checked');
	const list = document.getElementById('setting-full-recording-keys');
	scope.disabled = !enabled;
	list.classList.toggle('is-hidden', !enabled || !selectedMode || selectedMode.value !== 'selected');
}

function saveSettings() {
	const threshold = parseInt(document.getElementById('setting-threshold').value, 10);
	const slowThreshold = parseInt(document.getElementById('setting-slow-threshold').value, 10);
	if (isNaN(threshold) || threshold < 0) { toastErr('阈值必须 >= 0'); return; }
	if (isNaN(slowThreshold) || slowThreshold < 0) { toastErr('慢请求阈值必须 >= 0'); return; }
	const enabled = document.getElementById('setting-full-recording-enabled').checked;
	const mode = (document.querySelector('input[name="full-recording-mode"]:checked') || {}).value || 'all';
	let keyIDs = [];
	if (mode === 'selected') {
		keyIDs = Array.from(document.querySelectorAll('#setting-full-recording-keys input:checked')).map(input => Number(input.value));
		if (enabled && keyIDs.length === 0) { toastErr('请选择至少一个下游密钥'); return; }
	}
	api('/settings', {method:'PUT', body: JSON.stringify({
		auto_disable_threshold: threshold,
		slow_request_threshold_ms: slowThreshold,
		full_recording_enabled: enabled,
		full_recording_all_keys: mode === 'all',
		full_recording_key_ids: keyIDs
	})}).then(d => {
        if (d && d.error) { toastErr(d.error); return; }
		slowRequestThreshold = slowThreshold;
        loadSettings();
        toastOk('设置已保存');
    }).catch(() => toastErr('保存失败'));
}

// --- Test Models ---
let allTestModels = [];
function loadTestModels() {
    return api('/test-models').then(data => {
        allTestModels = Array.isArray(data) ? data.map(m => ({
            id: m.id || m.ID,
            name: m.name || m.Name || '',
            protocol: m.protocol || m.Protocol || 'openai',
            created_at: m.created_at || m.CreatedAt
        })) : [];
        renderTestModels();
        updateTuModelDatalist();
    }).catch(() => { allTestModels = []; renderTestModels(); });
}

function renderTestModels() {
    const search = (document.getElementById('tm-search').value || '').toLowerCase();
    const protocolFilter = document.getElementById('tm-filter-protocol').value;
    const tbody = document.getElementById('test-models-table');
    let filtered = allTestModels;
    if (protocolFilter) filtered = filtered.filter(m => (m.protocol||'') === protocolFilter);
    if (search) filtered = filtered.filter(m => (m.name||'').toLowerCase().indexOf(search) !== -1);
    if (filtered.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4" class="empty-state">暂无测试模型</td></tr>';
        return;
    }
    const protoLabel = {openai:'OpenAI',anthropic:'Anthropic',responses:'Responses'};
    tbody.innerHTML = filtered.map(m => {
        const name = m.name || '(未命名)';
        const proto = m.protocol || 'openai';
        return '<tr><td>'+m.id+'</td><td><code>'+esc(name)+'</code></td><td><span class="badge badge-purple">'+(protoLabel[proto]||proto)+'</span></td><td class="actions">'+
        '<button class="btn btn-ghost btn-sm" onclick="editTestModel('+m.id+')">编辑</button> '+
        '<button class="btn btn-danger btn-sm" onclick="deleteTestModel('+m.id+')">删除</button>'+
        '</td></tr>';
    }).join('');
}

function createTestModel() {
    const nameEl = document.getElementById('tm-new-name');
    const protoEl = document.getElementById('tm-new-protocol');
    const name = (nameEl.value || '').trim();
    const protocol = protoEl.value || 'openai';
    if (!name) { toastErr('请输入模型名称'); return; }
    api('/test-models', {method:'POST', body: JSON.stringify({name:name, protocol:protocol})}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        nameEl.value = '';
        loadTestModels();
    });
}

function editTestModel(id) {
    const m = allTestModels.find(x => x.id === id);
    if (!m) return;
    const row = document.querySelector('tr:has(button[onclick="editTestModel('+id+')"])');
    if (!row) return;
    const curName = m.name || '';
    const curProto = m.protocol || 'openai';
    row.innerHTML = '<td>'+m.id+'</td>'+
        '<td><input id="em-name-'+id+'" value="'+esc(curName)+'" style="font-size:0.85rem;padding:6px 10px;width:100%;"></td>'+
        '<td><select id="em-proto-'+id+'" style="font-size:0.85rem;padding:6px 10px;"><option value="openai"'+(curProto==='openai'?' selected':'')+'>OpenAI</option><option value="anthropic"'+(curProto==='anthropic'?' selected':'')+'>Anthropic</option><option value="responses"'+(curProto==='responses'?' selected':'')+'>Responses</option></select></td>'+
        '<td class="actions">'+
        '<button class="btn btn-primary btn-sm" onclick="saveTestModel('+id+')">保存</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="renderTestModels()">取消</button>'+
        '</td>';
}

function saveTestModel(id) {
    const name = (document.getElementById('em-name-'+id).value||'').trim();
    const protocol = document.getElementById('em-proto-'+id).value;
    if (!name) { toastErr('名称不能为空'); return; }
    api('/test-models/'+id, {method:'PUT', body: JSON.stringify({name:name, protocol:protocol})}).then(d => {
        if (d.error) toastErr(d.error); else { loadTestModels(); toastOk('已更新'); }
    });
}

async function deleteTestModel(id) {
    if (!await askConfirm('确认删除此测试模型？', {title:'删除模型', okText:'删除', danger:true})) return;
    api('/test-models/'+id, {method:'DELETE'}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        loadTestModels();
    });
}

function updateTuModelDatalist() {
    // Datalist lists unique model names across all protocols (same model may support many).
    const dl = document.getElementById('tu-model-list');
    if (!dl) return;
    const seen = {};
    const names = [];
    (allTestModels || []).forEach(m => {
        const n = (m.name || '').trim();
        if (!n || seen[n]) return;
        seen[n] = true;
        names.push(n);
    });
    names.sort((a, b) => a.localeCompare(b));
    dl.innerHTML = names.map(n => '<option value="'+esc(n)+'">').join('');
}

// --- 配置导入导出 ---
function exportConfig() {
    window.open('/admin/api/config/export', '_blank');
}

function importConfig() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async (e) => {
        const file = e.target.files[0];
        if (!file) return;
        const text = await file.text();
        try {
            JSON.parse(text);
        } catch(err) {
            toastErr('无效的 JSON 文件');
            return;
        }
        if (!await askConfirm('确认导入配置？将添加新的上游和 Key（不覆盖现有配置）。', {title:'导入配置', okText:'导入'})) return;
        api('/config/import', {method:'POST', body: text}).then(d => {
            if (d.error) toastErr(d.error);
            else { toastOk(d.message || '配置已导入'); loadUpstreams(); loadKeys(); }
        }).catch(() => toastErr('导入失败'));
    };
    input.click();
}
