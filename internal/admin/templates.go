package admin

// dashboardHTML is a minimal single-page admin dashboard.
var dashboardHTML = []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM Proxy 管理面板</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        :root {
            --bg: #0f1117; --bg-card: #1a1d27; --bg-hover: #252836;
            --border: #2a2d3a; --text: #e4e5eb; --text-dim: #8b8ea3;
            --accent: #6c5ce7; --accent-hover: #7c6ff7;
            --green: #00b894; --red: #e17055; --orange: #fdcb6e;
            --radius: 12px; --radius-sm: 8px;
        }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; }
        .container { max-width: 1200px; margin: 0 auto; padding: 24px; }

        /* Header */
        .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 32px; }
        .header h1 { font-size: 1.5rem; font-weight: 700; background: linear-gradient(135deg, var(--accent), #a29bfe); -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
        .header .logout-btn { background: none; border: 1px solid var(--border); color: var(--text-dim); padding: 8px 16px; border-radius: var(--radius-sm); cursor: pointer; font-size: 0.85rem; transition: all 0.2s; }
        .header .logout-btn:hover { border-color: var(--red); color: var(--red); }

        /* Auth */
        #auth-section { display: flex; align-items: center; justify-content: center; min-height: 80vh; }
        .auth-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: 48px; text-align: center; width: 400px; }
        .auth-card h2 { font-size: 1.4rem; margin-bottom: 8px; }
        .auth-card p { color: var(--text-dim); margin-bottom: 24px; font-size: 0.9rem; }
        .auth-card input { width: 100%; padding: 12px 16px; background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-size: 0.95rem; margin-bottom: 16px; transition: border-color 0.2s; }
        .auth-card input:focus { outline: none; border-color: var(--accent); }

        /* Buttons */
        .btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 10px 20px; border: 1px solid transparent; border-radius: var(--radius-sm); cursor: pointer; font-size: 0.875rem; font-weight: 500; transition: all 0.2s; font-family: inherit; white-space: nowrap; }
        .btn-primary { background: var(--accent); color: #fff; }
        .btn-primary:hover { background: var(--accent-hover); transform: translateY(-1px); }
        .btn-sm { padding: 6px 12px; font-size: 0.8rem; }
        .btn-ghost { background: transparent; color: var(--text-dim); border: 1px solid var(--border); }
        .btn-ghost:hover { background: var(--bg-hover); color: var(--text); }
        .btn-danger { background: transparent; color: var(--red); border: 1px solid transparent; }
        .btn-danger:hover { background: rgba(225,112,85,0.1); }
        .btn-success { background: transparent; color: var(--green); border: 1px solid transparent; }
        .btn-success:hover { background: rgba(0,184,148,0.1); }

        /* Tabs */
        .tab-nav { display: flex; gap: 4px; margin-bottom: 24px; background: var(--bg-card); border-radius: var(--radius); padding: 4px; border: 1px solid var(--border); }
        .tab-nav button { flex: 1; padding: 10px 16px; background: transparent; border: none; color: var(--text-dim); cursor: pointer; border-radius: var(--radius-sm); font-size: 0.875rem; font-weight: 500; transition: all 0.2s; font-family: inherit; white-space: nowrap; }
        .tab-nav button.active { background: var(--accent); color: #fff; }
        .tab-nav button:hover:not(.active) { background: var(--bg-hover); color: var(--text); }
        .tab-content { display: none; animation: fadeIn 0.3s ease; }
        .tab-content.active { display: block; }
        @keyframes fadeIn { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }

        /* Cards */
        .card { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: 24px; margin-bottom: 20px; }
        .card-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
        .card-header h2 { font-size: 1.15rem; font-weight: 600; }

        /* Tables */
        .table-container { overflow-x: auto; -webkit-overflow-scrolling: touch; }
        table { width: 100%; border-collapse: collapse; }
        thead th { text-align: left; padding: 12px 16px; font-size: 0.75rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-dim); border-bottom: 1px solid var(--border); white-space: nowrap; }
        tbody td { padding: 12px 16px; font-size: 0.875rem; border-bottom: 1px solid var(--border); vertical-align: middle; }
        tbody tr { transition: background 0.15s; }
        tbody tr:hover { background: var(--bg-hover); }
        tbody tr:last-child td { border-bottom: none; }

        /* Badges */
        .badge { display: inline-block; padding: 4px 10px; border-radius: 999px; font-size: 0.75rem; font-weight: 600; white-space: nowrap; }
        .badge-green { background: rgba(0,184,148,0.15); color: var(--green); }
        .badge-red { background: rgba(225,112,85,0.15); color: var(--red); }
        .badge-purple { background: rgba(108,92,231,0.15); color: var(--accent); }

        /* Forms */
        .form-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; align-items: end; }
        .form-grid.narrow { grid-template-columns: 1fr auto; }
        .form-group { display: flex; flex-direction: column; gap: 4px; }
        .form-group label { font-size: 0.75rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-dim); }
        input, select { width: 100%; padding: 10px 14px; background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-size: 0.875rem; font-family: inherit; transition: border-color 0.2s; }
        input:focus, select:focus { outline: none; border-color: var(--accent); }
        code { background: var(--bg); padding: 2px 8px; border-radius: 4px; font-size: 0.85em; word-break: break-all; }

        /* Key display */
        .key-display { font-family: 'SF Mono', 'JetBrains Mono', monospace; background: var(--bg); padding: 16px; border-radius: var(--radius-sm); word-break: break-all; border: 1px solid var(--accent); margin-top: 8px; font-size: 0.9rem; }
        .key-alert { background: rgba(108,92,231,0.1); border: 1px solid var(--accent); border-radius: var(--radius-sm); padding: 16px; margin-bottom: 16px; }
        .key-alert strong { color: var(--accent); }

        /* Dialog / Modal */
        dialog { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); color: var(--text); padding: 32px; max-width: 520px; width: 90%; position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); margin: 0; }
        dialog::backdrop { background: rgba(0,0,0,0.6); backdrop-filter: blur(4px); }
        dialog h3 { font-size: 1.1rem; margin-bottom: 20px; }
        dialog .form-group { margin-bottom: 12px; }
        dialog .dialog-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 24px; }

        /* Binding checkboxes */
        .binding-list { display: flex; flex-direction: column; gap: 8px; max-height: 300px; overflow-y: auto; }
        .binding-item { display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: var(--bg); border-radius: var(--radius-sm); cursor: pointer; transition: background 0.15s; }
        .binding-item:hover { background: var(--bg-hover); }
        .binding-item input[type="checkbox"] { accent-color: var(--accent); width: 16px; height: 16px; }
        .binding-label { flex: 1; font-size: 0.9rem; }
        .binding-url { color: var(--text-dim); font-size: 0.8rem; }


        /* Empty state */
        .empty-state { text-align: center; padding: 32px; color: var(--text-dim); font-size: 0.9rem; }

        /* Action buttons in table */
        .actions { display: flex; gap: 4px; flex-wrap: wrap; }

        /* Truncate URL */
        .truncate-url { display: inline-block; max-width: 150px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; vertical-align: middle; }

        /* Model pattern tags */
        .model-tags { display: flex; flex-wrap: wrap; gap: 4px; }
        .model-tag { display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px; background: rgba(108,92,231,0.12); color: var(--accent); border-radius: 4px; font-size: 0.75rem; font-family: 'SF Mono', 'JetBrains Mono', monospace; }
        .model-tag-all { color: var(--text-dim); font-size: 0.8rem; font-style: italic; }

        /* Responsive */
        @media (max-width: 768px) {
            body { padding: 8px; font-size: 0.9rem; }
            .card { padding: 12px; border-radius: var(--radius-sm); }
            .hide-on-mobile { display: none !important; }
            .tab-nav { flex-wrap: wrap; }
            .tab-nav button { flex: 1 1 calc(50% - 4px); justify-content: center; }
            .form-grid { grid-template-columns: 1fr; }
            .dialog-actions { flex-direction: column-reverse; }
            .dialog-actions button { width: 100%; }
            thead th, tbody td { padding: 10px 4px; font-size: 0.8rem; }
            .truncate-url { max-width: 90px; }
            .actions { gap: 6px; }
        }
    </style>
