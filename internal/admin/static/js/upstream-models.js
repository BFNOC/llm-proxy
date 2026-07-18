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
