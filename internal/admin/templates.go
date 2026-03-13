package admin

// dashboardHTML is a minimal single-page admin dashboard.
var dashboardHTML = []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM Proxy 管理面板</title>
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@2/css/pico.min.css">
    <style>
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        .tab-nav { display: flex; gap: 8px; margin-bottom: 20px; }
        .tab-nav button { cursor: pointer; }
        .tab-nav button.active { font-weight: bold; }
        .tab-content { display: none; }
        .tab-content.active { display: block; }
        table { width: 100%; }
        .key-display { font-family: monospace; background: #f0f0f0; padding: 8px; border-radius: 4px; word-break: break-all; }
        .badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 0.85em; }
        .badge-green { background: #d4edda; color: #155724; }
        .badge-red { background: #f8d7da; color: #721c24; }
    </style>
</head>
<body>
<main class="container">
    <h1>LLM Proxy 管理面板</h1>

    <div id="auth-section">
        <label>管理令牌: <input type="password" id="admin-token" placeholder="请输入管理令牌"></label>
        <button onclick="authenticate()">连接</button>
    </div>

    <div id="main-section" style="display:none;">
        <nav class="tab-nav">
            <button class="active" onclick="showTab('upstreams')">上游服务</button>
            <button onclick="showTab('keys')">密钥管理</button>
            <button onclick="showTab('logs')">请求日志</button>
            <button onclick="showTab('status')">系统状态</button>
        </nav>

        <!-- Upstreams Tab -->
        <div id="tab-upstreams" class="tab-content active">
            <h2>上游服务商</h2>
            <details><summary>添加上游</summary>
                <form onsubmit="createUpstream(event)">
                    <input name="name" placeholder="名称" required>
                    <input name="base_url" placeholder="https://api.example.com" required>
                    <input name="api_key" placeholder="API 密钥" required type="password">
                    <input name="priority" placeholder="优先级 (0=最高)" type="number" value="0">
                    <button type="submit">创建</button>
                </form>
            </details>
            <table><thead><tr><th>ID</th><th>名称</th><th>地址</th><th>优先级</th><th>操作</th></tr></thead>
            <tbody id="upstreams-table"></tbody></table>
        </div>

        <!-- Keys Tab -->
        <div id="tab-keys" class="tab-content">
            <h2>下游密钥</h2>
            <details><summary>创建密钥</summary>
                <form onsubmit="createKey(event)">
                    <input name="name" placeholder="密钥名称" required>
                    <input name="rpm_limit" placeholder="每分钟请求限制 (0=不限)" type="number" value="0">
                    <button type="submit">创建</button>
                </form>
            </details>
            <div id="new-key-display" style="display:none;">
                <article>
                    <strong>密钥已创建（请立即复制，仅显示一次）:</strong>
                    <div class="key-display" id="new-key-value"></div>
                </article>
            </div>
            <table><thead><tr><th>ID</th><th>前缀</th><th>名称</th><th>RPM</th><th>状态</th><th>操作</th></tr></thead>
            <tbody id="keys-table"></tbody></table>
        </div>

        <!-- Logs Tab -->
        <div id="tab-logs" class="tab-content">
            <h2>请求日志</h2>
            <form onsubmit="loadLogs(event)" style="display:flex;gap:8px;align-items:end;">
                <label>密钥 ID: <input name="key_id" type="number" placeholder="全部"></label>
                <label>条数: <input name="limit" type="number" value="50"></label>
                <button type="submit">查询</button>
            </form>
            <table><thead><tr><th>ID</th><th>密钥</th><th>风格</th><th>路径</th><th>状态码</th><th>延迟</th><th>时间</th></tr></thead>
            <tbody id="logs-table"></tbody></table>
        </div>

        <!-- Status Tab -->
        <div id="tab-status" class="tab-content">
            <h2>系统状态</h2>
            <pre id="status-display"></pre>
            <button onclick="loadStatus()">刷新</button>
        </div>
    </div>
</main>
<script>
let TOKEN = '';
function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
const api = (path, opts={}) => fetch('/admin/api'+path, {
    ...opts,
    headers: {'Authorization':'Bearer '+TOKEN, 'Content-Type':'application/json', ...(opts.headers||{})}
}).then(r => r.json());

function authenticate() {
    TOKEN = document.getElementById('admin-token').value;
    api('/status').then(d => {
        if (d.error) { alert('令牌无效'); return; }
        document.getElementById('auth-section').style.display = 'none';
        document.getElementById('main-section').style.display = 'block';
        loadUpstreams(); loadKeys();
    }).catch(() => alert('连接失败'));
}

function showTab(name) {
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-nav button').forEach(b => b.classList.remove('active'));
    document.getElementById('tab-'+name).classList.add('active');
    event.target.classList.add('active');
    if (name === 'status') loadStatus();
}

function loadUpstreams() {
    api('/upstreams').then(data => {
        const tbody = document.getElementById('upstreams-table');
        tbody.innerHTML = (data||[]).map(u =>
            '<tr><td>'+u.id+'</td><td>'+esc(u.name)+'</td><td>'+esc(u.base_url)+'</td><td>'+u.priority+'</td><td><button onclick="editUpstream('+u.id+','+JSON.stringify(esc(u.name)).replace(/"/g,'&quot;')+','+JSON.stringify(esc(u.base_url)).replace(/"/g,'&quot;')+','+u.priority+')">编辑</button> <button onclick="deleteUpstream('+u.id+')">删除</button></td></tr>'
        ).join('');
    });
}

function createUpstream(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    api('/upstreams', {method:'POST', body: JSON.stringify({
        name: f.get('name'), base_url: f.get('base_url'),
        api_key: f.get('api_key'), priority: parseInt(f.get('priority')||'0')
    })}).then(d => { if(d.error) alert(d.error); else { e.target.reset(); loadUpstreams(); }});
}

function editUpstream(id, name, url, priority) {
    const newName = prompt('名称:', name);
    if (newName === null) return;
    const newUrl = prompt('地址:', url);
    if (newUrl === null) return;
    const newKey = prompt('API 密钥 (留空则不修改):', '');
    if (newKey === null) return;
    const newPriority = prompt('优先级 (0=最高):', priority);
    if (newPriority === null) return;
    const body = {name: newName, base_url: newUrl, priority: parseInt(newPriority||'0')};
    if (newKey) body.api_key = newKey;
    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify(body)}).then(d => {
        if(d.error) alert(d.error); else loadUpstreams();
    });
}