</head>
<body>
<div class="container">
    <!-- Auth Section -->
    <div id="auth-section">
        <div class="auth-card">
            <h2>🔐 LLM Proxy</h2>
            <p>输入管理令牌以连接管理面板</p>
            <input type="password" id="admin-token" placeholder="管理令牌" onkeydown="if(event.key==='Enter')authenticate()">
            <button class="btn btn-primary" style="width:100%" onclick="authenticate()">连接</button>
        </div>
    </div>

    <!-- Main Section -->
    <div id="main-section" style="display:none;">
        <div class="header">
            <h1>⚡ LLM Proxy 管理面板</h1>
            <button class="logout-btn" onclick="logout()">退出登录</button>
        </div>

        <nav class="tab-nav">
            <button class="active" onclick="showTab('upstreams',this)">上游服务</button>
            <button onclick="showTab('keys',this)">密钥管理</button>
            <button onclick="showTab('models',this)">模型白名单</button>
            <button onclick="showTab('logs',this)">请求日志</button>
            <button onclick="showTab('status',this)">系统状态</button>
        </nav>

        <!-- Upstreams Tab -->
        <div id="tab-upstreams" class="tab-content active">
            <div class="card">
                <div class="card-header">
                    <h2>上游服务商</h2>
                    <button class="btn btn-primary btn-sm" onclick="document.getElementById('dlg-upstream').showModal()">+ 添加上游</button>
                </div>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th>名称</th><th>地址</th><th class="hide-on-mobile">代理</th><th class="hide-on-mobile">优先级</th><th class="hide-on-mobile">模型模式</th><th>状态</th><th>操作</th></tr></thead>
                <tbody id="upstreams-table"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Keys Tab -->
        <div id="tab-keys" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <h2>下游密钥</h2>
                    <button class="btn btn-primary btn-sm" onclick="document.getElementById('dlg-key').showModal()">+ 创建密钥</button>
                </div>
                <div id="new-key-display" style="display:none;">
                    <div class="key-alert">
                        <strong>⚠ 密钥已创建（请立即复制，仅显示一次）：</strong>
                        <div class="key-display" id="new-key-value"></div>
                    </div>
                </div>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th class="hide-on-mobile">前缀</th><th>名称</th><th>RPM</th><th>状态</th><th class="hide-on-mobile">绑定上游</th><th>操作</th></tr></thead>
                <tbody id="keys-table"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Models Tab -->
        <div id="tab-models" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <h2>模型白名单</h2>
                    <button class="btn btn-danger btn-sm" id="btn-batch-delete-models" style="display:none" onclick="batchDeleteModelPatterns()">批量删除</button>
                </div>
                <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">配置允许的模型（为空则不过滤）。支持 <code>*</code> 通配符（如 <code>claude-sonnet*</code>），不含通配符时精确匹配。</p>
                <form onsubmit="addModelPattern(event)" class="form-grid narrow" style="margin-bottom:20px;">
                    <div class="form-group"><input name="pattern" placeholder="如: claude-sonnet*" required></div>
                    <button type="submit" class="btn btn-primary" style="align-self:end;">添加</button>
                </form>
                <div class="table-container">
                <table><thead><tr><th style="width:32px"><input type="checkbox" id="model-select-all" onchange="toggleAllModelCheckboxes(this.checked)"></th><th class="hide-on-mobile">ID</th><th>模式</th><th class="hide-on-mobile">添加时间</th><th>操作</th></tr></thead>
                <tbody id="models-table"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Logs Tab -->
        <div id="tab-logs" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <h2>请求日志</h2>
                </div>
                <form onsubmit="loadLogs(event)" class="form-grid" style="margin-bottom:20px;">
                    <div class="form-group"><label>密钥 ID</label><input name="key_id" type="number" placeholder="全部"></div>
                    <div class="form-group"><label>条数</label><input name="limit" type="number" value="50"></div>
                    <div class="form-group"><label>&nbsp;</label><button type="submit" class="btn btn-primary">查询</button></div>
                </form>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th>密钥</th><th>上游</th><th class="hide-on-mobile">IP</th><th class="hide-on-mobile">风格</th><th class="hide-on-mobile">路径</th><th>状态码</th><th class="hide-on-mobile">延迟</th><th>时间</th></tr></thead>
                <tbody id="logs-table"></tbody></table>
                </div>
            </div>
        </div>

        <!-- Status Tab -->
        <div id="tab-status" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <h2>系统状态</h2>
                    <button class="btn btn-ghost btn-sm" onclick="loadStatus()">刷新</button>
                </div>
                <div id="status-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:12px;margin-bottom:20px;"></div>
                <h3 style="font-size:0.95rem;margin-bottom:12px;">健康上游</h3>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th>名称</th><th>地址</th></tr></thead>
                <tbody id="status-upstreams"></tbody></table>
                </div>
            </div>
        </div>
    </div>
