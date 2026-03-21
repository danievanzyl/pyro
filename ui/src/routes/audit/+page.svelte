<script>
	import { onMount } from 'svelte';

	let entries = $state([]);
	let error = $state('');

	const apiKey = typeof localStorage !== 'undefined'
		? localStorage.getItem('fclk_api_key') || ''
		: '';

	async function refresh() {
		try {
			const res = await fetch('/api/audit?limit=100', {
				headers: { 'Authorization': `Bearer ${apiKey}` }
			});
			if (res.ok) entries = await res.json();
		} catch (e) {
			error = e.message;
		}
	}

	onMount(() => {
		refresh();
		const interval = setInterval(refresh, 10000);
		return () => clearInterval(interval);
	});

	function actionColor(action) {
		if (action.includes('created')) return 'created';
		if (action.includes('destroyed') || action.includes('expired')) return 'destroyed';
		if (action.includes('exec')) return 'exec';
		return '';
	}
</script>

<h1>Audit Log</h1>

{#if error}
	<div class="error">{error}</div>
{/if}

<table>
	<thead>
		<tr>
			<th>Time</th>
			<th>Action</th>
			<th>Sandbox</th>
			<th>Detail</th>
		</tr>
	</thead>
	<tbody>
		{#each entries as entry}
			<tr>
				<td class="time">{new Date(entry.timestamp).toLocaleString()}</td>
				<td class="action {actionColor(entry.action)}">{entry.action}</td>
				<td class="mono">{entry.sandbox_id ? entry.sandbox_id.slice(0, 12) : '—'}</td>
				<td class="detail">{entry.detail || '—'}</td>
			</tr>
		{:else}
			<tr><td colspan="4" class="empty">No audit entries yet</td></tr>
		{/each}
	</tbody>
</table>

<style>
	h1 { font-size: 1.5rem; margin-bottom: 1.5rem; }
	table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
	th, td { text-align: left; padding: 0.75rem; border-bottom: 1px solid #222; }
	th { color: #666; font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.05em; }
	.time { color: #666; font-size: 0.8rem; }
	.mono { font-family: inherit; color: #888; }
	.action { font-weight: 600; }
	.action.created { color: #22c55e; }
	.action.destroyed { color: #ef4444; }
	.action.exec { color: #3b82f6; }
	.detail { color: #888; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.empty { color: #444; text-align: center; padding: 2rem; }
	.error { background: #2d1b1b; border: 1px solid #5c2828; color: #ff6b6b; padding: 0.75rem; border-radius: 6px; margin-bottom: 1rem; font-size: 0.85rem; }
</style>
