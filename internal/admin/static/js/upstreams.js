// --- Upstreams ---
let allUpstreams = [];
let allModelPatterns = {}; // upstream_id -> [patterns]
let allCircuitStatus = {};
function loadUpstreams() {
    return Promise.all([api('/upstreams'), api('/upstreams/models'), api('/upstreams/circuit-status')]).then(([data, mp, cs]) => {
        allUpstreams = data || [];
        allModelPatterns = mp || {};
        allCircuitStatus = cs || {};
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
        return '<tr draggable="true" ondragstart="onDragStart(event,'+u.id+')" ondragover="onDragOver(event)" ondrop="onDrop(event,'+u.id+')" ondragend="onDragEnd(event)" ondragleave="onDragLeave(event)"><td class="col-check"><input type="checkbox" class="upstream-cb" value="'+u.id+'" onchange="updateUpstreamBatchBtns()"></td><td class="hide-on-mobile">'+u.id+'</td><td><strong>'+esc(u.name)+'</strong>'+remarkHtml+'</td><td><code class="truncate-url" title="'+esc(u.base_url)+'">'+esc(u.base_url)+'</code></td><td class="hide-on-mobile">'+keyBadge+'</td><td class="hide-on-mobile">'+authBadge+'</td><td class="hide-on-mobile"><span style="font-size:0.75rem;color:'+schedColor+';font-weight:500;white-space:nowrap;">'+schedLabel+'</span></td><td class="hide-on-mobile">'+(u.proxy_url?'<code class="truncate-url" title="'+esc(u.proxy_url)+'">'+esc(u.proxy_url)+'</code>':'<span class="badge badge-muted">环境</span>')+'</td><td class="hide-on-mobile">'+u.priority+'</td><td class="hide-on-mobile"><div class="model-tags">'+modelHtml+'</div></td><td>'+
        (u.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
        (function(){ var cs = allCircuitStatus[String(u.id)]; if(cs==='open') return '<span class="badge badge-red" title="熔断中">熔断</span>'; if(cs==='half_open') return '<span class="badge badge-orange" title="恢复中">恢复中</span>'; return ''; })()+
        (u.websocket_enabled?'<span class="badge badge-purple" title="WebSocket 已启用" style="margin-left:4px;">WS</span>':'')+
        (u.auto_discover_models?'<span class="badge badge-green" title="模型自动发现" style="margin-left:4px;">发现</span>':'')+
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
        '<button onclick="toggleAutoDiscover('+u.id+','+(!u.auto_discover_models)+')">'+(u.auto_discover_models?'关闭自动发现':'开启自动发现')+'</button>'+
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
    if(!await askConfirm('删除上游「'+name+'」？可在 60 秒内撤销。', {title:'删除上游', okText:'删除', danger:true})) return;
    api('/upstreams/'+id, {method:'DELETE'}).then(d => {
        if(d && d.error) { toastErr(d.error); return; }
        loadUpstreams();
        showUndoToast(id, d.undo_seconds || 60);
    }).catch(() => toastErr('删除失败'));
}
function showUndoToast(id, seconds) {
    const toast = document.createElement('div');
    toast.className = 'toast-undo';
    toast.innerHTML = '上游已删除 <button onclick="undoDelete('+id+',this.parentElement)">撤销</button> <span class="countdown">'+seconds+'s</span>';
    document.body.appendChild(toast);
    let remaining = seconds;
    const timer = setInterval(() => {
        remaining--;
        toast.querySelector('.countdown').textContent = remaining+'s';
        if (remaining <= 0) { clearInterval(timer); toast.remove(); }
    }, 1000);
}
function undoDelete(id, toastEl) {
    api('/upstreams/'+id+'/undo', {method:'POST'}).then(d => {
        if(d.error) toastErr(d.error);
        else { toastOk('已撤销删除'); loadUpstreams(); }
        if(toastEl) toastEl.remove();
    });
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

