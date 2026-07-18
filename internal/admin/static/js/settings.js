function loadSettings() {
	const keysPromise = keysCache.length > 0 ? Promise.resolve(keysCache) : api('/keys').then(data => {
		if (Array.isArray(data)) keysCache = data;
		return keysCache;
	});
	Promise.all([api('/settings'), keysPromise]).then(([data, keys]) => {
		if (!data || data.error) {
			if (data && data.error) toastErr(data.error);
			return;
		}
		document.getElementById('setting-threshold').value = data.auto_disable_threshold ?? 2;
		document.getElementById('setting-slow-threshold').value = data.slow_request_threshold_ms ?? 30000;
		document.getElementById('setting-full-recording-enabled').checked = !!data.full_recording_enabled;
		const selectedIDs = Array.isArray(data.full_recording_key_ids) ? data.full_recording_key_ids.map(Number) : [];
		const allKeys = data.full_recording_all_keys !== false;
		document.getElementById('setting-full-recording-all-keys').checked = allKeys;
		const selectedMode = document.querySelector('input[name="full-recording-mode"][value="selected"]');
		selectedMode.checked = !allKeys;
		renderFullRecordingKeys(keys || [], selectedIDs);
		updateFullRecordingControls();
	}).catch(() => toastErr('加载设置失败'));
}

function renderFullRecordingKeys(keys, selectedIDs) {
	const box = document.getElementById('setting-full-recording-keys');
	const selected = new Set((selectedIDs || []).map(Number));
	if (!keys || keys.length === 0) {
		box.innerHTML = '<div class="empty-state">暂无下游密钥</div>';
		return;
	}
	box.innerHTML = keys.map(key => {
		const id = Number(key.id);
		return '<label class="recording-key-option" data-testid="full-recording-key"><input type="checkbox" value="' + id + '" ' + (selected.has(id) ? 'checked' : '') + '><span><strong>#' + id + ' ' + esc(key.name || '') + '</strong><code>' + esc(key.key_prefix || '') + '</code></span></label>';
	}).join('');
}

function updateFullRecordingControls() {
	const enabled = document.getElementById('setting-full-recording-enabled').checked;
	const scope = document.getElementById('setting-full-recording-scope');
	const selectedMode = document.querySelector('input[name="full-recording-mode"]:checked');
	const list = document.getElementById('setting-full-recording-keys');
	scope.disabled = !enabled;
	list.classList.toggle('is-hidden', !enabled || !selectedMode || selectedMode.value !== 'selected');
}

function saveSettings() {
	const threshold = parseInt(document.getElementById('setting-threshold').value, 10);
	const slowThreshold = parseInt(document.getElementById('setting-slow-threshold').value, 10);
	if (isNaN(threshold) || threshold < 0) { toastErr('阈值必须 >= 0'); return; }
	if (isNaN(slowThreshold) || slowThreshold < 0) { toastErr('慢请求阈值必须 >= 0'); return; }
	const enabled = document.getElementById('setting-full-recording-enabled').checked;
	const mode = (document.querySelector('input[name="full-recording-mode"]:checked') || {}).value || 'all';
	let keyIDs = [];
	if (mode === 'selected') {
		keyIDs = Array.from(document.querySelectorAll('#setting-full-recording-keys input:checked')).map(input => Number(input.value));
		if (enabled && keyIDs.length === 0) { toastErr('请选择至少一个下游密钥'); return; }
	}
	api('/settings', {method:'PUT', body: JSON.stringify({
		auto_disable_threshold: threshold,
		slow_request_threshold_ms: slowThreshold,
		full_recording_enabled: enabled,
		full_recording_all_keys: mode === 'all',
		full_recording_key_ids: keyIDs
	})}).then(d => {
        if (d && d.error) { toastErr(d.error); return; }
		slowRequestThreshold = slowThreshold;
        loadSettings();
        toastOk('设置已保存');
    }).catch(() => toastErr('保存失败'));
}

// --- Test Models ---
