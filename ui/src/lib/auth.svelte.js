// Shared API key — reads from localStorage, no reactive state.

const STORAGE_KEY = 'pyro_api_key';

export function getApiKey() {
	if (typeof localStorage === 'undefined') return '';
	return localStorage.getItem(STORAGE_KEY) || '';
}

export function setApiKey(key) {
	if (typeof localStorage === 'undefined') return;
	if (key) {
		localStorage.setItem(STORAGE_KEY, key);
	} else {
		localStorage.removeItem(STORAGE_KEY);
	}
}

// For template conditionals: {#if hasApiKey()}
export function hasApiKey() {
	return getApiKey() !== '';
}

// Fetch helper with auth header.
export async function apiFetch(path, opts = {}) {
	const key = getApiKey();
	if (!key) {
		return new Response(JSON.stringify({ error: 'no api key' }), { status: 401 });
	}
	return fetch(`/api${path}`, {
		...opts,
		headers: {
			'Authorization': `Bearer ${key}`,
			'Content-Type': 'application/json',
			...opts.headers
		}
	});
}