</div>

<!-- Create Upstream Dialog -->
<dialog id="dlg-upstream">
    <h3>添加上游</h3>
    <form onsubmit="createUpstream(event)">
        <div class="form-group"><label>名称</label><input name="name" required></div>
        <div class="form-group"><label>地址</label><input name="base_url" placeholder="https://api.example.com" required></div>
        <div class="form-group"><label>API 密钥</label><input name="api_key" type="password" required></div>
        <div class="form-group"><label>代理地址（可选）</label><input name="proxy_url" placeholder="socks5://127.0.0.1:1080"></div>
        <div class="form-group"><label>优先级 (0=最高)</label><input name="priority" type="number" value="0"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">创建</button>
        </div>
    </form>
</dialog>

<!-- Edit Upstream Dialog -->
<dialog id="dlg-edit-upstream">
    <h3>编辑上游</h3>
    <form onsubmit="submitEditUpstream(event)">
        <input type="hidden" name="id">
        <div class="form-group"><label>名称</label><input name="name" required></div>
        <div class="form-group"><label>地址</label><input name="base_url" required></div>
        <div class="form-group"><label>API 密钥（留空不修改）</label><input name="api_key" type="password"></div>
        <div class="form-group"><label>代理地址（留空=环境代理）</label><input name="proxy_url" placeholder="socks5://127.0.0.1:1080"></div>
        <div class="form-group"><label>优先级</label><input name="priority" type="number"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
        </div>
    </form>
