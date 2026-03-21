<script>
	import { onMount } from 'svelte';

	let health = $state(null);
	let sandboxes = $state([]);
	let error = $state('');
	let refreshInterval;

	const apiKey = typeof localStorage !== 'undefined'
		? localStorage.getItem('fclk_api_key') || ''
		: '';

	async function fetchData() {
		try {
			const healthRes = await fetch('/api/health');
			health = await healthRes.json();

			if (apiKey) {
				const sbRes = await fetch('/api/sandboxes', {
					headers: { 'Authorization': `Bearer ${apiKey}` }
				});
				if (sbRes.ok) sandboxes = await sbRes.json();
			}
			error = '';
		} catch (e) {
			error = e.message;
		}
	}

	onMount(() => {
		fetchData();
		refreshInterval = setInterval(fetchData, 5000);
		return () => clearInterval(refreshInterval);
	});
</script>

<h1>Dashboard</h1>

{#if error}
	<div class="error">{error}</div>
{/if}

<div class="grid">
	<div class="card">
		<div class="card-label">Status</div>
		<div class="card-value" class:ok={health?.status === 'ok'}>
			{health?.status ?? '...'}
		</div>
	</div>
	<div class="card">
		<div class="card-label">Active Sandboxes</div>
		<div class="card-value">{health?.active_sandboxes ?? '...'}</div>
	</div>
	<div class="card">
		<div class="card-label">Your Sandboxes</div>
		<div class="card-value">{sandboxes.length}</div>
	</div>
</div>

{#if !apiKey}
	<div class="setup">
		<p>Set your API key to see sandbox details:</p>
		<code>localStorage.setItem('fclk_api_key', 'your-key-here')</code>
	</div>
{/if}

{#if sandboxes.length > 0}
	<h2>Active Sandboxes</h2>
	<table>
		<thead>
			<tr>
				<th>ID</th>
				<th>Image</th>
				<th>State</th>
				<th>TTL Remaining</th>
				<th>Created</th>
			</tr>
		</thead>
		<tbody>
			{#each sandboxes as sb}
				<tr>
					<td><a href="/sandboxes/{sb.id}">{sb.id.slice(0, 8)}...</a></td>
					<td>{sb.image}</td>
					<td class="state" class:running={sb.state === 'running'}>{sb.state}</td>
					<td>{formatTTL(sb.expires_at)}</td>
					<td>{new Date(sb.created_at).toLocaleTimeString()}</td>
				</tr>
			{/each}
		</tbody>
	</table>
{/if}

<script>
	function formatTTL(expiresAt) {
		const remaining = new Date(expiresAt) - new Date();
		if (remaining <= 0) return 'expired';
		const mins = Math.floor(remaining / 60000);
		const secs = Math.floor((remaining % 60000) / 1000);
		return `${mins}m ${secs}s`;
	}
</script>

<style>
	h1 { font-size: 1.5rem; margin-bottom: 1.5rem; }
	h2 { font-size: 1.1rem; margin: 2rem 0 1rem; }

	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 1rem;
		margin-bottom: 2rem;
	}
	.card {
		background: #141414;
		border: 1px solid #222;
		border-radius: 8px;
		padding: 1.25rem;
	}
	.card-label {
		font-size: 0.75rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: #666;
		margin-bottom: 0.5rem;
	}
	.card-value {
		font-size: 1.5rem;
		font-weight: 700;
	}
	.card-value.ok { color: #22c55e; }

	.setup {
		background: #1a1a2e;
		border: 1px solid #333;
		border-radius: 8px;
		padding: 1.25rem;
		margin: 1rem 0;
	}
	.setup code {
		display: block;
		margin-top: 0.5rem;
		padding: 0.5rem;
		background: #0a0a0a;
		border-radius: 4px;
		font-size: 0.8rem;
		color: #ff6b35;
	}

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
		font-size: 0.75rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.state { font-weight: 600; }
	.state.running { color: #22c55e; }

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
