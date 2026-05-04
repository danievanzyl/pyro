<script>
	import { apiFetch, hasApiKey } from '$lib/auth.svelte.js';
	import { subscribe } from '$lib/events.svelte.js';
	import CreateImageModal from '$lib/CreateImageModal.svelte';

	let authenticated = $state(hasApiKey());
	let images = $state([]);
	let error = $state('');
	let modalOpen = $state(false);
	let prefill = $state(null);

	function openCreate() { prefill = null; modalOpen = true; }

	// Retry: reopen modal pre-filled with the failed row's name+source and
	// force=true. Modal resets state on open transition and honors prefill.
	function retry(img) {
		prefill = { name: img.name, source: img.source || '', force: true };
		modalOpen = true;
	}

	// 200/202 path: unshift returned ImageInfo (or replace by name).
	// SSE handlers drive subsequent state transitions for 202.
	function onCreated(info) {
		if (!info || !info.name) return;
		const i = images.findIndex((x) => x.name === info.name);
		if (i >= 0) images[i] = info;
		else images = [info, ...images];
	}

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

	// Pull a single image's full ImageInfo and merge into the list.
	// SSE image.ready payload only carries {name,digest,size}; size/kernel/
	// created_at/labels live on disk and need a refetch.
	async function hydrate(name) {
		try {
			const res = await apiFetch(`/images/${encodeURIComponent(name)}`);
			if (!res.ok) return;
			const info = await res.json();
			const i = images.findIndex((x) => x.name === name);
			if (i >= 0) images[i] = info;
			else images = [info, ...images];
		} catch {}
	}

	function upsert(name, patch) {
		const i = images.findIndex((x) => x.name === name);
		if (i >= 0) {
			images[i] = { ...images[i], ...patch };
		} else {
			images = [{ name, ...patch }, ...images];
		}
	}

	function chipClass(status) {
		if (status === 'ready') return 'badge-running';
		if (status === 'failed') return 'badge-error';
		return 'badge-creating'; // pulling | extracting | empty
	}

	function chipLabel(status) {
		return (status || 'ready').toUpperCase();
	}

	$effect(() => {
		const unsubs = [
			subscribe('image.pulling', (e) => {
				const d = JSON.parse(e.data);
				upsert(d.name, { status: 'pulling', source: d.source, error: '' });
			}),
			subscribe('image.extracting', (e) => {
				const d = JSON.parse(e.data);
				upsert(d.name, { status: 'extracting' });
			}),
			subscribe('image.ready', (e) => {
				const d = JSON.parse(e.data);
				hydrate(d.name);
			}),
			subscribe('image.failed', (e) => {
				const d = JSON.parse(e.data);
				upsert(d.name, { status: 'failed', error: d.error || '' });
			}),
			// image.force_replaced ignored — image.ready already triggered hydrate.
		];
		return () => unsubs.forEach((u) => u());
	});

	if (authenticated) refresh();
</script>

<div class="page-header">
	<div>
		<h1>Base Images</h1>
		<p class="subtitle">Rootfs images available for sandbox creation</p>
	</div>
	{#if authenticated}
		<button class="btn-primary" onclick={openCreate}>
			<span class="material-symbols-outlined" style="font-size:1rem;">add</span>
			Create image
		</button>
	{/if}
</div>

<CreateImageModal bind:open={modalOpen} {images} {prefill} {onCreated} />

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
					<th>Status</th>
					<th>Size</th>
					<th>Kernel</th>
					<th>Created</th>
				</tr>
			</thead>
			<tbody>
				{#each images as img (img.name)}
					{@const ready = img.status === 'ready' || !img.status}
					<tr>
						<td>
							<div class="image-name">
								<span class="material-symbols-outlined" style="font-size:1.1rem; color:var(--on-surface-variant);">inventory_2</span>
								<strong>{img.name}</strong>
							</div>
						</td>
						<td>
							<div class="status-cell">
								<span
									class="badge {chipClass(img.status)}"
									title={img.status === 'failed' ? img.error || 'Pull failed' : ''}
								>
									{chipLabel(img.status)}
								</span>
								{#if img.status === 'failed'}
									<button
										class="btn-icon retry"
										title="Retry pull"
										aria-label="Retry pull"
										onclick={() => retry(img)}
									>
										<span class="material-symbols-outlined" style="font-size:1rem;">refresh</span>
									</button>
								{/if}
							</div>
						</td>
						<td>{ready ? formatSize(img.size) : '—'}</td>
						<td class="mono" style="font-size:0.75rem;">{ready && img.kernel_path ? 'vmlinux' : '—'}</td>
						<td>{ready && img.created_at ? formatDate(img.created_at) : '—'}</td>
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
	.page-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		margin-bottom: 1.25rem;
	}
	h1 { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.8rem; margin-top: 0.15rem; }

	.image-name { display: flex; align-items: center; gap: 0.5rem; }
	.status-cell { display: flex; align-items: center; gap: 0.5rem; }
	.retry { width: 1.75rem; height: 1.75rem; }

	.cmd {
		display: inline-block;
		padding: 0.4rem 0.75rem;
		background: var(--surface-container);
		border-radius: var(--radius-sm);
		font-size: 0.8rem;
		color: var(--on-surface);
	}
</style>
