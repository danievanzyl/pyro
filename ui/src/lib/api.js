const API_BASE = '/api';

/** @param {string} apiKey */
export function createClient(apiKey) {
	/** @param {string} path @param {RequestInit} [opts] */
	async function request(path, opts = {}) {
		const res = await fetch(`${API_BASE}${path}`, {
			...opts,
			headers: {
				'Authorization': `Bearer ${apiKey}`,
				'Content-Type': 'application/json',
				...opts.headers
			}
		});
		if (!res.ok) {
			const body = await res.json().catch(() => ({ error: res.statusText }));
			throw new Error(body.error || res.statusText);
		}
		if (res.status === 204) return null;
		return res.json();
	}

	return {
		health: () => fetch(`${API_BASE}/health`).then(r => r.json()),

		listSandboxes: () => request('/sandboxes'),
		getSandbox: (/** @type {string} */ id) => request(`/sandboxes/${id}`),
		createSandbox: (/** @type {{ttl: number, image?: string}} */ body) =>
			request('/sandboxes', { method: 'POST', body: JSON.stringify(body) }),
		deleteSandbox: (/** @type {string} */ id) =>
			request(`/sandboxes/${id}`, { method: 'DELETE' }),
		execInSandbox: (/** @type {string} */ id, /** @type {{command: string[]}} */ body) =>
			request(`/sandboxes/${id}/exec`, { method: 'POST', body: JSON.stringify(body) }),

		listImages: () => request('/images'),

		listAudit: (/** @type {number} */ limit = 50) =>
			request(`/audit?limit=${limit}`),
		sandboxAudit: (/** @type {string} */ id) =>
			request(`/audit/sandbox/${id}`),
	};
}
