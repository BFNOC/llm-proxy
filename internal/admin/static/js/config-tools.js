function exportConfig() {
    window.open('/admin/api/config/export', '_blank');
}

function importConfig() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async (e) => {
        const file = e.target.files[0];
        if (!file) return;
        const text = await file.text();
        try {
            JSON.parse(text);
        } catch(err) {
            toastErr('无效的 JSON 文件');
            return;
        }
        if (!await askConfirm('确认导入配置？将添加新的上游和 Key（不覆盖现有配置）。', {title:'导入配置', okText:'导入'})) return;
        api('/config/import', {method:'POST', body: text}).then(d => {
            if (d.error) toastErr(d.error);
            else { toastOk(d.message || '配置已导入'); loadUpstreams(); loadKeys(); }
        }).catch(() => toastErr('导入失败'));
    };
    input.click();
}
