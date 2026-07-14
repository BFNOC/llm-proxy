// --- Upstreams ---
let allUpstreams = [];
let allModelPatterns = {}; // upstream_id -> [patterns]
function loadUpstreams() {
    return Promise.all([api('/upstreams'), api('/upstreams/models')]).then(([data, mp]) => {
        allUpstreams = data || [];
        allModelPatterns = mp || {};
        renderUpstreamsTable();
    }).catch(() => {
        document.getElementById('upstreams-table').innerHTML = '<tr><td colspan="12" class="empty-state"><strong>加载失败</strong><p>无法获取上游列表，请刷新重试</p></td></tr>';
        toastErr('加载上游失败');
    });
}
function selectedUpstreamIDs() {
    return Array.from(document.querySelectorAll('.upstream-cb:checked')).map(cb => parseInt(cb.value, 10)).filter(n => !isNaN(n) && n > 0);
}
function toggleAllUpstreamCheckboxes(checked) {
    document.querySelectorAll('.upstream-cb').forEach(cb => { cb.checked = checked; });
    updateUpstreamBatchBtns();
}
function updateUpstreamBatchBtns() {
    // Keep batch controls always in-layout (disabled when empty) so checkbox clicks
    // do not toggle display:none and reflow the table downward.
    const n = document.querySelectorAll('.upstream-cb:checked').length;
    const has = n > 0;
    const en = document.getElementById('btn-batch-enable-upstreams');
    const dis = document.getElementById('btn-batch-disable-upstreams');
    const del = document.getElementById('btn-batch-delete-upstreams');
    if (en) en.disabled = !has;
    if (dis) dis.disabled = !has;
    if (del) del.disabled = !has;
    const chip = document.getElementById('upstream-selected-count');
    if (chip) {
        chip.textContent = '已选 ' + n;
        chip.style.visibility = has ? 'visible' : 'hidden';
    }
    const selAll = document.getElementById('upstream-select-all');
    const total = document.querySelectorAll('.upstream-cb').length;
    if (selAll) {
        selAll.indeterminate = has && n < total;
        selAll.checked = total > 0 && n === total;
    }
}
function renderUpstreamsTable() {
    const tbody = document.getElementById('upstreams-table');
    const q = ((document.getElementById('upstream-search')||{}).value || '').trim().toLowerCase();
    const en = (document.getElementById('upstream-filter-enabled')||{}).value || '';
    let list = allUpstreams.slice();
    if (en === '1') list = list.filter(u => u.enabled);
    if (en === '0') list = list.filter(u => !u.enabled);
    if (q) {
        list = list.filter(u => {
            const hay = [u.name, u.base_url, u.remark, u.proxy_url, String(u.id)].join(' ').toLowerCase();
            return hay.indexOf(q) !== -1;
        });
    }
    const countEl = document.getElementById('upstream-count');
    if (countEl) countEl.textContent = list.length + (list.length !== allUpstreams.length ? ' / ' + allUpstreams.length : '');
    const selAll = document.getElementById('upstream-select-all');
    if (selAll) selAll.checked = false;
    updateUpstreamBatchBtns();
    if (allUpstreams.length === 0) {
        tbody.innerHTML = '<tr><td colspan="12" class="empty-state"><strong>还没有上游</strong><p>添加第一个上游后即可开始转发请求</p><button class="btn btn-primary btn-sm" onclick="document.getElementById(\'dlg-upstream\').showModal()">添加上游</button></td></tr>';
        return;
    }
    if (list.length === 0) {
        tbody.innerHTML = '<tr><td colspan="12" class="empty-state"><strong>无匹配结果</strong><p>试试调整搜索词或状态筛选</p></td></tr>';
        return;
    }
    tbody.innerHTML = list.map(u => {
        const patterns = allModelPatterns[u.id] || [];
        let modelHtml = '<span class="model-tag-all">*</span>';
        if (patterns.length > 0) {
            modelHtml = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
        }
        const keyDetails = u.api_key_details || [];
        const totalKeys = keyDetails.length || (u.api_keys || []).length;
        const enabledKeys = keyDetails.filter(k => k.enabled).length;
        const allEnabled = totalKeys > 0 && enabledKeys === totalKeys;
        let keyBadge = '';
        if (totalKeys === 0) {
            keyBadge = '<span class="badge badge-green">无鉴权</span>';
        } else if (allEnabled) {
            keyBadge = '<span class="badge badge-purple" style="cursor:pointer" onclick="openManageKeysDialog('+u.id+')" title="点击管理">'+totalKeys+' Key</span>';
        } else {
            keyBadge = '<span class="badge badge-orange" style="cursor:pointer" onclick="openManageKeysDialog('+u.id+')" title="点击管理">'+enabledKeys+'/'+totalKeys+' Key</span>';
        }
        const schedMode = u.key_scheduling_mode || 'round-robin';
        const schedLabel = schedMode === 'fill' ? '填充' : '轮询';
        const schedColor = schedMode === 'fill' ? 'var(--orange)' : 'var(--accent)';
        const authMode = u.auth_mode || 'api_key';
        const authBadge = authMode === 'oauth'
            ? '<span class="badge badge-orange" title="Authorization: Bearer">OAuth</span>'
            : '<span class="badge badge-muted" title="x-api-key">API Key</span>';
        const remarkHtml = u.remark ? '<div style="font-size:0.75rem;color:var(--text-dim);margin-top:2px;" title="'+esc(u.remark)+'">'+esc(u.remark.length>28?u.remark.substring(0,28)+'...':u.remark)+'</div>' : '';
        return '<tr><td class="col-check"><input type="checkbox" class="upstream-cb" value="'+u.id+'" onchange="updateUpstreamBatchBtns()"></td><td class="hide-on-mobile">'+u.id+'</td><td><strong>'+esc(u.name)+'</strong>'+remarkHtml+'</td><td><code class="truncate-url" title="'+esc(u.base_url)+'">'+esc(u.base_url)+'</code></td><td class="hide-on-mobile">'+keyBadge+'</td><td class="hide-on-mobile">'+authBadge+'</td><td class="hide-on-mobile"><span style="font-size:0.75rem;color:'+schedColor+';font-weight:500;white-space:nowrap;">'+schedLabel+'</span></td><td class="hide-on-mobile">'+(u.proxy_url?'<code class="truncate-url" title="'+esc(u.proxy_url)+'">'+esc(u.proxy_url)+'</code>':'<span class="badge badge-muted">环境</span>')+'</td><td class="hide-on-mobile">'+u.priority+'</td><td class="hide-on-mobile"><div class="model-tags">'+modelHtml+'</div></td><td>'+
        (u.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
        (u.websocket_enabled?'<span class="badge badge-purple" title="WebSocket 已启用" style="margin-left:4px;">WS</span>':'')+
        '</td><td class="actions">'+
        '<button class="btn btn-ghost btn-sm" onclick="testProxy(event,'+u.id+')">测试</button>'+
        '<button class="btn btn-ghost btn-sm" onclick="testWebSocket(event,'+u.id+')" title="测试 WebSocket 连接">WS</button>'+
        '<button class="btn btn-ghost btn-sm" onclick="editUpstream('+u.id+')">编辑</button>'+
        '<div class="action-more"><button class="action-more-btn" onclick="toggleActionMenu(event)">···</button>'+
        '<div class="action-menu">'+
        '<button onclick="showHealthHistory('+u.id+')">健康历史</button>'+
        '<button onclick="checkQuota(event,'+u.id+')">查额</button>'+
        '<button onclick="openCFDialog('+u.id+')" style="'+(getCFConfig(u.id)?'color:var(--green)':'')+'">CF 绕过</button>'+
        '<button onclick="toggleWebSocket('+u.id+','+(!u.websocket_enabled)+')">'+(u.websocket_enabled?'关闭 WS':'开启 WS')+'</button>'+
        '<button onclick="openModelPatternsDialog('+u.id+')">模型模式</button>'+
        '<button onclick="openDeclaredModelsDialog('+u.id+')">声明模型</button>'+
        '<button onclick="toggleUpstream('+u.id+','+(!u.enabled)+')">'+(u.enabled?'禁用':'启用')+'</button>'+
        '<button class="menu-danger" onclick="deleteUpstream('+u.id+')">删除</button>'+
        '</div></div>'+
        '</td></tr>';
    }).join('');
    updateUpstreamBatchBtns();
}

function createUpstream(e) {
    e.preventDefault();
    addAPIKeyFromInput('create');
    const f = new FormData(e.target);
    const apiKeys = getAPIKeyEditorKeys('create');
    api('/upstreams', {method:'POST', body: JSON.stringify({
        name: f.get('name'), base_url: f.get('base_url'),
        api_keys: apiKeys, proxy_url: f.get('proxy_url')||'',
        priority: parseInt(f.get('priority')||'0'),
        key_scheduling_mode: f.get('key_scheduling_mode')||'round-robin',
        auth_mode: f.get('auth_mode')||'api_key',
        remark: f.get('remark')||''
    })}).then(d => {
        if(d.error) toastErr(d.error);
        else { e.target.reset(); setAPIKeyEditor('create', []); document.getElementById('dlg-upstream').close(); loadUpstreams(); toastOk('上游已创建'); }
    }).catch(() => toastErr('创建上游失败'));
}

function editUpstream(id) {
    const u = allUpstreams.find(x => x.id === id);
    if (!u) return;
	    const dlg = document.getElementById('dlg-edit-upstream');
    dlg.querySelector('[name=id]').value = id;
    dlg.querySelector('[name=name]').value = u.name;
    dlg.querySelector('[name=base_url]').value = u.base_url;
	    setAPIKeyEditor('edit', u.api_key_details ? u.api_key_details : (u.api_keys || []).map(k => ({key:k})));
    dlg.querySelector('[name=proxy_url]').value = u.proxy_url||'';
    dlg.querySelector('[name=priority]').value = u.priority;
    dlg.querySelector('[name=key_scheduling_mode]').value = u.key_scheduling_mode || 'round-robin';
    dlg.querySelector('[name=auth_mode]').value = u.auth_mode || 'api_key';
    dlg.querySelector('[name=remark]').value = u.remark || '';
    dlg.showModal();
}

	function submitEditUpstream(e) {
	    e.preventDefault();
	    addAPIKeyFromInput('edit');
	    const f = new FormData(e.target);
	    const id = parseInt(f.get('id'), 10);
	    const originalRows = upstreamKeyEditorMeta.edit.originalRows || [];
	    const currentRows = upstreamKeyEditorMeta.edit.rows || [];
	    const currentRowIds = new Set(currentRows.filter(r => r.row_id).map(r => r.row_id));
	    const deletedRows = originalRows.filter(r => r.row_id && !currentRowIds.has(r.row_id));
	    const addedKeys = currentRows.filter(r => !r.row_id).map(r => r.key);
	    const body = {name: f.get('name'), base_url: f.get('base_url'), proxy_url: f.get('proxy_url')||'', priority: parseInt(f.get('priority')||'0'), key_scheduling_mode: f.get('key_scheduling_mode')||'round-robin', auth_mode: f.get('auth_mode')||'api_key', remark: f.get('remark')||''};
	    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify(body)}).then(d => {
	        if(d.error) { toastErr(d.error); return; }
	        return Promise.all([
	            ...deletedRows.map(row => api('/upstreams/'+id+'/apikeys/'+row.row_id, {method:'DELETE'})),
	            ...(addedKeys.length > 0 ? [api('/upstreams/'+id+'/apikeys', {method:'POST', body: JSON.stringify({api_keys: addedKeys})})] : [])
	        ]);
	    }).then(results => {
	        if (!results) return;
	        const failed = results.find(r => r && r.error);
	        if (failed) { toastErr(failed.error); return; }
	        document.getElementById('dlg-edit-upstream').close();
	        toastOk('上游已保存');
	        loadUpstreams();
	    });
	}

