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
