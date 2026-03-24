<script>
	import { apiFetch, getApiKey } from '$lib/auth.svelte.js';

	let sandboxes = $state([]);
	let kernels = $state([]);
	let error = $state('');
	let creating = $state(false);
	let newTTL = $state(3600);
	let newImage = $state('default');
	let newKernel = $state('');
	let newVCPU = $state(1);
	let newMemMiB = $state(256);

	async function refresh() {
		try {
			const res = await apiFetch('/sandboxes');
			if (res.ok) sandboxes = await res.json();
			error = '';
		} catch (e) { error = e.message; }
	}

	async function fetchKernels() {
		try {
			const res = await apiFetch('/kernels');
			if (res.ok) kernels = await res.json();
		} catch {}
	}

	let eventSource = null;
	let sseRetries = 0;

	function connectSSE() {
		const key = getApiKey();
		if (!key || sseRetries > 3) return;
		eventSource = new EventSource(`/api/events?api_key=${encodeURIComponent(key)}`);
		eventSource.addEventListener('sandbox.created', () => refresh());
		eventSource.addEventListener('sandbox.destroyed', () => refresh());
		eventSource.addEventListener('sandbox.exec', () => refresh());
		eventSource.addEventListener('connected', () => { sseRetries = 0; });
		eventSource.onerror = () => {
			sseRetries++;
			eventSource.close();
			eventSource = null;
		};
	}

	async function createSandbox() {
		creating = true;
		try {
			const body = { ttl: newTTL, image: newImage, vcpu: newVCPU, mem_mib: newMemMiB };
			if (newKernel) body.kernel = newKernel;
			const res = await apiFetch('/sandboxes', {
				method: 'POST',
				body: JSON.stringify(body),
			});
			if (!res.ok) {
				const data = await res.json();
				error = data.error || `Error ${res.status}`;
			} else {
				error = '';
				refresh();
			}
		} catch (e) { error = e.message; }
		creating = false;
	}

	async function destroySandbox(id) {
		await apiFetch(`/sandboxes/${id}`, { method: 'DELETE' });
		refresh();
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

	refresh();
	fetchKernels();
	connectSSE();
	setInterval(refresh, 10000); // Fallback polling (SSE handles real-time)
</script>

<div class="page-header">
	<h1>VM Instances</h1>
	<span class="count">{sandboxes.length} active</span>
</div>

{#if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
{/if}

<div class="create-panel card">
	<h3>Create New Sandbox</h3>
	<div class="create-fields">
		<div class="field">
			<label for="image-select">Image</label>
			<select id="image-select" bind:value={newImage}>
				<option value="default">default</option>
				<option value="minimal">minimal</option>
				<option value="ubuntu">ubuntu</option>
				<option value="python">python</option>
				<option value="node">node</option>
			</select>
		</div>
		<div class="field">
			<label for="kernel-select">Kernel</label>
			<select id="kernel-select" bind:value={newKernel}>
				<option value="">latest</option>
				{#each kernels as k}
					<option value={k.version}>{k.version}</option>
				{/each}
			</select>
		</div>
		<div class="field">
			<label for="ttl-input">TTL (seconds)</label>
			<input id="ttl-input" type="number" bind:value={newTTL} min="10" max="86400" />
		</div>
		<div class="field">
			<label for="vcpu-input">vCPUs</label>
			<input id="vcpu-input" type="number" bind:value={newVCPU} min="1" max="4" />
		</div>
		<div class="field">
			<label for="mem-input">Memory (MiB)</label>
			<input id="mem-input" type="number" bind:value={newMemMiB} min="128" max="2048" step="128" />
		</div>
		<div class="field field-action">
			<button class="btn-primary" onclick={createSandbox} disabled={creating}>
				{#if creating}
					<span class="material-symbols-outlined spin" style="font-size:1rem;">progress_activity</span>
					Provisioning...
				{:else}
					<span class="material-symbols-outlined" style="font-size:1rem;">rocket_launch</span>
					Launch
				{/if}
			</button>
		</div>
	</div>
</div>

{#if sandboxes.length > 0}
	<div class="card" style="padding:0; overflow:hidden; margin-top:1rem;">
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
						<td><a href="/sandboxes/{sb.id}" class="mono">{sb.id.slice(0, 12)}</a></td>
						<td>{sb.image}</td>
						<td class="mono">{sb.pid || '—'}</td>
						<td class="mono">{sb.vsock_cid}</td>
						<td>{formatTTL(sb.expires_at)}</td>
						<td>
							<div class="action-btns">
								<a href="/sandboxes/{sb.id}" class="btn-icon" title="Console">
									<span class="material-symbols-outlined" style="font-size:1rem;">terminal</span>
								</a>
								<button class="btn-icon" style="color:var(--error); border-color:var(--error);" title="Terminate" onclick={() => destroySandbox(sb.id)}>
									<span class="material-symbols-outlined" style="font-size:1rem;">delete</span>
								</button>
							</div>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{:else}
	<div class="empty-state card" style="margin-top:1rem;">
		<span class="material-symbols-outlined">cloud_off</span>
		<h3>No active sandboxes</h3>
		<p>Use the form above to provision a micro-VM</p>
	</div>
{/if}

<style>
	.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1.25rem; }
	h1 { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 700; }
	.count { font-size: 0.75rem; color: var(--on-surface-variant); background: var(--surface-container); padding: 0.2rem 0.6rem; border-radius: var(--radius-sm); }

	.create-panel { margin-bottom: 0; }
	.create-panel h3 { font-family: var(--font-headline); font-size: 0.9rem; font-weight: 600; margin-bottom: 0.75rem; }
	.create-fields { display: flex; gap: 0.75rem; align-items: flex-end; flex-wrap: wrap; }
	.field { display: flex; flex-direction: column; gap: 0.3rem; }
	.field label { font-size: 0.7rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--on-surface-variant); }
	.field select, .field input { min-width: 120px; height: 2.5rem; box-sizing: border-box; }
	.field-action { align-self: flex-end; }
	.field-action .btn-primary { height: 2.5rem; box-sizing: border-box; }

	.action-btns { display: flex; gap: 0.25rem; }

	@keyframes spin { to { transform: rotate(360deg); } }
	.spin { animation: spin 1s linear infinite; }
</style>
