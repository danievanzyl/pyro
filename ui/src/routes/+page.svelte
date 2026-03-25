<script>
	import { hasApiKey, getApiKey, setApiKey, apiFetch } from '$lib/auth.svelte.js';

	let health = $state(null);
	let sandboxes = $state([]);
	let error = $state('');
	let apiKeyInput = $state('');
	let authenticated = $state(hasApiKey());
	let authError = $state('');
	let validating = $state(false);
	let apiKeys = $state([]);
	let newKeyValue = $state('');

	async function saveKey() {
		const key = apiKeyInput.trim();
		if (!key) return;
		validating = true;
		authError = '';
		try {
			const res = await fetch('/api/sandboxes', {
				headers: { 'Authorization': `Bearer ${key}`, 'Content-Type': 'application/json' }
			});
			if (res.status === 401) {
				authError = 'Invalid API key';
				validating = false;
				return;
			}
			setApiKey(key);
			authenticated = true;
			fetchData();
			connectSSE();
		} catch (e) {
			authError = 'Connection failed: ' + e.message;
		}
		validating = false;
	}

	function disconnect() {
		setApiKey('');
		authenticated = false;
		sandboxes = [];
		health = null;
		apiKeys = [];
		if (eventSource) { eventSource.close(); eventSource = null; }
	}

	async function fetchKeys() {
		try {
			const res = await apiFetch('/keys');
			if (res.ok) apiKeys = await res.json();
		} catch {}
	}

	async function createNewKey() {
		const name = prompt('Key name:');
		if (!name) return;
		try {
			const res = await apiFetch('/keys', {
				method: 'POST',
				body: JSON.stringify({ name }),
			});
			if (res.ok) {
				const data = await res.json();
				newKeyValue = data.key;
				fetchKeys();
			}
		} catch {}
	}

	async function revokeKey(id) {
		if (!confirm('Revoke this API key? This cannot be undone.')) return;
		await apiFetch(`/keys/${id}`, { method: 'DELETE' });
		fetchKeys();
	}

	async function fetchData() {
		try {
			const healthRes = await fetch('/api/health');
			health = await healthRes.json();

			if (getApiKey()) {
				const sbRes = await apiFetch('/sandboxes');
				if (sbRes.ok) sandboxes = await sbRes.json();
			}
			error = '';
		} catch (e) {
			error = e.message;
		}
	}

	function formatTTL(expiresAt) {
		const remaining = new Date(expiresAt) - new Date();
		if (remaining <= 0) return 'expired';
		const mins = Math.floor(remaining / 60000);
		const secs = Math.floor((remaining % 60000) / 1000);
		if (mins > 60) return `${Math.floor(mins / 60)}h ${mins % 60}m`;
		return `${mins}m ${secs}s`;
	}

	function stateBadgeClass(state) {
		if (state === 'running') return 'badge-running';
		if (state === 'creating') return 'badge-creating';
		if (state === 'stopping') return 'badge-paused';
		return 'badge-stopped';
	}

	let eventSource;
	let sseRetries = 0;

	function connectSSE() {
		const key = getApiKey();
		if (!key || sseRetries > 3) return;
		if (eventSource) { eventSource.close(); eventSource = null; }

		eventSource = new EventSource(`/api/events?api_key=${key}`);
		eventSource.addEventListener('health.tick', (e) => {
			try {
				const data = JSON.parse(e.data);
				if (health) {
					health.active_sandboxes = data.data?.active_sandboxes ?? health.active_sandboxes;
					if (data.data?.pool_stats) health.pool_stats = data.data.pool_stats;
				}
			} catch {}
		});
		eventSource.addEventListener('sandbox.created', () => fetchData());
		eventSource.addEventListener('sandbox.destroyed', () => fetchData());
		eventSource.addEventListener('connected', () => { sseRetries = 0; });
		eventSource.onerror = () => {
			sseRetries++;
			if (eventSource) { eventSource.close(); eventSource = null; }
		};
	}

	fetchData();
	fetchKeys();
	connectSSE();
	setInterval(fetchData, 5000);
</script>

