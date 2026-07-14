// --- SSE 实时事件流 ---
// 注意：EventSource 不支持自定义 Header，因此 SSE 端点需通过 ?token= 查询参数接受认证令牌。
// 后端 /admin/api/events 需要同时支持 Bearer header 和 ?token= query param 认证。
let sseConnection = null;
function startSSE() {
    if (sseConnection) return;
    if (!TOKEN) return;
    var url = '/admin/api/events?token=' + encodeURIComponent(TOKEN);
    sseConnection = new EventSource(url);
    sseConnection.onmessage = function(e) {
        try {
            var data = JSON.parse(e.data);
            if (data.type === 'status') {
                updateLiveStats(data);
            }
        } catch(err) {}
    };
    sseConnection.onerror = function() {
        // EventSource 内置自动重连机制
    };
}
function stopSSE() {
    if (sseConnection) { sseConnection.close(); sseConnection = null; }
}
function updateLiveStats(data) {
    var rpmEl = document.querySelector('[data-live="rpm"]');
    if (rpmEl && data.rpm !== undefined) rpmEl.textContent = data.rpm;
    var rpsEl = document.querySelector('[data-live="rps"]');
    if (rpsEl && data.rps !== undefined) rpsEl.textContent = data.rps;
    var activeEl = document.querySelector('[data-live="active"]');
    if (activeEl && data.active_requests !== undefined) activeEl.textContent = data.active_requests;
}
