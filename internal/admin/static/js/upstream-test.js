function testProxy(e, id) {
    openTestUpstreamDialog(id, true);
}

const TU_PROTO_ORDER = ['openai', 'anthropic', 'responses'];
const TU_PROTO_LABEL = {
    openai: 'Chat Completions',
    anthropic: 'Anthropic',
    responses: 'Responses / Codex'
};
function tuLastKey(upstreamId) { return 'tu-last-v2-' + upstreamId; }
function loadTuLastConfig(upstreamId) {
    try {
        const raw = localStorage.getItem(tuLastKey(upstreamId));
        return raw ? JSON.parse(raw) : null;
    } catch (_) { return null; }
}
function saveTuLastConfig(upstreamId, cfg) {
    try { localStorage.setItem(tuLastKey(upstreamId), JSON.stringify(cfg)); } catch (_) {}
}
function inferTuProtocol(upstream) {
    if (!upstream) return 'openai';
    if ((upstream.auth_mode || '') === 'oauth') return 'anthropic';
    const url = (upstream.base_url || '').toLowerCase();
    if (url.indexOf('anthropic') >= 0) return 'anthropic';
    return 'openai';
}
/** Protocols registered for a model name in test_models (may be multiple). */
function protocolsForModel(modelName) {
    const name = (modelName || '').trim();
    if (!name) return [];
    const set = {};
    (allTestModels || []).forEach(m => {
        if ((m.name || '') === name) set[m.protocol || 'openai'] = true;
    });
    return TU_PROTO_ORDER.filter(p => set[p]);
}
function pickProtocolForModel(modelName, preferred) {
    const avail = protocolsForModel(modelName);
    if (preferred && (!avail.length || avail.indexOf(preferred) >= 0)) return preferred;
    if (avail.length) return avail[0];
    return preferred || 'openai';
}
function setTuProtocol(proto, opts) {
    const p = TU_PROTO_ORDER.indexOf(proto) >= 0 ? proto : 'openai';
    const el = document.getElementById('tu-protocol');
    if (el) el.value = p;
    // Style pills: active + "registered for this model" markers.
    const model = (document.getElementById('tu-model').value || '').trim();
    const avail = protocolsForModel(model);
    document.querySelectorAll('.tu-proto-pill').forEach(btn => {
        const bp = btn.getAttribute('data-proto');
        const active = bp === p;
        const registered = avail.indexOf(bp) >= 0;
        btn.classList.remove('btn-primary', 'btn-ghost', 'btn-success');
        if (active) btn.classList.add('btn-primary');
        else btn.classList.add('btn-ghost');
        // Dot marker for multi-protocol registration without locking choice.
        btn.style.boxShadow = registered && !active ? 'inset 0 0 0 1px var(--accent)' : '';
        btn.title = registered
            ? (TU_PROTO_LABEL[bp] + '（此模型在测试库中有登记）')
            : (TU_PROTO_LABEL[bp] + '（可手动选择，即使未在测试库登记）');
    });
    const hint = document.getElementById('tu-proto-hint');
    if (hint) {
        if (avail.length > 1) hint.textContent = '· 此模型登记了 ' + avail.length + ' 种协议，可切换';
        else if (avail.length === 1) hint.textContent = '· 测试库登记：' + (TU_PROTO_LABEL[avail[0]] || avail[0]);
        else hint.textContent = model ? '· 未在测试库登记，协议可自由选' : '';
    }
    updateTuSpoofHint(p);
    if (!(opts && opts.skipDatalist)) updateTuModelDatalist();
}
function onTuModelInput() {
    // Keep model free-text; only auto-adjust protocol when current one is
    // incompatible with registered multi-protocol set (or none selected yet).
    const model = (document.getElementById('tu-model').value || '').trim();
    const cur = document.getElementById('tu-protocol').value || 'openai';
    const avail = protocolsForModel(model);
    let next = cur;
    if (avail.length && avail.indexOf(cur) < 0) {
        next = avail[0];
    }
    setTuProtocol(next, {skipDatalist: true});
}
function getTuFormConfig() {
    return {
        protocol: document.getElementById('tu-protocol').value || 'openai',
        model: (document.getElementById('tu-model').value || '').trim(),
        prompt: document.getElementById('tu-prompt').value || '你是什么模型？',
        client_spoof: !!document.getElementById('tu-client-spoof').checked,
        key_row_id: document.getElementById('tu-key-select').value
    };
}

