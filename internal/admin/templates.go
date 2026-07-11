package admin

// dashboardHTML is a single-page admin dashboard (zero external assets).
var dashboardHTML = []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM Proxy · 控制台</title>
    <style>
        *, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }
        :root {
            /* Original cool indigo palette */
            --ink: oklch(0.22 0.02 265);
            --ink-soft: oklch(0.38 0.02 265);
            --paper: oklch(0.97 0.008 265);
            --paper-2: oklch(0.955 0.01 265);
            --surface: oklch(0.995 0.004 265);
            --surface-2: oklch(0.98 0.006 265);
            --line: oklch(0.90 0.012 265);
            --line-strong: oklch(0.82 0.014 265);
            --text: oklch(0.22 0.02 265);
            --text-dim: oklch(0.52 0.02 265);
            --text-faint: oklch(0.62 0.015 265);
            --accent: oklch(0.55 0.18 275);
            --accent-hover: oklch(0.48 0.18 275);
            --accent-soft: oklch(0.55 0.18 275 / 0.1);
            --accent-ring: oklch(0.55 0.18 275 / 0.18);
            --teal: oklch(0.55 0.12 230);
            --teal-soft: oklch(0.55 0.12 230 / 0.12);
            --green: oklch(0.62 0.16 155);
            --green-soft: oklch(0.62 0.16 155 / 0.12);
            --red: oklch(0.60 0.20 25);
            --red-soft: oklch(0.60 0.20 25 / 0.1);
            --orange: oklch(0.72 0.15 70);
            --orange-soft: oklch(0.72 0.15 70 / 0.14);
            --rail: oklch(0.99 0.004 265);
            --rail-text: oklch(0.22 0.02 265);
            --rail-dim: oklch(0.48 0.02 265);
            --rail-active: oklch(0.55 0.18 275);
            --rail-hover: oklch(0.55 0.18 275 / 0.08);
            --rail-active-bg: oklch(0.55 0.18 275 / 0.12);
            --radius: 10px;
            --radius-sm: 8px;
            --radius-xs: 6px;
            --shadow-sm: 0 1px 2px oklch(0.22 0.02 265 / 0.05);
            --shadow-md: 0 8px 24px oklch(0.22 0.02 265 / 0.08);
            --shadow-lg: 0 20px 60px oklch(0.22 0.02 265 / 0.16);
            --font: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Noto Sans SC", "Microsoft YaHei", sans-serif;
            --font-ui: var(--font);
            --font-mono: "SF Mono", "JetBrains Mono", "Cascadia Code", ui-monospace, Menlo, Consolas, monospace;
            --space-xs: 4px; --space-sm: 8px; --space-md: 16px; --space-lg: 24px; --space-xl: 32px;
            --rail-w: 220px;
            --focus: 0 0 0 3px var(--accent-ring);
            --ease: cubic-bezier(0.16, 1, 0.3, 1);
            --bg: var(--paper);
            --bg-card: var(--surface);
            --bg-hover: var(--paper-2);
            --border: var(--line);
            --text-secondary: var(--text-dim);
            --accent-light: var(--accent-soft);
            color-scheme: light;
        }
        html { height: 100%; }
        body {
            font-family: var(--font-ui);
            background: var(--paper);
            color: var(--text);
            min-height: 100%;
            -webkit-font-smoothing: antialiased;
            line-height: 1.5;
            font-size: 14px;
            letter-spacing: -0.01em;
        }
        body::before {
            content: "";
            position: fixed; inset: 0; pointer-events: none; z-index: 0;
            background:
                radial-gradient(1200px 600px at 100% -10%, oklch(0.55 0.18 275 / 0.07), transparent 55%),
                radial-gradient(900px 500px at -10% 100%, oklch(0.55 0.12 230 / 0.05), transparent 50%);
            opacity: 1;
        }
        .app-shell { position: relative; z-index: 1; min-height: 100vh; }

        /* ===== Auth ===== */
        #auth-section {
            display: flex; align-items: center; justify-content: center;
            min-height: 100vh; padding: 32px 20px;
            position: relative;
            background: var(--paper);
        }
        #auth-section::before {
            content: ""; position: absolute; inset: 0;
            background:
                radial-gradient(900px 500px at 50% 0%, oklch(0.55 0.18 275 / 0.08), transparent 55%),
                radial-gradient(700px 400px at 100% 100%, oklch(0.55 0.12 230 / 0.05), transparent 50%);
        }
        #auth-section::after { display: none; }
        .auth-card {
            position: relative; z-index: 1;
            width: min(420px, 100%);
            background: var(--surface);
            border: 1px solid var(--line);
            border-radius: 12px;
            padding: 36px 32px 32px;
            box-shadow: var(--shadow-md);
        }
        .auth-brand {
            display: flex; align-items: center; gap: 12px; margin-bottom: 22px;
        }
        .auth-mark {
            width: 42px; height: 42px; border-radius: 10px;
            background: linear-gradient(145deg, oklch(0.62 0.18 275), oklch(0.48 0.18 275));
            color: #fff; display: grid; place-items: center;
            font-family: var(--font); font-weight: 700; font-size: 1.05rem;
            box-shadow: 0 6px 16px oklch(0.55 0.18 275 / 0.35);
            letter-spacing: -0.04em;
        }
        .auth-card h2 {
            font-family: var(--font); font-size: 1.45rem; font-weight: 700;
            letter-spacing: -0.03em; line-height: 1.15; color: var(--ink);
        }
        .auth-card .auth-sub {
            color: var(--text-dim); font-size: 0.88rem; margin-top: 2px;
        }
        .auth-card > p {
            color: var(--text-dim); margin-bottom: 18px; font-size: 0.9rem;
            padding-bottom: 16px; border-bottom: 1px dashed var(--line);
        }
        .auth-card input {
            width: 100%; padding: 12px 14px;
            background: var(--paper); border: 1.5px solid var(--line);
            border-radius: var(--radius); color: var(--text); font-size: 0.95rem;
            margin-bottom: 12px; transition: border-color 0.15s, box-shadow 0.15s;
            font-family: var(--font-mono);
        }
        .auth-card input:focus { outline: none; border-color: var(--accent); box-shadow: var(--focus); background: var(--surface); }
        .auth-error {
            display: none; color: var(--red); font-size: 0.84rem;
            margin: -4px 0 12px; padding: 10px 12px;
            background: var(--red-soft); border-radius: var(--radius-sm);
            border-left: 3px solid var(--red);
        }
        .auth-error.show { display: block; }

        /* ===== Main shell: rail + canvas ===== */
        #main-section { display: none; min-height: 100vh; }
        #main-section.is-ready, #main-section[style*="block"] {
            display: grid !important;
            grid-template-columns: var(--rail-w) minmax(0, 1fr);
            min-height: 100vh;
        }
        .rail {
            position: sticky; top: 0; height: 100vh;
            background: var(--rail); color: var(--rail-text);
            display: flex; flex-direction: column;
            padding: 20px 14px 16px;
            border-right: 1px solid var(--line);
            z-index: 50;
        }
        .rail-brand {
            display: flex; align-items: center; gap: 10px;
            padding: 4px 8px 18px; margin-bottom: 8px;
            border-bottom: 1px solid var(--line);
        }
        .rail-mark {
            width: 34px; height: 34px; border-radius: 8px;
            background: linear-gradient(145deg, oklch(0.62 0.18 275), oklch(0.52 0.18 275));
            color: #fff; display: grid; place-items: center;
            font-family: var(--font); font-weight: 800; font-size: 0.95rem;
            flex: 0 0 auto; letter-spacing: -0.04em;
            box-shadow: 0 2px 8px oklch(0.55 0.18 275 / 0.25);
        }
        .rail-brand h1 {
            font-family: var(--font); font-size: 1.05rem; font-weight: 700;
            letter-spacing: -0.03em; line-height: 1.15; color: var(--ink);
        }
        .rail-brand small {
            display: block; font-family: var(--font-ui); font-size: 0.68rem;
            color: var(--rail-dim); font-weight: 500; letter-spacing: 0.04em;
            text-transform: uppercase; margin-top: 2px;
        }
        .tab-nav {
            display: flex; flex-direction: column; gap: 2px;
            flex: 1; overflow-y: auto; padding: 4px 0;
            scrollbar-width: thin; scrollbar-color: var(--line) transparent;
        }
        .tab-nav button {
            display: flex; align-items: center; gap: 10px;
            width: 100%; text-align: left;
            padding: 10px 12px; background: transparent; border: none;
            color: var(--rail-dim); cursor: pointer;
            border-radius: var(--radius); font-size: 0.88rem; font-weight: 550;
            transition: background 0.15s, color 0.15s, transform 0.15s;
            font-family: inherit; white-space: nowrap;
            position: relative;
        }
        .tab-nav button .nav-ico {
            width: 18px; height: 18px; opacity: 0.75; flex: 0 0 auto;
            display: grid; place-items: center;
        }
        .tab-nav button .nav-ico svg { width: 16px; height: 16px; }
        .tab-nav button:hover:not(.active) {
            background: var(--rail-hover); color: var(--rail-text);
        }
        .tab-nav button.active {
            background: var(--rail-active-bg);
            color: var(--accent);
            font-weight: 650;
            box-shadow: inset 3px 0 0 var(--rail-active);
        }
        .tab-nav button.active .nav-ico { opacity: 1; color: var(--accent); }
        .rail-foot {
            margin-top: auto; padding-top: 14px;
            border-top: 1px solid var(--line);
            display: flex; flex-direction: column; gap: 8px;
        }
        .rail-foot .count-chip {
            align-self: flex-start;
            background: var(--paper-2); border-color: var(--line);
            color: var(--rail-dim); font-family: var(--font-mono); font-size: 0.72rem;
        }
        .logout-btn {
            background: var(--surface); border: 1px solid var(--line);
            color: var(--rail-dim); padding: 8px 12px; border-radius: var(--radius);
            cursor: pointer; font-size: 0.82rem; font-family: inherit;
            transition: all 0.15s; width: 100%; text-align: left;
        }
        .logout-btn:hover {
            border-color: oklch(0.60 0.20 25 / 0.35); color: var(--red);
            background: var(--red-soft);
        }

        .canvas {
            min-width: 0; min-height: 100vh;
            display: flex; flex-direction: column;
        }
        .topbar {
            display: flex; align-items: center; justify-content: space-between;
            gap: 16px; padding: 18px 28px 10px;
            position: sticky; top: 0; z-index: 40;
            background: linear-gradient(to bottom, var(--paper) 65%, transparent);
            backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
        }
        .topbar h2 {
            font-family: var(--font); font-size: 1.45rem; font-weight: 700;
            letter-spacing: -0.03em; color: var(--ink); line-height: 1.2;
        }
        .topbar-meta { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
        .live-dot {
            display: inline-flex; align-items: center; gap: 6px;
            font-size: 0.75rem; color: var(--text-dim);
            background: var(--surface); border: 1px solid var(--line);
            border-radius: 999px; padding: 4px 10px 4px 8px;
        }
        .live-dot i {
            width: 7px; height: 7px; border-radius: 50%;
            background: var(--green); box-shadow: 0 0 0 3px var(--green-soft);
            animation: livePulse 2s ease infinite;
        }
        @keyframes livePulse {
            0%, 100% { opacity: 1; box-shadow: 0 0 0 3px var(--green-soft); }
            50% { opacity: 0.7; box-shadow: 0 0 0 6px transparent; }
        }
        .page {
            flex: 1; padding: 6px 28px 40px;
            max-width: 1400px; width: 100%;
        }

        .tab-content { display: none; animation: pageIn 0.22s var(--ease); }
        .tab-content.active { display: block; }
        @keyframes pageIn {
            from { opacity: 0; transform: translateY(6px); }
            to { opacity: 1; transform: translateY(0); }
        }
        @media (prefers-reduced-motion: reduce) {
            .tab-content, dialog[open], dialog::backdrop, .action-menu.show, .toast, .live-dot i {
                animation: none !important;
            }
        }

        /* ===== Buttons ===== */
        .btn {
            display: inline-flex; align-items: center; justify-content: center; gap: 6px;
            padding: 9px 16px; border: none; border-radius: var(--radius);
            cursor: pointer; font-size: 0.84rem; font-weight: 600;
            transition: background 0.15s, color 0.15s, border-color 0.15s, transform 0.15s, box-shadow 0.15s;
            font-family: inherit; white-space: nowrap; line-height: 1.2;
        }
        .btn-primary {
            background: var(--accent); color: #fff;
            box-shadow: 0 1px 3px oklch(0.55 0.18 275 / 0.3);
        }
        .btn-primary:hover {
            background: var(--accent-hover);
            transform: translateY(-1px);
            box-shadow: 0 4px 12px oklch(0.55 0.18 275 / 0.3);
        }
        .btn-primary:active { transform: translateY(0); }
        .btn-sm { padding: 6px 11px; font-size: 0.78rem; border-radius: var(--radius-sm); font-weight: 600; }
        .btn-ghost {
            background: var(--surface); color: var(--ink-soft);
            border: 1px solid var(--line); box-shadow: var(--shadow-sm);
        }
        .btn-ghost:hover {
            background: var(--surface-2); color: var(--ink);
            border-color: var(--line-strong);
        }
        .btn-danger {
            background: transparent; color: var(--red); border: 1px solid transparent;
        }
        .btn-danger:hover { background: var(--red-soft); border-color: oklch(0.54 0.18 25 / 0.2); }
        .btn-success {
            background: transparent; color: var(--green); border: 1px solid transparent;
        }
        .btn-success:hover { background: var(--green-soft); }
        .btn:focus-visible, .logout-btn:focus-visible, .action-more-btn:focus-visible, .tab-nav button:focus-visible {
            outline: none; box-shadow: var(--focus);
        }
        .btn:disabled { opacity: 0.55; cursor: not-allowed; transform: none !important; box-shadow: none !important; }

        /* ===== Cards ===== */
        .card {
            background: var(--surface);
            border: 1px solid var(--line);
            border-radius: 10px;
            padding: 20px 22px;
            margin-bottom: 16px;
            box-shadow: var(--shadow-sm);
            position: relative;
            overflow: visible;
        }
        .card::before {
            content: ""; position: absolute; left: 0; top: 14px; bottom: 14px;
            width: 3px; border-radius: 0 2px 2px 0;
            background: linear-gradient(180deg, var(--accent), oklch(0.62 0.14 265));
            opacity: 0.85;
        }
        .card-header {
            display: flex; align-items: center; justify-content: space-between;
            gap: 12px; margin-bottom: 10px; flex-wrap: wrap;
        }
        .card-header h2 {
            font-family: var(--font); font-size: 1.12rem; font-weight: 700;
            letter-spacing: -0.02em; color: var(--ink);
        }
        .card-desc {
            color: var(--text-dim); font-size: 0.84rem; margin: -2px 0 14px;
            max-width: 62ch; line-height: 1.55;
        }
        .toolbar {
            display: flex; gap: 8px; align-items: center; flex-wrap: wrap;
            margin-bottom: 14px; padding: 10px;
            background: var(--paper-2); border: 1px solid var(--line);
            border-radius: var(--radius); 
        }
        .toolbar .search-input { flex: 1; min-width: 180px; max-width: 360px; background: var(--surface); }
        .count-chip {
            font-size: 0.74rem; color: var(--text-dim);
            background: var(--surface); border: 1px solid var(--line);
            border-radius: 999px; padding: 4px 10px; white-space: nowrap;
            font-family: var(--font-mono); font-variant-numeric: tabular-nums;
        }

        /* ===== Tables ===== */
        .table-container { overflow: visible; border-radius: var(--radius); }
        table { width: 100%; border-collapse: collapse; table-layout: auto; }
        thead th {
            text-align: left; padding: 10px 12px;
            font-size: 0.68rem; font-weight: 700; text-transform: uppercase;
            letter-spacing: 0.08em; color: var(--text-faint);
            border-bottom: 1.5px solid var(--line);
            white-space: nowrap; background: transparent;
        }
        tbody td {
            padding: 12px; font-size: 0.86rem;
            border-bottom: 1px solid var(--line); vertical-align: middle;
        }
        tbody tr { transition: background 0.12s; }
        tbody tr:nth-child(even) { background: oklch(0.22 0.02 265 / 0.018); }
        tbody tr:hover { background: oklch(0.55 0.18 275 / 0.05); }
        tbody tr:last-child td { border-bottom: none; }

        /* ===== Badges ===== */
        .badge {
            display: inline-flex; align-items: center; gap: 4px;
            padding: 3px 9px; border-radius: 999px;
            font-size: 0.7rem; font-weight: 700; white-space: nowrap;
            letter-spacing: 0.02em; border: 1px solid transparent;
        }
        .badge-green { background: var(--green-soft); color: oklch(0.38 0.1 155); border-color: oklch(0.52 0.13 155 / 0.2); }
        .badge-red { background: var(--red-soft); color: var(--red); border-color: oklch(0.54 0.18 25 / 0.2); }
        .badge-purple { background: var(--accent-soft); color: var(--accent); border-color: oklch(0.55 0.18 275 / 0.22); }
        .badge-orange { background: var(--orange-soft); color: oklch(0.48 0.12 70); border-color: oklch(0.72 0.15 70 / 0.25); }
        .badge-muted { background: var(--paper-2); color: var(--text-dim); border-color: var(--line); }

        /* ===== Forms ===== */
        .form-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; align-items: end; }
        .form-grid.narrow { grid-template-columns: 1fr auto; }
        .form-group { display: flex; flex-direction: column; gap: 6px; }
        .form-group label {
            font-size: 0.74rem; font-weight: 700; color: var(--ink-soft);
            letter-spacing: 0.03em; text-transform: uppercase;
        }
        input, select, textarea {
            width: 100%; padding: 9px 12px;
            background: var(--paper); border: 1.5px solid var(--line);
            border-radius: var(--radius); color: var(--text);
            font-size: 0.875rem; font-family: inherit;
            transition: border-color 0.15s, box-shadow 0.15s, background 0.15s;
        }
        input:focus, select:focus, textarea:focus {
            outline: none; border-color: var(--accent); box-shadow: var(--focus);
            background: var(--surface);
        }
        textarea { resize: vertical; line-height: 1.6; font-family: var(--font-mono); font-size: 0.82rem; }
        input::placeholder, textarea::placeholder { color: var(--text-faint); }
        code {
            font-family: var(--font-mono);
            background: var(--paper-2); padding: 2px 7px; border-radius: var(--radius-xs);
            font-size: 0.84em; word-break: break-all;
            border: 1px solid var(--line); color: var(--ink-soft);
        }

        /* Key display */
        .key-display {
            font-family: var(--font-mono); background: var(--paper);
            padding: 14px 16px; border-radius: var(--radius);
            word-break: break-all; border: 1.5px dashed var(--accent);
            margin-top: 8px; font-size: 0.88rem; color: var(--ink);
        }
        .key-alert {
            background: linear-gradient(135deg, var(--accent-soft), oklch(0.55 0.18 275 / 0.06));
            border: 1px solid oklch(0.55 0.18 275 / 0.22);
            border-radius: var(--radius); padding: 16px; margin-bottom: 16px;
        }
        .key-alert strong { color: var(--accent); font-family: var(--font); }

        /* Dialogs */
        dialog {
            background: var(--surface); border: 1px solid var(--line);
            border-radius: 12px; color: var(--text); padding: 0;
            width: min(520px, calc(100vw - 24px));
            max-height: min(90dvh, 900px);
            position: fixed; top: 50%; left: 50%;
            transform: translate(-50%, -50%); margin: 0;
            box-shadow: var(--shadow-lg);
            display: none; flex-direction: column; overflow: hidden;
        }
        dialog[open] { display: flex; animation: dialogIn 0.24s var(--ease); }
        dialog.dlg-wide { width: min(640px, calc(100vw - 24px)); }
        dialog.dlg-sm { width: min(440px, calc(100vw - 24px)); }
        dialog::backdrop {
            background: oklch(0.22 0.04 275 / 0.45);
            backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
            animation: backdropIn 0.2s ease;
        }
        @keyframes backdropIn { from { opacity: 0; } to { opacity: 1; } }
        @keyframes dialogIn {
            from { opacity: 0; transform: translate(-50%, -46%) scale(0.97); }
            to { opacity: 1; transform: translate(-50%, -50%) scale(1); }
        }
        @keyframes spin { to { transform: rotate(360deg); } }
        dialog > form { display: flex; flex-direction: column; min-height: 0; flex: 1; padding: 0; margin: 0; }
        .dlg-header {
            flex: 0 0 auto; padding: 22px 24px 0;
            border-bottom: none;
        }
        .dlg-header h3, dialog h3 {
            font-family: var(--font); font-size: 1.18rem; font-weight: 700;
            letter-spacing: -0.025em; margin: 0; color: var(--ink);
        }
        .dlg-desc { color: var(--text-dim); font-size: 0.84rem; margin: 6px 0 0; line-height: 1.5; }
        .dlg-body {
            flex: 1 1 auto; min-height: 0; padding: 16px 24px;
            display: flex; flex-direction: column; gap: 12px;
            overflow-y: auto; overscroll-behavior: contain;
            scrollbar-width: thin;
        }
        dialog .form-group { margin-bottom: 0; }
        dialog .form-group label { font-size: 0.74rem; color: var(--text-dim); }
        .dialog-actions {
            flex: 0 0 auto; display: flex; gap: 10px; justify-content: flex-end; align-items: center;
            margin: 0; padding: 14px 24px;
            border-top: 1px solid var(--line);
            background: var(--paper-2);
        }
        .dialog-actions .spacer { flex: 1; }

        /* Binding */
        .binding-list { display: flex; flex-direction: column; gap: 6px; }
        .binding-item {
            display: flex; align-items: center; gap: 10px;
            padding: 12px 14px; background: var(--paper);
            border-radius: var(--radius); cursor: pointer;
            transition: all 0.15s; border: 1px solid var(--line);
        }
        .binding-item:hover { background: var(--surface-2); border-color: var(--line-strong); }
        .binding-item:has(input:checked) {
            border-color: oklch(0.55 0.18 275 / 0.4);
            background: var(--accent-soft);
        }
        .binding-item input[type="checkbox"] { accent-color: var(--accent); width: 16px; height: 16px; }
        .binding-label { flex: 1; font-size: 0.9rem; font-weight: 600; }
        .binding-url { color: var(--text-dim); font-size: 0.8rem; font-family: var(--font-mono); }

        /* Empty / loading */
        .empty-state {
            text-align: center; padding: 48px 20px;
            color: var(--text-dim); font-size: 0.9rem;
        }
        .empty-state strong {
            display: block; color: var(--ink);
            font-family: var(--font); font-size: 1.05rem; margin-bottom: 8px;
        }
        .empty-state p { margin: 0 auto 16px; max-width: 340px; line-height: 1.55; }
        .loading-cell { text-align: center; padding: 32px; color: var(--text-dim); font-size: 0.88rem; }
        .loading-dot {
            display: inline-block; width: 7px; height: 7px; border-radius: 50%;
            background: var(--accent); margin: 0 3px;
            animation: pulse 1s ease infinite;
        }
        .loading-dot:nth-child(2) { animation-delay: 0.15s; }
        .loading-dot:nth-child(3) { animation-delay: 0.3s; }
        @keyframes pulse {
            0%, 100% { opacity: 0.25; transform: scale(0.8); }
            50% { opacity: 1; transform: scale(1); }
        }

        /* Toast */
        #toast-root {
            position: fixed; top: 16px; right: 16px; z-index: 9999;
            display: flex; flex-direction: column; gap: 8px;
            pointer-events: none; max-width: min(380px, calc(100vw - 24px));
        }
        .toast {
            pointer-events: auto; background: var(--surface);
            border: 1px solid var(--line); border-radius: var(--radius);
            box-shadow: var(--shadow-md); padding: 12px 14px;
            font-size: 0.88rem; color: var(--text);
            display: flex; gap: 10px; align-items: flex-start;
            animation: toastIn 0.22s var(--ease);
            border-left: 3px solid var(--accent);
        }
        .toast-ok { border-left-color: var(--green); }
        .toast-err { border-left-color: var(--red); }
        .toast-info { border-left-color: var(--teal); }
        .toast-icon {
            flex: 0 0 auto; width: 20px; height: 20px; border-radius: 50%;
            display: inline-flex; align-items: center; justify-content: center;
            font-size: 0.68rem; font-weight: 800; color: #fff; margin-top: 1px;
        }
        .toast-ok .toast-icon { background: var(--green); }
        .toast-err .toast-icon { background: var(--red); }
        .toast-info .toast-icon { background: var(--teal); }
        .toast-body { flex: 1; min-width: 0; line-height: 1.45; word-break: break-word; }
        .toast-close {
            flex: 0 0 auto; background: none; border: none;
            color: var(--text-dim); cursor: pointer; font-size: 1rem;
            line-height: 1; padding: 0 2px;
        }
        @keyframes toastIn {
            from { opacity: 0; transform: translateY(-8px) scale(0.98); }
            to { opacity: 1; transform: translateY(0) scale(1); }
        }
        #dlg-confirm .confirm-msg { color: var(--text-dim); font-size: 0.9rem; margin: 0; line-height: 1.55; }

        /* Actions */
        .actions { display: flex; gap: 4px; align-items: center; justify-content: flex-end; flex-wrap: wrap; }
        .action-more { position: static; }
        .action-more-btn {
            background: var(--surface); border: 1px solid var(--line);
            color: var(--text-dim); padding: 5px 9px; border-radius: var(--radius-sm);
            cursor: pointer; font-size: 0.85rem; line-height: 1; transition: all 0.15s;
            font-weight: 700; letter-spacing: 0.08em;
        }
        .action-more-btn:hover { background: var(--paper-2); color: var(--ink); border-color: var(--line-strong); }
        .action-menu {
            display: none; position: fixed; z-index: 10000;
            background: var(--surface); border: 1px solid var(--line);
            border-radius: var(--radius); box-shadow: var(--shadow-md);
            min-width: 156px; padding: 5px;
        }
        .action-menu.show { display: block; animation: pageIn 0.12s ease; }
        .action-menu button {
            display: block; width: 100%; text-align: left;
            padding: 9px 12px; background: none; border: none;
            color: var(--text); font-size: 0.82rem; cursor: pointer;
            border-radius: var(--radius-xs); transition: background 0.1s;
            font-family: inherit; font-weight: 500;
        }
        .action-menu button:hover { background: var(--paper-2); }
        .action-menu button.menu-danger { color: var(--red); }
        .action-menu button.menu-danger:hover { background: var(--red-soft); }

        .truncate-url {
            display: inline-block; max-width: 180px; white-space: nowrap;
            overflow: hidden; text-overflow: ellipsis; vertical-align: middle;
            font-family: var(--font-mono); font-size: 0.8rem;
        }

        .model-tags { display: flex; flex-wrap: wrap; gap: 4px; }
        .model-tag {
            display: inline-flex; align-items: center; gap: 4px;
            padding: 3px 8px; background: var(--accent-soft); color: var(--accent);
            border-radius: var(--radius-xs); font-size: 0.72rem; font-weight: 600;
            font-family: var(--font-mono); border: 1px solid oklch(0.55 0.18 275 / 0.15);
        }
        .model-tag-all { color: var(--text-dim); font-size: 0.8rem; font-style: italic; }
        .api-key-editor { display: flex; flex-direction: column; gap: 8px; }
        .api-key-add { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; align-items: center; }
        .api-key-list {
            display: flex; flex-direction: column; gap: 6px; padding: 8px;
            background: var(--paper); border: 1px solid var(--line); border-radius: var(--radius);
        }
        .api-key-list.is-empty {
            color: var(--text-dim); font-size: 0.82rem;
            align-items: center; justify-content: center; min-height: 48px;
        }
        .api-key-row {
            display: flex; align-items: center; gap: 8px;
            padding: 8px 10px; background: var(--surface);
            border: 1px solid var(--line); border-radius: var(--radius-xs);
        }
        .api-key-row code {
            flex: 1; min-width: 0; background: transparent; padding: 0; border: none;
            font-size: 0.78rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
        }
        .api-key-row button { flex: 0 0 auto; }


        .badge, .actions { white-space: nowrap; }
        tbody td code.truncate-url { max-width: min(200px, 22vw); }

        .dlg-grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }

        /* Status metric polish via descendant selectors for inline-generated cards */
        #status-grid > div, #key-stats-grid > div, #status-upstreams > div {
            border-radius: 8px !important;
            border-color: var(--line) !important;
            background: var(--paper) !important;
            transition: transform 0.15s var(--ease), box-shadow 0.15s;
        }
        #status-grid > div:hover, #status-upstreams > div:hover {
            transform: translateY(-2px);
            box-shadow: var(--shadow-sm);
        }
        #status-grid > div > div:first-child {
            font-family: var(--font) !important;
            font-variant-numeric: tabular-nums;
            letter-spacing: -0.03em;
        }

        /* Mobile */
        @media (max-width: 900px) {
            #main-section.is-ready, #main-section[style*="block"] {
                grid-template-columns: 1fr !important;
            }
            .rail {
                position: sticky; top: 0; height: auto;
                flex-direction: row; flex-wrap: wrap; align-items: center;
                padding: 10px 12px; gap: 8px;
                border-right: none; border-bottom: 1px solid oklch(1 0 0 / 0.08);
            }
            .rail-brand { border-bottom: none; padding: 0 8px 0 0; margin: 0; }
            .rail-brand small { display: none; }
            .tab-nav {
                flex-direction: row; flex: 1; overflow-x: auto;
                gap: 2px; padding: 0; scrollbar-width: none;
            }
            .tab-nav::-webkit-scrollbar { display: none; }
            .tab-nav button { width: auto; padding: 8px 12px; font-size: 0.8rem; }
            .tab-nav button .nav-ico { display: none; }
            .tab-nav button.active { box-shadow: inset 0 -2px 0 var(--rail-active); }
            .rail-foot {
                flex-direction: row; align-items: center; margin: 0;
                padding: 0; border: none; gap: 6px;
            }
            .logout-btn { width: auto; padding: 6px 10px; }
            .topbar { padding: 14px 16px 8px; }
            .page { padding: 4px 16px 32px; }
            .topbar h2 { font-size: 1.2rem; }
        }
        @media (max-width: 768px) {
            .dlg-grid-2 { grid-template-columns: 1fr; }
            .card { padding: 16px; border-radius: 8px; }
            .card::before { top: 10px; bottom: 10px; }
            .hide-on-mobile { display: none !important; }
            .form-grid { grid-template-columns: 1fr; }
            dialog { width: calc(100vw - 16px); max-height: min(92dvh, 900px); }
            .dlg-header, .dlg-body, .dialog-actions { padding-left: 16px; padding-right: 16px; }
            .dialog-actions { flex-wrap: wrap; }
            .dialog-actions .btn { flex: 1 1 auto; min-width: 100px; }
            .table-container table { font-size: 0.82rem; }
            .table-container thead th { padding: var(--space-sm) var(--space-xs); }
            .table-container tbody td { padding: 10px var(--space-xs); }
            .truncate-url { max-width: 90px; }
            .actions { gap: var(--space-sm); }
            .api-key-add { grid-template-columns: 1fr; }
            .auth-card { padding: 28px 22px; }
        }
    </style>