async function deleteUpstream(id) {
    const u = allUpstreams.find(x => x.id === id);
    const name = u ? u.name : ('#'+id);
    if(!await askConfirm('删除上游「'+name+'」后，相关 Key 与绑定也会被清理。此操作不可恢复。', {title:'删除上游', okText:'删除', danger:true})) return;
    api('/upstreams/'+id, {method:'DELETE'}).then(d => {
        if(d && d.error) toastErr(d.error); else { loadUpstreams(); toastOk('已删除上游'); }
    }).catch(() => toastErr('删除失败'));
}

function toggleUpstream(id, enabled) {
    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if(d.error) toastErr(d.error); else { loadUpstreams(); toastOk('已更新'); }
    });
}

async function batchSetUpstreamsEnabled(enabled) {
    const ids = selectedUpstreamIDs();
    if (ids.length === 0) { toastErr('请先勾选上游'); return; }
    const action = enabled ? '启用' : '禁用';
    if (!await askConfirm('确认批量' + action + '选中的 ' + ids.length + ' 个上游？', {title:'批量' + action, okText:action})) return;
    api('/upstreams/batch/enabled', {method:'PUT', body: JSON.stringify({ids: ids, enabled: enabled})}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        toastOk('已' + action + ' ' + (d.updated != null ? d.updated : ids.length) + ' 个上游');
        loadUpstreams();
    }).catch(() => toastErr('批量' + action + '失败'));
}