function openTestUpstreamDialog(upstreamId, resetFields) {
    document.getElementById('tu-upstream-id').value = upstreamId;
    document.getElementById('tu-result').style.display = 'none';
    const upstream = allUpstreams.find(u => u.id === parseInt(upstreamId, 10));
    const last = loadTuLastConfig(upstreamId);
    let defaultProto = (last && last.protocol) || inferTuProtocol(upstream);
    const defaultModel = (last && last.model) || '';
    const defaultPrompt = (last && last.prompt) || '你是什么模型？';
    const defaultSpoof = last && typeof last.client_spoof === 'boolean' ? last.client_spoof : true;

    document.getElementById('tu-prompt').value = defaultPrompt;
    document.getElementById('tu-client-spoof').checked = defaultSpoof;
    document.getElementById('tu-model').value = defaultModel;

    const sel = document.getElementById('tu-key-select');
    sel.innerHTML = '<option value="">加载中...</option>';
    const dlg = document.getElementById('dlg-test-upstream');
    if (!dlg.open) dlg.showModal();

    loadTestModels().then(() => {
        // Prefer last protocol; if model has multiple registered, keep last if valid.
        // Do NOT auto-fill a model when empty — leave blank so user can type/search.
        defaultProto = pickProtocolForModel(defaultModel, defaultProto);
        updateTuModelDatalist();
        setTuProtocol(defaultProto);
    });

    api('/upstreams/'+upstreamId+'/apikeys').then(data => {
        if (!data || data.length === 0) {
            sel.innerHTML = '<option value="0">无鉴权（公益站）</option>';
            return;
        }
        let selected = last && last.key_row_id != null ? String(last.key_row_id) : '';
        const firstEnabledIndex = data.findIndex(kd => kd.enabled);
        const ids = data.map(kd => String(kd.row_id));
        if (!selected || ids.indexOf(selected) < 0) {
            selected = String(data[firstEnabledIndex >= 0 ? firstEnabledIndex : 0].row_id);
        }
        sel.innerHTML = data.map(kd => {
            const shortKey = kd.key.length > 20 ? kd.key.substring(0, 10) + '...' + kd.key.substring(kd.key.length - 8) : kd.key;
            return '<option value="'+kd.row_id+'"'+(String(kd.row_id)===selected?' selected':'')+'>('+kd.row_id+') '+esc(shortKey)+(kd.enabled?'':' [已禁用]')+'</option>';
        }).join('');
    });
}

function updateTuSpoofHint(proto) {
    const hint = document.getElementById('tu-spoof-hint');
    if (!hint) return;
    if (proto === 'anthropic') {
        hint.textContent = '开启后：OAuth Anthropic 走 Claude Code 伪装（MacOS Stainless + 随机 session/device + utls）。API Key 模式仍为简单探测。仅影响本次测试。';
    } else if (proto === 'responses') {
        hint.textContent = '开启后：按真实 codex-tui 伪装（Mac OS UA、Originator、Session-Id/Thread-Id、X-Codex-*）。仅影响本次测试。';
    } else {
        hint.textContent = 'Chat Completions 协议下伪装开关无效，始终发送标准 OpenAI 探测请求。';
    }
}

