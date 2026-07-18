
function prettyJSON(value) {
    try { return JSON.stringify(value || {}, null, 2); } catch (_) { return String(value || ''); }
}
function prettyBody(value) {
    if (!value) return '';
    try { return JSON.stringify(JSON.parse(value), null, 2); } catch (_) { return value; }
}

function contentToText(content) {
    if (typeof content === 'string') return content;
    if (!Array.isArray(content)) return '';
    return content.map(block => {
        if (!block || typeof block !== 'object') return '';
        return block.text || block.input_text || block.output_text || '';
    }).filter(Boolean).join('\n');
}

function extractRequestText(body) {
	try {
		const data = JSON.parse(body);
		if (typeof data.input === 'string') return data.input;
		if (typeof data.prompt === 'string') return data.prompt;
        const messages = Array.isArray(data.messages) ? data.messages : (Array.isArray(data.input) ? data.input : []);
        for (let i = messages.length - 1; i >= 0; i--) {
            if (messages[i] && messages[i].role === 'user') {
                const text = contentToText(messages[i].content);
                if (text) return text;
            }
        }
    } catch (_) {}
    return '';
}

function extractResponseText(body) {
    const extract = data => {
        if (!data || typeof data !== 'object') return '';
		if (typeof data.output_text === 'string') return data.output_text;
		if (data.choices && data.choices[0] && typeof data.choices[0].text === 'string') return data.choices[0].text;
		if (data.choices && data.choices[0] && data.choices[0].message) return contentToText(data.choices[0].message.content);
		if (data.choices && data.choices[0] && data.choices[0].delta) return contentToText(data.choices[0].delta.content);
		if (Array.isArray(data.content)) return contentToText(data.content);
        if (Array.isArray(data.output)) {
            return data.output.map(item => contentToText(item && item.content)).filter(Boolean).join('\n');
        }
        if (data.response) return extract(data.response);
        if (data.message) return extract(data.message);
        if (typeof data.delta === 'string') return data.delta;
        if (data.delta && typeof data.delta.text === 'string') return data.delta.text;
        return '';
    };
    try { return extract(JSON.parse(body)); } catch (_) {}
    const chunks = [];
    String(body || '').split(/\r?\n/).forEach(line => {
        if (!line.trim().startsWith('data:')) return;
        const payload = line.trim().slice(5).trim();
        if (!payload || payload === '[DONE]') return;
        try {
            const text = extract(JSON.parse(payload));
            if (text) chunks.push(text);
        } catch (_) {}
    });
    return chunks.join('');
}