async function batchDeleteUpstreams() {
    const ids = selectedUpstreamIDs();
    if (ids.length === 0) { toastErr('请先勾选上游'); return; }
    if (!await askConfirm('删除选中的 ' + ids.length + ' 个上游后，相关 Key 与绑定也会被清理。此操作不可恢复。', {title:'批量删除上游', okText:'删除', danger:true})) return;
    api('/upstreams/batch', {method:'DELETE', body: JSON.stringify({ids: ids})}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        toastOk('已删除 ' + (d.deleted != null ? d.deleted : ids.length) + ' 个上游');
        loadUpstreams();
    }).catch(() => toastErr('批量删除失败'));
}

function normalizeAPIKeyInput(raw) {
    return (raw || '').split(/\r?\n|,/).map(s => s.trim()).filter(Boolean);
}
function shortAPIKey(key) {
    return key.length > 24 ? key.substring(0, 12) + '...' + key.substring(key.length - 8) : key;
}
	function setAPIKeyEditor(mode, keys) {
	    upstreamKeyEditors[mode] = [];
	    const rows = [];
	    (keys || []).forEach(item => {
	        const rowId = item && typeof item === 'object' ? item.row_id : null;
	        const rawKey = item && typeof item === 'object' ? item.key : item;
	        normalizeAPIKeyInput(rawKey).forEach(k => {
	            if (!upstreamKeyEditors[mode].includes(k)) {
	                upstreamKeyEditors[mode].push(k);
	                rows.push({key:k, row_id:rowId});
	            }
	        });
	    });
	    upstreamKeyEditorMeta[mode] = {rows: rows, originalRows: rows.map(r => ({...r}))};
	    renderAPIKeyEditor(mode);
	}
