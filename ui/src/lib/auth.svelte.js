// Shared API key — always reads from localStorage (synchronous, no race).

const STORAGE_KEY = 'fclk_api_key';

let _apiKey = $state('');

export function getApiKey() {
	if (typeof localStorage !== 'undefined') {
		_apiKey = localStorage.getItem(STORAGE_KEY) || '';
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

// Reactive getter for use in templates ({#if apiKey()}).
export function apiKey() {
	// Re-read from storage to stay in sync.
	if (typeof localStorage !== 'undefined') {
		_apiKey = localStorage.getItem(STORAGE_KEY) || '';
	}
	return _apiKey;
}

// Fetch helper with auth header.
export async function apiFetch(path, opts = {}) {
	const key = getApiKey();
	if (!key) {
		// Return a fake 401 so callers handle it gracefully.
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
