// --- Keys ---
function loadKeys() {
    Promise.all([api('/keys'), api('/keys/bindings'), api('/key-rpm'), api('/keys/model-overrides')]).then(([data, bindMap, rpmData, overrideMap]) => {
        keysCache = data || [];
        keysBindMap = bindMap || {};
        keysRpmData = rpmData || {};
        keysOverrideMap = overrideMap || {};
        renderKeysTable();
    }).catch(() => {
        document.getElementById('keys-table').innerHTML = '<tr><td colspan="9" class="empty-state"><strong>加载失败</strong><p>无法获取密钥列表</p></td></tr>';
        toastErr('加载密钥失败');
    });
    loadKeyUsageStats();
}
function renderKeysTable() {
    const tbody = document.getElementById('keys-table');
    const q = ((document.getElementById('key-search')||{}).value || '').trim().toLowerCase();
    const en = (document.getElementById('key-filter-enabled')||{}).value || '';
    let list = keysCache.slice();
    if (en === '1') list = list.filter(k => k.enabled);
    if (en === '0') list = list.filter(k => !k.enabled);
    if (q) {
        list = list.filter(k => {
            const hay = [k.name, k.key_prefix, String(k.id)].join(' ').toLowerCase();
            return hay.indexOf(q) !== -1;
        });
    }
    const countEl = document.getElementById('key-count');
    if (countEl) countEl.textContent = list.length + (list.length !== keysCache.length ? ' / ' + keysCache.length : '');
    if (keysCache.length === 0) {
        tbody.innerHTML = '<tr><td colspan="9" class="empty-state"><strong>还没有下游密钥</strong><p>创建密钥后，客户端用它访问代理</p><button class="btn btn-primary btn-sm" onclick="document.getElementById(\'dlg-key\').showModal()">创建密钥</button></td></tr>';
        return;
    }
    if (list.length === 0) {
        tbody.innerHTML = '<tr><td colspan="9" class="empty-state"><strong>无匹配结果</strong><p>试试调整搜索词或状态筛选</p></td></tr>';
        return;
    }
    tbody.innerHTML = list.map(k => {
        const bound = keysBindMap[k.id] || [];
        let bindText = '<span class="badge badge-purple">全部</span>';
        if (bound.length > 0) {
            const names = bound.map(uid => { const u = allUpstreams.find(x=>x.id===uid); return u ? esc(u.name) : uid; });
            bindText = names.join(', ');
        }
        const overrides = keysOverrideMap[k.id] || [];
        let overrideText = '<span style="color:var(--text-dim)">无</span>';
        if (overrides.length > 0) {
            const patterns = [...new Set(overrides.map(o => o.ModelPattern))];
            overrideText = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
        }
        const currentRpm = keysRpmData[k.id] || 0;
        const limitText = k.rpm_limit || '不限';
        const rpmColor = k.rpm_limit > 0 && currentRpm >= k.rpm_limit * 0.8 ? 'var(--red)' : currentRpm > 0 ? 'var(--green)' : 'var(--text-dim)';
        return '<tr><td class="hide-on-mobile">'+k.id+'</td><td class="hide-on-mobile"><code>'+esc(k.key_prefix)+'...</code></td><td>'+esc(k.name)+'</td><td>'+(k.rpm_limit||'不限')+'</td><td><span style="color:'+rpmColor+';font-weight:600">'+currentRpm+'</span><span style="color:var(--text-dim)">/'+ limitText+'</span></td><td>'+
        (k.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
        '</td><td class="hide-on-mobile">'+bindText+'</td><td class="hide-on-mobile"><div class="model-tags" style="gap:4px">'+overrideText+'</div></td><td class="actions">'+
        '<button class="btn btn-ghost btn-sm" onclick="copyKey(event,'+k.id+')">复制</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="openBindingDialog('+k.id+')">绑定</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="openOverrideDialog('+k.id+')">路由</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="editKey('+k.id+')">编辑</button> '+
        '<button class="btn btn-success btn-sm" onclick="toggleKey('+k.id+','+(!k.enabled)+')">'+(k.enabled?'禁用':'启用')+'</button> '+
        '<button class="btn btn-danger btn-sm" onclick="deleteKey('+k.id+')">删除</button>'+
        '</td></tr>';
    }).join('');
}

function loadKeyUsageStats() {
    api('/logs/key-stats').then(data => {
        const grid = document.getElementById('key-stats-grid');
        const card = document.getElementById('key-stats-card');
        if (!data || data.length === 0) {
            card.style.display = 'none';
            return;
        }
        card.style.display = 'block';
        grid.innerHTML = data.map(s => {
            const successRate = s.total > 0 ? (s.success / s.total * 100).toFixed(1) : '0.0';
            const rateColor = successRate >= 99 ? 'var(--green)' : successRate >= 95 ? 'var(--orange)' : 'var(--red)';
            return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px;">'+
                '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">'+
                '<span style="font-weight:600;font-size:0.9rem;">Key #'+s.key_id+'</span>'+
                '<span class="badge badge-purple" style="font-size:0.7rem;">'+s.total+' 次请求</span></div>'+
                '<div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;font-size:0.82rem;">'+
                '<div><span style="color:var(--text-dim);">成功率</span> <strong style="color:'+rateColor+'">'+successRate+'%</strong></div>'+
                '<div><span style="color:var(--text-dim);">平均延迟</span> <strong>'+Math.round(s.avg_latency_ms)+'ms</strong></div>'+
                '<div><span style="color:var(--text-dim);">成功</span> <strong style="color:var(--green)">'+s.success+'</strong></div>'+
                '<div><span style="color:var(--text-dim);">失败</span> <strong style="color:var(--red)">'+s.error+'</strong></div>'+
                '</div></div>';
        }).join('');
    });
}

function copyKey(e, id) {
    const btn = e.target;
    const orig = btn.textContent;
    btn.disabled = true;
    btn.textContent = '...';
    api('/keys/'+id+'/reveal').then(d => {
        if (d.error) { toastErr(d.error); btn.textContent = orig; btn.disabled = false; return; }
        btn.textContent = orig;
        btn.disabled = false;
        copyTextToClipboard(d.key, btn);
    }).catch(() => { btn.textContent = orig; btn.disabled = false; });
}

function createKey(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    api('/keys', {method:'POST', body: JSON.stringify({
        name: f.get('name'), rpm_limit: parseInt(f.get('rpm_limit')||'0')
    })}).then(d => {
        if(d.error) { toastErr(d.error); return; }
        document.getElementById('new-key-value').textContent = d.key;
        document.getElementById('new-key-display').style.display = 'block';
        e.target.reset(); document.getElementById('dlg-key').close(); loadKeys(); toastOk('密钥已创建，请立即复制');
    });
}

function copyNewKey(btn) {
    const key = document.getElementById('new-key-value').textContent;
    copyTextToClipboard(key, btn);
}

function editKey(id) {
    api('/keys').then(keys => {
        const k = (keys||[]).find(x => x.id === id);
        if (!k) return;
        const dlg = document.getElementById('dlg-edit-key');
        dlg.querySelector('[name=id]').value = id;
        dlg.querySelector('[name=name]').value = k.name;
        dlg.querySelector('[name=rpm_limit]').value = k.rpm_limit;
        dlg.showModal();
    });
}

function submitEditKey(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    const id = f.get('id');
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({
        name: f.get('name'), rpm_limit: parseInt(f.get('rpm_limit')||'0')
    })}).then(d => {
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-edit-key').close(); loadKeys(); }
    });
}

