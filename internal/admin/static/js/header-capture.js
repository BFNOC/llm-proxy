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
