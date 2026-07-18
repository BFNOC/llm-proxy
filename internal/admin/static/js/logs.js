// --- Logs ---
let slowRequestThreshold = 30000;
let currentLogSession = null;
let logListRequestVersion = 0;
let logSessionRequestVersion = 0;

function loadSlowThreshold() {
    api('/settings').then(data => {
        if (data && data.slow_request_threshold_ms != null) slowRequestThreshold = data.slow_request_threshold_ms;
    }).catch(() => {});
}

function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '-';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1048576).toFixed(1) + ' MB';
}

function populateLogKeyOptions() {
    const select = document.getElementById('logs-key-id');
    if (!select) return Promise.resolve();
    const render = keys => {
        const current = select.value;
        select.innerHTML = '<option value="">全部</option>' + (keys || []).map(k =>
            '<option value="' + Number(k.id) + '">#' + Number(k.id) + ' ' + esc(k.name || k.key_prefix || '') + '</option>'
        ).join('');
        select.value = current;
    };
    if (keysCache.length > 0) {
        render(keysCache);
        return Promise.resolve();
    }
    return api('/keys').then(data => {
        if (Array.isArray(data)) {
            keysCache = data;
            render(keysCache);
        }
    }).catch(() => {});
}

function buildLogQuery(extra, options) {
    const form = document.getElementById('logs-query-form');
    const formData = form ? new FormData(form) : new FormData();
    const params = new URLSearchParams();
    const fields = ['key_id', 'model', 'path', 'status_code'];
    if (!options || options.includeLimit !== false) fields.push('limit');
    fields.forEach(name => {
        const value = String(formData.get(name) || '').trim();
        if (value) params.set(name, value);
    });
    Object.keys(extra || {}).forEach(name => {
        const value = extra[name];
        if (value !== null && value !== undefined && value !== '') params.set(name, String(value));
    });
    return '?' + params.toString();
}

function loadLogs(e) {
    if (e) e.preventDefault();
    const list = document.getElementById('log-session-list');
    if (!list) return;
    populateLogKeyOptions();
    loadSlowThreshold();
    const requestVersion = ++logListRequestVersion;
    list.innerHTML = '<div class="empty-state"><span class="loading-dot"></span><span class="loading-dot"></span><span class="loading-dot"></span></div>';
    api('/logs/sessions' + buildLogQuery()).then(data => {
        if (requestVersion !== logListRequestVersion) return;
        if (data && data.error) {
            list.innerHTML = '<div class="empty-state"><strong>查询失败</strong><p>' + esc(data.error) + '</p></div>';
            return;
        }
        renderLogSessions(Array.isArray(data) ? data : []);
    }).catch(() => {
        if (requestVersion !== logListRequestVersion) return;
        list.innerHTML = '<div class="empty-state"><strong>查询失败</strong><p>无法获取请求会话</p></div>';
    });
}

function renderLogSessions(sessions) {
    const list = document.getElementById('log-session-list');
    if (sessions.length === 0) {
        list.innerHTML = '<div class="empty-state"><strong>暂无会话</strong><p>当前条件下没有完整请求记录</p></div>';
        return;
    }
    const sourceLabels = {
        'header:x-claude-code-session-id': 'Claude Code',
        'header:session-id': 'Session Header',
        'header:thread-id': 'Thread Header',
        'body:conversation': 'Responses Conversation',
        'body:session_id': '请求 Session',
        'body:client_metadata.session_id': '客户端 Session',
        'body:metadata.user_id.session_id': 'Claude Metadata',
        'body:prompt_cache_key': 'Prompt Cache',
        'derived:message_root': '历史指纹',
        'response_id': 'Responses 链',
        'previous_response_id': 'Responses 链'
    };
    list.innerHTML = sessions.map(session => {
        const encodedSessionID = encodeURIComponent(session.session_id || '');
        const source = sourceLabels[session.session_source] || session.session_source || '会话';
        const preview = session.session_preview || session.session_id || '未命名会话';
        const errors = Number(session.error_count || 0);
        return '<article class="log-session-item" data-testid="log-session" data-key-id="' + Number(session.downstream_key_id) + '" data-session-id="' + encodedSessionID + '">' +
            '<div class="log-session-main"><div class="log-session-kicker"><span class="badge badge-purple">' + esc(source) + '</span><span>Key #' + Number(session.downstream_key_id) + '</span></div>' +
            '<h3>' + esc(preview) + '</h3><code class="log-session-id">' + esc(session.session_id) + '</code></div>' +
            '<div class="log-session-stats"><span><strong>' + Number(session.request_count || 0) + '</strong> 次调用</span>' +
            (errors > 0 ? '<span class="badge badge-red">' + errors + ' 错误</span>' : '<span class="badge badge-green">无错误</span>') +
            '<span>' + fmtTime(session.first_at) + ' 至 ' + fmtTime(session.last_at) + '</span></div>' +
            '<button type="button" class="btn btn-ghost btn-sm" data-action="open-session">查看会话</button></article>';
    }).join('');
    list.querySelectorAll('[data-action="open-session"]').forEach(button => {
        button.addEventListener('click', () => {
            const item = button.closest('[data-testid="log-session"]');
            openLogSession(Number(item.dataset.keyId), decodeURIComponent(item.dataset.sessionId));
        });
    });
}