function toggleKey(id, enabled) {
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if (d && d.error) toastErr(d.error); else { loadKeys(); toastOk(enabled ? '已启用' : '已禁用'); }
    });
}

async function deleteKey(id) {
    if(!await askConfirm('确定删除密钥 #'+id+' 吗？绑定与路由覆盖也会清除。', {title:'删除密钥', okText:'删除', danger:true})) return;
    api('/keys/'+id, {method:'DELETE'}).then(d => {
        if (d && d.error) toastErr(d.error); else { loadKeys(); toastOk('已删除密钥'); }
    });
}

// --- Upstream Binding ---
function openBindingDialog(keyId) {
    document.getElementById('binding-key-id').value = keyId;
    api('/keys/'+keyId+'/upstreams').then(data => {
        const bound = data.upstream_ids || [];
        const list = document.getElementById('binding-list');
        if (allUpstreams.length === 0) {
            list.innerHTML = '<div class="empty-state">暂无上游可绑定</div>';
        } else {
            list.innerHTML = allUpstreams.map(u =>
                '<label class="binding-item"><input type="checkbox" value="'+u.id+'" '+(bound.includes(u.id)?'checked':'')+'><span class="binding-label">'+esc(u.name)+'</span><span class="binding-url">'+esc(u.base_url)+'</span></label>'
            ).join('');
        }
        document.getElementById('dlg-binding').showModal();
    });
}

