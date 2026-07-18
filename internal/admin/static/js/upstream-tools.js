function getCFConfig(upstreamId) {
    try {
        const raw = localStorage.getItem('cf_config_'+upstreamId);
        if (!raw) return null;
        const cfg = JSON.parse(raw);
        if (cfg.cf_clearance && cfg.cf_user_agent) return cfg;
    } catch(e) {}
    return null;
}
function openCFDialog(upstreamId) {
    document.getElementById('cf-upstream-id').value = upstreamId;
    const cfg = getCFConfig(upstreamId) || {};
    document.getElementById('cf-clearance').value = cfg.cf_clearance || '';
    document.getElementById('cf-ua').value = cfg.cf_user_agent || '';
    document.getElementById('dlg-cf').showModal();
}

function saveCFConfig() {
    const id = document.getElementById('cf-upstream-id').value;
    const clearance = document.getElementById('cf-clearance').value.trim();
    const ua = document.getElementById('cf-ua').value.trim();
    if (!clearance && !ua) {
        localStorage.removeItem('cf_config_'+id);
    } else if (!clearance || !ua) {
        toastErr('cf_clearance 和 User-Agent 需要同时填写');
        return;
    } else {
        localStorage.setItem('cf_config_'+id, JSON.stringify({cf_clearance: clearance, cf_user_agent: ua}));
    }
    document.getElementById('dlg-cf').close();
    loadUpstreams();
}

function clearCFConfig() {
    const id = document.getElementById('cf-upstream-id').value;
    localStorage.removeItem('cf_config_'+id);
    document.getElementById('cf-clearance').value = '';
    document.getElementById('cf-ua').value = '';
    document.getElementById('dlg-cf').close();
    loadUpstreams();
}

// --- WebSocket 测试与开关 ---
function testWebSocket(e, id) {
    const btn = e.target;
    const row = btn.closest('tr');
    const existingRow = document.getElementById('ws-row-'+id);
    if (existingRow) { existingRow.remove(); return; }
    document.querySelectorAll('[id^="ws-row-"]').forEach(r => r.remove());
    const origText = btn.textContent;
    btn.textContent = '...';
    btn.disabled = true;
    api('/upstreams/'+id+'/test-websocket', {method:'POST'}).then(d => {
        btn.textContent = origText;
        btn.disabled = false;
        const tr = document.createElement('tr');
        tr.id = 'ws-row-'+id;
        const td = document.createElement('td');
        td.colSpan = 12;
        td.style.cssText = 'padding:0;border:none;';
        if (d.websocket_supported) {
            td.innerHTML = '<div style="background:var(--bg);border:1px solid rgba(16,185,129,0.25);border-radius:var(--radius-sm);padding:14px 18px;margin:8px 16px;display:flex;align-items:center;justify-content:space-between;">'+
                '<div style="display:flex;align-items:center;gap:8px;"><span style="color:var(--green);font-weight:600;">✓ WebSocket 支持</span><span style="font-size:0.8rem;color:var(--text-dim);">'+esc(d.message||'')+'</span></div>'+
                '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()">✕</button></div>';
            loadUpstreams();
        } else {
            td.innerHTML = '<div style="background:var(--bg);border:1px solid rgba(239,68,68,0.25);border-radius:var(--radius-sm);padding:14px 18px;margin:8px 16px;display:flex;align-items:center;justify-content:space-between;">'+
                '<div style="display:flex;align-items:center;gap:8px;"><span style="color:var(--red);font-weight:600;">✗ WebSocket 不支持</span><span style="font-size:0.8rem;color:var(--text-dim);">'+esc(d.message||d.error||'')+'</span></div>'+
                '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()">✕</button></div>';
        }
        tr.appendChild(td);
        row.after(tr);
    }).catch(err => {
        btn.textContent = origText;
        btn.disabled = false;
        toastErr('WebSocket 测试失败: '+err.message);
    });
}

function toggleWebSocket(id, enabled) {
    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify({websocket_enabled:enabled})}).then(d => {
        if(d.error) toastErr(d.error); else { loadUpstreams(); toastOk(enabled?'已开启 WebSocket':'已关闭 WebSocket'); }
    });
}

// --- 模型自动发现开关 ---
function toggleAutoDiscover(id, enabled) {
    api('/upstreams/'+id+'/auto-discover', {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(function(d) {
        if(d.error) toastErr(d.error); else { loadUpstreams(); toastOk(enabled?'已开启模型自动发现':'已关闭模型自动发现'); }
    });
}

// --- 拖拽排序 ---
var dragSrcId = null;
function onDragStart(e, id) {
    dragSrcId = id;
    e.dataTransfer.effectAllowed = 'move';
    e.target.closest('tr').style.opacity = '0.4';
}
function onDragOver(e) {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    var tr = e.target.closest('tr');
    if (tr) tr.style.borderTop = '2px solid var(--accent)';
}
function onDragLeave(e) {
    var tr = e.target.closest('tr');
    if (tr) tr.style.borderTop = '';
}
function onDragEnd(e) {
    e.target.closest('tr').style.opacity = '';
    document.querySelectorAll('#upstreams-table tr').forEach(function(r) { r.style.borderTop = ''; });
}
function onDrop(e, targetId) {
    e.preventDefault();
    var tr = e.target.closest('tr');
    if (tr) tr.style.borderTop = '';
    if (dragSrcId === targetId) return;
    var ids = allUpstreams.map(function(u) { return u.id; });
    var srcIdx = ids.indexOf(dragSrcId);
    var tgtIdx = ids.indexOf(targetId);
    if (srcIdx < 0 || tgtIdx < 0) return;
    ids.splice(srcIdx, 1);
    ids.splice(tgtIdx, 0, dragSrcId);
    api('/upstreams/reorder', {method:'PUT', body: JSON.stringify({ids: ids})}).then(function(d) {
        if(d.error) toastErr(d.error); else { loadUpstreams(); toastOk('排序已更新'); }
    });
}

// --- 上游模板 ---
function loadTemplates() {
    api('/upstream-templates').then(templates => {
        const container = document.getElementById('template-buttons');
        if (!container || !templates || !templates.length) return;
        container.innerHTML = templates.map(t =>
            '<button class="btn btn-ghost btn-sm" onclick="applyTemplate(\''+esc(t.name)+'\',\''+esc(t.base_url)+'\',\''+esc(t.auth_mode)+'\')">'+esc(t.name)+'</button>'
        ).join('');
    });
}
function applyTemplate(name, baseURL, authMode) {
    document.querySelector('#dlg-upstream [name=name]').value = name;
    document.querySelector('#dlg-upstream [name=base_url]').value = baseURL;
    if (authMode) document.querySelector('#dlg-upstream [name=auth_mode]').value = authMode;
    document.getElementById('dlg-upstream').showModal();
}
