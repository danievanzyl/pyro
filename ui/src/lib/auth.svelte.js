// Shared API key state — persisted to localStorage, shared across all pages.

const STORAGE_KEY = 'fclk_api_key';

let _apiKey = $state('');
let _initialized = false;

export function getApiKey() {
	if (!_initialized && typeof localStorage !== 'undefined') {
		_apiKey = localStorage.getItem(STORAGE_KEY) || '';
		_initialized = true;
	}
	return _apiKey;
}

export function setApiKey(key) {
	_apiKey = key;
	if (typeof localStorage !== 'undefined') {
		if (key) {
			localStorage.setItem(STORAGE_KEY, key);
		} else {
			localStorage.removeItem(STORAGE_KEY);
		}
	}
}

export function initAuth() {
	if (typeof localStorage !== 'undefined') {
		_apiKey = localStorage.getItem(STORAGE_KEY) || '';
		_initialized = true;
	}
}

// Reactive getter for use in components.
export function apiKey() {
	return _apiKey;
}

// Fetch helper with auth header.
export async function apiFetch(path, opts = {}) {
	const key = getApiKey();
	return fetch(`/api${path}`, {
		...opts,
		headers: {
			...(key ? { 'Authorization': `Bearer ${key}` } : {}),
			'Content-Type': 'application/json',
			...opts.headers
		}
	});
}
