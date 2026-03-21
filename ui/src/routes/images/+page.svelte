<script>
	import { onMount } from 'svelte';

	let images = $state([]);
	let error = $state('');

	const apiKey = typeof localStorage !== 'undefined'
		? localStorage.getItem('fclk_api_key') || ''
		: '';

	async function refresh() {
		try {
			const res = await fetch('/api/images', {
				headers: { 'Authorization': `Bearer ${apiKey}` }
			});
			if (res.ok) images = await res.json();
		} catch (e) {
			error = e.message;
		}
	}

	onMount(refresh);

	function formatSize(bytes) {
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
		if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
		return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
	}
</script>

<h1>Base Images</h1>

{#if error}
	<div class="error">{error}</div>
{/if}

<table>
	<thead>
		<tr>
			<th>Name</th>
			<th>Size</th>
			<th>Created</th>
		</tr>
	</thead>
	<tbody>
		{#each images as img}
			<tr>
				<td class="name">{img.name}</td>
				<td>{formatSize(img.size)}</td>
				<td>{new Date(img.created_at).toLocaleString()}</td>
			</tr>
		{:else}
			<tr><td colspan="3" class="empty">No images found. Add rootfs + kernel to the images directory.</td></tr>
		{/each}
	</tbody>
</table>

<style>
	h1 { font-size: 1.5rem; margin-bottom: 1.5rem; }
	table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
	th, td { text-align: left; padding: 0.75rem; border-bottom: 1px solid #222; }
	th { color: #666; font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.05em; }
	.name { color: #ff6b35; font-weight: 600; }
	.empty { color: #444; text-align: center; padding: 2rem; }
	.error { background: #2d1b1b; border: 1px solid #5c2828; color: #ff6b6b; padding: 0.75rem; border-radius: 6px; margin-bottom: 1rem; font-size: 0.85rem; }
</style>