function getAPIKeyEditorKeys(mode) {
    return (upstreamKeyEditors[mode] || []).slice();
}
function renderAPIKeyEditor(mode) {
    const editor = document.querySelector('[data-key-editor="'+mode+'"]');
    if (!editor) return;
    const list = editor.querySelector('[data-key-list]');
	    const keys = upstreamKeyEditors[mode] || [];
	    const rows = (upstreamKeyEditorMeta[mode] && upstreamKeyEditorMeta[mode].rows) || [];
	    if (keys.length === 0) {
        list.className = 'api-key-list is-empty';
        list.innerHTML = '暂无 API Key';
        return;
    }
	    list.className = 'api-key-list';
	    list.innerHTML = keys.map((key, idx) => (
	        '<div class="api-key-row">'+
	        '<code title="'+esc(key)+'">'+esc(shortAPIKey(key))+'</code>'+
	        (rows[idx] && rows[idx].enabled === false ? '<span class="badge badge-red">禁用</span>' : '')+
	        '<button type="button" class="btn btn-ghost btn-sm" onclick="copyAPIKeyFromEditor(\''+mode+'\','+idx+',this)">复制</button>'+
	        '<button type="button" class="btn btn-danger btn-sm" onclick="removeAPIKeyFromEditor(\''+mode+'\','+idx+')">删除</button>'+
	        '</div>'
    )).join('');
}
function copyAPIKeyFromEditor(mode, index, btn) {
    const key = (upstreamKeyEditors[mode] || [])[index];
    if (!key) return;
    copyTextToClipboard(key, btn);
}
function addAPIKeyFromInput(mode) {
    const editor = document.querySelector('[data-key-editor="'+mode+'"]');
    if (!editor) return;
    const input = editor.querySelector('[data-key-input]');
    const incoming = normalizeAPIKeyInput(input.value);
    if (incoming.length === 0) return;
	    upstreamKeyEditors[mode] = upstreamKeyEditors[mode] || [];
	    upstreamKeyEditorMeta[mode] = upstreamKeyEditorMeta[mode] || {rows: [], originalRows: []};
	    upstreamKeyEditorMeta[mode].rows = upstreamKeyEditorMeta[mode].rows || [];
	    incoming.forEach(key => {
	        if (!upstreamKeyEditors[mode].includes(key)) {
	            upstreamKeyEditors[mode].push(key);
	            upstreamKeyEditorMeta[mode].rows.push({key:key, row_id:null});
	        }
	    });
    input.value = '';
    renderAPIKeyEditor(mode);
    input.focus();
}
	function removeAPIKeyFromEditor(mode, index) {
	    upstreamKeyEditors[mode].splice(index, 1);
	    if (upstreamKeyEditorMeta[mode] && upstreamKeyEditorMeta[mode].rows) {
	        upstreamKeyEditorMeta[mode].rows.splice(index, 1);
	    }
	    renderAPIKeyEditor(mode);
	}