</dialog>

<!-- Create Key Dialog -->
<dialog id="dlg-key">
    <h3>创建密钥</h3>
    <form onsubmit="createKey(event)">
        <div class="form-group"><label>密钥名称</label><input name="name" required></div>
        <div class="form-group"><label>每分钟请求限制 (0=不限)</label><input name="rpm_limit" type="number" value="0"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">创建</button>
        </div>
    </form>
</dialog>

<!-- Edit Key Dialog -->
<dialog id="dlg-edit-key">
    <h3>编辑密钥</h3>
    <form onsubmit="submitEditKey(event)">
        <input type="hidden" name="id">
        <div class="form-group"><label>名称</label><input name="name" required></div>
        <div class="form-group"><label>每分钟请求限制 (0=不限)</label><input name="rpm_limit" type="number"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
        </div>
    </form>
</dialog>

<!-- Upstream Binding Dialog -->
<dialog id="dlg-binding">
    <h3>配置上游绑定</h3>
    <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">选择此密钥允许使用的上游。不选择任何上游表示允许全部。</p>
    <input type="hidden" id="binding-key-id">
    <div id="binding-list" class="binding-list"></div>
    <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
        <button type="button" class="btn btn-primary" onclick="saveBindings()">保存</button>
    </div>
</dialog>

<!-- Model Patterns Dialog -->
<dialog id="dlg-model-patterns">
    <h3>配置模型模式</h3>
    <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">配置此上游支持的模型。支持 <code>*</code> 通配符（如 <code>claude-*</code>）。空则接受所有模型。</p>
    <input type="hidden" id="mp-upstream-id">
    <div style="display:flex;gap:8px;margin-bottom:16px;">
        <input id="mp-new-pattern" placeholder="如: claude-*" style="flex:1" onkeydown="if(event.key==='Enter'){event.preventDefault();addModelPatternTag()}">
        <button type="button" class="btn btn-primary btn-sm" onclick="addModelPatternTag()">添加</button>
    </div>
    <div id="mp-tags" class="model-tags" style="min-height:32px;margin-bottom:16px;"></div>
    <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
        <button type="button" class="btn btn-primary" onclick="saveModelPatterns()">保存</button>
    </div>
</dialog>

<script>
let TOKEN = '';
// --- Cookie helpers ---
function saveToken(t) {
    document.cookie = 'admin_token=' + encodeURIComponent(t) + '; max-age=604800; path=/admin; SameSite=Strict; Secure';
}
function readToken() {
    const m = document.cookie.match(/(?:^|;\s*)admin_token=([^;]*)/);
    return m ? decodeURIComponent(m[1]) : '';
}
function clearToken() {
    document.cookie = 'admin_token=; max-age=0; path=/admin';
}

function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }
const api = (path, opts={}) => fetch('/admin/api'+path, {
    ...opts,
    headers: {'Authorization':'Bearer '+TOKEN, 'Content-Type':'application/json', ...(opts.headers||{})}
}).then(r => r.json());

// --- Auth ---
function authenticate() {
    TOKEN = document.getElementById('admin-token').value;
    api('/status').then(d => {
        if (d.error) { alert('令牌无效'); return; }
        saveToken(TOKEN);
        document.getElementById('auth-section').style.display = 'none';
        document.getElementById('main-section').style.display = 'block';
        loadUpstreams().then(() => loadKeys());
    }).catch(() => alert('连接失败'));
}
function logout() {
    clearToken();
    TOKEN = '';
    document.getElementById('main-section').style.display = 'none';
    document.getElementById('auth-section').style.display = 'flex';
}
// Auto-login from cookie
window.addEventListener('DOMContentLoaded', () => {
    const saved = readToken();
    if (saved) {
        TOKEN = saved;
        api('/status').then(d => {
            if (d.error) { clearToken(); return; }
            document.getElementById('auth-section').style.display = 'none';
            document.getElementById('main-section').style.display = 'block';
            loadUpstreams(); loadKeys();
        }).catch(() => { clearToken(); });
    }
});

// --- Tabs ---
function showTab(name, btn) {
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-nav button').forEach(b => b.classList.remove('active'));
    document.getElementById('tab-'+name).classList.add('active');
    btn.classList.add('active');
    if (name === 'status') loadStatus();
    if (name === 'models') loadModelWhitelist();
}