function saveBindings() {
    const keyId = document.getElementById('binding-key-id').value;
    const ids = Array.from(document.querySelectorAll('#binding-list input:checked')).map(cb => parseInt(cb.value));
    api('/keys/'+keyId+'/upstreams', {method:'PUT', body: JSON.stringify({upstream_ids: ids})}).then(d => {
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-binding').close(); loadKeys(); }
    });
}

// --- Key Model Override ---
let moCurrentRules = []; // [{model_pattern, upstream_id}]
function openOverrideDialog(keyId) {
    document.getElementById('mo-key-id').value = keyId;
    // Populate upstream select
    const sel = document.getElementById('mo-new-upstream');
    sel.innerHTML = allUpstreams.map(u => '<option value="'+u.id+'">'+esc(u.name)+'</option>').join('');
    document.getElementById('mo-new-pattern').value = '';
    // Load existing overrides
    api('/keys/'+keyId+'/model-overrides').then(data => {
        moCurrentRules = (data || []).map(o => ({model_pattern: o.ModelPattern, upstream_id: o.UpstreamID}));
        renderOverrideRules();
        document.getElementById('dlg-model-override').showModal();
    });
}

function renderOverrideRules() {
    const container = document.getElementById('mo-rules');
    if (moCurrentRules.length === 0) {
        container.innerHTML = '<div class="empty-state" style="padding:16px;">无覆盖规则（使用默认路由）</div>';
        return;
    }
    container.innerHTML = '<table style="width:100%"><thead><tr><th>模型模式</th><th>目标上游</th><th></th></tr></thead><tbody>' +
        moCurrentRules.map((r, i) => {
            const u = allUpstreams.find(x => x.id === r.upstream_id);
            const uName = u ? esc(u.name) : 'ID:'+r.upstream_id;
            return '<tr><td><code>'+esc(r.model_pattern)+'</code></td><td>'+uName+'</td><td><button class="btn btn-danger btn-sm" onclick="removeOverrideRule('+i+')">删除</button></td></tr>';
        }).join('') + '</tbody></table>';
}

function addOverrideRule() {
    const pattern = document.getElementById('mo-new-pattern').value.trim();
    const upstreamId = parseInt(document.getElementById('mo-new-upstream').value);
    if (!pattern) { toastErr('请输入模型模式'); return; }
    if (!upstreamId) { toastErr('请选择目标上游'); return; }
    // Check duplicate
    if (moCurrentRules.some(r => r.model_pattern === pattern && r.upstream_id === upstreamId)) {
        toastErr('规则已存在'); return;
    }
    moCurrentRules.push({model_pattern: pattern, upstream_id: upstreamId});
    document.getElementById('mo-new-pattern').value = '';
    renderOverrideRules();
}

function removeOverrideRule(idx) {
    moCurrentRules.splice(idx, 1);
    renderOverrideRules();
}

function saveOverrides() {
    const keyId = document.getElementById('mo-key-id').value;
    api('/keys/'+keyId+'/model-overrides', {method:'PUT', body: JSON.stringify({overrides: moCurrentRules})}).then(d => {
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-model-override').close(); loadKeys(); }
    });
}