{#if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
{/if}

{#if !authenticated}
	<div class="key-card card">
		<span class="material-symbols-outlined" style="font-size:2rem; opacity:0.3;">key</span>
		<h3>Connect to your cluster</h3>
		<p>Enter your API key to monitor sandboxes</p>
		{#if authError}
			<div class="auth-error">{authError}</div>
		{/if}
		<div class="key-row">
			<input type="password" bind:value={apiKeyInput} placeholder="pk_..."
				onkeydown={(e) => e.key === 'Enter' && saveKey()} />
			<button class="btn-primary" onclick={saveKey} disabled={validating}>
				{validating ? 'Validating...' : 'Connect'}
			</button>
		</div>
	</div>
{:else}
	<div class="page-header">
		<div>
			<h1>Fleet Overview</h1>
			<p class="subtitle">Firecracker microVM fleet status</p>
		</div>
		<div style="display:flex; align-items:center; gap:0.75rem;">
			{#if health?.status === 'ok'}
				<div class="live-indicator">
					<span class="status-dot running pulse"></span>
					Live
				</div>
			{/if}
			<button class="btn-secondary" style="font-size:0.7rem; padding:0.25rem 0.5rem;" onclick={disconnect}>
				Disconnect
			</button>
		</div>
	</div>

	<div class="metrics-strip">
		<div class="metric">
			<div class="metric-label">Status</div>
			<div class="metric-value" style="color: #0a6b2a">{health?.status === 'ok' ? 'Healthy' : '...'}</div>
		</div>
		<div class="metric">
			<div class="metric-label">Active VMs</div>
			<div class="metric-value">{health?.active_sandboxes ?? '—'}</div>
		</div>
		<div class="metric">
			<div class="metric-label">Your Sandboxes</div>
			<div class="metric-value">{sandboxes.length}</div>
		</div>
		{#if health?.pool_stats}
			<div class="metric">
				<div class="metric-label">Pool (warm)</div>
				<div class="metric-value">
					{Object.values(health.pool_stats).reduce((a, b) => a + b, 0)}
				</div>
			</div>
		{/if}
	</div>

	<div class="section-header">
		<h2>VM Inventory</h2>
		<a href="/sandboxes" class="btn-secondary">
			<span class="material-symbols-outlined" style="font-size:1rem;">add</span>
			New VM
		</a>
	</div>

	{#if sandboxes.length > 0}
		<div class="card" style="padding:0; overflow:hidden;">
			<table>
				<thead>
					<tr>
						<th>Status</th>
						<th>ID</th>
						<th>Image</th>
						<th>PID</th>
						<th>CID</th>
						<th>TTL</th>
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each sandboxes as sb}
						<tr>
							<td>
								<span class="badge {stateBadgeClass(sb.state)}">
									<span class="status-dot {sb.state}" class:pulse={sb.state === 'running'}></span>
									{sb.state.toUpperCase()}
								</span>
							</td>
							<td><code class="mono">{sb.id.slice(0, 12)}</code></td>
							<td>{sb.image}</td>
							<td class="mono">{sb.pid || '—'}</td>
							<td class="mono">{sb.vsock_cid}</td>
							<td>{formatTTL(sb.expires_at)}</td>
							<td>
								<div class="action-btns">
									<a href="/sandboxes/{sb.id}" class="btn-icon" title="Details">
										<span class="material-symbols-outlined" style="font-size:1rem;">terminal</span>
									</a>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<div class="empty-state card">
			<span class="material-symbols-outlined">cloud_off</span>
			<h3>No active sandboxes</h3>
			<p>Provision a new micro-VM to get started</p>
			<a href="/sandboxes" class="btn-primary">
				<span class="material-symbols-outlined" style="font-size:1rem;">add</span>
				Create Sandbox
			</a>
		</div>
	{/if}

	<div class="section-header" style="margin-top:2rem;">
		<h2>API Keys</h2>
		<button class="btn-secondary" onclick={createNewKey}>
			<span class="material-symbols-outlined" style="font-size:1rem;">add</span>
			New Key
		</button>
	</div>

	{#if newKeyValue}
		<div class="new-key-banner card">
			<span class="material-symbols-outlined" style="color:var(--primary);">key</span>
			<div>
				<strong>New API key created</strong>
				<p style="font-size:0.75rem; color:var(--on-surface-variant);">Copy this key now — it won't be shown again.</p>
			</div>
			<code class="mono" style="font-size:0.8rem; background:var(--surface-container); padding:0.3rem 0.6rem; border-radius:var(--radius-sm); user-select:all;">{newKeyValue}</code>
			<button class="btn-icon" title="Dismiss" onclick={() => newKeyValue = ''}>
				<span class="material-symbols-outlined" style="font-size:1rem;">close</span>
			</button>
		</div>
	{/if}

	{#if apiKeys.length > 0}
		<div class="card" style="padding:0; overflow:hidden;">
			<table>
				<thead>
					<tr><th>Name</th><th>Key</th><th>Created</th><th></th></tr>
				</thead>
				<tbody>
					{#each apiKeys as k}
						<tr>
							<td>{k.name}</td>
							<td class="mono">{k.prefix}</td>
							<td>{new Date(k.created_at).toLocaleDateString()}</td>
							<td>
								<button class="btn-icon" style="color:var(--error); border-color:var(--error);" title="Revoke" onclick={() => revokeKey(k.id)}>
									<span class="material-symbols-outlined" style="font-size:1rem;">delete</span>
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<p style="color:var(--on-surface-variant); font-size:0.8rem;">No API keys found.</p>
	{/if}
{/if}

<style>
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 1.5rem; }
	h1 { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.8rem; margin-top: 0.15rem; }
	.live-indicator { display: flex; align-items: center; gap: 0.4rem; font-size: 0.75rem; font-weight: 600; color: #0a6b2a; }

	.metrics-strip {
		display: flex;
		gap: 2rem;
		padding: 1rem 0;
		margin-bottom: 1.5rem;
		border-bottom: 1px solid var(--surface-container);
	}
	.metric-label { font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.08em; color: var(--on-surface-variant); margin-bottom: 0.2rem; }
	.metric-value { font-family: var(--font-headline); font-size: 1.25rem; font-weight: 700; }

	.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.75rem; }
	h2 { font-family: var(--font-headline); font-size: 1rem; font-weight: 600; }

	code { font-family: var(--font-mono); font-size: 0.8rem; }
	.action-btns { display: flex; gap: 0.25rem; }

	.key-card { display: flex; flex-direction: column; align-items: center; padding: 3rem; text-align: center; gap: 0.5rem; max-width: 420px; margin: 4rem auto; }
	.key-card h3 { font-family: var(--font-headline); font-size: 1rem; }
	.key-card p { color: var(--on-surface-variant); font-size: 0.8rem; }
	.key-row { display: flex; gap: 0.5rem; margin-top: 0.5rem; }
	.key-row input { width: 260px; }
	.auth-error { color: var(--error); font-size: 0.8rem; font-weight: 500; }
	.new-key-banner { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1rem; padding: 0.75rem 1rem; }
</style>