// --- Upstreams ---
let allUpstreams = [];
let allModelPatterns = {}; // upstream_id -> [patterns]
function loadUpstreams() {
    return Promise.all([api('/upstreams'), api('/upstreams/models')]).then(([data, mp]) => {
        allUpstreams = data || [];
        allModelPatterns = mp || {};
        const tbody = document.getElementById('upstreams-table');
        if (allUpstreams.length === 0) {
            tbody.innerHTML = '<tr><td colspan="8" class="empty-state">暂无上游服务</td></tr>';
            return;
        }
        tbody.innerHTML = allUpstreams.map(u => {
            const patterns = allModelPatterns[u.id] || [];
            let modelHtml = '<span class="model-tag-all">*</span>';
            if (patterns.length > 0) {
                modelHtml = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
            }
            return '<tr><td class="hide-on-mobile">'+u.id+'</td><td>'+esc(u.name)+'</td><td><code class="truncate-url" title="'+esc(u.base_url)+'">'+esc(u.base_url)+'</code></td><td class="hide-on-mobile">'+(u.proxy_url?'<code class="truncate-url" title="'+esc(u.proxy_url)+'">'+esc(u.proxy_url)+'</code>':'<span class="badge badge-green">环境代理</span>')+'</td><td class="hide-on-mobile">'+u.priority+'</td><td class="hide-on-mobile"><div class="model-tags">'+modelHtml+'</div></td><td>'+
            (u.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
            '</td><td class="actions">'+
            '<button class="btn btn-ghost btn-sm" onclick="testProxy(event,'+u.id+')">测试</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="checkQuota(event,'+u.id+')">查额</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="openModelPatternsDialog('+u.id+')">模型</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="toggleUpstream('+u.id+','+(!u.enabled)+')">切换</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="editUpstream('+u.id+')">编辑</button> '+
            '<button class="btn btn-danger btn-sm" onclick="deleteUpstream('+u.id+')">删除</button>'+
            '</td></tr>';
        }).join('');
    });
}

function createUpstream(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    api('/upstreams', {method:'POST', body: JSON.stringify({
        name: f.get('name'), base_url: f.get('base_url'),
        api_key: f.get('api_key'), proxy_url: f.get('proxy_url')||'',
        priority: parseInt(f.get('priority')||'0')
    })}).then(d => {
        if(d.error) alert(d.error);
        else { e.target.reset(); document.getElementById('dlg-upstream').close(); loadUpstreams(); }
    });
}

function editUpstream(id) {
    const u = allUpstreams.find(x => x.id === id);
    if (!u) return;
    const dlg = document.getElementById('dlg-edit-upstream');
    dlg.querySelector('[name=id]').value = id;
    dlg.querySelector('[name=name]').value = u.name;
    dlg.querySelector('[name=base_url]').value = u.base_url;
    dlg.querySelector('[name=api_key]').value = '';
    dlg.querySelector('[name=proxy_url]').value = u.proxy_url||'';
    dlg.querySelector('[name=priority]').value = u.priority;
    dlg.showModal();
}

function submitEditUpstream(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    const id = f.get('id');
    const body = {name: f.get('name'), base_url: f.get('base_url'), proxy_url: f.get('proxy_url')||'', priority: parseInt(f.get('priority')||'0')};
    const key = f.get('api_key');
    if (key) body.api_key = key;
    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify(body)}).then(d => {
        if(d.error) alert(d.error);
        else { document.getElementById('dlg-edit-upstream').close(); loadUpstreams(); }
    });
}

function deleteUpstream(id) {
    if(!confirm('确定删除上游 '+id+' 吗？')) return;
    api('/upstreams/'+id, {method:'DELETE'}).then(() => loadUpstreams());
}

function toggleUpstream(id, enabled) {
    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if(d.error) alert(d.error); else loadUpstreams();
    });
}

