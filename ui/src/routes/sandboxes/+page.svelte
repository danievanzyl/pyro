<script>
	import { onMount } from 'svelte';

	let sandboxes = $state([]);
	let error = $state('');
	let creating = $state(false);
	let newTTL = $state(3600);
	let newImage = $state('default');

	let apiKey = $state('');

	async function apiFetch(path, opts = {}) {
		return fetch(`/api${path}`, {
			...opts,
			headers: { 'Authorization': `Bearer ${apiKey}`, 'Content-Type': 'application/json', ...opts.headers }
		});
	}

	async function refresh() {
		try {
			const res = await apiFetch('/sandboxes');
			if (res.ok) sandboxes = await res.json();
			error = '';
		} catch (e) { error = e.message; }
	}

	async function createSandbox() {
		creating = true;
		try {
			const res = await apiFetch('/sandboxes', {
				method: 'POST',
				body: JSON.stringify({ ttl: newTTL, image: newImage })
			});
			if (!res.ok) { const body = await res.json(); throw new Error(body.error); }
			await refresh();
		} catch (e) { error = e.message; }
		finally { creating = false; }
	}

	async function destroySandbox(id) {
		try {
			await apiFetch(`/sandboxes/${id}`, { method: 'DELETE' });
			await refresh();
		} catch (e) { error = e.message; }
	}

	function formatTTL(expiresAt) {
		const r = new Date(expiresAt) - new Date();
		if (r <= 0) return 'expired';
		const m = Math.floor(r / 60000), s = Math.floor((r % 60000) / 1000);
		return m > 60 ? `${Math.floor(m/60)}h ${m%60}m` : `${m}m ${s}s`;
	}

	function stateColor(state) {
		if (state === 'running') return 'var(--tertiary)';
		if (state === 'creating') return 'var(--primary)';
		return 'var(--secondary)';
	}

	onMount(() => { apiKey = localStorage.getItem('fclk_api_key') || ''; refresh(); const i = setInterval(refresh, 3000); return () => clearInterval(i); });
</script>

<div class="page-header">
	<div>
		<h1>VM Instances</h1>
		<p class="subtitle">Manage ephemeral Firecracker micro-VM sandboxes</p>
	</div>
	<div class="count-badge">{sandboxes.length} active</div>
</div>

