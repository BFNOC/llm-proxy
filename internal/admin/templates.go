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
            --bg: #f0f2f5; --bg-card: #ffffff; --bg-hover: #f5f6f8;
            --border: #e2e5ea; --text: #1a1d23; --text-dim: #6b7280;
            --accent: #6366f1; --accent-hover: #4f46e5; --accent-light: rgba(99,102,241,0.08);
            --green: #10b981; --red: #ef4444; --orange: #f59e0b;
            --radius: 14px; --radius-sm: 10px; --radius-xs: 6px;
            --shadow-sm: 0 1px 3px rgba(0,0,0,0.06); --shadow-md: 0 4px 16px rgba(0,0,0,0.08);
        }
        body { font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Display', 'Segoe UI', Roboto, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; -webkit-font-smoothing: antialiased; }
        .container { max-width: 1200px; margin: 0 auto; padding: 28px 24px; }

        /* Header */
        .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 32px; }
        .header h1 { font-size: 1.6rem; font-weight: 700; background: linear-gradient(135deg, #6366f1, #8b5cf6, #a78bfa); -webkit-background-clip: text; -webkit-text-fill-color: transparent; letter-spacing: -0.02em; }
        .header .logout-btn { background: none; border: 1px solid var(--border); color: var(--text-dim); padding: 8px 16px; border-radius: var(--radius-sm); cursor: pointer; font-size: 0.85rem; transition: all 0.2s; }
        .header .logout-btn:hover { border-color: var(--red); color: var(--red); }

        /* Auth */
        #auth-section { display: flex; align-items: center; justify-content: center; min-height: 80vh; }
        .auth-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: 48px 40px; text-align: center; width: 400px; box-shadow: var(--shadow-md); }
        .auth-card h2 { font-size: 1.4rem; margin-bottom: 8px; }
        .auth-card p { color: var(--text-dim); margin-bottom: 24px; font-size: 0.9rem; }
        .auth-card input { width: 100%; padding: 12px 16px; background: var(--bg); border: 1.5px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-size: 0.95rem; margin-bottom: 16px; transition: all 0.2s; }
        .auth-card input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-light); }

        /* Buttons */
        .btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 10px 20px; border: none; border-radius: var(--radius-sm); cursor: pointer; font-size: 0.875rem; font-weight: 500; transition: all 0.2s; font-family: inherit; white-space: nowrap; }
        .btn-primary { background: var(--accent); color: #fff; box-shadow: 0 1px 3px rgba(99,102,241,0.3); }
        .btn-primary:hover { background: var(--accent-hover); transform: translateY(-1px); box-shadow: 0 4px 12px rgba(99,102,241,0.3); }
        .btn-sm { padding: 6px 12px; font-size: 0.8rem; border-radius: var(--radius-xs); }
        .btn-ghost { background: transparent; color: var(--text-dim); border: 1px solid var(--border); }
        .btn-ghost:hover { background: var(--bg-hover); color: var(--text); border-color: #c8ccd4; }
        .btn-danger { background: transparent; color: var(--red); border: 1px solid transparent; }
        .btn-danger:hover { background: rgba(239,68,68,0.08); }
        .btn-success { background: transparent; color: var(--green); border: 1px solid transparent; }
        .btn-success:hover { background: rgba(16,185,129,0.08); }

        /* Tabs */
        .tab-nav { display: flex; gap: 2px; margin-bottom: 28px; background: var(--bg-card); border-radius: var(--radius); padding: 5px; border: 1px solid var(--border); box-shadow: var(--shadow-sm); }
        .tab-nav button { flex: 1; padding: 10px 16px; background: transparent; border: none; color: var(--text-dim); cursor: pointer; border-radius: var(--radius-sm); font-size: 0.875rem; font-weight: 500; transition: all 0.2s; font-family: inherit; white-space: nowrap; }
        .tab-nav button.active { background: var(--accent); color: #fff; box-shadow: 0 1px 4px rgba(99,102,241,0.25); }
        .tab-nav button:hover:not(.active) { background: var(--bg-hover); color: var(--text); }
        .tab-content { display: none; animation: fadeIn 0.25s ease; }
        .tab-content.active { display: block; }
        @keyframes fadeIn { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: translateY(0); } }

        /* Cards */
        .card { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: 24px; margin-bottom: 20px; box-shadow: var(--shadow-sm); }
        .card-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 18px; }
        .card-header h2 { font-size: 1.15rem; font-weight: 600; letter-spacing: -0.01em; }

        /* Tables */
        .table-container { overflow-x: auto; -webkit-overflow-scrolling: touch; }
        table { width: 100%; border-collapse: collapse; }
        thead th { text-align: left; padding: 10px 16px; font-size: 0.7rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-dim); border-bottom: 1.5px solid var(--border); white-space: nowrap; }
        tbody td { padding: 14px 16px; font-size: 0.875rem; border-bottom: 1px solid #f0f1f3; vertical-align: middle; }
        tbody tr { transition: background 0.15s; }
        tbody tr:hover { background: var(--bg-hover); }
        tbody tr:last-child td { border-bottom: none; }

        /* Badges */
        .badge { display: inline-block; padding: 3px 10px; border-radius: 999px; font-size: 0.7rem; font-weight: 600; white-space: nowrap; letter-spacing: 0.02em; }
        .badge-green { background: rgba(16,185,129,0.1); color: #059669; }
        .badge-red { background: rgba(239,68,68,0.1); color: var(--red); }
        .badge-purple { background: var(--accent-light); color: var(--accent); }

        /* Forms */
        .form-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; align-items: end; }
        .form-grid.narrow { grid-template-columns: 1fr auto; }
        .form-group { display: flex; flex-direction: column; gap: 6px; }
        .form-group label { font-size: 0.75rem; font-weight: 600; color: var(--text); letter-spacing: 0.01em; }
        input, select, textarea { width: 100%; padding: 10px 14px; background: var(--bg); border: 1.5px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-size: 0.875rem; font-family: inherit; transition: all 0.2s; }
        input:focus, select:focus, textarea:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-light); background: #fff; }
        textarea { resize: vertical; line-height: 1.6; }
        input::placeholder, textarea::placeholder { color: #b0b5be; }
        code { background: var(--bg); padding: 2px 8px; border-radius: var(--radius-xs); font-size: 0.85em; word-break: break-all; }

        /* Key display */
        .key-display { font-family: 'SF Mono', 'JetBrains Mono', monospace; background: var(--bg); padding: 16px; border-radius: var(--radius-sm); word-break: break-all; border: 1px solid var(--accent); margin-top: 8px; font-size: 0.9rem; }
        .key-alert { background: var(--accent-light); border: 1px solid rgba(99,102,241,0.2); border-radius: var(--radius-sm); padding: 16px; margin-bottom: 16px; }
        .key-alert strong { color: var(--accent); }

        /* Dialog / Modal */
        dialog { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); color: var(--text); padding: 0; max-width: 520px; width: 92%; position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); margin: 0; box-shadow: 0 20px 60px rgba(0,0,0,0.15); overflow: hidden; }
        dialog::backdrop { background: rgba(0,0,0,0.4); backdrop-filter: blur(8px); }
        dialog[open] { animation: dialogIn 0.2s ease; }
        @keyframes dialogIn { from { opacity: 0; transform: translate(-50%, -48%) scale(0.97); } to { opacity: 1; transform: translate(-50%, -50%) scale(1); } }
        @keyframes spin { to { transform: rotate(360deg); } }
        dialog > form { padding: 28px 32px 24px; }
        dialog h3 { font-size: 1.15rem; font-weight: 600; letter-spacing: -0.01em; }
        dialog .form-group { margin-bottom: 14px; }
        dialog .form-group label { font-size: 0.8rem; color: var(--text-dim); }
        dialog .dialog-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 24px; padding-top: 16px; border-top: 1px solid #f0f1f3; }

        /* Binding checkboxes */
        .binding-list { display: flex; flex-direction: column; gap: 6px; max-height: 300px; overflow-y: auto; }
        .binding-item { display: flex; align-items: center; gap: 10px; padding: 12px 14px; background: var(--bg); border-radius: var(--radius-sm); cursor: pointer; transition: all 0.15s; border: 1px solid transparent; }
        .binding-item:hover { background: var(--bg-hover); border-color: var(--border); }
        .binding-item input[type="checkbox"] { accent-color: var(--accent); width: 16px; height: 16px; }
        .binding-label { flex: 1; font-size: 0.9rem; font-weight: 500; }
        .binding-url { color: var(--text-dim); font-size: 0.8rem; }

        /* Empty state */
        .empty-state { text-align: center; padding: 32px; color: var(--text-dim); font-size: 0.9rem; }

        /* Action buttons in table */
        .actions { display: flex; gap: 4px; flex-wrap: wrap; justify-content: flex-end; }

        /* Truncate URL */
        .truncate-url { display: inline-block; max-width: 150px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; vertical-align: middle; }

        /* Model pattern tags */
        .model-tags { display: flex; flex-wrap: wrap; gap: 4px; }
        .model-tag { display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; background: var(--accent-light); color: var(--accent); border-radius: var(--radius-xs); font-size: 0.72rem; font-weight: 500; font-family: 'SF Mono', 'JetBrains Mono', monospace; }
        .model-tag-all { color: var(--text-dim); font-size: 0.8rem; font-style: italic; }

        /* Responsive */
        @media (max-width: 768px) {
            body { padding: 8px; font-size: 0.9rem; }
            .card { padding: 12px; border-radius: var(--radius-sm); }
            .hide-on-mobile { display: none !important; }
            .tab-nav { flex-wrap: wrap; }
            .tab-nav button { flex: 1 1 calc(50% - 4px); justify-content: center; }
            .form-grid { grid-template-columns: 1fr; }
            dialog > form { padding: 20px; }
            dialog h3 { padding: 16px 20px 0; }
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
            <button onclick="showTab('tools',this)">实用工具</button>
        </nav>

        <!-- Upstreams Tab -->
        <div id="tab-upstreams" class="tab-content active">
            <div class="card">
                <div class="card-header">
                    <h2>上游服务商</h2>
                    <button class="btn btn-primary btn-sm" onclick="document.getElementById('dlg-upstream').showModal()">+ 添加上游</button>
                </div>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th>名称</th><th>地址</th><th class="hide-on-mobile">密钥</th><th class="hide-on-mobile">调度</th><th class="hide-on-mobile">代理</th><th class="hide-on-mobile">优先级</th><th class="hide-on-mobile">模型模式</th><th>状态</th><th>操作</th></tr></thead>
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
                        <div style="display:flex;align-items:center;gap:8px;margin-top:8px;">
                            <div class="key-display" id="new-key-value" style="flex:1;margin-top:0;"></div>
                            <button class="btn btn-primary btn-sm" onclick="copyNewKey(this)" style="white-space:nowrap;">📋 复制</button>
                        </div>
                    </div>
                </div>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th class="hide-on-mobile">前缀</th><th>名称</th><th>RPM</th><th>当前 RPM</th><th>状态</th><th class="hide-on-mobile">绑定上游</th><th class="hide-on-mobile">模型路由</th><th>操作</th></tr></thead>
                <tbody id="keys-table"></tbody></table>
                </div>
            </div>
            <div class="card" id="key-stats-card" style="display:none;">
                <div class="card-header">
                    <h2>密钥使用统计</h2>
                    <button class="btn btn-ghost btn-sm" onclick="loadKeyUsageStats()">刷新</button>
                </div>
                <div id="key-stats-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:12px;"></div>
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
                <table><thead><tr><th class="hide-on-mobile">ID</th><th>密钥</th><th>上游</th><th class="hide-on-mobile">Key#</th><th class="hide-on-mobile">模型</th><th class="hide-on-mobile">代理</th><th class="hide-on-mobile">IP</th><th>地区</th><th class="hide-on-mobile">风格</th><th class="hide-on-mobile">路径</th><th>状态码</th><th class="hide-on-mobile">延迟</th><th>时间</th></tr></thead>
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
                <div id="status-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:12px;margin-bottom:24px;"></div>
                <h3 style="font-size:0.95rem;margin-bottom:14px;">上游健康</h3>
                <div id="status-upstreams" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:12px;"></div>
            </div>
        </div>

        <!-- Tools Tab -->
        <div id="tab-tools" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <h2>系统设置</h2>
                </div>
                <div class="form-grid" style="margin-bottom:0;">
                    <div class="form-group">
                        <label>429 自动禁用阈值</label>
                        <input id="setting-threshold" type="number" min="0" style="width:100px;">
                        <p style="color:var(--text-dim);font-size:0.78rem;margin-top:4px;">连续 429 达到此次数立即禁用 Key，0 = 不禁用</p>
                    </div>
                    <div class="form-group">
                        <label>&nbsp;</label>
                        <button class="btn btn-primary btn-sm" onclick="saveSettings()">保存</button>
                    </div>
                </div>
            </div>
            <div class="card">
                <div class="card-header">
                    <h2>额度 JSON 解析</h2>
                </div>
                <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:12px;">粘贴 new-api 查额返回的 JSON，自动解析并展示额度信息。</p>
                <textarea id="tools-json-input" rows="6" style="width:100%;font-family:'SF Mono','JetBrains Mono',monospace;font-size:0.8rem;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:var(--radius-sm);padding:12px;resize:vertical;" placeholder='粘贴 JSON 如: {"code":true,"data":{...}}'></textarea>
                <div style="margin-top:12px;">
                    <button class="btn btn-primary btn-sm" onclick="parseQuotaJSON()">解析</button>
                    <button class="btn btn-ghost btn-sm" onclick="document.getElementById('tools-json-input').value='';document.getElementById('tools-result').innerHTML=''">清空</button>
                </div>
                <div id="tools-result" style="margin-top:16px;"></div>
            </div>
            <div class="card">
                <div class="card-header">
                    <h2>测试模型管理</h2>
                </div>
                <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:12px;">管理测试对话框中可选择的模型列表。添加后在测试上游时可快速选择。</p>
                <div style="display:flex;gap:8px;margin-bottom:16px;">
                    <input id="tm-search" placeholder="搜索模型..." style="flex:1;font-size:0.85rem;" oninput="renderTestModels()">
                    <select id="tm-filter-protocol" style="width:140px;font-size:0.85rem;" onchange="renderTestModels()">
                        <option value="">全部协议</option>
                        <option value="openai">OpenAI</option>
                        <option value="anthropic">Anthropic</option>
                        <option value="responses">Responses</option>
                    </select>
                </div>
                <div style="display:flex;gap:8px;margin-bottom:16px;">
                    <input id="tm-new-name" placeholder="模型名称" style="flex:1;font-size:0.85rem;">
                    <select id="tm-new-protocol" style="width:120px;font-size:0.85rem;">
                        <option value="openai">OpenAI</option>
                        <option value="anthropic">Anthropic</option>
                        <option value="responses">Responses</option>
                    </select>
                    <button class="btn btn-primary btn-sm" onclick="createTestModel()">添加</button>
                </div>
                <div class="table-container">
                    <table><thead><tr><th>ID</th><th>模型名称</th><th>协议</th><th>操作</th></tr></thead>
                    <tbody id="test-models-table"></tbody></table>
                </div>
            </div>
        </div>
    </div>
</div>

<!-- Create Upstream Dialog -->
<dialog id="dlg-upstream">
    <h3>添加上游</h3>
    <form onsubmit="createUpstream(event)">
        <div class="form-group"><label>名称</label><input name="name" placeholder="如 openai-sgp" required></div>
        <div class="form-group"><label>地址</label><input name="base_url" placeholder="https://api.example.com" required></div>
        <div class="form-group"><label>API 密钥 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（每行一个，支持多个，留空则无鉴权接入）</span></label><textarea name="api_keys" rows="3" style="font-family:'SF Mono','JetBrains Mono',monospace;font-size:0.82rem;resize:vertical;letter-spacing:0.02em;" placeholder="sk-key1&#10;sk-key2（留空 = 无鉴权）"></textarea></div>
        <div class="form-group"><label>Key 调度模式</label><select name="key_scheduling_mode"><option value="round-robin">轮询 (Round-Robin)</option><option value="fill">填充 (Fill — 优先用满当前 Key)</option></select></div>
        <div class="form-group"><label>备注 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（可选，如 Key 来源）</span></label><input name="remark" placeholder="如：网友A分享的 Claude 额度"></div>
        <div class="form-group"><label>代理地址 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（可选）</span></label><input name="proxy_url" placeholder="socks5://127.0.0.1:1080"></div>
        <div class="form-group"><label>优先级 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（0 = 最高）</span></label><input name="priority" type="number" value="0" min="0"></div>
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
        <div class="form-group"><label>地址</label><input name="base_url" placeholder="https://api.example.com" required></div>
        <div class="form-group"><label>API 密钥 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（每行一个，留空不修改）</span></label><textarea name="api_keys" rows="3" style="font-family:'SF Mono','JetBrains Mono',monospace;font-size:0.82rem;resize:vertical;letter-spacing:0.02em;" placeholder="留空不修改"></textarea></div>
        <div class="form-group"><label>Key 调度模式</label><select name="key_scheduling_mode"><option value="round-robin">轮询 (Round-Robin)</option><option value="fill">填充 (Fill — 优先用满当前 Key)</option></select></div>
        <div class="form-group"><label>备注</label><input name="remark" placeholder="如：网友A分享的 Claude 额度"></div>
        <div class="form-group"><label>代理地址 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（留空 = 环境代理）</span></label><input name="proxy_url" placeholder="socks5://127.0.0.1:1080"></div>
        <div class="form-group"><label>优先级</label><input name="priority" type="number" min="0"></div>
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
        <div class="form-group"><label>密钥名称</label><input name="name" placeholder="如 user-1" required></div>
        <div class="form-group"><label>每分钟请求限制 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（0 = 不限）</span></label><input name="rpm_limit" type="number" value="0" min="0"></div>
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
        <div class="form-group"><label>每分钟请求限制 <span style="font-weight:400;color:var(--text-dim);text-transform:none;letter-spacing:0">（0 = 不限）</span></label><input name="rpm_limit" type="number" min="0"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
        </div>
    </form>
</dialog>

<!-- Upstream Binding Dialog -->
<dialog id="dlg-binding">
    <h3>配置上游绑定</h3>
    <form>
        <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">选择此密钥允许使用的上游。不选择任何上游表示允许全部。</p>
        <input type="hidden" id="binding-key-id">
        <div id="binding-list" class="binding-list"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveBindings()">保存</button>
        </div>
    </form>
</dialog>

<!-- Model Patterns Dialog -->
<dialog id="dlg-model-patterns">
    <h3>配置模型模式</h3>
    <form>
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
    </form>
</dialog>

<!-- Key Model Override Dialog -->
<dialog id="dlg-model-override" style="max-width:600px;">
    <h3>配置模型路由覆盖</h3>
    <form>
        <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">为此密钥指定特定模型走指定上游。支持 <code>*</code> 通配符。精确匹配优先于通配。覆盖上游不可用时请求将被拒绝。</p>
        <input type="hidden" id="mo-key-id">
        <div style="display:flex;gap:8px;margin-bottom:16px;align-items:end;">
            <div class="form-group" style="flex:1">
                <label>模型模式</label>
                <input id="mo-new-pattern" placeholder="如: claude-opus-4-6">
            </div>
            <div class="form-group" style="flex:1">
                <label>目标上游</label>
                <select id="mo-new-upstream"></select>
            </div>
            <button type="button" class="btn btn-primary btn-sm" style="margin-bottom:2px;" onclick="addOverrideRule()">添加</button>
        </div>
        <div id="mo-rules" style="min-height:32px;margin-bottom:16px;"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveOverrides()">保存</button>
        </div>
    </form>
</dialog>

<!-- Per-Key API Key Management Dialog -->
<dialog id="dlg-manage-keys" style="max-width:520px;">
    <h3>管理 API Keys</h3>
    <input type="hidden" id="mk-upstream-id">
    <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">启用或禁用单个 Key。</p>
    <div id="mk-keys-list" style="max-height:400px;overflow-y:auto;"></div>
    <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" onclick="document.getElementById('dlg-manage-keys').close()">关闭</button>
    </div>
</dialog>

<!-- Upstream Test Dialog -->
<dialog id="dlg-test-upstream" style="max-width:520px;">
    <div style="padding:24px 28px 0;">
        <h3 style="margin-bottom:4px;">测试上游连接</h3>
        <p style="font-size:0.8rem;color:var(--text-dim);margin-bottom:20px;">选择 Key 和协议，发送测试请求验证连通性</p>
    </div>
    <input type="hidden" id="tu-upstream-id">
    <div style="padding:0 28px;">
        <div class="form-group" style="margin-bottom:16px;">
            <label>使用 Key</label>
            <select id="tu-key-select" style="font-size:0.85rem;"></select>
        </div>
        <div class="form-group" style="margin-bottom:16px;">
            <label>协议</label>
            <select id="tu-protocol" onchange="onTuProtocolChange()" style="font-size:0.85rem;">
                <option value="openai">OpenAI (Chat Completions)</option>
                <option value="anthropic">Anthropic (Messages)</option>
                <option value="responses">OpenAI (Responses / Codex)</option>
            </select>
        </div>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:14px;margin-bottom:16px;">
            <div class="form-group">
                <label>模型</label>
                <input id="tu-model" value="" placeholder="输入或选择模型" list="tu-model-list" style="font-size:0.85rem;">
                <datalist id="tu-model-list"></datalist>
            </div>
            <div class="form-group">
                <label>提示词</label>
                <input id="tu-prompt" value="你是什么模型？" style="font-size:0.85rem;">
            </div>
        </div>
    </div>
    <div id="tu-result" style="display:none;margin:0 28px 16px;"></div>
    <div class="dialog-actions" style="padding:16px 28px;">
        <button type="button" class="btn btn-ghost" onclick="document.getElementById('dlg-test-upstream').close()">关闭</button>
        <button type="button" class="btn btn-primary" id="btn-tu-test" onclick="submitUpstreamTest()">
            <svg style="width:14px;height:14px;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
            测试
        </button>
    </div>
</dialog>

<!-- CF Bypass Config Dialog -->
<dialog id="dlg-cf" style="max-width:480px;">
    <h3>CF 防御绕过</h3>
    <form>
        <p style="color:var(--text-dim);font-size:0.85rem;margin-bottom:16px;">填入从浏览器获取的 <code>cf_clearance</code> Cookie 和 <code>User-Agent</code>，用于绕过 Cloudflare 验证。保存在浏览器 localStorage。</p>
        <input type="hidden" id="cf-upstream-id">
        <div class="form-group"><label>cf_clearance</label><input id="cf-clearance" placeholder="cf_clearance cookie 值"></div>
        <div class="form-group" style="margin-top:12px;"><label>User-Agent</label><input id="cf-ua" placeholder="与获取 cookie 时相同的浏览器 UA"></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-danger btn-sm" onclick="clearCFConfig()">清除</button>
            <div style="flex:1"></div>
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveCFConfig()">保存</button>
        </div>
    </form>
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
    stopStatusTimer();
    // 清理 CF 绕过配置
    Object.keys(localStorage).filter(k => k.startsWith('cf_config_')).forEach(k => localStorage.removeItem(k));
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
let statusTimer = null;
function showTab(name, btn) {
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-nav button').forEach(b => b.classList.remove('active'));
    document.getElementById('tab-'+name).classList.add('active');
    btn.classList.add('active');
    if (name === 'status') { loadStatus(); startStatusTimer(); } else { stopStatusTimer(); }
    if (name === 'models') loadModelWhitelist();
    if (name === 'keys') loadKeys();
    if (name === 'tools') { loadTestModels(); loadSettings(); }
}
function startStatusTimer() {
    stopStatusTimer();
    statusTimer = setInterval(loadStatus, 5000);
}
function stopStatusTimer() {
    if (statusTimer) { clearInterval(statusTimer); statusTimer = null; }
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
            tbody.innerHTML = '<tr><td colspan="10" class="empty-state">暂无上游服务</td></tr>';
            return;
        }
        tbody.innerHTML = allUpstreams.map(u => {
            const patterns = allModelPatterns[u.id] || [];
            let modelHtml = '<span class="model-tag-all">*</span>';
            if (patterns.length > 0) {
                modelHtml = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
            }
            // Key 摘要：显示总数和启用数
            const keyDetails = u.api_key_details || [];
            const totalKeys = keyDetails.length || (u.api_keys || []).length;
            const enabledKeys = keyDetails.filter(k => k.enabled).length;
            const allEnabled = totalKeys > 0 && enabledKeys === totalKeys;
            let keyBadge = '';
            if (totalKeys === 0) {
                keyBadge = '<span class="badge badge-green">无鉴权</span>';
            } else if (allEnabled) {
                keyBadge = '<span class="badge badge-purple" style="cursor:pointer" onclick="openManageKeysDialog('+u.id+')" title="点击管理">'+totalKeys+' Key</span>';
            } else {
                keyBadge = '<span class="badge badge-purple" style="cursor:pointer;background:rgba(245,158,11,0.1);color:var(--orange)" onclick="openManageKeysDialog('+u.id+')" title="点击管理">'+enabledKeys+'/'+totalKeys+' Key</span>';
            }
            const schedMode = u.key_scheduling_mode || 'round-robin';
            const schedLabel = schedMode === 'fill' ? '填充' : '轮询';
            const schedColor = schedMode === 'fill' ? 'var(--orange)' : 'var(--accent)';
            const remarkHtml = u.remark ? '<div style="font-size:0.75rem;color:var(--text-dim);margin-top:2px;font-style:italic;" title="'+esc(u.remark)+'">'+esc(u.remark.length>20?u.remark.substring(0,20)+'...':u.remark)+'</div>' : '';
            return '<tr><td class="hide-on-mobile">'+u.id+'</td><td><strong>'+esc(u.name)+'</strong>'+remarkHtml+'</td><td><code class="truncate-url" title="'+esc(u.base_url)+'">'+esc(u.base_url)+'</code></td><td class="hide-on-mobile">'+keyBadge+'</td><td class="hide-on-mobile"><span style="font-size:0.75rem;color:'+schedColor+';font-weight:500;">'+schedLabel+'</span></td><td class="hide-on-mobile">'+(u.proxy_url?'<code class="truncate-url" title="'+esc(u.proxy_url)+'">'+esc(u.proxy_url)+'</code>':'<span class="badge badge-green">环境代理</span>')+'</td><td class="hide-on-mobile">'+u.priority+'</td><td class="hide-on-mobile"><div class="model-tags">'+modelHtml+'</div></td><td>'+
            (u.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
            '</td><td class="actions">'+
            '<button class="btn btn-ghost btn-sm" onclick="testProxy(event,'+u.id+')">测试</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="checkQuota(event,'+u.id+')">查额</button> '+
            '<button class="btn btn-ghost btn-sm" style="'+(getCFConfig(u.id)?'color:var(--green)':'')+';font-size:0.8em" onclick="openCFDialog('+u.id+')">CF</button> '+
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
    const keysRaw = f.get('api_keys') || '';
    const apiKeys = keysRaw.split('\n').map(s => s.trim()).filter(s => s.length > 0);
    api('/upstreams', {method:'POST', body: JSON.stringify({
        name: f.get('name'), base_url: f.get('base_url'),
        api_keys: apiKeys, proxy_url: f.get('proxy_url')||'',
        priority: parseInt(f.get('priority')||'0'),
        key_scheduling_mode: f.get('key_scheduling_mode')||'round-robin',
        remark: f.get('remark')||''
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
    dlg.querySelector('[name=api_keys]').value = (u.api_keys || []).join('\n');
    dlg.querySelector('[name=proxy_url]').value = u.proxy_url||'';
    dlg.querySelector('[name=priority]').value = u.priority;
    dlg.querySelector('[name=key_scheduling_mode]').value = u.key_scheduling_mode || 'round-robin';
    dlg.querySelector('[name=remark]').value = u.remark || '';
    dlg.showModal();
}

function submitEditUpstream(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    const id = f.get('id');
    const body = {name: f.get('name'), base_url: f.get('base_url'), proxy_url: f.get('proxy_url')||'', priority: parseInt(f.get('priority')||'0'), key_scheduling_mode: f.get('key_scheduling_mode')||'round-robin', remark: f.get('remark')||''};
    const keysRaw = f.get('api_keys') || '';
    const apiKeys = keysRaw.split('\n').map(s => s.trim()).filter(s => s.length > 0);
    if (apiKeys.length > 0) body.api_keys = apiKeys;
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

// --- Per-Key API Key Management ---
function openManageKeysDialog(upstreamId) {
    document.getElementById('mk-upstream-id').value = upstreamId;
    const list = document.getElementById('mk-keys-list');
    list.innerHTML = '<div style="text-align:center;padding:16px;color:var(--text-dim)">加载中...</div>';
    document.getElementById('dlg-manage-keys').showModal();
    api('/upstreams/'+upstreamId+'/apikeys').then(data => {
        if (!data || data.length === 0) {
            list.innerHTML = '<div class="empty-state">无 API Key</div>';
            return;
        }
        list.innerHTML = data.map(kd => {
            const shortKey = kd.key.length > 20 ? kd.key.substring(0, 10) + '...' + kd.key.substring(kd.key.length - 8) : kd.key;
            return '<div style="display:flex;align-items:center;gap:10px;padding:12px 14px;background:var(--bg);border-radius:var(--radius-sm);margin-bottom:8px;border:1px solid '+(kd.enabled?'var(--border)':'rgba(239,68,68,0.2)')+';'+(!kd.enabled?'opacity:0.6;':'')+'">'+
                '<code style="flex:1;font-size:0.82rem;word-break:break-all;" title="'+esc(kd.key)+'">'+esc(shortKey)+'</code>'+
                '<label style="cursor:pointer;display:flex;align-items:center;gap:6px;font-size:0.8rem;white-space:nowrap;color:'+(kd.enabled?'var(--green)':'var(--text-dim)')+';font-weight:500;" onclick="toggleAPIKey('+upstreamId+','+kd.row_id+','+(!kd.enabled)+')">'+(kd.enabled?'<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--green)"></span> 启用':'<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--text-dim)"></span> 禁用')+'</label>'+
                '</div>';
        }).join('');
    });
}

function toggleAPIKey(upstreamId, keyRowId, enabled) {
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId+'/enabled', {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if(d.error) alert(d.error); else { loadUpstreams(); openManageKeysDialog(upstreamId); }
    });
}

function testProxy(e, id) {
    // 打开测试对话框，让用户选择 Key、协议、模型
    openTestUpstreamDialog(id);
}

function openTestUpstreamDialog(upstreamId) {
    document.getElementById('tu-upstream-id').value = upstreamId;
    document.getElementById('tu-result').style.display = 'none';
    document.getElementById('tu-protocol').value = 'openai';
    document.getElementById('tu-model').value = '';
    document.getElementById('tu-prompt').value = '你是什么模型？';
    // 加载测试模型列表并更新 datalist
    loadTestModels().then(() => updateTuModelDatalist('openai'));
    const sel = document.getElementById('tu-key-select');
    sel.innerHTML = '<option value="">加载中...</option>';
    document.getElementById('dlg-test-upstream').showModal();
    api('/upstreams/'+upstreamId+'/apikeys').then(data => {
        if (!data || data.length === 0) {
            sel.innerHTML = '<option value="0">无鉴权（公益站）</option>';
            return;
        }
        sel.innerHTML = data.map((kd, i) => {
            const shortKey = kd.key.length > 20 ? kd.key.substring(0, 10) + '...' + kd.key.substring(kd.key.length - 8) : kd.key;
            return '<option value="'+kd.row_id+'"'+(i===0?' selected':'')+'>('+kd.row_id+') '+esc(shortKey)+(kd.enabled?'':' [已禁用]')+'</option>';
        }).join('');
    });
}

function onTuProtocolChange() {
    const proto = document.getElementById('tu-protocol').value;
    document.getElementById('tu-model').value = '';
    updateTuModelDatalist(proto);
}

function submitUpstreamTest() {
    const upstreamId = document.getElementById('tu-upstream-id').value;
    const keyRowId = document.getElementById('tu-key-select').value;
    if (!keyRowId && keyRowId !== '0') { alert('请选择一个 Key'); return; }
    const btn = document.getElementById('btn-tu-test');
    const resultDiv = document.getElementById('tu-result');
    btn.innerHTML = '<svg style="width:14px;height:14px;animation:spin 1s linear infinite;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg> 测试中...';
    btn.disabled = true;
    resultDiv.style.display = 'none';
    const cfBody = getCFConfig(parseInt(upstreamId));
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId+'/test', {method:'POST', body: JSON.stringify({
        protocol: document.getElementById('tu-protocol').value,
        model: document.getElementById('tu-model').value,
        prompt: document.getElementById('tu-prompt').value,
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
            html += '<div style="padding:14px 18px;border-top:1px solid rgba(16,185,129,0.12);font-size:0.82rem;color:var(--text-dim);display:flex;gap:20px;">';
            html += '<span>模型: <strong style="color:var(--text);font-weight:600;">'+esc(d.actual_model||d.model)+'</strong></span>';
            html += '<span>协议: <strong style="color:var(--text);font-weight:600;">'+esc(d.protocol)+'</strong></span>';
            html += '</div>';
            if (d.reply) {
                html += '<div style="border-top:1px solid rgba(16,185,129,0.12);padding:14px 18px;">';
                html += '<div style="font-size:0.72rem;font-weight:600;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.05em;margin-bottom:8px;">回复内容</div>';
                html += '<div style="font-size:0.85rem;line-height:1.7;white-space:pre-wrap;word-break:break-word;max-height:200px;overflow-y:auto;padding:12px 14px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);">'+esc(d.reply)+'</div>';
                html += '</div>';
            }
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
                html += '<div style="color:var(--text);">'+esc(d.error_message)+'</div>';
            } else if (d.error) {
                html += '<div style="color:var(--text);">'+esc(d.error)+'</div>';
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
    const cfBody = getCFConfig(id);
    api('/upstreams/'+id+'/check-quota', {method:'POST', body: JSON.stringify(cfBody||{})}).then(d => {
        btn.textContent = origText;
        btn.disabled = false;
        const tr = document.createElement('tr');
        tr.id = 'quota-row-'+id;
        const td = document.createElement('td');
        td.colSpan = 9;
        td.style.cssText = 'padding:0;border:none;';

        if (d.success) {
            const data = d.data;
            let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;">';
            html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">';
            html += '<span style="font-weight:600;">📊 ' + esc(data.name) + '</span>';
            html += '<button class="btn btn-ghost btn-sm" onclick="this.closest(\'tr\').remove()" style="padding:2px 8px;">✕</button></div>';
            html += renderQuotaDetails(data);
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
                html += '<pre style="margin-top:8px;padding:8px;background:var(--bg);border-radius:4px;font-size:0.75rem;overflow-x:auto;max-height:120px;color:var(--text-dim);white-space:pre-wrap;word-break:break-all;">' + esc(d.origin_content) + '</pre>';
            }
            if (d.origin_content || (msg && msg.indexOf('403') !== -1)) {
                html += '<div style="margin-top:8px;"><button class="btn btn-ghost btn-sm" style="color:var(--orange)" onclick="this.closest(\'tr\').remove();openCFDialog('+id+')">🔧 可能需要配置 CF 绕过</button></div>';
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
        td.colSpan = 9;
        td.innerHTML = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;color:var(--red);">请求失败: '+esc(err.message)+'</div>';
        tr.appendChild(td);
        row.after(tr);
    });
}

// --- Keys ---
function loadKeys() {
    Promise.all([api('/keys'), api('/keys/bindings'), api('/key-rpm'), api('/keys/model-overrides')]).then(([data, bindMap, rpmData, overrideMap]) => {
        const keys = data || [];
        bindMap = bindMap || {};
        rpmData = rpmData || {};
        overrideMap = overrideMap || {};
        const tbody = document.getElementById('keys-table');
        if (keys.length === 0) {
            tbody.innerHTML = '<tr><td colspan="9" class="empty-state">暂无密钥</td></tr>';
            return;
        }
        tbody.innerHTML = keys.map(k => {
            const bound = bindMap[k.id] || [];
            let bindText = '<span class="badge badge-purple">全部</span>';
            if (bound.length > 0) {
                const names = bound.map(uid => { const u = allUpstreams.find(x=>x.id===uid); return u ? esc(u.name) : uid; });
                bindText = names.join(', ');
            }
            const overrides = overrideMap[k.id] || [];
            let overrideText = '<span style="color:var(--text-dim)">无</span>';
            if (overrides.length > 0) {
                const patterns = [...new Set(overrides.map(o => o.ModelPattern))];
                overrideText = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
            }
            const currentRpm = rpmData[k.id] || 0;
            const limitText = k.rpm_limit || '不限';
            const rpmColor = k.rpm_limit > 0 && currentRpm >= k.rpm_limit * 0.8 ? 'var(--red)' : currentRpm > 0 ? 'var(--green)' : 'var(--text-dim)';
            return '<tr><td class="hide-on-mobile">'+k.id+'</td><td class="hide-on-mobile"><code>'+esc(k.key_prefix)+'...</code></td><td>'+esc(k.name)+'</td><td>'+(k.rpm_limit||'不限')+'</td><td><span style="color:'+rpmColor+';font-weight:600">'+currentRpm+'</span><span style="color:var(--text-dim)">/'+ limitText+'</span></td><td>'+
            (k.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
            '</td><td class="hide-on-mobile">'+bindText+'</td><td class="hide-on-mobile"><div class="model-tags" style="gap:4px">'+overrideText+'</div></td><td class="actions">'+
            '<button class="btn btn-ghost btn-sm" onclick="copyKey(event,'+k.id+')">复制</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="openBindingDialog('+k.id+')">绑定</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="openOverrideDialog('+k.id+')">路由</button> '+
            '<button class="btn btn-ghost btn-sm" onclick="editKey('+k.id+')">编辑</button> '+
            '<button class="btn btn-success btn-sm" onclick="toggleKey('+k.id+','+(!k.enabled)+')">切换</button> '+
            '<button class="btn btn-danger btn-sm" onclick="deleteKey('+k.id+')">删除</button>'+
            '</td></tr>';
        }).join('');
    });
    loadKeyUsageStats();
}

function loadKeyUsageStats() {
    api('/logs/key-stats').then(data => {
        const grid = document.getElementById('key-stats-grid');
        const card = document.getElementById('key-stats-card');
        if (!data || data.length === 0) {
            card.style.display = 'none';
            return;
        }
        card.style.display = 'block';
        grid.innerHTML = data.map(s => {
            const successRate = s.total > 0 ? (s.success / s.total * 100).toFixed(1) : '0.0';
            const rateColor = successRate >= 99 ? 'var(--green)' : successRate >= 95 ? 'var(--orange)' : 'var(--red)';
            return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:14px;">'+
                '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">'+
                '<span style="font-weight:600;font-size:0.9rem;">Key #'+s.key_id+'</span>'+
                '<span class="badge badge-purple" style="font-size:0.7rem;">'+s.total+' 次请求</span></div>'+
                '<div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;font-size:0.82rem;">'+
                '<div><span style="color:var(--text-dim);">成功率</span> <strong style="color:'+rateColor+'">'+successRate+'%</strong></div>'+
                '<div><span style="color:var(--text-dim);">平均延迟</span> <strong>'+Math.round(s.avg_latency_ms)+'ms</strong></div>'+
                '<div><span style="color:var(--text-dim);">成功</span> <strong style="color:var(--green)">'+s.success+'</strong></div>'+
                '<div><span style="color:var(--text-dim);">失败</span> <strong style="color:var(--red)">'+s.error+'</strong></div>'+
                '</div></div>';
        }).join('');
    });
}

function copyKey(e, id) {
    const btn = e.target;
    const orig = btn.textContent;
    btn.disabled = true;
    btn.textContent = '...';
    api('/keys/'+id+'/reveal').then(d => {
        if (d.error) { alert(d.error); btn.textContent = orig; btn.disabled = false; return; }
        navigator.clipboard.writeText(d.key).then(() => {
            btn.textContent = '✅ 已复制';
            setTimeout(() => { btn.textContent = orig; btn.disabled = false; }, 1500);
        }).catch(() => {
            prompt('复制密钥:', d.key);
            btn.textContent = orig; btn.disabled = false;
        });
    }).catch(() => { btn.textContent = orig; btn.disabled = false; });
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

function copyNewKey(btn) {
    const key = document.getElementById('new-key-value').textContent;
    navigator.clipboard.writeText(key).then(() => {
        const orig = btn.textContent;
        btn.textContent = '✅ 已复制';
        setTimeout(() => btn.textContent = orig, 1500);
    }).catch(() => {
        const range = document.createRange();
        range.selectNodeContents(document.getElementById('new-key-value'));
        const sel = window.getSelection();
        sel.removeAllRanges(); sel.addRange(range);
        document.execCommand('copy');
        btn.textContent = '✅ 已复制';
        setTimeout(() => btn.textContent = '📋 复制', 1500);
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

// --- Key Model Override ---
let moCurrentRules = []; // [{model_pattern, upstream_id}]
function openOverrideDialog(keyId) {
    document.getElementById('mo-key-id').value = keyId;
    // Populate upstream select
    const sel = document.getElementById('mo-new-upstream');
    sel.innerHTML = allUpstreams.map(u => '<option value="'+u.id+'">'+esc(u.name)+'</option>').join('');
    document.getElementById('mo-new-pattern').value = '';
    // Load existing overrides
    api('/keys/'+keyId+'/model-overrides').then(data => {
        moCurrentRules = (data || []).map(o => ({model_pattern: o.ModelPattern, upstream_id: o.UpstreamID}));
        renderOverrideRules();
        document.getElementById('dlg-model-override').showModal();
    });
}

function renderOverrideRules() {
    const container = document.getElementById('mo-rules');
    if (moCurrentRules.length === 0) {
        container.innerHTML = '<div class="empty-state" style="padding:16px;">无覆盖规则（使用默认路由）</div>';
        return;
    }
    container.innerHTML = '<table style="width:100%"><thead><tr><th>模型模式</th><th>目标上游</th><th></th></tr></thead><tbody>' +
        moCurrentRules.map((r, i) => {
            const u = allUpstreams.find(x => x.id === r.upstream_id);
            const uName = u ? esc(u.name) : 'ID:'+r.upstream_id;
            return '<tr><td><code>'+esc(r.model_pattern)+'</code></td><td>'+uName+'</td><td><button class="btn btn-danger btn-sm" onclick="removeOverrideRule('+i+')">删除</button></td></tr>';
        }).join('') + '</tbody></table>';
}

function addOverrideRule() {
    const pattern = document.getElementById('mo-new-pattern').value.trim();
    const upstreamId = parseInt(document.getElementById('mo-new-upstream').value);
    if (!pattern) { alert('请输入模型模式'); return; }
    if (!upstreamId) { alert('请选择目标上游'); return; }
    // Check duplicate
    if (moCurrentRules.some(r => r.model_pattern === pattern && r.upstream_id === upstreamId)) {
        alert('规则已存在'); return;
    }
    moCurrentRules.push({model_pattern: pattern, upstream_id: upstreamId});
    document.getElementById('mo-new-pattern').value = '';
    renderOverrideRules();
}

function removeOverrideRule(idx) {
    moCurrentRules.splice(idx, 1);
    renderOverrideRules();
}

function saveOverrides() {
    const keyId = document.getElementById('mo-key-id').value;
    api('/keys/'+keyId+'/model-overrides', {method:'PUT', body: JSON.stringify({overrides: moCurrentRules})}).then(d => {
        if(d.error) alert(d.error);
        else { document.getElementById('dlg-model-override').close(); loadKeys(); }
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

// --- CF Bypass Config (localStorage) ---
function getCFConfig(upstreamId) {
    try {
        const raw = localStorage.getItem('cf_config_'+upstreamId);
        if (!raw) return null;
        const cfg = JSON.parse(raw);
        if (cfg.cf_clearance && cfg.cf_user_agent) return cfg;
    } catch(e) {}
    return null;
}

function openCFDialog(upstreamId) {
    document.getElementById('cf-upstream-id').value = upstreamId;
    const cfg = getCFConfig(upstreamId) || {};
    document.getElementById('cf-clearance').value = cfg.cf_clearance || '';
    document.getElementById('cf-ua').value = cfg.cf_user_agent || '';
    document.getElementById('dlg-cf').showModal();
}

function saveCFConfig() {
    const id = document.getElementById('cf-upstream-id').value;
    const clearance = document.getElementById('cf-clearance').value.trim();
    const ua = document.getElementById('cf-ua').value.trim();
    if (!clearance && !ua) {
        localStorage.removeItem('cf_config_'+id);
    } else if (!clearance || !ua) {
        alert('cf_clearance 和 User-Agent 需要同时填写');
        return;
    } else {
        localStorage.setItem('cf_config_'+id, JSON.stringify({cf_clearance: clearance, cf_user_agent: ua}));
    }
    document.getElementById('dlg-cf').close();
    loadUpstreams();
}

function clearCFConfig() {
    const id = document.getElementById('cf-upstream-id').value;
    localStorage.removeItem('cf_config_'+id);
    document.getElementById('cf-clearance').value = '';
    document.getElementById('cf-ua').value = '';
    document.getElementById('dlg-cf').close();
    loadUpstreams();
}

// --- Quota Rendering (shared by checkQuota and Tools tab) ---
function renderQuotaDetails(data) {
    const fmt = n => n.toLocaleString();
    const toUSD = n => '$' + (n / 500000).toFixed(2);
    let html = '';
    if (data.unlimited_quota) {
        html += '<span class="badge badge-green">无限额度</span>';
    } else {
        const pct = data.total_granted > 0 ? (data.total_used / data.total_granted * 100).toFixed(1) : '0.0';
        const barColor = pct > 80 ? 'var(--red)' : pct > 50 ? 'var(--orange)' : 'var(--green)';
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
        const expDate = new Date(data.expires_at * 1000);
        const remain = data.expires_at * 1000 - Date.now();
        let remainStr = '', remainColor = 'var(--text-dim)';
        if (remain <= 0) { remainStr = '已过期'; remainColor = 'var(--red)'; }
        else {
            const days = Math.floor(remain / 86400000);
            const hrs = Math.floor((remain % 86400000) / 3600000);
            const mins = Math.floor((remain % 3600000) / 60000);
            if (days > 0) remainStr = days + '天' + hrs + '小时';
            else if (hrs > 0) remainStr = hrs + '小时' + mins + '分';
            else remainStr = mins + '分钟';
            remainStr = '剩余 ' + remainStr;
            if (remain < 86400000) remainColor = 'var(--red)';
            else if (remain < 86400000 * 3) remainColor = 'var(--orange)';
        }
        html += '<div style="font-size:0.8rem;margin-top:8px;">过期时间: ' + fmtTime(expDate.toISOString()) + ' <span style="color:' + remainColor + ';font-weight:600;">(' + remainStr + ')</span></div>';
    }
    if (data.model_limits_enabled) {
        html += '<div style="font-size:0.8rem;margin-top:8px;color:var(--text-dim);">模型限制: <span class="badge badge-green">已启用</span></div>';
    }
    if (data.model_limits && typeof data.model_limits === 'object') {
        const models = Object.keys(data.model_limits).filter(k => data.model_limits[k]);
        if (models.length > 0) {
            html += '<div style="margin-top:8px;"><div style="font-size:0.75rem;color:var(--text-dim);margin-bottom:4px;">可用模型 (' + models.length + ')</div>';
            html += '<div class="model-tags">' + models.map(m => '<span class="model-tag">' + esc(m) + '</span>').join('') + '</div></div>';
        }
    }
    return html;
}

function parseQuotaJSON() {
    const input = document.getElementById('tools-json-input').value.trim();
    const container = document.getElementById('tools-result');
    if (!input) { container.innerHTML = '<div style="color:var(--text-dim);">请粘贴 JSON</div>'; return; }
    let parsed;
    try { parsed = JSON.parse(input); } catch(e) {
        container.innerHTML = '<div style="color:var(--red);">JSON 解析失败: ' + esc(e.message) + '</div>';
        return;
    }
    // 检测格式：sub2api 有 isValid 字段
    if (parsed.isValid !== undefined) {
        container.innerHTML = renderSub2apiDetails(parsed);
    } else {
        // new-api 格式
        const data = parsed.data || parsed;
        let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">';
        html += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">';
        html += '<span style="font-weight:600;">📊 ' + esc(data.name || '未知') + '</span></div>';
        html += renderQuotaDetails(data);
        html += '</div>';
        container.innerHTML = html;
    }
}

function renderSub2apiDetails(d) {
    const toUSD = n => '$' + Number(n).toFixed(2);
    const fmtN = n => Number(n).toLocaleString();
    let html = '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">';
    // 标题行
    html += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:12px;flex-wrap:wrap;">';
    html += '<span style="font-weight:600;font-size:1.05rem;">📊 ' + esc(d.planName || 'Sub2API') + '</span>';
    const modeMap = {quota_limited:'额度限制',unrestricted:'无限制'};
    const modeLabel = modeMap[d.mode] || d.mode || '未知';
    const modeColor = d.mode === 'unrestricted' ? 'badge-green' : 'badge-orange';
    html += '<span class="badge ' + modeColor + '">' + esc(modeLabel) + '</span>';
    if (d.status) html += '<span class="badge badge-green">' + esc(d.status) + '</span>';
    if (!d.isValid) html += '<span class="badge badge-red">无效</span>';
    html += '</div>';

    // 额度信息
    if (d.quota && d.quota.limit > 0) {
        const used = d.quota.used || 0, limit = d.quota.limit || 0, remain = d.quota.remaining || 0;
        const pct = limit > 0 ? (used / limit * 100).toFixed(1) : '0.0';
        const barColor = pct > 80 ? 'var(--red)' : pct > 50 ? 'var(--orange)' : 'var(--green)';
        html += '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:12px;">';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">剩余</div><div style="font-size:1.1rem;font-weight:700;color:var(--green);">' + toUSD(remain) + '</div></div>';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">已用</div><div style="font-size:1.1rem;font-weight:700;color:var(--orange);">' + toUSD(used) + '</div></div>';
        html += '<div style="text-align:center;"><div style="font-size:0.75rem;color:var(--text-dim);">总额</div><div style="font-size:1.1rem;font-weight:700;">' + toUSD(limit) + '</div></div>';
        html += '</div>';
        html += '<div style="background:var(--bg-card);border-radius:4px;height:8px;overflow:hidden;">';
        html += '<div style="height:100%;width:' + pct + '%;background:' + barColor + ';border-radius:4px;transition:width 0.3s;"></div></div>';
        html += '<div style="text-align:right;font-size:0.75rem;color:var(--text-dim);margin-top:4px;">使用率 ' + pct + '%</div>';
    } else if (d.mode === 'unrestricted') {
        html += '<span class="badge badge-green">无限额度</span>';
        if (d.balance !== undefined) html += ' <span style="font-size:0.85rem;color:var(--text-dim);">余额: ' + toUSD(d.balance) + '</span>';
    }

    // 用量统计
    if (d.usage) {
        html += '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(120px,1fr));gap:8px;margin-top:12px;">';
        if (d.usage.rpm !== undefined) html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">RPM</div><div style="font-weight:600;">' + fmtN(d.usage.rpm) + '</div></div>';
        if (d.usage.tpm !== undefined) html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">TPM</div><div style="font-weight:600;">' + fmtN(d.usage.tpm) + '</div></div>';
        if (d.usage.average_duration_ms) html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">平均延迟</div><div style="font-weight:600;">' + (d.usage.average_duration_ms/1000).toFixed(1) + 's</div></div>';
        const today = d.usage.today;
        if (today) {
            html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">今日请求</div><div style="font-weight:600;">' + fmtN(today.requests) + '</div></div>';
            html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">今日花费</div><div style="font-weight:600;">' + toUSD(today.cost) + '</div></div>';
            html += '<div style="text-align:center;padding:8px;background:var(--bg-card);border-radius:var(--radius-sm);"><div style="font-size:0.7rem;color:var(--text-dim);">今日 Tokens</div><div style="font-weight:600;">' + fmtN(today.total_tokens) + '</div></div>';
        }
        html += '</div>';
    }

    // 模型用量明细
    if (d.model_stats && d.model_stats.length > 0) {
        const stats = d.model_stats.slice().sort((a,b) => (b.cost||0) - (a.cost||0));
        html += '<div style="margin-top:12px;"><div style="font-size:0.75rem;color:var(--text-dim);margin-bottom:6px;">模型用量明细 (' + stats.length + ')</div>';
        html += '<div class="table-container"><table style="font-size:0.8rem;"><thead><tr><th>模型</th><th>请求数</th><th>输入</th><th>输出</th><th>缓存读取</th><th>花费</th></tr></thead><tbody>';
        stats.forEach(m => {
            html += '<tr><td><span class="model-tag">' + esc(m.model) + '</span></td>';
            html += '<td>' + fmtN(m.requests) + '</td>';
            html += '<td>' + fmtN(m.input_tokens) + '</td>';
            html += '<td>' + fmtN(m.output_tokens) + '</td>';
            html += '<td>' + fmtN(m.cache_read_tokens) + '</td>';
            html += '<td>' + toUSD(m.cost) + '</td></tr>';
        });
        html += '</tbody></table></div></div>';
    }

    html += '</div>';
    return html;
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
            tbody.innerHTML = '<tr><td colspan="13" class="empty-state">暂无日志</td></tr>';
            return;
        }
        tbody.innerHTML = (data||[]).map(l => {
            const keyIdx = l.UpstreamKeyIdx;
            const keyIdxText = keyIdx >= 0 ? '#' + (keyIdx + 1) : '-';
            const modelText = l.Model || '-';
            const proxyText = l.UsedProxy ? esc(l.UsedProxy) : '<span style="color:var(--text-secondary)">直连</span>';
            return '<tr><td class="hide-on-mobile">'+l.ID+'</td><td>'+l.DownstreamKeyID+'</td><td>'+esc(l.UpstreamName||'-')+'</td><td class="hide-on-mobile"><span class="badge badge-purple" style="font-size:0.7rem">'+keyIdxText+'</span></td><td class="hide-on-mobile"><code style="font-size:0.78rem">'+esc(modelText)+'</code></td><td class="hide-on-mobile" style="font-size:0.78rem;max-width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(l.UsedProxy||'')+'">'+proxyText+'</td><td class="hide-on-mobile">'+esc(l.ClientIP||'-')+'</td><td>'+esc(l.IPRegion||'-')+'</td><td class="hide-on-mobile">'+esc(l.ProviderStyle)+'</td><td class="hide-on-mobile">'+esc(l.Path)+'</td><td><span class="badge '+(l.StatusCode<400?'badge-green':'badge-red')+'">'+l.StatusCode+'</span></td><td class="hide-on-mobile">'+l.LatencyMs+'ms</td><td>'+fmtTime(l.CreatedAt)+'</td></tr>';
        }).join('');
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

// --- Settings ---
function loadSettings() {
    api('/settings').then(data => {
        if (data) {
            document.getElementById('setting-threshold').value = data.auto_disable_threshold ?? 2;
        }
    });
}
function saveSettings() {
    const val = parseInt(document.getElementById('setting-threshold').value, 10);
    if (isNaN(val) || val < 0) { alert('阈值必须 >= 0'); return; }
    api('/settings', {method:'PUT', body: JSON.stringify({auto_disable_threshold: val})}).then(() => {
        loadSettings();
    });
}

// --- Test Models ---
let allTestModels = [];
function loadTestModels() {
    return api('/test-models').then(data => {
        allTestModels = Array.isArray(data) ? data.map(m => ({
            id: m.id || m.ID,
            name: m.name || m.Name || '',
            protocol: m.protocol || m.Protocol || 'openai',
            created_at: m.created_at || m.CreatedAt
        })) : [];
        renderTestModels();
        updateTuModelDatalist();
    }).catch(() => { allTestModels = []; renderTestModels(); });
}

function renderTestModels() {
    const search = (document.getElementById('tm-search').value || '').toLowerCase();
    const protocolFilter = document.getElementById('tm-filter-protocol').value;
    const tbody = document.getElementById('test-models-table');
    let filtered = allTestModels;
    if (protocolFilter) filtered = filtered.filter(m => (m.protocol||'') === protocolFilter);
    if (search) filtered = filtered.filter(m => (m.name||'').toLowerCase().indexOf(search) !== -1);
    if (filtered.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4" class="empty-state">暂无测试模型</td></tr>';
        return;
    }
    const protoLabel = {openai:'OpenAI',anthropic:'Anthropic',responses:'Responses'};
    tbody.innerHTML = filtered.map(m => {
        const name = m.name || '(未命名)';
        const proto = m.protocol || 'openai';
        return '<tr><td>'+m.id+'</td><td><code>'+esc(name)+'</code></td><td><span class="badge badge-purple">'+(protoLabel[proto]||proto)+'</span></td><td class="actions">'+
        '<button class="btn btn-ghost btn-sm" onclick="editTestModel('+m.id+')">编辑</button> '+
        '<button class="btn btn-danger btn-sm" onclick="deleteTestModel('+m.id+')">删除</button>'+
        '</td></tr>';
    }).join('');
}

function createTestModel() {
    const nameEl = document.getElementById('tm-new-name');
    const protoEl = document.getElementById('tm-new-protocol');
    const name = (nameEl.value || '').trim();
    const protocol = protoEl.value || 'openai';
    if (!name) { alert('请输入模型名称'); return; }
    api('/test-models', {method:'POST', body: JSON.stringify({name:name, protocol:protocol})}).then(d => {
        if (d.error) { alert(d.error); return; }
        nameEl.value = '';
        loadTestModels();
    });
}

function editTestModel(id) {
    const m = allTestModels.find(x => x.id === id);
    if (!m) return;
    const row = document.querySelector('tr:has(button[onclick="editTestModel('+id+')"])');
    if (!row) return;
    const curName = m.name || '';
    const curProto = m.protocol || 'openai';
    row.innerHTML = '<td>'+m.id+'</td>'+
        '<td><input id="em-name-'+id+'" value="'+esc(curName)+'" style="font-size:0.85rem;padding:6px 10px;width:100%;"></td>'+
        '<td><select id="em-proto-'+id+'" style="font-size:0.85rem;padding:6px 10px;"><option value="openai"'+(curProto==='openai'?' selected':'')+'>OpenAI</option><option value="anthropic"'+(curProto==='anthropic'?' selected':'')+'>Anthropic</option><option value="responses"'+(curProto==='responses'?' selected':'')+'>Responses</option></select></td>'+
        '<td class="actions">'+
        '<button class="btn btn-primary btn-sm" onclick="saveTestModel('+id+')">保存</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="renderTestModels()">取消</button>'+
        '</td>';
}

function saveTestModel(id) {
    const name = (document.getElementById('em-name-'+id).value||'').trim();
    const protocol = document.getElementById('em-proto-'+id).value;
    if (!name) { alert('名称不能为空'); return; }
    api('/test-models/'+id, {method:'PUT', body: JSON.stringify({name:name, protocol:protocol})}).then(d => {
        if (d.error) alert(d.error); else loadTestModels();
    });
}

function deleteTestModel(id) {
    if (!confirm('确认删除此测试模型？')) return;
    api('/test-models/'+id, {method:'DELETE'}).then(d => {
        if (d.error) { alert(d.error); return; }
        loadTestModels();
    });
}

function updateTuModelDatalist(protocol) {
    const dl = document.getElementById('tu-model-list');
    if (!dl) return;
    let models = allTestModels;
    if (protocol) models = models.filter(m => (m.protocol||'') === protocol);
    dl.innerHTML = models.map(m => '<option value="'+esc(m.name||'')+'">').join('');
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
            statCard('并发请求', d.active_requests||0, 'var(--accent)') +
            statCard('RPM', d.rpm||0, 'var(--green)') +
            statCard('RPS', d.rps||'0.0', 'var(--orange)') +
            statCard('审计丢弃', d.audit_dropped||0, d.audit_dropped>0?'var(--red)':'var(--green)');

        const container = document.getElementById('status-upstreams');
        const ups = d.healthy_upstreams || [];
        if (ups.length === 0) {
            container.innerHTML = '<div class="empty-state">暂无健康上游</div>';
        } else {
            container.innerHTML = ups.map(u => {
                const mode = u.key_scheduling_mode || 'round-robin';
                const modeLabel = mode === 'fill' ? '填充' : '轮询';
                const modeColor = mode === 'fill' ? 'var(--orange)' : 'var(--accent)';
                return '<div style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-sm);padding:16px;">'+
                    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">'+
                    '<strong style="font-size:0.95rem;">'+esc(u.name)+'</strong>'+
                    '<span class="badge badge-green">健康</span></div>'+
                    '<div style="font-size:0.82rem;color:var(--text-dim);margin-bottom:6px;"><code style="font-size:0.78rem;" title="'+esc(u.url)+'">'+esc(u.url)+'</code></div>'+
                    '<div style="display:flex;gap:12px;font-size:0.8rem;">'+
                    '<span>Keys: <strong>'+u.key_count+'</strong></span>'+
                    '<span>调度: <span style="color:'+modeColor+';font-weight:500;">'+modeLabel+'</span></span>'+
                    '</div></div>';
            }).join('');
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