function testProxy(e, id) {
    const btn = e.target;
    const row = btn.closest('tr');
    // 如果已有展开的测试行，则收起
    const existingRow = document.getElementById('test-row-'+id);
    if (existingRow) { existingRow.remove(); return; }
    // 移除其他已展开的测试行
    document.querySelectorAll('[id^="test-row-"]').forEach(r => r.remove());

    const origText = btn.textContent;
    btn.textContent = '测试中...';
    btn.disabled = true;
    api('/upstreams/'+id+'/test-proxy', {method:'POST'}).then(d => {
        btn.textContent = origText;
        btn.disabled = false;
        const tr = document.createElement('tr');
        tr.id = 'test-row-'+id;
        const td = document.createElement('td');
        td.colSpan = 8;
        td.style.cssText = 'padding:0;border:none;';

        if (d.success) {
            const models = d.models || [];
            let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">';
            html += '<span style="color:var(--green);font-weight:600;">✅ 连接成功</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            html += '<div style="color:var(--text);font-size:0.85rem;margin-top:4px;">状态码: <strong>' + d.status_code + '</strong> &nbsp;|&nbsp; 延迟: <strong>' + d.latency_ms + 'ms</strong> &nbsp;|&nbsp; 模型数: <strong>' + models.length + '</strong></div>';
            if (models.length > 0) {
                html += '<div style="margin-top:12px;max-height:200px;overflow-y:auto;display:flex;flex-wrap:wrap;gap:6px;">';
                models.forEach(m => {
                    html += '<span class="model-tag">' + esc(m) + '</span>';
                });
                html += '</div>';
            } else {
                html += '<div style="margin-top:8px;color:var(--text-dim);font-size:0.85rem;">未能解析模型列表（上游可能非标准 OpenAI 格式）</div>';
            }
            html += '</div>';
            td.innerHTML = html;
        } else {
            let msg = d.error || '未知错误';
            let html = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">';
            html += '<span style="color:var(--red);font-weight:600;">❌ 连接失败</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            html += '<div style="color:var(--text-dim);font-size:0.85rem;">' + esc(msg) + '</div>';
            html += '<div style="color:var(--text);font-size:0.85rem;margin-top:8px;">延迟: <strong>' + (d.latency_ms||0) + 'ms</strong></div>';
            html += '</div>';
            td.innerHTML = html;
        }
        tr.appendChild(td);
        row.after(tr);
    }).catch(err => {
        btn.textContent = origText;
        btn.disabled = false;
        let tr = document.createElement('tr');
        tr.id = 'test-row-'+id;
        let td = document.createElement('td');
        td.colSpan = 8;
        td.innerHTML = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;color:var(--red);">请求失败: '+esc(err.message)+'</div>';
        tr.appendChild(td);
        row.after(tr);
    });
}

function checkQuota(e, id) {
    const btn = e.target;
    const row = btn.closest('tr');
    // 如果已有展开的额度行，则收起
    const existingRow = document.getElementById('quota-row-'+id);
    if (existingRow) { existingRow.remove(); return; }
    // 移除其他已展开的额度行
    document.querySelectorAll('[id^="quota-row-"]').forEach(r => r.remove());

    const origText = btn.textContent;
    btn.textContent = '查询中...';
    btn.disabled = true;
    api('/upstreams/'+id+'/check-quota', {method:'POST'}).then(d => {
        btn.textContent = origText;
        btn.disabled = false;
        const tr = document.createElement('tr');
        tr.id = 'quota-row-'+id;
        const td = document.createElement('td');
        td.colSpan = 8;
        td.style.cssText = 'padding:0;border:none;';

        if (d.success) {
            const data = d.data;
            const fmt = n => n.toLocaleString();
            const toUSD = n => '$' + (n / 500000).toFixed(2);
            const pct = data.total_granted > 0 ? (data.total_used / data.total_granted * 100).toFixed(1) : '0.0';
            const barColor = pct > 80 ? 'var(--red)' : pct > 50 ? 'var(--orange)' : 'var(--green)';
            let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">';
            html += '<span style="font-weight:600;">📊 ' + esc(data.name) + '</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            if (data.unlimited_quota) {
                html += '<span class="badge badge-green">无限额度</span>';
            } else {
                html += '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:12px;">';
                html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">可用</div><div style="font-size:1.1rem;font-weight:700;color:var(--green);">' + toUSD(data.total_available) + '</div><div style="font-size:0.7rem;color:var(--text-dim);">' + fmt(data.total_available) + '</div></div>';
                html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">已用</div><div style="font-size:1.1rem;font-weight:700;color:var(--orange);">' + toUSD(data.total_used) + '</div><div style="font-size:0.7rem;color:var(--text-dim);">' + fmt(data.total_used) + '</div></div>';
                html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">总额</div><div style="font-size:1.1rem;font-weight:700;">' + toUSD(data.total_granted) + '</div><div style="font-size:0.7rem;color:var(--text-dim);">' + fmt(data.total_granted) + '</div></div>';
                html += '</div>';
                html += '<div style="background:var(--bg-card);border-radius:4px;height:8px;overflow:hidden;">';
                html += '<div style="height:100%;width:' + pct + '%;background:' + barColor + ';border-radius:4px;transition:width 0.3s;"></div></div>';
                html += '<div style="text-align:right;font-size:0.75rem;color:var(--text-dim);margin-top:4px;">使用率 ' + pct + '%</div>';
            }
            if (data.expires_at > 0) {
                html += '<div style="font-size:0.8rem;color:var(--text-dim);margin-top:8px;">过期时间: ' + fmtTime(new Date(data.expires_at * 1000).toISOString()) + '</div>';
            }
            html += '</div>';
            td.innerHTML = html;
        } else {
            let msg = d.message || '未知错误';
            let html = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">';
            html += '<span style="color:var(--red);font-weight:600;">❌ 查询失败</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            html += '<div style="color:var(--text-dim);font-size:0.85rem;">' + esc(msg) + '</div>';
            if (d.origin_content) {
                html += '<pre style="margin-top:8px;padding:8px;background:var(--bg);border-radius:4px;font-size:0.75rem;overflow-x:auto;max-height:120px;color:var(--text-dim);">' + esc(d.origin_content) + '</pre>';
            }
            html += '</div>';
            td.innerHTML = html;
        }
        tr.appendChild(td);
        row.after(tr);
    }).catch(err => {
        btn.textContent = origText;
        btn.disabled = false;
        let tr = document.createElement('tr');
        tr.id = 'quota-row-'+id;
        let td = document.createElement('td');
        td.colSpan = 8;
        td.innerHTML = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;color:var(--red);">请求失败: '+esc(err.message)+'</div>';
        tr.appendChild(td);
        row.after(tr);
    });
}