</head>
<body>
<div class="app-shell">
    <!-- Auth Section -->
    <div id="auth-section">
        <div class="auth-card">
            <div class="auth-brand">
                <div class="auth-mark">LP</div>
                <div>
                    <h2>LLM Proxy</h2>
                    <div class="auth-sub">管理控制台</div>
                </div>
            </div>
            <p>使用 <code>ADMIN_TOKEN</code> 登录。令牌仅存于本机 Cookie，不会上传第三方。</p>
            <input type="password" id="admin-token" placeholder="粘贴管理令牌" autocomplete="current-password" onkeydown="if(event.key==='Enter')authenticate()">
            <div id="auth-error" class="auth-error"></div>
            <button class="btn btn-primary" id="btn-auth" style="width:100%" onclick="authenticate()">进入控制台</button>
        </div>
    </div>

    <!-- Main Section -->
    <div id="main-section" style="display:none;">
        <aside class="rail">
            <div class="rail-brand">
                <div class="rail-mark">LP</div>
                <div>
                    <h1>LLM Proxy</h1>
                    <small>Relay Console</small>
                </div>
            </div>
            <nav class="tab-nav">
                <button class="active" onclick="showTab('upstreams',this)">
                    <span class="nav-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v6"/><path d="m4.93 10.93 4.24 4.24"/><path d="M2 18h6"/><path d="m19.07 10.93-4.24 4.24"/><path d="M22 18h-6"/><circle cx="12" cy="18" r="3"/></svg></span>
                    上游
                </button>
                <button onclick="showTab('keys',this)">
                    <span class="nav-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m21 2-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0 3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></span>
                    密钥
                </button>
                <button onclick="showTab('models',this)">
                    <span class="nav-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span>
                    白名单
                </button>
                <button onclick="showTab('logs',this)">
                    <span class="nav-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M16 13H8"/><path d="M16 17H8"/><path d="M10 9H8"/></svg></span>
                    日志
                </button>
                <button onclick="showTab('status',this)">
                    <span class="nav-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg></span>
                    状态
                </button>
                <button onclick="showTab('tools',this)">
                    <span class="nav-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg></span>
                    工具
                </button>
            </nav>
            <div class="rail-foot">
                <span id="header-version" class="count-chip" style="display:none"></span>
                <button class="logout-btn" onclick="logout()">退出登录</button>
            </div>
        </aside>

        <div class="canvas">
            <div class="topbar">
                <h2 id="page-title">上游服务</h2>
                <div class="topbar-meta">
                    <span class="live-dot"><i></i> 控制台已连接</span>
                </div>
            </div>
            <div class="page">

        <!-- Upstreams Tab -->
        <div id="tab-upstreams" class="tab-content active">
            <div class="card">
                <div class="card-header">
                    <h2>上游服务</h2>
                    <button class="btn btn-primary btn-sm" onclick="document.getElementById('dlg-upstream').showModal()">添加上游</button>
                </div>
                <p class="card-desc">配置转发目标。OAuth 模式会用 Authorization: Bearer 发送上游 Key。</p>
                <div class="toolbar">
                    <input class="search-input" id="upstream-search" placeholder="搜索名称 / 地址 / 备注" oninput="renderUpstreamsTable()">
                    <select id="upstream-filter-enabled" style="width:120px" onchange="renderUpstreamsTable()">
                        <option value="">全部状态</option>
                        <option value="1">仅启用</option>
                        <option value="0">仅禁用</option>
                    </select>
                    <span id="upstream-count" class="count-chip">0</span>
                </div>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th>名称</th><th>地址</th><th class="hide-on-mobile">密钥</th><th class="hide-on-mobile">鉴权</th><th class="hide-on-mobile">调度</th><th class="hide-on-mobile">代理</th><th class="hide-on-mobile">优先级</th><th class="hide-on-mobile">模型模式</th><th>状态</th><th>操作</th></tr></thead>
                <tbody id="upstreams-table"><tr><td colspan="11" class="loading-cell"><span class="loading-dot"></span><span class="loading-dot"></span><span class="loading-dot"></span></td></tr></tbody></table>
                </div>
            </div>
        </div>

        <!-- Keys Tab -->
        <div id="tab-keys" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <h2>下游密钥</h2>
                    <button class="btn btn-primary btn-sm" onclick="document.getElementById('dlg-key').showModal()">创建密钥</button>
                </div>
                <div class="toolbar">
                    <input class="search-input" id="key-search" placeholder="搜索名称 / 前缀 / ID" oninput="renderKeysTable()">
                    <select id="key-filter-enabled" style="width:120px" onchange="renderKeysTable()">
                        <option value="">全部状态</option>
                        <option value="1">仅启用</option>
                        <option value="0">仅禁用</option>
                    </select>
                    <span id="key-count" class="count-chip">0</span>
                </div>
                <div id="new-key-display" style="display:none;">
                    <div class="key-alert">
                        <strong>密钥已创建（请立即复制，仅显示一次）</strong>
                        <div style="display:flex;align-items:center;gap:8px;margin-top:8px;">
                            <div class="key-display" id="new-key-value" style="flex:1;margin-top:0;"></div>
                            <button class="btn btn-primary btn-sm" onclick="copyNewKey(this)" style="white-space:nowrap;">复制</button>
                        </div>
                    </div>
                </div>
                <div class="table-container">
                <table><thead><tr><th class="hide-on-mobile">ID</th><th class="hide-on-mobile">前缀</th><th>名称</th><th>RPM</th><th>当前 RPM</th><th>状态</th><th class="hide-on-mobile">绑定上游</th><th class="hide-on-mobile">模型路由</th><th>操作</th></tr></thead>
                <tbody id="keys-table"><tr><td colspan="9" class="loading-cell"><span class="loading-dot"></span><span class="loading-dot"></span><span class="loading-dot"></span></td></tr></tbody></table>
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
                <p class="card-desc">为空则不过滤。支持 <code>*</code> 通配符（如 <code>claude-sonnet*</code>）；不含通配符时精确匹配。</p>
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
                <h3 style="font-family:var(--font);font-size:1rem;margin-bottom:14px;letter-spacing:-0.02em;">上游健康</h3>
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
                    <h2>客户端 Header 抓取</h2>
                    <div style="display:flex;gap:8px;align-items:center;">
                        <span id="hc-status" class="count-chip">已关闭</span>
                        <button class="btn btn-primary btn-sm" id="hc-toggle" onclick="toggleHeaderCapture()">开启抓取</button>
                        <button class="btn btn-ghost btn-sm" onclick="loadHeaderCapture()">刷新</button>
                        <button class="btn btn-danger btn-sm" onclick="clearHeaderCapture()">清空</button>
                    </div>
                </div>
                <p class="card-desc">
                    <strong>完整抓取</strong>入站 Header + Body（含 Authorization 等明文密钥）。仅在可信本机使用。
                    将 CC 的 <code>ANTHROPIC_BASE_URL</code> 设为
                    <code id="hc-base-url">http://127.0.0.1:端口</code>
                    ，用下游 Key 发一条消息后点刷新。Body 默认最多保留 2MB。
                </p>
                <div id="hc-list" class="empty-state" style="padding:20px;">尚未抓取到请求。先开启抓取，再从 Claude Code 发一条消息。</div>
            </div>
            <div class="card">
                <div class="card-header">
                    <h2>额度 JSON 解析</h2>
                </div>
                <p class="card-desc">粘贴 new-api 查额返回的 JSON，解析并展示额度。</p>
                <textarea id="tools-json-input" rows="6" style="width:100%;font-family:var(--font-mono);font-size:0.8rem;background:var(--paper);color:var(--text);border:1px solid var(--line);border-radius:var(--radius);padding:12px;resize:vertical;" placeholder='粘贴 JSON 如: {"code":true,"data":{...}}'></textarea>
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
                <p class="card-desc">管理测试对话框中的模型列表，测试上游时可快速选择。</p>
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

            </div><!-- /.page -->
        </div><!-- /.canvas -->
    </div><!-- /#main-section -->