{#if error}
	<div class="error-banner glass-panel">
		<span class="material-symbols-outlined">error</span> {error}
	</div>
{/if}

<div class="provision-panel glass-panel">
	<h3><span class="material-symbols-outlined" style="font-size:1.1rem; color: var(--primary)">add_circle</span> Provision New Sandbox</h3>
	<div class="provision-fields">
		<div class="field">
			<label>TTL (seconds)</label>
			<input type="number" bind:value={newTTL} min="10" max="86400" />
		</div>
		<div class="field">
			<label>Base Image</label>
			<input type="text" bind:value={newImage} placeholder="default" />
		</div>
		<button class="btn-primary" onclick={createSandbox} disabled={creating}>
			{#if creating}
				<span class="material-symbols-outlined spinning" style="font-size:1rem">progress_activity</span> Provisioning...
			{:else}
				<span class="material-symbols-outlined" style="font-size:1rem">rocket_launch</span> Launch
			{/if}
		</button>
	</div>
</div>

{#if sandboxes.length > 0}
	<div class="vm-list">
		{#each sandboxes as sb}
			<div class="vm-card glass-panel">
				<div class="vm-card-header">
					<div class="vm-identity">
						<span class="status-dot" style="background: {stateColor(sb.state)}; box-shadow: 0 0 8px {stateColor(sb.state)}"></span>
						<div>
							<div class="vm-id">{sb.id.slice(0, 12)}</div>
							<div class="vm-meta">{sb.image} &middot; PID {sb.pid || '—'}</div>
						</div>
					</div>
					<button class="btn-destroy" onclick={() => destroySandbox(sb.id)}>
						<span class="material-symbols-outlined" style="font-size:1rem">stop_circle</span> Terminate
					</button>
				</div>
				<div class="vm-card-stats">
					<div class="stat">
						<span class="stat-label">State</span>
						<span class="stat-value" style="color: {stateColor(sb.state)}">{sb.state}</span>
					</div>
					<div class="stat">
						<span class="stat-label">vSock CID</span>
						<span class="stat-value">{sb.vsock_cid}</span>
					</div>
					<div class="stat">
						<span class="stat-label">TTL Remaining</span>
						<span class="stat-value">{formatTTL(sb.expires_at)}</span>
					</div>
					<div class="stat">
						<span class="stat-label">Created</span>
						<span class="stat-value">{new Date(sb.created_at).toLocaleTimeString()}</span>
					</div>
				</div>
			</div>
		{/each}
	</div>
{:else}
	<div class="empty-state glass-panel">
		<span class="material-symbols-outlined" style="font-size: 3rem; color: var(--outline)">dns</span>
		<h3>No instances running</h3>
		<p>Launch a sandbox above to begin</p>
	</div>
{/if}

<style>
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 2rem; }
	h1 { font-family: var(--font-headline); font-size: 1.75rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.85rem; margin-top: 0.25rem; }
	.count-badge { padding: 0.35rem 0.75rem; border-radius: 9999px; background: var(--surface-container-highest); color: var(--on-surface-variant); font-size: 0.75rem; font-weight: 600; }

	.error-banner { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem; margin-bottom: 1.5rem; color: var(--error); font-size: 0.85rem; }

	.provision-panel { padding: 1.25rem; margin-bottom: 2rem; }
	.provision-panel h3 { display: flex; align-items: center; gap: 0.5rem; font-family: var(--font-headline); font-size: 0.9rem; font-weight: 600; margin-bottom: 1rem; }
	.provision-fields { display: flex; gap: 1rem; align-items: flex-end; }
	.field { display: flex; flex-direction: column; gap: 0.3rem; }
	.field label { font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.08em; color: var(--on-surface-variant); }
	.field input { width: 160px; }

	.btn-primary {
		display: inline-flex; align-items: center; gap: 0.4rem; padding: 0.55rem 1.25rem;
		border-radius: 0.75rem; background: linear-gradient(135deg, var(--primary), var(--primary-dim));
		color: #4a0076; font-weight: 600; font-size: 0.85rem;
	}
	.btn-primary:hover { opacity: 0.9; }
	.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

	@keyframes spin { to { transform: rotate(360deg); } }
	:global(.spinning) { animation: spin 1s linear infinite; }

	.vm-list { display: flex; flex-direction: column; gap: 0.75rem; }
	.vm-card { padding: 1.25rem; }
	.vm-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
	.vm-identity { display: flex; align-items: center; gap: 0.75rem; }
	.status-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
	.vm-id { font-family: var(--font-headline); font-size: 0.95rem; font-weight: 600; }
	.vm-meta { font-size: 0.75rem; color: var(--on-surface-variant); }

	.btn-destroy {
		display: flex; align-items: center; gap: 0.35rem; padding: 0.4rem 0.75rem;
		border-radius: 0.5rem; background: rgba(255, 110, 132, 0.1); color: var(--error);
		font-size: 0.75rem; font-weight: 600;
	}
	.btn-destroy:hover { background: rgba(255, 110, 132, 0.2); }

	.vm-card-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 1rem; }
	.stat { display: flex; flex-direction: column; gap: 0.15rem; }
	.stat-label { font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.08em; color: var(--on-surface-variant); }
	.stat-value { font-size: 0.85rem; font-weight: 600; }

	.empty-state { display: flex; flex-direction: column; align-items: center; padding: 4rem; text-align: center; gap: 0.75rem; }
	.empty-state h3 { font-family: var(--font-headline); font-size: 1.1rem; }
	.empty-state p { color: var(--on-surface-variant); font-size: 0.85rem; }
</style>
