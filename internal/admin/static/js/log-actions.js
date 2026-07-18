function exportLogSessions() {
    const limitInput = document.getElementById('logs-session-limit');
    const sessionLimit = Math.max(1, Number(limitInput && limitInput.value) || 50);
    downloadLogExport(buildLogQuery({full_only: true, session_limit: sessionLimit}, {includeLimit: false}), 'request-logs.ndjson');
}

function exportCurrentLogSession() {
    if (!currentLogSession) return;
    const query = '?key_id=' + encodeURIComponent(currentLogSession.keyID) + '&session_id=' + encodeURIComponent(currentLogSession.sessionID);
    downloadLogExport(query, 'request-session.ndjson');
}

function downloadLogExport(query, filename) {
    fetch('/admin/api/logs/export' + query, {headers: {'Authorization': 'Bearer ' + TOKEN}}).then(async response => {
        if (!response.ok) {
            let message = '导出失败';
            try { message = (await response.json()).error || message; } catch (_) {}
            throw new Error(message);
        }
        return {
            blob: await response.blob(),
            truncated: response.headers.get('X-Export-Truncated') === 'true',
            recordLimit: Number(response.headers.get('X-Export-Record-Limit') || 10000)
        };
    }).then(result => {
        const url = URL.createObjectURL(result.blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(url);
        if (result.truncated) toastErr('导出已达到 ' + result.recordLimit + ' 条上限，文件仅包含最近的请求记录。');
    }).catch(error => toastErr(error.message));
}

function replayLog(logId) {
    api('/logs/' + logId + '/replay', {method: 'POST'}).then(function(d) {
        if (d.error) { toastErr(d.error); return; }
        var upstream = allUpstreams.find(function(u) { return u.name === d.upstream_name; });
		if (upstream) {
			const sessionDialog = document.getElementById('dlg-log-session');
			if (sessionDialog && sessionDialog.open) sessionDialog.close();
            document.querySelector('[data-tab="upstreams"]').click();
            openTestUpstreamDialog(upstream.id, true);
            setTimeout(function() {
                var modelInput = document.getElementById('tu-model');
                if (modelInput && d.model) modelInput.value = d.model;
                var protoMap = {openai: 'openai', anthropic: 'anthropic', responses: 'responses'};
                if (d.provider_style && protoMap[d.provider_style]) setTuProtocol(protoMap[d.provider_style]);
                var promptInput = document.getElementById('tu-prompt');
                var prompt = extractRequestText(d.request_body || '');
                if (promptInput && prompt) promptInput.value = prompt;
            }, 200);
        } else {
            toastErr('未找到上游: ' + d.upstream_name);
        }
    }).catch(error => toastErr(error && error.message ? error.message : '重放失败'));
}
