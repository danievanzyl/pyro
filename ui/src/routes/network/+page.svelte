<script>
	import { apiFetch } from '$lib/auth.svelte.js';

	let health = $state(null);
	let sandboxes = $state([]);
	let error = $state('');

	async function fetchData() {
		try {
			const healthRes = await fetch('/api/health');
			health = await healthRes.json();

			const sbRes = await apiFetch('/sandboxes');
			if (sbRes.ok) sandboxes = await sbRes.json();
			error = '';
		} catch (e) { error = e.message; }
	}

	fetchData();
	setInterval(fetchData, 5000);
</script>

<div class="page-header">
	<div>
		<h1>Network Configuration</h1>
		<p class="subtitle">Bridge, TAP devices, and VM networking</p>
	</div>
</div>

{#if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
{/if}

<div class="net-grid">
	<div class="card">
		<h3>Bridge</h3>
		<dl>
			<dt>Interface</dt><dd class="mono">fcbr0</dd>
			<dt>Subnet</dt><dd class="mono">172.16.0.0/24</dd>
			<dt>Gateway</dt><dd class="mono">172.16.0.1</dd>
			<dt>NAT</dt><dd>Enabled (MASQUERADE)</dd>
		</dl>
	</div>

	<div class="card">
		<h3>Status</h3>
		<dl>
			<dt>Active VMs</dt><dd>{health?.active_sandboxes ?? '—'}</dd>
			<dt>TAP Devices</dt><dd>{sandboxes.length}</dd>
			<dt>IP Forwarding</dt><dd>Enabled</dd>
			<dt>Health</dt><dd style="color:#0a6b2a;">{health?.status === 'ok' ? 'Healthy' : '...'}</dd>
		</dl>
	</div>
</div>

{#if sandboxes.length > 0}
	<h2 style="margin: 1.5rem 0 0.75rem; font-family: var(--font-headline); font-size: 1rem; font-weight: 600;">TAP Devices</h2>
	<div class="card" style="padding:0; overflow:hidden;">
		<table>
			<thead>
				<tr>
					<th>TAP Device</th>
					<th>Sandbox ID</th>
					<th>Image</th>
					<th>State</th>
					<th>CID</th>
				</tr>
			</thead>
			<tbody>
				{#each sandboxes as sb}
					<tr>
						<td class="mono">{sb.tap_device || `tap-${sb.id.slice(0, 8)}`}</td>
						<td><a href="/sandboxes/{sb.id}" class="mono">{sb.id.slice(0, 12)}</a></td>
						<td>{sb.image}</td>
						<td>
							<span class="badge badge-{sb.state === 'running' ? 'running' : 'stopped'}">
								{sb.state.toUpperCase()}
							</span>
						</td>
						<td class="mono">{sb.vsock_cid}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{:else}
	<div class="empty-state card" style="margin-top:1.5rem;">
		<span class="material-symbols-outlined">lan</span>
		<h3>No active network devices</h3>
		<p>TAP devices will appear here when VMs are running</p>
	</div>
{/if}

<style>
	.page-header { margin-bottom: 1.25rem; }
	h1 { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.8rem; margin-top: 0.15rem; }

	.net-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 0.5rem; }
	.net-grid h3 { font-family: var(--font-headline); font-size: 0.9rem; font-weight: 600; margin-bottom: 0.75rem; }
	dl { display: grid; grid-template-columns: auto 1fr; gap: 0.4rem 1rem; font-size: 0.8rem; }
	dt { color: var(--on-surface-variant); font-weight: 500; }
	dd { color: var(--on-surface); }
</style>