</div><!-- /.app-shell -->

<dialog id="dlg-upstream">
    <form onsubmit="createUpstream(event)">
        <div class="dlg-header"><h3>添加上游</h3><p class="dlg-desc">配置转发目标与鉴权方式</p></div>
        <div class="dlg-body">
            <div class="form-group"><label>名称</label><input name="name" placeholder="如 openai-sgp" required></div>
            <div class="form-group"><label>地址</label><input name="base_url" placeholder="https://api.example.com" required></div>
            <div class="form-group">
                <label>API 密钥 <span style="font-weight:400;color:var(--text-dim)">（可多个，留空=无鉴权）</span></label>
                <div class="api-key-editor" data-key-editor="create">
                    <div class="api-key-add">
                        <input data-key-input placeholder="粘贴 API Key，回车添加" autocomplete="off" onkeydown="handleAPIKeyInputKeydown(event,'create')">
                        <button type="button" class="btn btn-ghost btn-sm" onclick="addAPIKeyFromInput('create')">添加</button>
                    </div>
                    <div class="api-key-list is-empty" data-key-list>暂无 API Key</div>
                </div>
            </div>
            <div class="form-group"><label>Key 调度模式</label><select name="key_scheduling_mode"><option value="round-robin">轮询 (Round-Robin)</option><option value="fill">填充 (Fill)</option></select></div>
            <div class="form-group"><label>Anthropic 鉴权模式</label><select name="auth_mode"><option value="api_key">API Key (x-api-key)</option><option value="oauth">OAuth (Authorization: Bearer)</option></select></div>
            <div class="form-group"><label>备注</label><input name="remark" placeholder="如：Key 来源"></div>
            <div class="form-group"><label>代理地址</label><input name="proxy_url" placeholder="socks5://127.0.0.1:1080"></div>
            <div class="form-group"><label>优先级 <span style="font-weight:400;color:var(--text-dim)">（0 = 最高）</span></label><input name="priority" type="number" value="0" min="0"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">创建</button>
        </div>
    </form>