// --- Keys ---
function loadKeys() {
    Promise.all([api('/keys'), api('/keys/bindings')]).then(([data, bindMap]) => {
        const keys = data || [];
        bindMap = bindMap || {};
        const tbody = document.getElementById('keys-table');
        if (keys.length === 0) {
            tbody.innerHTML = '<tr><td colspan="7" class="empty-state">暂无密钥</td></tr>';
            return;
        }
        tbody.innerHTML = keys.map(k => {
            const bound = bindMap[k.id] || [];
            let bindText = '<span class="badge badge-purple">全部</span>';
            if (bound.length > 0) {
                const names = bound.map(uid => { const u = allUpstreams.find(x=>x.id===uid); return u ? esc(u.name) : uid; });
                bindText = names.join(', ');
            }
            return '<tr><td class="hide-on-mobile">'+k.id+'</td><td class="hide-on-mobile"><code>'+esc(k.key_prefix)+'...</code></td><td>'+esc(k.name)+'</td><td>'+(k.rpm_limit||'不限')+'</td><td>'+
            (k.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
            '</td><td class="hide-on-mobile">'+bindText+'</td><td class="actions">'+
            '<button class="btn btn-ghost btn-sm" onclick="openBindingDialog('+k.id+')">绑定</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="editKey('+k.id+')">编辑</button> '+
            '<button class="btn btn-success btn-sm" onclick="toggleKey('+k.id+','+(!k.enabled)+')">切换</button> '+
            '<button class="btn btn-danger btn-sm" onclick="deleteKey('+k.id+')">删除</button>'+
            '</td></tr>';
        }).join('');
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
        e.target.reset(); document.getElementById('dlg-key').close(); loadKeys();
    });
}

function editKey(id) {
    api('/keys').then(keys => {
        const k = (keys||[]).find(x => x.id === id);
        if (!k) return;
        const dlg = document.getElementById('dlg-edit-key');
        dlg.querySelector('[name=id]').value = id;
        dlg.querySelector('[name=name]').value = k.name;
        dlg.querySelector('[name=rpm_limit]').value = k.rpm_limit;
        dlg.showModal();
    });
}

function submitEditKey(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    const id = f.get('id');
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({
        name: f.get('name'), rpm_limit: parseInt(f.get('rpm_limit')||'0')
    })}).then(d => {
        if(d.error) alert(d.error);
        else { document.getElementById('dlg-edit-key').close(); loadKeys(); }
    });
}

function toggleKey(id, enabled) {
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(() => loadKeys());
}

function deleteKey(id) {
    if(!confirm('确定删除密钥 '+id+' 吗？')) return;
    api('/keys/'+id, {method:'DELETE'}).then(() => loadKeys());
}

// --- Upstream Binding ---
function openBindingDialog(keyId) {
    document.getElementById('binding-key-id').value = keyId;
    api('/keys/'+keyId+'/upstreams').then(data => {
        const bound = data.upstream_ids || [];
        const list = document.getElementById('binding-list');
        if (allUpstreams.length === 0) {
            list.innerHTML = '<div class="empty-state">暂无上游可绑定</div>';
        } else {
            list.innerHTML = allUpstreams.map(u =>
                '<label class="binding-item"><input type="checkbox" value="'+u.id+'" '+(bound.includes(u.id)?'checked':'')+'><span class="binding-label">'+esc(u.name)+'</span><span class="binding-url">'+esc(u.base_url)+'</span></label>'
            ).join('');
        }
        document.getElementById('dlg-binding').showModal();
    });
}

