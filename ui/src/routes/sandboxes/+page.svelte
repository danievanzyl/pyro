<script>
	import { onMount } from 'svelte';

	let sandboxes = $state([]);
	let error = $state('');
	let creating = $state(false);
	let newTTL = $state(3600);
	let newImage = $state('default');

	const apiKey = typeof localStorage !== 'undefined'
		? localStorage.getItem('fclk_api_key') || ''
		: '';

	async function apiFetch(path, opts = {}) {
		return fetch(`/api${path}`, {
			...opts,
			headers: {
				'Authorization': `Bearer ${apiKey}`,
				'Content-Type': 'application/json',
				...opts.headers
			}
		});
	}

	async function refresh() {
		try {
			const res = await apiFetch('/sandboxes');
			if (res.ok) sandboxes = await res.json();
			error = '';
		} catch (e) {
			error = e.message;
		}
	}

	async function createSandbox() {
		creating = true;
		try {
			const res = await apiFetch('/sandboxes', {
				method: 'POST',
				body: JSON.stringify({ ttl: newTTL, image: newImage })
			});
			if (!res.ok) {
				const body = await res.json();
				throw new Error(body.error);
			}
			await refresh();
		} catch (e) {
			error = e.message;
		} finally {
			creating = false;
		}
	}

	async function destroySandbox(id) {
		try {
			await apiFetch(`/sandboxes/${id}`, { method: 'DELETE' });
			await refresh();
		} catch (e) {
			error = e.message;
		}
	}

	onMount(() => {
		refresh();
		const interval = setInterval(refresh, 5000);
		return () => clearInterval(interval);
	});
</script>

<h1>Sandboxes</h1>

{#if error}
	<div class="error">{error}</div>
{/if}

<div class="create-form">
	<div class="field">
		<label for="ttl">TTL (seconds)</label>
		<input id="ttl" type="number" bind:value={newTTL} min="10" max="86400" />
	</div>
	<div class="field">
		<label for="image">Image</label>
		<input id="image" type="text" bind:value={newImage} placeholder="default" />
	</div>
	<button onclick={createSandbox} disabled={creating}>
		{creating ? 'Creating...' : 'Create Sandbox'}
	</button>
</div>

<table>
	<thead>
		<tr>
			<th>ID</th>
			<th>Image</th>
			<th>State</th>
			<th>PID</th>
			<th>CID</th>
			<th>Expires</th>
			<th></th>
		</tr>
	</thead>
	<tbody>
		{#each sandboxes as sb}
			<tr>
				<td class="mono">{sb.id.slice(0, 12)}</td>
				<td>{sb.image}</td>
				<td class:running={sb.state === 'running'} class:creating={sb.state === 'creating'}>
					{sb.state}
				</td>
				<td class="mono">{sb.pid || '—'}</td>
				<td class="mono">{sb.vsock_cid || '—'}</td>
				<td>{new Date(sb.expires_at).toLocaleString()}</td>
				<td>
					<button class="btn-danger" onclick={() => destroySandbox(sb.id)}>Kill</button>
				</td>
			</tr>
		{:else}
			<tr><td colspan="7" class="empty">No sandboxes running</td></tr>
		{/each}
	</tbody>
</table>

<style>
	h1 { font-size: 1.5rem; margin-bottom: 1.5rem; }

	.create-form {
		display: flex;
		gap: 1rem;
		align-items: end;
		margin-bottom: 2rem;
		padding: 1.25rem;
		background: #141414;
		border: 1px solid #222;
		border-radius: 8px;
	}
	.field {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}
	.field label {
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: #666;
	}
	input {
		background: #0a0a0a;
		border: 1px solid #333;
		color: #e0e0e0;
		padding: 0.5rem 0.75rem;
		border-radius: 4px;
		font-family: inherit;
		font-size: 0.85rem;
		width: 150px;
	}
	button {
		background: #ff6b35;
		color: #fff;
		border: none;
		padding: 0.5rem 1rem;
		border-radius: 4px;
		cursor: pointer;
		font-family: inherit;
		font-size: 0.85rem;
		font-weight: 600;
	}
	button:hover { background: #e85d2c; }
	button:disabled { opacity: 0.5; cursor: not-allowed; }

	.btn-danger {
		background: #dc2626;
		font-size: 0.75rem;
		padding: 0.3rem 0.6rem;
	}
	.btn-danger:hover { background: #b91c1c; }

	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.85rem;
	}
	th, td {
		text-align: left;
		padding: 0.75rem;
		border-bottom: 1px solid #222;
	}
	th {
		color: #666;
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.mono { font-family: inherit; color: #888; }
	.running { color: #22c55e; font-weight: 600; }
	.creating { color: #eab308; font-weight: 600; }
	.empty { color: #444; text-align: center; padding: 2rem; }

	.error {
		background: #2d1b1b;
		border: 1px solid #5c2828;
		color: #ff6b6b;
		padding: 0.75rem;
		border-radius: 6px;
		margin-bottom: 1rem;
		font-size: 0.85rem;
	}
</style>