</dialog>

<!-- Edit Upstream Dialog -->
<dialog id="dlg-edit-upstream">
    <form onsubmit="submitEditUpstream(event)">
        <div class="dlg-header"><h3>编辑上游</h3></div>
        <div class="dlg-body">
            <input type="hidden" name="id">
            <div class="form-group"><label>名称</label><input name="name" required></div>
            <div class="form-group"><label>地址</label><input name="base_url" placeholder="https://api.example.com" required></div>
            <div class="form-group">
                <label>API 密钥</label>
                <div class="api-key-editor" data-key-editor="edit">
                    <div class="api-key-add">
                        <input data-key-input placeholder="粘贴 API Key，回车添加" autocomplete="off" onkeydown="handleAPIKeyInputKeydown(event,'edit')">
                        <button type="button" class="btn btn-ghost btn-sm" onclick="addAPIKeyFromInput('edit')">添加</button>
                    </div>
                    <div class="api-key-list is-empty" data-key-list>暂无 API Key</div>
                </div>
            </div>
            <div class="form-group"><label>Key 调度模式</label><select name="key_scheduling_mode"><option value="round-robin">轮询 (Round-Robin)</option><option value="fill">填充 (Fill)</option></select></div>
            <div class="form-group"><label>Anthropic 鉴权模式</label><select name="auth_mode"><option value="api_key">API Key (x-api-key)</option><option value="oauth">OAuth (Authorization: Bearer)</option></select></div>
            <div class="form-group"><label>备注</label><input name="remark" placeholder="如：Key 来源"></div>
            <div class="form-group"><label>代理地址 <span style="font-weight:400;color:var(--text-dim)">（留空=环境代理）</span></label><input name="proxy_url" placeholder="socks5://127.0.0.1:1080"></div>
            <div class="form-group"><label>优先级</label><input name="priority" type="number" min="0"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
        </div>
    </form>