/** Collapsed-by-default fingerprint dump for test results (success & failure). */
function renderTuFingerprintBlock(d, borderColor) {
    if (!d || !(d.auth_mode || d.request_headers || d.test_session_id || d.test_device_id || d.test_installation_id)) {
        return '';
    }
    const payload = {
        auth_mode: d.auth_mode,
        client_spoof: d.client_spoof,
        spoof_client: d.spoof_client,
        test_session_id: d.test_session_id,
        test_device_id: d.test_device_id,
        test_installation_id: d.test_installation_id,
        test_turn_id: d.test_turn_id,
        test_url: d.test_url,
        headers: d.request_headers || {}
    };
    // Drop empty keys for a cleaner dump.
    Object.keys(payload).forEach(k => {
        if (payload[k] == null || payload[k] === '' || (typeof payload[k] === 'object' && !Object.keys(payload[k]).length && k === 'headers' && !d.request_headers)) {
            if (payload[k] == null || payload[k] === '') delete payload[k];
        }
    });
    let html = '<details style="margin:8px 0;border-top:1px solid '+(borderColor||'var(--border)')+';padding-top:10px;">';
    html += '<summary style="cursor:pointer;font-size:0.8rem;color:var(--text-dim);user-select:none;font-weight:600;">请求指纹（默认折叠，点击展开）</summary>';
    html += '<pre style="margin:8px 0 0;font-size:0.75rem;line-height:1.5;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);color:var(--text-dim);max-height:280px;overflow:auto;">'+esc(JSON.stringify(payload, null, 2))+'</pre>';
    html += '</details>';
    return html;
}

