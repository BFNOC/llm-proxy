	let TOKEN = '';
	let upstreamKeyEditors = {create: [], edit: []};
	let upstreamKeyEditorMeta = {create: {}, edit: {}};
	let manageAPIKeyRows = [];
	let keysCache = [];
	let keysBindMap = {};
	let keysRpmData = {};
	let keysOverrideMap = {};
	let confirmResolver = null;
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

function esc(s) {
    return (s == null ? '' : String(s)).replace(/[&<>"']/g, ch => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    })[ch]);
}
function toast(msg, type) {
    type = type || 'info';
    const root = document.getElementById('toast-root');
    if (!root) { console.log(msg); return; }
    const el = document.createElement('div');
    el.className = 'toast toast-' + type;
    const icon = type === 'ok' ? '✓' : type === 'err' ? '!' : 'i';
    el.innerHTML = '<span class="toast-icon">'+icon+'</span><div class="toast-body">'+esc(msg)+'</div><button type="button" class="toast-close" aria-label="关闭">×</button>';
    const close = () => { if (el.parentNode) el.remove(); };
    el.querySelector('.toast-close').onclick = close;
    root.appendChild(el);
    setTimeout(close, type === 'err' ? 5000 : 2800);
}
function toastOk(msg) { toast(msg, 'ok'); }
function toastErr(msg) { toast(msg || '操作失败', 'err'); }
function askConfirm(message, opts) {
    opts = opts || {};
    return new Promise(resolve => {
        const dlg = document.getElementById('dlg-confirm');
        document.getElementById('confirm-title').textContent = opts.title || '确认操作';
        document.getElementById('confirm-msg').textContent = message || '';
        const okBtn = document.getElementById('confirm-ok');
        okBtn.textContent = opts.okText || '确认';
        okBtn.className = 'btn ' + (opts.danger ? 'btn-danger' : 'btn-primary');
        if (confirmResolver) confirmResolver(false);
        confirmResolver = resolve;
        dlg.showModal();
    });
}
document.getElementById('confirm-form').addEventListener('submit', (e) => {
    e.preventDefault();
    document.getElementById('dlg-confirm').close();
    if (confirmResolver) { const r = confirmResolver; confirmResolver = null; r(true); }
});
document.getElementById('confirm-cancel').addEventListener('click', () => {
    document.getElementById('dlg-confirm').close();
    if (confirmResolver) { const r = confirmResolver; confirmResolver = null; r(false); }
});
document.getElementById('dlg-confirm').addEventListener('cancel', () => {
    if (confirmResolver) { const r = confirmResolver; confirmResolver = null; r(false); }
});
function copyTextToClipboard(text, btn) {
    const orig = btn ? btn.textContent : '';
    if (btn) {
        btn.disabled = true;
        btn.textContent = '...';
    }
    const done = (ok) => {
        if (btn) {
            btn.textContent = ok ? '已复制' : orig;
            btn.disabled = false;
            if (ok) setTimeout(() => { btn.textContent = orig; }, 1500);
        }
        if (ok) toastOk('已复制到剪贴板');
        else toastErr('复制失败，请手动复制');
    };
    if (!navigator.clipboard || !navigator.clipboard.writeText) {
        try {
            const ta = document.createElement('textarea');
            ta.value = text; ta.style.position = 'fixed'; ta.style.left = '-9999px';
            document.body.appendChild(ta); ta.select();
            const ok = document.execCommand('copy');
            document.body.removeChild(ta);
            done(ok);
        } catch (_) { done(false); }
        return Promise.resolve();
    }
    return navigator.clipboard.writeText(text).then(() => done(true)).catch(() => done(false));
}
function closeActionMenus() {
    document.querySelectorAll('.action-menu.show').forEach(m => {
        m.classList.remove('show');
        m.style.top = '';
        m.style.left = '';
        m.style.right = '';
    });
}
function positionActionMenu(btn, menu) {
    // fixed positioning escapes table/card overflow so last-row menus stay fully visible
    menu.style.visibility = 'hidden';
    menu.classList.add('show');
    const br = btn.getBoundingClientRect();
    const mw = menu.offsetWidth || 148;
    const mh = menu.offsetHeight || 200;
    let left = br.right - mw;
    if (left < 8) left = 8;
    if (left + mw > window.innerWidth - 8) left = window.innerWidth - mw - 8;
    const spaceBelow = window.innerHeight - br.bottom;
    let top;
    if (spaceBelow < mh + 8 && br.top > mh + 8) {
        top = br.top - mh - 4; // open upward
    } else {
        top = br.bottom + 4;
        if (top + mh > window.innerHeight - 8) top = Math.max(8, window.innerHeight - mh - 8);
    }
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
    menu.style.right = 'auto';
    menu.style.visibility = '';
}
function toggleActionMenu(e) {
    e.stopPropagation();
    const btn = e.currentTarget;
    const menu = btn.nextElementSibling;
    if (!menu || !menu.classList.contains('action-menu')) return;
    const wasOpen = menu.classList.contains('show');
    closeActionMenus();
    if (!wasOpen) positionActionMenu(btn, menu);
}
document.addEventListener('click', closeActionMenus);
document.addEventListener('scroll', closeActionMenus, true);
window.addEventListener('resize', closeActionMenus);
const api = (path, opts={}) => fetch('/admin/api'+path, {
    ...opts,
    headers: {'Authorization':'Bearer '+TOKEN, 'Content-Type':'application/json', ...(opts.headers||{})}
}).then(r => r.json());

// --- Auth ---
function showAuthError(msg) {
    const el = document.getElementById('auth-error');
    if (!el) return;
    el.textContent = msg || '';
    el.classList.toggle('show', !!msg);
}
function enterDashboard(statusData) {
    document.getElementById('auth-section').style.display = 'none';
    document.getElementById('main-section').style.display = 'block';
    const ver = document.getElementById('header-version');
    if (ver && statusData && statusData.version) {
        ver.textContent = 'v' + statusData.version;
        ver.style.display = '';
    }
    loadUpstreams().then(() => loadKeys());
    if (typeof startSSE === 'function') startSSE();
}
function authenticate() {
    const input = document.getElementById('admin-token');
    const btn = document.getElementById('btn-auth');
    TOKEN = (input.value || '').trim();
    if (!TOKEN) { showAuthError('请输入管理令牌'); input.focus(); return; }
    showAuthError('');
    btn.disabled = true;
    btn.textContent = '验证中...';
    api('/status').then(d => {
        btn.disabled = false;
        btn.textContent = '进入控制台';
        if (d.error) { showAuthError('令牌无效，请检查后重试'); return; }
        saveToken(TOKEN);
        enterDashboard(d);
        toastOk('已登录');
    }).catch(() => {
        btn.disabled = false;
        btn.textContent = '进入控制台';
        showAuthError('连接失败，请确认服务已启动');
    });
}
function logout() {
    clearToken();
    TOKEN = '';
    stopStatusTimer();
    if (typeof stopSSE === 'function') stopSSE();
    Object.keys(localStorage).filter(k => k.startsWith('cf_config_')).forEach(k => localStorage.removeItem(k));
    document.getElementById('main-section').style.display = 'none';
    document.getElementById('auth-section').style.display = 'flex';
    showAuthError('');
    const input = document.getElementById('admin-token');
    if (input) input.value = '';
}
// Auto-login from cookie
window.addEventListener('DOMContentLoaded', () => {
    const saved = readToken();
    if (saved) {
        TOKEN = saved;
        api('/status').then(d => {
            if (d.error) { clearToken(); return; }
            enterDashboard(d);
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
    const titles = {upstreams:'上游服务', keys:'下游密钥', models:'模型白名单', logs:'请求日志', status:'系统状态', tools:'工具与设置'};
    const pt = document.getElementById('page-title');
    if (pt && titles[name]) pt.textContent = titles[name];
    if (name === 'status') { loadStatus(); startStatusTimer(); } else { stopStatusTimer(); }
	if (name === 'models') loadModelWhitelist();
	if (name === 'keys') loadKeys();
	if (name === 'logs') loadLogs();
	if (name === 'tools') { loadTestModels(); loadSettings(); loadHeaderCapture(); }
}
function startStatusTimer() {
    stopStatusTimer();
    statusTimer = setInterval(loadStatus, 5000);
}
function stopStatusTimer() {
    if (statusTimer) { clearInterval(statusTimer); statusTimer = null; }
}


// --- Helpers ---
function fmtTime(s) {
    if (!s) return '-';
    const d = new Date(s);
    if (isNaN(d)) return esc(s);
    const pad = n => String(n).padStart(2,'0');
    return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+' '+pad(d.getHours())+':'+pad(d.getMinutes())+':'+pad(d.getSeconds());
}