</dialog>

<!-- Create Key Dialog -->
<dialog id="dlg-key" class="dlg-sm">
    <form onsubmit="createKey(event)">
        <div class="dlg-header"><h3>创建密钥</h3><p class="dlg-desc">明文仅显示一次，请立即复制</p></div>
        <div class="dlg-body">
            <div class="form-group"><label>密钥名称</label><input name="name" placeholder="如 user-1" required></div>
            <div class="form-group"><label>RPM 限制 <span style="font-weight:400;color:var(--text-dim)">（0 = 不限）</span></label><input name="rpm_limit" type="number" value="0" min="0"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">创建</button>
        </div>
    </form>
</dialog>

<!-- Edit Key Dialog -->
<dialog id="dlg-edit-key" class="dlg-sm">
    <form onsubmit="submitEditKey(event)">
        <div class="dlg-header"><h3>编辑密钥</h3></div>
        <div class="dlg-body">
            <input type="hidden" name="id">
            <div class="form-group"><label>名称</label><input name="name" required></div>
            <div class="form-group"><label>RPM 限制 <span style="font-weight:400;color:var(--text-dim)">（0 = 不限）</span></label><input name="rpm_limit" type="number" min="0"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
        </div>
    </form>
</dialog>

<!-- Upstream Binding Dialog -->
<dialog id="dlg-binding">
    <form>
        <div class="dlg-header"><h3>配置上游绑定</h3><p class="dlg-desc">不选任何上游表示允许全部</p></div>
        <div class="dlg-body">
            <input type="hidden" id="binding-key-id">
            <div id="binding-list" class="binding-list"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveBindings()">保存</button>
        </div>
    </form>
</dialog>

<!-- Model Patterns Dialog -->
<dialog id="dlg-model-patterns">
    <form>
        <div class="dlg-header"><h3>配置模型模式</h3><p class="dlg-desc">支持 <code>*</code> 通配。空则接受所有模型</p></div>
        <div class="dlg-body">
            <input type="hidden" id="mp-upstream-id">
            <div style="display:flex;gap:8px;">
                <input id="mp-new-pattern" placeholder="如: claude-*" style="flex:1" onkeydown="if(event.key==='Enter'){event.preventDefault();addModelPatternTag()}">
                <button type="button" class="btn btn-primary btn-sm" onclick="addModelPatternTag()">添加</button>
            </div>
            <div id="mp-tags" class="model-tags" style="min-height:32px;"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveModelPatterns()">保存</button>
        </div>
    </form>
</dialog>

<!-- Declared Models Dialog -->
<dialog id="dlg-declared-models">
    <form>
        <div class="dlg-header"><h3>配置声明模型</h3><p class="dlg-desc">用于 <code>/v1/models</code> 聚合，适合无 models 接口的上游</p></div>
        <div class="dlg-body">
            <input type="hidden" id="dm-upstream-id">
            <div style="display:flex;gap:8px;">
                <input id="dm-new-model" placeholder="如: mimo-v2.5-pro" style="flex:1" onkeydown="if(event.key==='Enter'){event.preventDefault();addDeclaredModelTag()}">
                <button type="button" class="btn btn-primary btn-sm" onclick="addDeclaredModelTag()">添加</button>
            </div>
            <div id="dm-tags" class="model-tags" style="min-height:32px;"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveDeclaredModels()">保存</button>
        </div>
    </form>
</dialog>

<!-- Key Model Override Dialog -->
<dialog id="dlg-model-override" class="dlg-wide">
    <form>
        <div class="dlg-header"><h3>配置模型路由覆盖</h3><p class="dlg-desc">精确匹配优先于通配；覆盖上游不可用时请求会被拒绝</p></div>
        <div class="dlg-body">
            <input type="hidden" id="mo-key-id">
            <div style="display:flex;gap:8px;align-items:end;flex-wrap:wrap;">
                <div class="form-group" style="flex:1;min-width:140px">
                    <label>模型模式</label>
                    <input id="mo-new-pattern" placeholder="如: claude-opus-4-6">
                </div>
                <div class="form-group" style="flex:1;min-width:140px">
                    <label>目标上游</label>
                    <select id="mo-new-upstream"></select>
                </div>
                <button type="button" class="btn btn-primary btn-sm" onclick="addOverrideRule()">添加</button>
            </div>
            <div id="mo-rules" style="min-height:32px;"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveOverrides()">保存</button>
        </div>
    </form>
</dialog>

<!-- Per-Key API Key Management Dialog -->
<dialog id="dlg-manage-keys">
    <div class="dlg-header"><h3>管理 API Keys</h3><p class="dlg-desc">启用或禁用单个 Key</p></div>
    <div class="dlg-body">
        <input type="hidden" id="mk-upstream-id">
        <div id="mk-keys-list"></div>
    </div>
    <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" onclick="document.getElementById('dlg-manage-keys').close()">关闭</button>
    </div>
</dialog>

<!-- Upstream Test Dialog -->
<dialog id="dlg-test-upstream">
    <div class="dlg-header"><h3>测试上游连接</h3><p class="dlg-desc">选择 Key 与协议，验证连通性</p></div>
    <div class="dlg-body">
        <input type="hidden" id="tu-upstream-id">
        <div class="form-group">
            <label>使用 Key</label>
            <select id="tu-key-select"></select>
        </div>
        <div class="form-group">
            <label>协议</label>
            <select id="tu-protocol" onchange="onTuProtocolChange()">
                <option value="openai">OpenAI (Chat Completions)</option>
                <option value="anthropic">Anthropic (Messages)</option>
                <option value="responses">OpenAI (Responses / Codex)</option>
            </select>
        </div>
        <div class="dlg-grid-2">
            <div class="form-group">
                <label>模型</label>
                <input id="tu-model" value="" placeholder="输入或选择模型" list="tu-model-list">
                <datalist id="tu-model-list"></datalist>
            </div>
            <div class="form-group">
                <label>提示词</label>
                <input id="tu-prompt" value="你是什么模型？">
            </div>
        </div>
        <div id="tu-result" style="display:none;"></div>
    </div>
    <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" onclick="document.getElementById('dlg-test-upstream').close()">关闭</button>
        <button type="button" class="btn btn-primary" id="btn-tu-test" onclick="submitUpstreamTest()">测试</button>
    </div>
</dialog>

<!-- CF Bypass Config Dialog -->
<dialog id="dlg-cf" class="dlg-sm">
    <form>
        <div class="dlg-header"><h3>CF 防御绕过</h3><p class="dlg-desc">从浏览器获取 <code>cf_clearance</code> 与 User-Agent，保存在本机 localStorage</p></div>
        <div class="dlg-body">
            <input type="hidden" id="cf-upstream-id">
            <div class="form-group"><label>cf_clearance</label><input id="cf-clearance" placeholder="cf_clearance cookie 值"></div>
            <div class="form-group"><label>User-Agent</label><input id="cf-ua" placeholder="与获取 cookie 时相同的浏览器 UA"></div>
        </div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-danger btn-sm" onclick="clearCFConfig()">清除</button>
            <span class="spacer"></span>
            <button type="button" class="btn btn-ghost" onclick="this.closest('dialog').close()">取消</button>
            <button type="button" class="btn btn-primary" onclick="saveCFConfig()">保存</button>
        </div>
    </form>
</dialog>

<div id="toast-root" aria-live="polite" aria-atomic="true"></div>
<dialog id="dlg-confirm" class="dlg-sm">
    <form method="dialog" id="confirm-form">
        <div class="dlg-header"><h3 id="confirm-title">确认</h3></div>
        <div class="dlg-body"><p class="confirm-msg" id="confirm-msg"></p></div>
        <div class="dialog-actions">
            <button type="button" class="btn btn-ghost" id="confirm-cancel">取消</button>
            <button type="submit" class="btn btn-primary" id="confirm-ok">确认</button>
        </div>
    </form>
</dialog>

	<script>
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