function submitUpstreamTest() {
    const upstreamId = document.getElementById('tu-upstream-id').value;
    const cfg = getTuFormConfig();
    const keyRowId = cfg.key_row_id;
    if (!keyRowId && keyRowId !== '0') { toastErr('请选择一个 Key'); return; }
    if (!cfg.model) { toastErr('请填写模型'); return; }
    const btn = document.getElementById('btn-tu-test');
    const resultDiv = document.getElementById('tu-result');
    btn.innerHTML = '<svg style="width:14px;height:14px;animation:spin 1s linear infinite;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg> 测试中...';
    btn.disabled = true;
    resultDiv.style.display = 'none';
    const cfBody = getCFConfig(parseInt(upstreamId));
    saveTuLastConfig(upstreamId, cfg);
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId+'/test', {method:'POST', body: JSON.stringify({
        protocol: cfg.protocol,
        model: cfg.model,
        prompt: cfg.prompt,
        client_spoof: cfg.client_spoof,
        cf_clearance: cfBody ? cfBody.cf_clearance : '',
        cf_user_agent: cfBody ? cfBody.cf_user_agent : ''
    })}).then(d => {
        btn.innerHTML = '<svg style="width:14px;height:14px;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg> 测试';
        btn.disabled = false;
        resultDiv.style.display = 'block';
        if (d.success) {
            let html = '<div style="border:1px solid rgba(16,185,129,0.25);border-radius:var(--radius-sm);overflow:hidden;">';
            html += '<div style="background:linear-gradient(135deg,rgba(16,185,129,0.1),rgba(16,185,129,0.05));padding:14px 18px;display:flex;align-items:center;justify-content:space-between;">';
            html += '<div style="display:flex;align-items:center;gap:8px;"><span style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:50%;background:var(--green);color:#fff;font-size:12px;">&#10003;</span><span style="font-weight:600;color:var(--green);font-size:0.9rem;">连接成功</span></div>';
            html += '<span style="font-size:0.78rem;color:var(--text-dim);background:rgba(16,185,129,0.08);padding:2px 10px;border-radius:999px;">'+d.latency_ms+'ms</span></div>';
            html += '<div style="padding:14px 18px;border-top:1px solid rgba(16,185,129,0.12);font-size:0.82rem;color:var(--text-dim);display:flex;gap:20px;flex-wrap:wrap;">';
            html += '<span>模型: <strong style="color:var(--text);font-weight:600;">'+esc(d.actual_model||d.model)+'</strong></span>';
            html += '<span>协议: <strong style="color:var(--text);font-weight:600;">'+esc(d.protocol)+'</strong></span>';
            html += '<span>伪装: <strong style="color:var(--text);font-weight:600;">'+(d.client_spoof?(d.spoof_client||'on'):'off')+'</strong></span>';
            html += '</div>';
            if (d.reply) {
                html += '<div style="border-top:1px solid rgba(16,185,129,0.12);padding:14px 18px;">';
                html += '<div style="font-size:0.72rem;font-weight:600;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.05em;margin-bottom:8px;">回复内容</div>';
                html += '<div style="font-size:0.85rem;line-height:1.7;white-space:pre-wrap;word-break:break-word;padding:12px 14px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);">'+esc(d.reply)+'</div>';
                html += '</div>';
            }
            html += renderTuFingerprintBlock(d, 'rgba(16,185,129,0.12)');
            html += '</div>';
            resultDiv.innerHTML = html;
        } else {
            let html = '<div style="border:1px solid rgba(239,68,68,0.25);border-radius:var(--radius-sm);overflow:hidden;">';
            html += '<div style="background:linear-gradient(135deg,rgba(239,68,68,0.1),rgba(239,68,68,0.05));padding:14px 18px;display:flex;align-items:center;justify-content:space-between;">';
            html += '<div style="display:flex;align-items:center;gap:8px;"><span style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:50%;background:var(--red);color:#fff;font-size:12px;">&#10007;</span><span style="font-weight:600;color:var(--red);font-size:0.9rem;">连接失败</span></div>';
            html += '<span style="font-size:0.78rem;color:var(--text-dim);background:rgba(239,68,68,0.08);padding:2px 10px;border-radius:999px;">'+(d.latency_ms||0)+'ms</span></div>';
            html += '<div style="padding:14px 18px;border-top:1px solid rgba(239,68,68,0.12);font-size:0.85rem;">';
            html += '<div style="color:var(--text-dim);margin-bottom:4px;">HTTP '+(d.status_code||'?')+'</div>';
            if (d.error_message) {
                html += '<div style="color:var(--text);margin-bottom:8px;">'+esc(d.error_message)+'</div>';
            } else if (d.error) {
                html += '<div style="color:var(--text);margin-bottom:8px;">'+esc(d.error)+'</div>';
            }
            if (d.hint) {
                html += '<div style="color:var(--text-dim);font-size:0.8rem;margin-bottom:8px;line-height:1.5;">'+esc(d.hint)+'</div>';
            }
            html += renderTuFingerprintBlock(d, 'rgba(239,68,68,0.12)');
            if (d.raw_body) {
                let rawText = d.raw_body;
                try { rawText = JSON.stringify(JSON.parse(d.raw_body), null, 2); } catch (_) {}
                html += '<details style="margin:8px 0;">';
                html += '<summary style="cursor:pointer;font-size:0.8rem;color:var(--text-dim);user-select:none;">上游原始响应</summary>';
                html += '<pre style="margin:8px 0 0;font-size:0.78rem;line-height:1.5;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);color:var(--text);max-height:320px;overflow:auto;">'+esc(rawText)+'</pre>';
                html += '</details>';
            }
            const statusCode = Number(d.status_code || 0);
            if (keyRowId !== '0' && statusCode >= 400 && statusCode < 500) {
                html += '<div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:12px;padding-top:12px;border-top:1px solid rgba(239,68,68,0.12);">';
                html += '<button class="btn btn-ghost btn-sm" style="color:var(--orange)" onclick="quickDisableTestKey('+upstreamId+','+keyRowId+')">禁用此 Key</button>';
                html += '<button class="btn btn-danger btn-sm" onclick="quickDeleteTestKey('+upstreamId+','+keyRowId+')">删除此 Key</button>';
                html += '</div>';
            }
            html += '</div></div>';
            resultDiv.innerHTML = html;
        }
    }).catch(err => {
        btn.innerHTML = '<svg style="width:14px;height:14px;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg> 测试';
        btn.disabled = false;
        resultDiv.style.display = 'block';
        resultDiv.innerHTML = '<div style="border:1px solid rgba(239,68,68,0.25);border-radius:var(--radius-sm);padding:14px 18px;color:var(--red);font-size:0.85rem;">请求失败: '+esc(err.message)+'</div>';
    });
}

function quickDisableTestKey(upstreamId, keyRowId) {
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId+'/enabled', {method:'PUT', body: JSON.stringify({enabled:false})}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        loadUpstreams();
        openTestUpstreamDialog(upstreamId);
    });
}
async function quickDeleteTestKey(upstreamId, keyRowId) {
    if(!await askConfirm('确认删除当前测试失败的 API Key？', {title:'删除 Key', okText:'删除', danger:true})) return;
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId, {method:'DELETE'}).then(d => {
        if (d.error) { toastErr(d.error); return; }
        loadUpstreams();
        openTestUpstreamDialog(upstreamId);
    });
}

