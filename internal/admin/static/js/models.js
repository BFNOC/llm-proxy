// --- Model Whitelist ---
function loadModelWhitelist() {
    api('/models/whitelist').then(data => {
        const tbody = document.getElementById('models-table');
        const selAll = document.getElementById('model-select-all');
        const batchBtn = document.getElementById('btn-batch-delete-models');
        if (selAll) selAll.checked = false;
        if (batchBtn) batchBtn.style.display = 'none';
        if (!data || data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-state">未配置白名单，所有模型均放行</td></tr>';
            return;
        }
        tbody.innerHTML = data.map(e =>
            '<tr><td class="col-check"><input type="checkbox" class="model-cb" value="'+e.ID+'" onchange="updateModelBatchBtn()"></td><td class="hide-on-mobile">'+e.ID+'</td><td><code>'+esc(e.Pattern)+'</code></td><td class="hide-on-mobile">'+fmtTime(e.CreatedAt)+'</td><td>'+
            '<button class="btn btn-danger btn-sm" onclick="deleteModelPattern('+e.ID+')">删除</button></td></tr>'
        ).join('');
    });
}

function toggleAllModelCheckboxes(checked) {
    document.querySelectorAll('.model-cb').forEach(cb => cb.checked = checked);
    updateModelBatchBtn();
}

function updateModelBatchBtn() {
    const checked = document.querySelectorAll('.model-cb:checked').length;
    const btn = document.getElementById('btn-batch-delete-models');
    if (btn) {
        btn.style.display = checked > 0 ? 'inline-flex' : 'none';
        btn.textContent = '批量删除 (' + checked + ')';
    }
    const selAll = document.getElementById('model-select-all');
    const total = document.querySelectorAll('.model-cb').length;
    if (selAll) selAll.checked = total > 0 && checked === total;
}

function addModelPattern(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    api('/models/whitelist', {method:'POST', body: JSON.stringify({
        pattern: f.get('pattern')
    })}).then(d => {
        if(d.error) toastErr(d.error);
        else { e.target.reset(); loadModelWhitelist(); toastOk('已添加'); }
    });
}

async function deleteModelPattern(id) {
    if (!await askConfirm('确认删除此白名单模式？', {title:'删除模式', okText:'删除', danger:true})) return;
    api('/models/whitelist/'+id, {method:'DELETE'}).then(d => {
        if(d.error) toastErr(d.error);
        else { loadModelWhitelist(); toastOk('已删除'); }
    });
}

async function batchDeleteModelPatterns() {
    const ids = Array.from(document.querySelectorAll('.model-cb:checked')).map(cb => parseInt(cb.value));
    if (ids.length === 0) return;
    if (!await askConfirm('确认删除选中的 ' + ids.length + ' 个模式？', {title:'批量删除', okText:'删除', danger:true})) return;
    api('/models/whitelist/batch', {method:'DELETE', body: JSON.stringify({ids: ids})}).then(d => {
        if(d.error) toastErr(d.error);
        else { loadModelWhitelist(); toastOk('已批量删除'); }
    });
}