function esc(s) { const d = document.createElement('div'); d.textContent = s == null ? '' : String(s); return d.innerHTML; }
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
    if (name === 'tools') { loadTestModels(); loadSettings(); loadHeaderCapture(); }
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
        renderUpstreamsTable();
    }).catch(() => {
        document.getElementById('upstreams-table').innerHTML = '<tr><td colspan="11" class="empty-state"><strong>加载失败</strong><p>无法获取上游列表，请刷新重试</p></td></tr>';
        toastErr('加载上游失败');
    });
}
function renderUpstreamsTable() {
    const tbody = document.getElementById('upstreams-table');
    const q = ((document.getElementById('upstream-search')||{}).value || '').trim().toLowerCase();
    const en = (document.getElementById('upstream-filter-enabled')||{}).value || '';
    let list = allUpstreams.slice();
    if (en === '1') list = list.filter(u => u.enabled);
    if (en === '0') list = list.filter(u => !u.enabled);
    if (q) {
        list = list.filter(u => {
            const hay = [u.name, u.base_url, u.remark, u.proxy_url, String(u.id)].join(' ').toLowerCase();
            return hay.indexOf(q) !== -1;
        });
    }
    const countEl = document.getElementById('upstream-count');
    if (countEl) countEl.textContent = list.length + (list.length !== allUpstreams.length ? ' / ' + allUpstreams.length : '');
    if (allUpstreams.length === 0) {
        tbody.innerHTML = '<tr><td colspan="11" class="empty-state"><strong>还没有上游</strong><p>添加第一个上游后即可开始转发请求</p><button class="btn btn-primary btn-sm" onclick="document.getElementById(\'dlg-upstream\').showModal()">添加上游</button></td></tr>';
        return;
    }
    if (list.length === 0) {
        tbody.innerHTML = '<tr><td colspan="11" class="empty-state"><strong>无匹配结果</strong><p>试试调整搜索词或状态筛选</p></td></tr>';
        return;
    }
    tbody.innerHTML = list.map(u => {
        const patterns = allModelPatterns[u.id] || [];
        let modelHtml = '<span class="model-tag-all">*</span>';
        if (patterns.length > 0) {
            modelHtml = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
        }
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
            keyBadge = '<span class="badge badge-orange" style="cursor:pointer" onclick="openManageKeysDialog('+u.id+')" title="点击管理">'+enabledKeys+'/'+totalKeys+' Key</span>';
        }
        const schedMode = u.key_scheduling_mode || 'round-robin';
        const schedLabel = schedMode === 'fill' ? '填充' : '轮询';
        const schedColor = schedMode === 'fill' ? 'var(--orange)' : 'var(--accent)';
        const authMode = u.auth_mode || 'api_key';
        const authBadge = authMode === 'oauth'
            ? '<span class="badge badge-orange" title="Authorization: Bearer">OAuth</span>'
            : '<span class="badge badge-muted" title="x-api-key">API Key</span>';
        const remarkHtml = u.remark ? '<div style="font-size:0.75rem;color:var(--text-dim);margin-top:2px;" title="'+esc(u.remark)+'">'+esc(u.remark.length>28?u.remark.substring(0,28)+'...':u.remark)+'</div>' : '';
        return '<tr><td class="hide-on-mobile">'+u.id+'</td><td><strong>'+esc(u.name)+'</strong>'+remarkHtml+'</td><td><code class="truncate-url" title="'+esc(u.base_url)+'">'+esc(u.base_url)+'</code></td><td class="hide-on-mobile">'+keyBadge+'</td><td class="hide-on-mobile">'+authBadge+'</td><td class="hide-on-mobile"><span style="font-size:0.75rem;color:'+schedColor+';font-weight:500;white-space:nowrap;">'+schedLabel+'</span></td><td class="hide-on-mobile">'+(u.proxy_url?'<code class="truncate-url" title="'+esc(u.proxy_url)+'">'+esc(u.proxy_url)+'</code>':'<span class="badge badge-muted">环境</span>')+'</td><td class="hide-on-mobile">'+u.priority+'</td><td class="hide-on-mobile"><div class="model-tags">'+modelHtml+'</div></td><td>'+
        (u.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
        '</td><td class="actions">'+
        '<button class="btn btn-ghost btn-sm" onclick="testProxy(event,'+u.id+')">测试</button>'+
        '<button class="btn btn-ghost btn-sm" onclick="editUpstream('+u.id+')">编辑</button>'+
        '<div class="action-more"><button class="action-more-btn" onclick="toggleActionMenu(event)">···</button>'+
        '<div class="action-menu">'+
        '<button onclick="checkQuota(event,'+u.id+')">查额</button>'+
        '<button onclick="openCFDialog('+u.id+')" style="'+(getCFConfig(u.id)?'color:var(--green)':'')+'">CF 绕过</button>'+
        '<button onclick="openModelPatternsDialog('+u.id+')">模型模式</button>'+
        '<button onclick="openDeclaredModelsDialog('+u.id+')">声明模型</button>'+
        '<button onclick="toggleUpstream('+u.id+','+(!u.enabled)+')">'+(u.enabled?'禁用':'启用')+'</button>'+
        '<button class="menu-danger" onclick="deleteUpstream('+u.id+')">删除</button>'+
        '</div></div>'+
        '</td></tr>';
    }).join('');
}

function createUpstream(e) {
    e.preventDefault();
    addAPIKeyFromInput('create');
    const f = new FormData(e.target);
    const apiKeys = getAPIKeyEditorKeys('create');
    api('/upstreams', {method:'POST', body: JSON.stringify({
        name: f.get('name'), base_url: f.get('base_url'),
        api_keys: apiKeys, proxy_url: f.get('proxy_url')||'',
        priority: parseInt(f.get('priority')||'0'),
        key_scheduling_mode: f.get('key_scheduling_mode')||'round-robin',
        auth_mode: f.get('auth_mode')||'api_key',
        remark: f.get('remark')||''
    })}).then(d => {
        if(d.error) toastErr(d.error);
        else { e.target.reset(); setAPIKeyEditor('create', []); document.getElementById('dlg-upstream').close(); loadUpstreams(); toastOk('上游已创建'); }
    }).catch(() => toastErr('创建上游失败'));
}

function editUpstream(id) {
    const u = allUpstreams.find(x => x.id === id);
    if (!u) return;
	    const dlg = document.getElementById('dlg-edit-upstream');
    dlg.querySelector('[name=id]').value = id;
    dlg.querySelector('[name=name]').value = u.name;
    dlg.querySelector('[name=base_url]').value = u.base_url;
	    setAPIKeyEditor('edit', u.api_key_details ? u.api_key_details : (u.api_keys || []).map(k => ({key:k})));
    dlg.querySelector('[name=proxy_url]').value = u.proxy_url||'';
    dlg.querySelector('[name=priority]').value = u.priority;
    dlg.querySelector('[name=key_scheduling_mode]').value = u.key_scheduling_mode || 'round-robin';
    dlg.querySelector('[name=auth_mode]').value = u.auth_mode || 'api_key';
    dlg.querySelector('[name=remark]').value = u.remark || '';
    dlg.showModal();
}

	function submitEditUpstream(e) {
	    e.preventDefault();
	    addAPIKeyFromInput('edit');
	    const f = new FormData(e.target);
	    const id = parseInt(f.get('id'), 10);
	    const originalRows = upstreamKeyEditorMeta.edit.originalRows || [];
	    const currentRows = upstreamKeyEditorMeta.edit.rows || [];
	    const currentRowIds = new Set(currentRows.filter(r => r.row_id).map(r => r.row_id));
	    const deletedRows = originalRows.filter(r => r.row_id && !currentRowIds.has(r.row_id));
	    const addedKeys = currentRows.filter(r => !r.row_id).map(r => r.key);
	    const body = {name: f.get('name'), base_url: f.get('base_url'), proxy_url: f.get('proxy_url')||'', priority: parseInt(f.get('priority')||'0'), key_scheduling_mode: f.get('key_scheduling_mode')||'round-robin', auth_mode: f.get('auth_mode')||'api_key', remark: f.get('remark')||''};
	    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify(body)}).then(d => {
	        if(d.error) { toastErr(d.error); return; }
	        return Promise.all([
	            ...deletedRows.map(row => api('/upstreams/'+id+'/apikeys/'+row.row_id, {method:'DELETE'})),
	            ...(addedKeys.length > 0 ? [api('/upstreams/'+id+'/apikeys', {method:'POST', body: JSON.stringify({api_keys: addedKeys})})] : [])
	        ]);
	    }).then(results => {
	        if (!results) return;
	        const failed = results.find(r => r && r.error);
	        if (failed) { toastErr(failed.error); return; }
	        document.getElementById('dlg-edit-upstream').close();
	        toastOk('上游已保存');
	        loadUpstreams();
	    });
	}

async function deleteUpstream(id) {
    const u = allUpstreams.find(x => x.id === id);
    const name = u ? u.name : ('#'+id);
    if(!await askConfirm('删除上游「'+name+'」后，相关 Key 与绑定也会被清理。此操作不可恢复。', {title:'删除上游', okText:'删除', danger:true})) return;
    api('/upstreams/'+id, {method:'DELETE'}).then(d => {
        if(d && d.error) toastErr(d.error); else { loadUpstreams(); toastOk('已删除上游'); }
    }).catch(() => toastErr('删除失败'));
}

function toggleUpstream(id, enabled) {
    api('/upstreams/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if(d.error) toastErr(d.error); else { loadUpstreams(); toastOk('已更新'); }
    });
}

function normalizeAPIKeyInput(raw) {
    return (raw || '').split(/\r?\n|,/).map(s => s.trim()).filter(Boolean);
}
function shortAPIKey(key) {
    return key.length > 24 ? key.substring(0, 12) + '...' + key.substring(key.length - 8) : key;
}
	function setAPIKeyEditor(mode, keys) {
	    upstreamKeyEditors[mode] = [];
	    const rows = [];
	    (keys || []).forEach(item => {
	        const rowId = item && typeof item === 'object' ? item.row_id : null;
	        const rawKey = item && typeof item === 'object' ? item.key : item;
	        normalizeAPIKeyInput(rawKey).forEach(k => {
	            if (!upstreamKeyEditors[mode].includes(k)) {
	                upstreamKeyEditors[mode].push(k);
	                rows.push({key:k, row_id:rowId});
	            }
	        });
	    });
	    upstreamKeyEditorMeta[mode] = {rows: rows, originalRows: rows.map(r => ({...r}))};
	    renderAPIKeyEditor(mode);
	}