function deleteUpstream(id) {
    if(!confirm('确定删除上游 '+id+' 吗？')) return;
    api('/upstreams/'+id, {method:'DELETE'}).then(() => loadUpstreams());
}

function loadKeys() {
    api('/keys').then(data => {
        const tbody = document.getElementById('keys-table');
        tbody.innerHTML = (data||[]).map(k =>
            '<tr><td>'+k.id+'</td><td><code>'+esc(k.key_prefix)+'...</code></td><td>'+esc(k.name)+'</td><td>'+(k.rpm_limit||'不限')+'</td><td>'+(k.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+'</td><td><button onclick="editKey('+k.id+','+JSON.stringify(esc(k.name)).replace(/"/g,'&quot;')+','+k.rpm_limit+')">编辑</button> <button onclick="toggleKey('+k.id+','+(!k.enabled)+')">切换</button> <button onclick="deleteKey('+k.id+')">删除</button></td></tr>'
        ).join('');
    });
}

function createKey(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    api('/keys', {method:'POST', body: JSON.stringify({
        name: f.get('name'), rpm_limit: parseInt(f.get('rpm_limit')||'0')
    })}).then(d => {
        if(d.error) { alert(d.error); return; }
        document.getElementById('new-key-value').textContent = d.key;
        document.getElementById('new-key-display').style.display = 'block';
        e.target.reset(); loadKeys();
    });
}

function editKey(id, name, rpm) {
    const newName = prompt('名称:', name);
    if (newName === null) return;
    const newRpm = prompt('每分钟请求限制 (0=不限):', rpm);
    if (newRpm === null) return;
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({
        name: newName, rpm_limit: parseInt(newRpm||'0')
    })}).then(d => { if(d.error) alert(d.error); else loadKeys(); });
}

function toggleKey(id, enabled) {
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(() => loadKeys());
}

function deleteKey(id) {
    if(!confirm('确定删除密钥 '+id+' 吗？')) return;
    api('/keys/'+id, {method:'DELETE'}).then(() => loadKeys());
}

function loadLogs(e) {
    if(e) e.preventDefault();
    const f = e ? new FormData(e.target) : new FormData();
    let q = '?limit='+(f.get('limit')||50);
    if(f.get('key_id')) q += '&key_id='+f.get('key_id');
    api('/logs'+q).then(data => {
        const tbody = document.getElementById('logs-table');
        tbody.innerHTML = (data||[]).map(l =>
            '<tr><td>'+l.ID+'</td><td>'+l.DownstreamKeyID+'</td><td>'+esc(l.ProviderStyle)+'</td><td>'+esc(l.Path)+'</td><td>'+l.StatusCode+'</td><td>'+l.LatencyMs+'ms</td><td>'+esc(l.CreatedAt)+'</td></tr>'
        ).join('');
    });
}

function loadStatus() {
    api('/status').then(d => {
        document.getElementById('status-display').textContent = JSON.stringify(d, null, 2);
    });
}
</script>
</body>
</html>`)
