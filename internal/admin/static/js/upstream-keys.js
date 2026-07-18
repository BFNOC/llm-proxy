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