function getAPIKeyEditorKeys(mode) {
    return (upstreamKeyEditors[mode] || []).slice();
}
function renderAPIKeyEditor(mode) {
    const editor = document.querySelector('[data-key-editor="'+mode+'"]');
    if (!editor) return;
    const list = editor.querySelector('[data-key-list]');
	    const keys = upstreamKeyEditors[mode] || [];
	    const rows = (upstreamKeyEditorMeta[mode] && upstreamKeyEditorMeta[mode].rows) || [];
	    if (keys.length === 0) {
        list.className = 'api-key-list is-empty';
        list.innerHTML = '暂无 API Key';
        return;
    }
	    list.className = 'api-key-list';
	    list.innerHTML = keys.map((key, idx) => (
	        '<div class="api-key-row">'+
	        '<code title="'+esc(key)+'">'+esc(shortAPIKey(key))+'</code>'+
	        (rows[idx] && rows[idx].enabled === false ? '<span class="badge badge-red">禁用</span>' : '')+
	        '<button type="button" class="btn btn-ghost btn-sm" onclick="copyAPIKeyFromEditor(\''+mode+'\','+idx+',this)">复制</button>'+
	        '<button type="button" class="btn btn-danger btn-sm" onclick="removeAPIKeyFromEditor(\''+mode+'\','+idx+')">删除</button>'+
	        '</div>'
    )).join('');
}
function copyAPIKeyFromEditor(mode, index, btn) {
    const key = (upstreamKeyEditors[mode] || [])[index];
    if (!key) return;
    copyTextToClipboard(key, btn);
}
function addAPIKeyFromInput(mode) {
    const editor = document.querySelector('[data-key-editor="'+mode+'"]');
    if (!editor) return;
    const input = editor.querySelector('[data-key-input]');
    const incoming = normalizeAPIKeyInput(input.value);
    if (incoming.length === 0) return;
	    upstreamKeyEditors[mode] = upstreamKeyEditors[mode] || [];
	    upstreamKeyEditorMeta[mode] = upstreamKeyEditorMeta[mode] || {rows: [], originalRows: []};
	    upstreamKeyEditorMeta[mode].rows = upstreamKeyEditorMeta[mode].rows || [];
	    incoming.forEach(key => {
	        if (!upstreamKeyEditors[mode].includes(key)) {
	            upstreamKeyEditors[mode].push(key);
	            upstreamKeyEditorMeta[mode].rows.push({key:key, row_id:null});
	        }
	    });
    input.value = '';
    renderAPIKeyEditor(mode);
    input.focus();
}
	function removeAPIKeyFromEditor(mode, index) {
	    upstreamKeyEditors[mode].splice(index, 1);
	    if (upstreamKeyEditorMeta[mode] && upstreamKeyEditorMeta[mode].rows) {
	        upstreamKeyEditorMeta[mode].rows.splice(index, 1);
	    }
	    renderAPIKeyEditor(mode);
	}
function handleAPIKeyInputKeydown(event, mode) {
    if (event.key === 'Enter') {
        event.preventDefault();
        addAPIKeyFromInput(mode);
    }
}

// --- Per-Key API Key Management ---
let manageKeysFetchSeq = 0;
function openManageKeysDialog(upstreamId) {
    document.getElementById('mk-upstream-id').value = upstreamId;
    const list = document.getElementById('mk-keys-list');
    list.innerHTML = '<div style="text-align:center;padding:16px;color:var(--text-dim)">加载中...</div>';
    const dlg = document.getElementById('dlg-manage-keys');
    if (!dlg.open) dlg.showModal();
    const seq = ++manageKeysFetchSeq;
    api('/upstreams/'+upstreamId+'/apikeys').then(data => {
        // Ignore stale responses so a slow GET cannot overwrite a newer toggle refresh.
        if (seq !== manageKeysFetchSeq) return;
        manageAPIKeyRows = data || [];
        if (manageAPIKeyRows.length === 0) {
            list.innerHTML = '<div class="empty-state">无 API Key</div>';
            return;
        }
        list.innerHTML = manageAPIKeyRows.map((kd, idx) => {
            const shortKey = kd.key.length > 20 ? kd.key.substring(0, 10) + '...' + kd.key.substring(kd.key.length - 8) : kd.key;
            const isOn = !!kd.enabled;
            return '<div style="display:flex;align-items:center;gap:10px;padding:12px 14px;background:var(--bg);border-radius:var(--radius-sm);margin-bottom:8px;border:1px solid '+(isOn?'var(--border)':'rgba(239,68,68,0.2)')+';'+(isOn?'':'opacity:0.6;')+'">'+
                '<code style="flex:1;font-size:0.82rem;word-break:break-all;" title="'+esc(kd.key)+'">'+esc(shortKey)+'</code>'+
                '<button type="button" class="btn btn-ghost btn-sm" onclick="copyManagedAPIKey('+idx+',this)">复制</button>'+
                '<button type="button" class="btn '+(isOn?'btn-ghost':'btn-success')+' btn-sm" onclick="toggleAPIKey('+upstreamId+','+kd.row_id+','+(!isOn)+')">'+(isOn?'禁用':'启用')+'</button>'+
                '<button type="button" class="btn btn-danger btn-sm" onclick="deleteAPIKey('+upstreamId+','+kd.row_id+')">删除</button>'+
                '</div>';
        }).join('');
    });
}

function copyManagedAPIKey(index, btn) {
    const row = manageAPIKeyRows[index];
    if (!row || !row.key) return;
    copyTextToClipboard(row.key, btn);
}

function toggleAPIKey(upstreamId, keyRowId, enabled) {
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId+'/enabled', {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if(d.error) { toastErr(d.error); return; }
        loadUpstreams();
        openManageKeysDialog(upstreamId);
        toastOk(enabled ? '已启用' : '已禁用');
    });
}

async function deleteAPIKey(upstreamId, keyRowId) {
    if(!await askConfirm('确认删除此 API Key？删除后不可恢复。', {title:'删除 Key', okText:'删除', danger:true})) return;
    api('/upstreams/'+upstreamId+'/apikeys/'+keyRowId, {method:'DELETE'}).then(d => {
        if(d.error) toastErr(d.error); else { loadUpstreams(); openManageKeysDialog(upstreamId); toastOk('已删除'); }
    });
}

function testProxy(e, id) {
    // 打开测试对话框，让用户选择 Key、协议、模型
    openTestUpstreamDialog(id, true);
}

function openTestUpstreamDialog(upstreamId, resetFields) {
    const currentProtocol = document.getElementById('tu-protocol').value || 'openai';
    const currentModel = document.getElementById('tu-model').value || '';
    const currentPrompt = document.getElementById('tu-prompt').value || '你是什么模型？';
    document.getElementById('tu-upstream-id').value = upstreamId;
    document.getElementById('tu-result').style.display = 'none';
    document.getElementById('tu-protocol').value = resetFields ? 'openai' : currentProtocol;
    document.getElementById('tu-model').value = resetFields ? '' : currentModel;
    document.getElementById('tu-prompt').value = resetFields ? '你是什么模型？' : currentPrompt;
    const protocol = resetFields ? 'openai' : currentProtocol;
    // 加载测试模型列表并更新 datalist
    loadTestModels().then(() => updateTuModelDatalist(protocol));
    const sel = document.getElementById('tu-key-select');
    sel.innerHTML = '<option value="">加载中...</option>';
    const dlg = document.getElementById('dlg-test-upstream');
    if (!dlg.open) dlg.showModal();
    api('/upstreams/'+upstreamId+'/apikeys').then(data => {
        if (!data || data.length === 0) {
            sel.innerHTML = '<option value="0">无鉴权（公益站）</option>';
            return;
        }
        const firstEnabledIndex = data.findIndex(kd => kd.enabled);
        sel.innerHTML = data.map((kd, i) => {
            const shortKey = kd.key.length > 20 ? kd.key.substring(0, 10) + '...' + kd.key.substring(kd.key.length - 8) : kd.key;
            return '<option value="'+kd.row_id+'"'+(i===(firstEnabledIndex >= 0 ? firstEnabledIndex : 0)?' selected':'')+'>('+kd.row_id+') '+esc(shortKey)+(kd.enabled?'':' [已禁用]')+'</option>';
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
    if (!keyRowId && keyRowId !== '0') { toastErr('请选择一个 Key'); return; }
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
                html += '<div style="font-size:0.85rem;line-height:1.7;white-space:pre-wrap;word-break:break-word;padding:12px 14px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);">'+esc(d.reply)+'</div>';
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
                html += '<div style="color:var(--text);margin-bottom:8px;">'+esc(d.error_message)+'</div>';
            } else if (d.error) {
                html += '<div style="color:var(--text);margin-bottom:8px;">'+esc(d.error)+'</div>';
            }
            if (d.auth_mode || d.request_headers) {
                html += '<div style="font-size:0.72rem;font-weight:600;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.05em;margin:8px 0 6px;">本地面板发出的请求指纹</div>';
                html += '<pre style="margin:0 0 8px;font-size:0.75rem;line-height:1.5;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);color:var(--text-dim);">'+esc(JSON.stringify({auth_mode:d.auth_mode, headers:d.request_headers}, null, 2))+'</pre>';
            }
            if (d.raw_body) {
                let rawText = d.raw_body;
                try { rawText = JSON.stringify(JSON.parse(d.raw_body), null, 2); } catch (_) {}
                html += '<div style="font-size:0.72rem;font-weight:600;color:var(--text-dim);text-transform:uppercase;letter-spacing:0.05em;margin:8px 0 6px;">上游原始响应</div>';
                html += '<pre style="margin:0;font-size:0.78rem;line-height:1.5;white-space:pre-wrap;word-break:break-word;padding:10px 12px;background:var(--bg);border-radius:var(--radius-xs);border:1px solid var(--border);color:var(--text);">'+esc(rawText)+'</pre>';
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
        td.colSpan = 11;
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
                html += '<pre style="margin-top:8px;padding:8px;background:var(--bg);border-radius:4px;font-size:0.75rem;color:var(--text-dim);white-space:pre-wrap;word-break:break-all;">' + esc(d.origin_content) + '</pre>';
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
        td.colSpan = 11;
        td.innerHTML = '<div style="background:rgba(225,112,85,0.08);border:1px solid var(--red);border-radius:var(--radius-sm);padding:16px;margin:8px 16px;color:var(--red);">请求失败: '+esc(err.message)+'</div>';
        tr.appendChild(td);
        row.after(tr);
    });
}

// --- Keys ---
function loadKeys() {
    Promise.all([api('/keys'), api('/keys/bindings'), api('/key-rpm'), api('/keys/model-overrides')]).then(([data, bindMap, rpmData, overrideMap]) => {
        keysCache = data || [];
        keysBindMap = bindMap || {};
        keysRpmData = rpmData || {};
        keysOverrideMap = overrideMap || {};
        renderKeysTable();
    }).catch(() => {
        document.getElementById('keys-table').innerHTML = '<tr><td colspan="9" class="empty-state"><strong>加载失败</strong><p>无法获取密钥列表</p></td></tr>';
        toastErr('加载密钥失败');
    });
    loadKeyUsageStats();
}
function renderKeysTable() {
    const tbody = document.getElementById('keys-table');
    const q = ((document.getElementById('key-search')||{}).value || '').trim().toLowerCase();
    const en = (document.getElementById('key-filter-enabled')||{}).value || '';
    let list = keysCache.slice();
    if (en === '1') list = list.filter(k => k.enabled);
    if (en === '0') list = list.filter(k => !k.enabled);
    if (q) {
        list = list.filter(k => {
            const hay = [k.name, k.key_prefix, String(k.id)].join(' ').toLowerCase();
            return hay.indexOf(q) !== -1;
        });
    }
    const countEl = document.getElementById('key-count');
    if (countEl) countEl.textContent = list.length + (list.length !== keysCache.length ? ' / ' + keysCache.length : '');
    if (keysCache.length === 0) {
        tbody.innerHTML = '<tr><td colspan="9" class="empty-state"><strong>还没有下游密钥</strong><p>创建密钥后，客户端用它访问代理</p><button class="btn btn-primary btn-sm" onclick="document.getElementById(\'dlg-key\').showModal()">创建密钥</button></td></tr>';
        return;
    }
    if (list.length === 0) {
        tbody.innerHTML = '<tr><td colspan="9" class="empty-state"><strong>无匹配结果</strong><p>试试调整搜索词或状态筛选</p></td></tr>';
        return;
    }
    tbody.innerHTML = list.map(k => {
        const bound = keysBindMap[k.id] || [];
        let bindText = '<span class="badge badge-purple">全部</span>';
        if (bound.length > 0) {
            const names = bound.map(uid => { const u = allUpstreams.find(x=>x.id===uid); return u ? esc(u.name) : uid; });
            bindText = names.join(', ');
        }
        const overrides = keysOverrideMap[k.id] || [];
        let overrideText = '<span style="color:var(--text-dim)">无</span>';
        if (overrides.length > 0) {
            const patterns = [...new Set(overrides.map(o => o.ModelPattern))];
            overrideText = patterns.map(p => '<span class="model-tag">' + esc(p) + '</span>').join('');
        }
        const currentRpm = keysRpmData[k.id] || 0;
        const limitText = k.rpm_limit || '不限';
        const rpmColor = k.rpm_limit > 0 && currentRpm >= k.rpm_limit * 0.8 ? 'var(--red)' : currentRpm > 0 ? 'var(--green)' : 'var(--text-dim)';
        return '<tr><td class="hide-on-mobile">'+k.id+'</td><td class="hide-on-mobile"><code>'+esc(k.key_prefix)+'...</code></td><td>'+esc(k.name)+'</td><td>'+(k.rpm_limit||'不限')+'</td><td><span style="color:'+rpmColor+';font-weight:600">'+currentRpm+'</span><span style="color:var(--text-dim)">/'+ limitText+'</span></td><td>'+
        (k.enabled?'<span class="badge badge-green">启用</span>':'<span class="badge badge-red">禁用</span>')+
        '</td><td class="hide-on-mobile">'+bindText+'</td><td class="hide-on-mobile"><div class="model-tags" style="gap:4px">'+overrideText+'</div></td><td class="actions">'+
        '<button class="btn btn-ghost btn-sm" onclick="copyKey(event,'+k.id+')">复制</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="openBindingDialog('+k.id+')">绑定</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="openOverrideDialog('+k.id+')">路由</button> '+
        '<button class="btn btn-ghost btn-sm" onclick="editKey('+k.id+')">编辑</button> '+
        '<button class="btn btn-success btn-sm" onclick="toggleKey('+k.id+','+(!k.enabled)+')">'+(k.enabled?'禁用':'启用')+'</button> '+
        '<button class="btn btn-danger btn-sm" onclick="deleteKey('+k.id+')">删除</button>'+
        '</td></tr>';
    }).join('');
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
        if (d.error) { toastErr(d.error); btn.textContent = orig; btn.disabled = false; return; }
        btn.textContent = orig;
        btn.disabled = false;
        copyTextToClipboard(d.key, btn);
    }).catch(() => { btn.textContent = orig; btn.disabled = false; });
}

