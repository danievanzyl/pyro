// Shared API key — reactive $state, persisted to localStorage.

const STORAGE_KEY = 'pyro_api_key';

function loadStored() {
	if (typeof localStorage === 'undefined') return '';
	return localStorage.getItem(STORAGE_KEY) || '';
}

export const authState = $state({ apiKey: loadStored() });

export function getApiKey() {
	return authState.apiKey;
}

export function setApiKey(key) {
	const next = key || '';
	authState.apiKey = next;
	if (typeof localStorage === 'undefined') return;
	if (next) {
		localStorage.setItem(STORAGE_KEY, next);
	} else {
		localStorage.removeItem(STORAGE_KEY);
	}
}

// For template conditionals: {#if hasApiKey()}
export function hasApiKey() {
	return authState.apiKey !== '';
}

// Fetch helper with auth header.
export async function apiFetch(path, opts = {}) {
	const key = authState.apiKey;
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