function saveBindings() {
    const keyId = document.getElementById('binding-key-id').value;
    const ids = Array.from(document.querySelectorAll('#binding-list input:checked')).map(cb => parseInt(cb.value));
    api('/keys/'+keyId+'/upstreams', {method:'PUT', body: JSON.stringify({upstream_ids: ids})}).then(d => {
        if(d.error) alert(d.error);
        else { document.getElementById('dlg-binding').close(); loadKeys(); }
    });
}

// --- Upstream Model Patterns ---
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
        if(d.error) alert(d.error);
        else { document.getElementById('dlg-model-patterns').close(); loadUpstreams(); }
    });
}

// --- Logs ---
function loadLogs(e) {
    if(e) e.preventDefault();
    const f = e ? new FormData(e.target) : new FormData();
    let q = '?limit='+(f.get('limit')||50);
    if(f.get('key_id')) q += '&key_id='+f.get('key_id');
    api('/logs'+q).then(data => {
        const tbody = document.getElementById('logs-table');
        if (!data || data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="9" class="empty-state">暂无日志</td></tr>';
            return;
        }
        tbody.innerHTML = (data||[]).map(l =>
            '<tr><td class="hide-on-mobile">'+l.ID+'</td><td>'+l.DownstreamKeyID+'</td><td>'+esc(l.UpstreamName||'-')+'</td><td class="hide-on-mobile">'+esc(l.ClientIP||'-')+'</td><td class="hide-on-mobile">'+esc(l.ProviderStyle)+'</td><td class="hide-on-mobile">'+esc(l.Path)+'</td><td><span class="badge '+(l.StatusCode<400?'badge-green':'badge-red')+'">'+l.StatusCode+'</span></td><td class="hide-on-mobile">'+l.LatencyMs+'ms</td><td>'+fmtTime(l.CreatedAt)+'</td></tr>'
        ).join('');
    });
}

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
            '<tr><td><input type="checkbox" class="model-cb" value="'+e.ID+'" onchange="updateModelBatchBtn()"></td><td class="hide-on-mobile">'+e.ID+'</td><td><code>'+esc(e.Pattern)+'</code></td><td class="hide-on-mobile">'+fmtTime(e.CreatedAt)+'</td><td>'+
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
        if(d.error) alert(d.error);
        else { e.target.reset(); loadModelWhitelist(); }
    });
}

function deleteModelPattern(id) {
    if (!confirm('确认删除此模式？')) return;
    api('/models/whitelist/'+id, {method:'DELETE'}).then(d => {
        if(d.error) alert(d.error);
        else loadModelWhitelist();
    });
}

function batchDeleteModelPatterns() {
    const ids = Array.from(document.querySelectorAll('.model-cb:checked')).map(cb => parseInt(cb.value));
    if (ids.length === 0) return;
    if (!confirm('确认删除选中的 ' + ids.length + ' 个模式？')) return;
    api('/models/whitelist/batch', {method:'DELETE', body: JSON.stringify({ids: ids})}).then(d => {
        if(d.error) alert(d.error);
        else loadModelWhitelist();
    });
}

// --- Status ---
function loadStatus() {
    api('/status').then(d => {
        const grid = document.getElementById('status-grid');
        const statCard = (label, value, color) => '<div style="background:var(--bg);padding:16px;border-radius:var(--radius-sm);border:1px solid var(--border);text-align:center;"><div style="font-size:1.5rem;font-weight:700;color:'+(color||'var(--text)')+';">'+value+'</div><div style="font-size:0.75rem;color:var(--text-dim);margin-top:4px;">'+label+'</div></div>';
        grid.innerHTML = statCard('版本', esc(d.version||'-'), 'var(--accent)') +
            statCard('运行时间', esc(d.uptime||'-'), 'var(--green)') +
            statCard('密钥数量', d.total_keys||0) +
            statCard('今日请求', d.today_requests||0, 'var(--orange)') +
            statCard('审计丢弃', d.audit_dropped||0, d.audit_dropped>0?'var(--red)':'var(--green)');

        const tbody = document.getElementById('status-upstreams');
        const ups = d.healthy_upstreams || [];
        if (ups.length === 0) {
            tbody.innerHTML = '<tr><td colspan="3" class="empty-state">暂无健康上游</td></tr>';
        } else {
            tbody.innerHTML = ups.map(u => '<tr><td class="hide-on-mobile">'+u.id+'</td><td>'+esc(u.name)+'</td><td><code class="truncate-url" title="'+esc(u.url)+'">'+esc(u.url)+'</code></td></tr>').join('');
        }
    });
}

// --- Helpers ---
function fmtTime(s) {
    if (!s) return '-';
    const d = new Date(s);
    if (isNaN(d)) return esc(s);
    const pad = n => String(n).padStart(2,'0');
    return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+' '+pad(d.getHours())+':'+pad(d.getMinutes())+':'+pad(d.getSeconds());
}
</script>
</body>
</html>`)