function openLogSession(keyID, sessionID) {
    const dialog = document.getElementById('dlg-log-session');
    const recordsBox = document.getElementById('log-session-records');
    document.getElementById('log-session-title').textContent = '请求会话';
    document.getElementById('log-session-meta').textContent = 'Key #' + keyID + ' · ' + sessionID;
    recordsBox.innerHTML = '<div class="empty-state"><span class="loading-dot"></span><span class="loading-dot"></span><span class="loading-dot"></span></div>';
    currentLogSession = {keyID: keyID, sessionID: sessionID};
    const requestVersion = ++logSessionRequestVersion;
    document.getElementById('log-session-export').onclick = exportCurrentLogSession;
    dialog.showModal();
    api('/logs/session?key_id=' + encodeURIComponent(keyID) + '&session_id=' + encodeURIComponent(sessionID) + '&limit=5000').then(data => {
        if (requestVersion !== logSessionRequestVersion) return;
        if (data && data.error) {
            recordsBox.innerHTML = '<div class="empty-state"><strong>加载失败</strong><p>' + esc(data.error) + '</p></div>';
            return;
        }
        renderLogSessionRecords(Array.isArray(data.records) ? data.records : [], data.truncated === true, Number(data.limit || 5000));
    }).catch(() => {
        if (requestVersion !== logSessionRequestVersion) return;
        recordsBox.innerHTML = '<div class="empty-state"><strong>加载失败</strong><p>无法获取会话详情</p></div>';
    });
}

function renderLogSessionRecords(records, truncated, limit) {
    const box = document.getElementById('log-session-records');
    if (records.length === 0) {
        box.innerHTML = '<div class="empty-state">暂无会话记录</div>';
        return;
    }
    const warning = truncated
        ? '<div class="log-session-warning">会话过长，仅显示最近 ' + Number(limit || records.length) + ' 次调用。</div>'
        : '';
    box.innerHTML = warning + records.map((record, index) => renderLogTurn(record, index)).join('');
    box.querySelectorAll('[data-action="replay-log"]').forEach(button => {
        button.addEventListener('click', () => replayLog(Number(button.dataset.logId)));
    });
}

function renderLogTurn(record, index) {
    const log = record.log || {};
    const detail = record.detail || {};
    const requestText = extractRequestText(detail.request_body || '');
    const responseText = extractResponseText(detail.response_body || '');
    const isSlow = slowRequestThreshold > 0 && Number(log.LatencyMs || 0) > slowRequestThreshold;
    const truncated = detail.request_body_truncated || detail.response_body_truncated;
    return '<section class="log-turn" data-testid="log-session-record" data-log-id="' + Number(log.ID || 0) + '">' +
        '<div class="log-turn-marker">' + (index + 1) + '</div><div class="log-turn-content">' +
        '<div class="log-turn-header"><div><strong>' + fmtTime(log.CreatedAt) + '</strong><span>' + esc(log.Model || '-') + ' · ' + esc(log.Path || '-') + '</span></div>' +
        '<div class="log-turn-badges"><span class="badge ' + (Number(log.StatusCode) < 400 ? 'badge-green' : 'badge-red') + '">' + Number(log.StatusCode || 0) + '</span>' +
        '<span class="badge ' + (isSlow ? 'badge-red' : 'badge-muted') + '">' + Number(log.LatencyMs || 0) + 'ms</span>' +
        (truncated ? '<span class="badge badge-orange">已截断</span>' : '') +
        '<button type="button" class="btn btn-ghost btn-sm" data-action="replay-log" data-log-id="' + Number(log.ID || 0) + '">重放</button></div></div>' +
        '<div class="log-turn-dialogue"><div class="log-message log-message-request"><span>请求</span><p data-testid="request-body">' + esc(requestText || '无可读文本') + '</p></div>' +
        '<div class="log-message log-message-response"><span>响应</span><p data-testid="response-body">' + esc(responseText || '无可读文本') + '</p></div></div>' +
        '<div class="log-turn-details">' +
        renderLogDetails('请求 Header', prettyJSON(detail.request_headers)) +
        renderLogDetails('请求 Body · ' + formatBytes(log.request_size || log.RequestSize), prettyBody(detail.request_body || '')) +
        renderLogDetails('响应 Header', prettyJSON(detail.response_headers)) +
        renderLogDetails('响应 Body · ' + formatBytes(log.response_size || log.ResponseSize), prettyBody(detail.response_body || '')) +
        '</div></div></section>';
}

function renderLogDetails(label, value) {
    return '<details><summary>' + esc(label) + '</summary><pre>' + esc(value || '') + '</pre></details>';
}