function handleAPIKeyInputKeydown(event, mode) {
    if (event.key === 'Enter') {
        event.preventDefault();
        addAPIKeyFromInput(mode);
    }
}

// --- Per-Key API Key Management ---
let manageKeysFetchSeq = 0;
function openManageKeysDialog(upstreamId) {
    document.getElementById('mk-upstream-id').value = upstreamId;
    const list = document.getElementById('mk-keys-list');
    list.innerHTML = '<div style="text-align:center;padding:16px;color:var(--text-dim)">加载中...</div>';
    const dlg = document.getElementById('dlg-manage-keys');
    if (!dlg.open) dlg.showModal();
    const seq = ++manageKeysFetchSeq;
    api('/upstreams/'+upstreamId+'/apikeys').then(data => {
        // Ignore stale responses so a slow GET cannot overwrite a newer toggle refresh.
        if (seq !== manageKeysFetchSeq) return;
        manageAPIKeyRows = data || [];
        if (manageAPIKeyRows.length === 0) {
            list.innerHTML = '<div class="empty-state">无 API Key</div>';
            return;
        }
        list.innerHTML = manageAPIKeyRows.map((kd, idx) => {
            const shortKey = kd.key.length > 20 ? kd.key.substring(0, 10) + '...' + kd.key.substring(kd.key.length - 8) : kd.key;
            const isOn = !!kd.enabled;
            return '<div style="display:flex;align-items:center;gap:10px;padding:12px 14px;background:var(--bg);border-radius:var(--radius-sm);margin-bottom:8px;border:1px solid '+(isOn?'var(--border)':'rgba(239,68,68,0.2)')+';'+(isOn?'':'opacity:0.6;')+'">'+
                '<code style="flex:1;font-size:0.82rem;word-break:break-all;" title="'+esc(kd.key)+'">'+esc(shortKey)+'</code>'+
                '<button type="button" class="btn btn-ghost btn-sm" onclick="copyManagedAPIKey('+idx+',this)">复制</button>'+
                '<button type="button" class="btn '+(isOn?'btn-ghost':'btn-success')+' btn-sm" onclick="toggleAPIKey('+upstreamId+','+kd.row_id+','+(!isOn)+')">'+(isOn?'禁用':'启用')+'</button>'+
                '<button type="button" class="btn btn-danger btn-sm" onclick="deleteAPIKey('+upstreamId+','+kd.row_id+')">删除</button>'+
                '</div>';
        }).join('');
    });
}

function copyManagedAPIKey(index, btn) {
    const row = manageAPIKeyRows[index];
    if (!row || !row.key) return;
    copyTextToClipboard(row.key, btn);
}

