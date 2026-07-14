// --- 快捷操作面板 ---
function pauseAllUpstreams() {
    askConfirm('确认暂停所有上游？暂停后所有请求将返回 503。', {title:'暂停所有上游', okText:'暂停', danger:true}).then(function(ok) {
        if (!ok) return;
        api('/actions/pause-all', {method:'POST'}).then(function(d) {
            if(d.error) toastErr(d.error); else { loadUpstreams(); loadStatus(); toastOk('已暂停 ' + (d.affected||0) + ' 个上游'); }
        });
    });
}
function resumeAllUpstreams() {
    api('/actions/resume-all', {method:'POST'}).then(function(d) {
        if(d.error) toastErr(d.error); else { loadUpstreams(); loadStatus(); toastOk('已恢复 ' + (d.affected||0) + ' 个上游'); }
    });
}
function refreshAllCaches() {
    api('/actions/refresh-caches', {method:'POST'}).then(function(d) {
        if(d.error) toastErr(d.error); else { toastOk('所有缓存已刷新'); }
    });
}