function createKey(e) {
    e.preventDefault();
    const f = new FormData(e.target);
    api('/keys', {method:'POST', body: JSON.stringify({
        name: f.get('name'), rpm_limit: parseInt(f.get('rpm_limit')||'0')
    })}).then(d => {
        if(d.error) { toastErr(d.error); return; }
        document.getElementById('new-key-value').textContent = d.key;
        document.getElementById('new-key-display').style.display = 'block';
        e.target.reset(); document.getElementById('dlg-key').close(); loadKeys(); toastOk('密钥已创建，请立即复制');
    });
}

function copyNewKey(btn) {
    const key = document.getElementById('new-key-value').textContent;
    copyTextToClipboard(key, btn);
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
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-edit-key').close(); loadKeys(); }
    });
}

function toggleKey(id, enabled) {
    api('/keys/'+id, {method:'PUT', body: JSON.stringify({enabled:enabled})}).then(d => {
        if (d && d.error) toastErr(d.error); else { loadKeys(); toastOk(enabled ? '已启用' : '已禁用'); }
    });
}

async function deleteKey(id) {
    if(!await askConfirm('确定删除密钥 #'+id+' 吗？绑定与路由覆盖也会清除。', {title:'删除密钥', okText:'删除', danger:true})) return;
    api('/keys/'+id, {method:'DELETE'}).then(d => {
        if (d && d.error) toastErr(d.error); else { loadKeys(); toastOk('已删除密钥'); }
    });
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
        if(d.error) toastErr(d.error);
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
    if (!pattern) { toastErr('请输入模型模式'); return; }
    if (!upstreamId) { toastErr('请选择目标上游'); return; }
    // Check duplicate
    if (moCurrentRules.some(r => r.model_pattern === pattern && r.upstream_id === upstreamId)) {
        toastErr('规则已存在'); return;
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
        if(d.error) toastErr(d.error);
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
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-model-patterns').close(); loadUpstreams(); }
    });
}

// --- Declared Models ---
let dmCurrentModels = [];
function openDeclaredModelsDialog(upstreamId) {
    document.getElementById('dm-upstream-id').value = upstreamId;
    document.getElementById('dm-new-model').value = '';
    api('/upstreams/'+upstreamId+'/declared-models').then(d => {
        dmCurrentModels = (d && d.models) ? d.models.slice() : [];
        renderDeclaredModelTags();
    });
    document.getElementById('dlg-declared-models').showModal();
}

function renderDeclaredModelTags() {
    const container = document.getElementById('dm-tags');
    if (dmCurrentModels.length === 0) {
        container.innerHTML = '<span class="model-tag-all">无声明模型</span>';
        return;
    }
    container.innerHTML = dmCurrentModels.map((m, i) =>
        '<span class="model-tag">' + esc(m) + ' <span style="cursor:pointer;margin-left:2px;opacity:0.7" onclick="removeDeclaredModelTag('+i+')">✕</span></span>'
    ).join('');
}

function addDeclaredModelTag() {
    const input = document.getElementById('dm-new-model');
    const v = input.value.trim();
    if (!v) return;
    if (dmCurrentModels.includes(v)) { input.value = ''; return; }
    dmCurrentModels.push(v);
    input.value = '';
    renderDeclaredModelTags();
}

function removeDeclaredModelTag(idx) {
    dmCurrentModels.splice(idx, 1);
    renderDeclaredModelTags();
}

function saveDeclaredModels() {
    const id = document.getElementById('dm-upstream-id').value;
    api('/upstreams/'+id+'/declared-models', {method:'PUT', body: JSON.stringify({models: dmCurrentModels})}).then(d => {
        if(d.error) toastErr(d.error);
        else { document.getElementById('dlg-declared-models').close(); loadUpstreams(); }
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
        toastErr('cf_clearance 和 User-Agent 需要同时填写');
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
        if(d.error) toastErr(d.error);
        else { e.target.reset(); loadModelWhitelist(); toastOk('已添加'); }
    });
}

async function deleteModelPattern(id) {
    if (!await askConfirm('确认删除此白名单模式？', {title:'删除模式', okText:'删除', danger:true})) return;
    api('/models/whitelist/'+id, {method:'DELETE'}).then(d => {
        if(d.error) toastErr(d.error);
        else { loadModelWhitelist(); toastOk('已删除'); }
    });
}

async function batchDeleteModelPatterns() {
    const ids = Array.from(document.querySelectorAll('.model-cb:checked')).map(cb => parseInt(cb.value));
    if (ids.length === 0) return;
    if (!await askConfirm('确认删除选中的 ' + ids.length + ' 个模式？', {title:'批量删除', okText:'删除', danger:true})) return;
    api('/models/whitelist/batch', {method:'DELETE', body: JSON.stringify({ids: ids})}).then(d => {
        if(d.error) toastErr(d.error);
        else { loadModelWhitelist(); toastOk('已批量删除'); }
    });
}

// --- Settings ---
// --- Header Capture (Claude Code fingerprints) ---
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
        toastOk(d.enabled ? '已开启抓取，请用 Claude Code 发一条消息' : '已停止抓取');
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
function renderHeaderCaptures(list) {
    const box = document.getElementById('hc-list');
    if (!box) return;
    if (!list.length) {
        box.className = 'empty-state';
        box.style.padding = '20px';
        box.innerHTML = '尚未抓取到请求。先开启抓取，再从 Claude Code 发一条消息。';
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
                lk.indexOf('stainless') >= 0 || lk === 'authorization' || lk === 'x-api-key';
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

function loadSettings() {
    api('/settings').then(data => {
        if (data) {
            document.getElementById('setting-threshold').value = data.auto_disable_threshold ?? 2;
        }
    });
}
function saveSettings() {
    const val = parseInt(document.getElementById('setting-threshold').value, 10);
    if (isNaN(val) || val < 0) { toastErr('阈值必须 >= 0'); return; }
    api('/settings', {method:'PUT', body: JSON.stringify({auto_disable_threshold: val})}).then(d => {
        if (d && d.error) { toastErr(d.error); return; }
        loadSettings();
        toastOk('设置已保存');
    }).catch(() => toastErr('保存失败'));
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
    if (!name) { toastErr('请输入模型名称'); return; }
    api('/test-models', {method:'POST', body: JSON.stringify({name:name, protocol:protocol})}).then(d => {
        if (d.error) { toastErr(d.error); return; }
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
    if (!name) { toastErr('名称不能为空'); return; }
    api('/test-models/'+id, {method:'PUT', body: JSON.stringify({name:name, protocol:protocol})}).then(d => {
        if (d.error) toastErr(d.error); else { loadTestModels(); toastOk('已更新'); }
    });
}

async function deleteTestModel(id) {
    if (!await askConfirm('确认删除此测试模型？', {title:'删除模型', okText:'删除', danger:true})) return;
    api('/test-models/'+id, {method:'DELETE'}).then(d => {
        if (d.error) { toastErr(d.error); return; }
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
