<script>
	import { page } from '$app/state';
	import { apiFetch, hasApiKey } from '$lib/auth.svelte.js';
	import { subscribe } from '$lib/events.svelte.js';

	const name = $derived(page.params.name);

	let image = $state(null);
	let loading = $state(true);
	let notFound = $state(false);
	let error = $state('');
	let authenticated = $state(hasApiKey());

	async function load() {
		loading = true;
		notFound = false;
		error = '';
		try {
			const res = await apiFetch(`/images/${encodeURIComponent(name)}`);
			if (res.status === 404) {
				notFound = true;
				image = null;
			} else if (!res.ok) {
				error = `Error ${res.status}`;
			} else {
				image = await res.json();
			}
		} catch (e) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	function patch(p) {
		image = image ? { ...image, ...p } : { name, ...p };
	}

	function formatSize(bytes) {
		if (!bytes) return '—';
		if (bytes < 1024) return bytes + ' B';
		if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + ' KB';
		if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
		return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
	}

	function formatDate(d) {
		if (!d) return '—';
		return new Date(d).toLocaleString();
	}

	function chipClass(status) {
		if (status === 'ready') return 'badge-running';
		if (status === 'failed') return 'badge-error';
		return 'badge-creating';
	}

	function chipLabel(status) {
		return (status || 'ready').toUpperCase();
	}

	const ready = $derived(image && (image.status === 'ready' || !image.status));
	const labels = $derived((image && image.labels) || {});
	const labelKeys = $derived(Object.keys(labels).sort());

	$effect(() => {
		// re-load on name change (deep-link / nav between detail pages).
		if (authenticated && name) load();
	});

	$effect(() => {
		// SSE handlers gated on the current name. Closed-over `name` is fine
		// because $effect re-runs and unsubs on name change.
		const target = name;
		const unsubs = [
			subscribe('image.extracting', (e) => {
				const d = JSON.parse(e.data);
				if (d.name !== target) return;
				patch({ status: 'extracting' });
			}),
			subscribe('image.ready', (e) => {
				const d = JSON.parse(e.data);
				if (d.name !== target) return;
				// payload is partial; refetch to hydrate labels/kernel/size/etc.
				load();
			}),
			subscribe('image.failed', (e) => {
				const d = JSON.parse(e.data);
				if (d.name !== target) return;
				patch({ status: 'failed', error: d.error || '' });
			}),
		];
		return () => unsubs.forEach((u) => u());
	});
</script>

{#if !authenticated}
	<div class="empty-state card">
		<span class="material-symbols-outlined">key</span>
		<h3>Connect to view image</h3>
		<p>Enter your API key on the <a href="/">Fleet</a> page</p>
	</div>
{:else if loading}
	<div class="loading">Loading...</div>
{:else if notFound}
	<div class="empty-state card">
		<span class="material-symbols-outlined">inventory_2</span>
		<h3>Image not found</h3>
		<p>No image named <code class="mono">{name}</code></p>
		<a href="/images" class="back-link">&larr; Back to Images</a>
	</div>
{:else if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
	<a href="/images" class="back-link">&larr; Back to Images</a>
{:else if image}
	<div class="detail-header">
		<div class="detail-title">
			<a href="/images" class="back-link">&larr;</a>
			<span class="material-symbols-outlined" style="font-size:1.25rem; color:var(--on-surface-variant);">inventory_2</span>
			<strong class="img-name">{image.name}</strong>
			<span
				class="badge {chipClass(image.status)}"
				title={image.status === 'failed' ? image.error || 'Pull failed' : ''}
			>
				{chipLabel(image.status)}
			</span>
			<span class="detail-meta">
				{ready ? formatSize(image.size) : '—'}
				&middot;
				{ready ? formatDate(image.created_at) : '—'}
			</span>
		</div>
	</div>

	{#if image.status === 'failed' && image.error}
		<div class="error-banner">
			<span class="material-symbols-outlined">error</span>
			{image.error}
		</div>
	{/if}

	{#if labelKeys.length > 0}
		<div class="card section">
			<h4>OCI Labels</h4>
			<dl class="labels">
				{#each labelKeys as k (k)}
					<dt class="mono">{k}</dt><dd class="mono">{labels[k]}</dd>
				{/each}
			</dl>
		</div>
	{/if}

	<div class="card section">
		<h4>Details</h4>
		<dl>
			{#if image.source}
				<dt>Source</dt><dd class="mono">{image.source}</dd>
			{/if}
			{#if image.digest}
				<dt>Digest</dt><dd class="mono digest">{image.digest}</dd>
			{/if}
			{#if image.kernel_path}
				<dt>Kernel</dt><dd class="mono">{image.kernel_path}</dd>
			{/if}
			{#if image.rootfs_path}
				<dt>Rootfs</dt><dd class="mono">{image.rootfs_path}</dd>
			{/if}
		</dl>
	</div>
{/if}

<style>
	.detail-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
	.detail-title { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
	.back-link { color: var(--on-surface-variant); text-decoration: none; font-size: 1.1rem; }
	.back-link:hover { color: var(--on-surface); text-decoration: none; }
	.img-name { font-family: var(--font-headline); font-size: 1.1rem; }
	.detail-meta { color: var(--on-surface-variant); font-size: 0.8rem; }

	.loading { color: var(--on-surface-variant); padding: 2rem; text-align: center; }

	.section { margin-bottom: 1rem; }
	.section h4 {
		font-family: var(--font-headline);
		font-size: 0.85rem;
		font-weight: 600;
		margin-bottom: 0.75rem;
	}
	dl { display: grid; grid-template-columns: auto 1fr; gap: 0.4rem 1rem; font-size: 0.8rem; }
	dt { color: var(--on-surface-variant); font-weight: 500; }
	dd { color: var(--on-surface); word-break: break-all; }
	dl.labels dt { font-size: 0.75rem; }
	dl.labels dd { font-size: 0.75rem; }
	.digest { font-size: 0.7rem; }
</style>
