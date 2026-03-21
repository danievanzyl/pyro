<script>
	import { onMount } from 'svelte';

	let images = $state([]);
	let error = $state('');

	const apiKey = typeof localStorage !== 'undefined'
		? localStorage.getItem('fclk_api_key') || '' : '';

	async function refresh() {
		try {
			const res = await fetch('/api/images', {
				headers: { 'Authorization': `Bearer ${apiKey}` }
			});
			if (res.ok) images = await res.json();
		} catch (e) { error = e.message; }
	}

	onMount(refresh);

	function formatSize(bytes) {
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
		if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
		return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
	}
</script>

<div class="page-header">
	<div>
		<h1>Base Images</h1>
		<p class="subtitle">Rootfs + kernel pairs available for sandbox provisioning</p>
	</div>
</div>

{#if error}
	<div class="error-banner glass-panel">
		<span class="material-symbols-outlined">error</span> {error}
	</div>
{/if}

{#if images.length > 0}
	<div class="image-grid">
		{#each images as img}
			<div class="image-card glass-panel">
				<div class="image-icon">
					<span class="material-symbols-outlined">storage</span>
				</div>
				<div class="image-info">
					<div class="image-name">{img.name}</div>
					<div class="image-meta">{formatSize(img.size)} &middot; {new Date(img.created_at).toLocaleDateString()}</div>
				</div>
				<div class="image-badge">rootfs + kernel</div>
			</div>
		{/each}
	</div>
{:else}
	<div class="empty-state glass-panel">
		<span class="material-symbols-outlined" style="font-size: 3rem; color: var(--outline)">inventory_2</span>
		<h3>No images found</h3>
		<p>Add rootfs.ext4 + vmlinux pairs to the images directory on the host</p>
		<code>/opt/firecrackerlacker/images/{'{name}'}/rootfs.ext4</code>
	</div>
{/if}

<style>
	.page-header { margin-bottom: 2rem; }
	h1 { font-family: var(--font-headline); font-size: 1.75rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.85rem; margin-top: 0.25rem; }
	.error-banner { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem; margin-bottom: 1.5rem; color: var(--error); font-size: 0.85rem; }

	.image-grid { display: flex; flex-direction: column; gap: 0.75rem; }
	.image-card { display: flex; align-items: center; gap: 1rem; padding: 1.25rem; transition: background 0.15s; }
	.image-card:hover { background: rgba(39, 36, 49, 0.8); }
	.image-icon { width: 40px; height: 40px; border-radius: 0.5rem; background: rgba(163, 67, 231, 0.1); display: flex; align-items: center; justify-content: center; color: var(--primary); }
	.image-info { flex: 1; }
	.image-name { font-family: var(--font-headline); font-size: 0.95rem; font-weight: 600; }
	.image-meta { font-size: 0.75rem; color: var(--on-surface-variant); }
	.image-badge { padding: 0.25rem 0.6rem; border-radius: 9999px; background: var(--surface-container-highest); color: var(--on-surface-variant); font-size: 0.65rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; }

	.empty-state { display: flex; flex-direction: column; align-items: center; padding: 4rem; text-align: center; gap: 0.75rem; }
	.empty-state h3 { font-family: var(--font-headline); font-size: 1.1rem; }
	.empty-state p { color: var(--on-surface-variant); font-size: 0.85rem; }
	.empty-state code { display: block; margin-top: 0.5rem; padding: 0.5rem 1rem; background: var(--surface-container-low); border-radius: 0.5rem; font-size: 0.8rem; color: var(--primary); }
</style>
