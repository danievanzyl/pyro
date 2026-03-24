<script>
	import { apiFetch, hasApiKey } from '$lib/auth.svelte.js';

	let authenticated = $state(hasApiKey());
	let images = $state([]);
	let error = $state('');

	function formatSize(bytes) {
		if (bytes < 1024) return bytes + ' B';
		if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + ' KB';
		if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
		return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
	}

	function formatDate(d) {
		return new Date(d).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
	}

	async function refresh() {
		try {
			const res = await apiFetch('/images');
			if (res.ok) images = await res.json();
			error = '';
		} catch (e) { error = e.message; }
	}

	if (authenticated) refresh();
</script>

<div class="page-header">
	<div>
		<h1>Base Images</h1>
		<p class="subtitle">Rootfs images available for sandbox creation</p>
	</div>
</div>

{#if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
{/if}

{#if images.length > 0}
	<div class="card" style="padding:0; overflow:hidden;">
		<table>
			<thead>
				<tr>
					<th>Name</th>
					<th>Size</th>
					<th>Kernel</th>
					<th>Created</th>
				</tr>
			</thead>
			<tbody>
				{#each images as img}
					<tr>
						<td>
							<div class="image-name">
								<span class="material-symbols-outlined" style="font-size:1.1rem; color:var(--on-surface-variant);">inventory_2</span>
								<strong>{img.name}</strong>
							</div>
						</td>
						<td>{formatSize(img.size)}</td>
						<td class="mono" style="font-size:0.75rem;">{img.kernel_path ? 'vmlinux' : '—'}</td>
						<td>{img.created_at ? formatDate(img.created_at) : '—'}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{:else if !authenticated}
	<div class="empty-state card">
		<span class="material-symbols-outlined">key</span>
		<h3>Connect to view images</h3>
		<p>Enter your API key on the <a href="/">Fleet</a> page</p>
	</div>
{:else}
	<div class="empty-state card">
		<span class="material-symbols-outlined">inventory_2</span>
		<h3>No images built</h3>
		<p>Build images with the CLI</p>
		<code class="mono cmd">pyro build-image ubuntu</code>
	</div>
{/if}

<style>
	.page-header { margin-bottom: 1.25rem; }
	h1 { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.8rem; margin-top: 0.15rem; }

	.image-name { display: flex; align-items: center; gap: 0.5rem; }

	.cmd {
		display: inline-block;
		padding: 0.4rem 0.75rem;
		background: var(--surface-container);
		border-radius: var(--radius-sm);
		font-size: 0.8rem;
		color: var(--on-surface);
	}
</style>
