package admin

// dashboardHTML is a minimal single-page admin dashboard.
var dashboardHTML = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM Proxy Admin</title>
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
    <h1>LLM Proxy Admin</h1>

    <div id="auth-section">
        <label>Admin Token: <input type="password" id="admin-token" placeholder="Enter admin token"></label>
        <button onclick="authenticate()">Connect</button>
    </div>

    <div id="main-section" style="display:none;">
        <nav class="tab-nav">
            <button class="active" onclick="showTab('upstreams')">Upstreams</button>
            <button onclick="showTab('keys')">Keys</button>
            <button onclick="showTab('logs')">Logs</button>
            <button onclick="showTab('status')">Status</button>
        </nav>

        <!-- Upstreams Tab -->
        <div id="tab-upstreams" class="tab-content active">
            <h2>Upstream Providers</h2>
            <details><summary>Add Upstream</summary>
                <form onsubmit="createUpstream(event)">
                    <input name="name" placeholder="Name" required>
                    <input name="base_url" placeholder="https://api.example.com" required>
                    <input name="api_key" placeholder="API Key" required type="password">
                    <input name="priority" placeholder="Priority (0=highest)" type="number" value="0">
                    <button type="submit">Create</button>
                </form>
            </details>
            <table><thead><tr><th>ID</th><th>Name</th><th>Base URL</th><th>Priority</th><th>Actions</th></tr></thead>
            <tbody id="upstreams-table"></tbody></table>
        </div>

        <!-- Keys Tab -->
        <div id="tab-keys" class="tab-content">
            <h2>Downstream Keys</h2>
            <details><summary>Create Key</summary>
                <form onsubmit="createKey(event)">
                    <input name="name" placeholder="Key Name" required>
                    <input name="rpm_limit" placeholder="RPM Limit (0=unlimited)" type="number" value="0">
                    <button type="submit">Create</button>
                </form>
            </details>
            <div id="new-key-display" style="display:none;">
                <article>
                    <strong>New Key Created (copy now, shown once):</strong>
                    <div class="key-display" id="new-key-value"></div>
                </article>
            </div>
            <table><thead><tr><th>ID</th><th>Prefix</th><th>Name</th><th>RPM</th><th>Status</th><th>Actions</th></tr></thead>
            <tbody id="keys-table"></tbody></table>
        </div>

        <!-- Logs Tab -->
        <div id="tab-logs" class="tab-content">
            <h2>Request Logs</h2>
            <form onsubmit="loadLogs(event)" style="display:flex;gap:8px;align-items:end;">
                <label>Key ID: <input name="key_id" type="number" placeholder="All"></label>
                <label>Limit: <input name="limit" type="number" value="50"></label>
                <button type="submit">Load</button>
            </form>
            <table><thead><tr><th>ID</th><th>Key</th><th>Style</th><th>Path</th><th>Status</th><th>Latency</th><th>Time</th></tr></thead>
            <tbody id="logs-table"></tbody></table>
        </div>

        <!-- Status Tab -->
        <div id="tab-status" class="tab-content">
            <h2>System Status</h2>
            <pre id="status-display"></pre>
            <button onclick="loadStatus()">Refresh</button>
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
        if (d.error) { alert('Invalid token'); return; }
        document.getElementById('auth-section').style.display = 'none';
        document.getElementById('main-section').style.display = 'block';
        loadUpstreams(); loadKeys();
    }).catch(() => alert('Connection failed'));
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
            '<tr><td>'+u.id+'</td><td>'+esc(u.name)+'</td><td>'+esc(u.base_url)+'</td><td>'+u.priority+'</td><td><button onclick="deleteUpstream('+u.id+')">Delete</button></td></tr>'
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

function deleteUpstream(id) {
    if(!confirm('Delete upstream '+id+'?')) return;
    api('/upstreams/'+id, {method:'DELETE'}).then(() => loadUpstreams());
}

function loadKeys() {
    api('/keys').then(data => {
        const tbody = document.getElementById('keys-table');
        tbody.innerHTML = (data||[]).map(k =>
            '<tr><td>'+k.id+'</td><td><code>'+esc(k.key_prefix)+'...</code></td><td>'+esc(k.name)+'</td><td>'+(k.rpm_limit||'unlimited')+'</td><td>'+(k.enabled?'<span class="badge badge-green">Active</span>':'<span class="badge badge-red">Disabled</span>')+'</td><td><button onclick="toggleKey('+k.id+','+(!k.enabled)+')">Toggle</button> <button onclick="deleteKey('+k.id+')">Delete</button></td></tr>'
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

function toggleKey(id, enabled) {
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(() => loadKeys());
}

function deleteKey(id) {
    if(!confirm('Delete key '+id+'?')) return;
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
