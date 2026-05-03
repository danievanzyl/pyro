// Shared SSE singleton. One EventSource for the SPA.
// Lifecycle: connect(apiKey) on auth, disconnect() on logout.
// Subscribers register named-event handlers via subscribe(name, handler).

const BACKOFF_INITIAL_MS = 1000;
const BACKOFF_MAX_MS = 30000;

let es = null;
let currentKey = '';
let backoffMs = BACKOFF_INITIAL_MS;
let retryTimer = null;

const listeners = new Map(); // event name -> Set<handler>

export const connectionStatus = $state({ value: 'disconnected' });

export function connect(apiKey) {
	if (!apiKey) {
		disconnect();
		return;
	}
	if (currentKey === apiKey && es) return;
	closeStream();
	currentKey = apiKey;
	backoffMs = BACKOFF_INITIAL_MS;
	open();
}

export function disconnect() {
	currentKey = '';
	backoffMs = BACKOFF_INITIAL_MS;
	if (retryTimer) {
		clearTimeout(retryTimer);
		retryTimer = null;
	}
	closeStream();
	connectionStatus.value = 'disconnected';
}

export function subscribe(eventName, handler) {
	let set = listeners.get(eventName);
	if (!set) {
		set = new Set();
		listeners.set(eventName, set);
	}
	set.add(handler);
	if (es) es.addEventListener(eventName, handler);
	return () => {
		const s = listeners.get(eventName);
		if (s) {
			s.delete(handler);
			if (s.size === 0) listeners.delete(eventName);
		}
		if (es) es.removeEventListener(eventName, handler);
	};
}

function open() {
	if (!currentKey || typeof EventSource === 'undefined') return;
	connectionStatus.value = 'connecting';
	es = new EventSource(`/api/events?api_key=${encodeURIComponent(currentKey)}`);
	es.addEventListener('connected', () => {
		connectionStatus.value = 'connected';
		backoffMs = BACKOFF_INITIAL_MS;
	});
	for (const [name, set] of listeners) {
		for (const h of set) es.addEventListener(name, h);
	}
	es.onerror = () => {
		if (!currentKey) return;
		connectionStatus.value = 'error';
		closeStream();
		scheduleReconnect();
	};
}

function closeStream() {
	if (es) {
		es.close();
		es = null;
	}
}

function scheduleReconnect() {
	if (retryTimer) return;
	const delay = backoffMs;
	backoffMs = Math.min(backoffMs * 2, BACKOFF_MAX_MS);
	retryTimer = setTimeout(() => {
		retryTimer = null;
		if (currentKey) open();
	}, delay);
}