function toggleAPIKey(upstreamId, keyRowId, enabled) {
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId+'/enabled', {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if(d.error) { toastErr(d.error); return; }
        loadUpstreams();
        openManageKeysDialog(upstreamId);
        toastOk(enabled ? '已启用' : '已禁用');
    });
}

async function deleteAPIKey(upstreamId, keyRowId) {
    if(!await askConfirm('确认删除此 API Key？删除后不可恢复。', {title:'删除 Key', okText:'删除', danger:true})) return;
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId, {method:'DELETE'}).then(d => {
        if(d.error) toastErr(d.error); else { loadUpstreams(); openManageKeysDialog(upstreamId); toastOk('已删除'); }
    });
}


// --- Upstream Model Patterns ---
let mpCurrentPatterns = [];
function openModelPatternsDialog(upstreamId) {
    document.getElementById('mp-upstream-id').value = upstreamId;
    document.getElementById('mp-new-pattern').value = '';
    mpCurrentPatterns = (allModelPatterns[upstreamId] || []).slice();
    renderModelPatternTags();
    document.getElementById('dlg-model-patterns').showModal();
}

function renderModelPatternTags() {
    const container = document.getElementById('mp-tags');
    if (mpCurrentPatterns.length === 0) {
        container.innerHTML = '<span class="model-tag-all">无模式（*）</span>';
        return;
    }
    container.innerHTML = mpCurrentPatterns.map((p, i) =>
        '<span class="model-tag">' + esc(p) + ' <span style="cursor:pointer;margin-left:2px;opacity:0.7" onclick="removeModelPatternTag('+i+')">✕</span></span>'
    ).join('');
}

function addModelPatternTag() {
    const input = document.getElementById('mp-new-pattern');
    const v = input.value.trim();
    if (!v) return;
    if (mpCurrentPatterns.includes(v)) { input.value = ''; return; }
    mpCurrentPatterns.push(v);
    input.value = '';
    renderModelPatternTags();
}

function removeModelPatternTag(idx) {
    mpCurrentPatterns.splice(idx, 1);
    renderModelPatternTags();
}

function saveModelPatterns() {
    const id = document.getElementById('mp-upstream-id').value;
    api('/upstreams/'+id+'/models', {method:'PUT', body: JSON.stringify({patterns: mpCurrentPatterns})}).then(d => {
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-model-patterns').close(); loadUpstreams(); }
    });
}

// --- Declared Models ---
let dmCurrentModels = [];
function openDeclaredModelsDialog(upstreamId) {
    document.getElementById('dm-upstream-id').value = upstreamId;
    document.getElementById('dm-new-model').value = '';
    api('/upstreams/'+upstreamId+'/declared-models').then(d => {
        dmCurrentModels = (d && d.models) ? d.models.slice() : [];
        renderDeclaredModelTags();
    });
    document.getElementById('dlg-declared-models').showModal();
}

function renderDeclaredModelTags() {
    const container = document.getElementById('dm-tags');
    if (dmCurrentModels.length === 0) {
        container.innerHTML = '<span class="model-tag-all">无声明模型</span>';
        return;
    }
    container.innerHTML = dmCurrentModels.map((m, i) =>
        '<span class="model-tag">' + esc(m) + ' <span style="cursor:pointer;margin-left:2px;opacity:0.7" onclick="removeDeclaredModelTag('+i+')">✕</span></span>'
    ).join('');
}

function addDeclaredModelTag() {
    const input = document.getElementById('dm-new-model');
    const v = input.value.trim();
    if (!v) return;
    if (dmCurrentModels.includes(v)) { input.value = ''; return; }
    dmCurrentModels.push(v);
    input.value = '';
    renderDeclaredModelTags();
}

function removeDeclaredModelTag(idx) {
    dmCurrentModels.splice(idx, 1);
    renderDeclaredModelTags();
}

function saveDeclaredModels() {
    const id = document.getElementById('dm-upstream-id').value;
    api('/upstreams/'+id+'/declared-models', {method:'PUT', body: JSON.stringify({models: dmCurrentModels})}).then(d => {
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-declared-models').close(); loadUpstreams(); }
    });
}

// --- CF Bypass Config (localStorage) ---
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

