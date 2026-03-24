<script>
	import { apiFetch, hasApiKey, getApiKey } from '$lib/auth.svelte.js';

	let authenticated = $state(hasApiKey());
	let entries = $state([]);
	let error = $state('');
	let severityFilter = $state('ALL');
	let filtered = $derived(
		severityFilter === 'ALL' ? entries : entries.filter(e => severity(e.action) === severityFilter)
	);
	let stats = $derived({
		total: entries.length,
		errors: entries.filter(e => severity(e.action) === 'ERROR').length,
		warns: entries.filter(e => severity(e.action) === 'WARN').length,
	});

	function severity(action) {
		if (action.includes('error') || action.includes('expired')) return 'ERROR';
		if (action.includes('destroyed') || action.includes('file')) return 'WARN';
		return 'INFO';
	}

	function severityColor(sev) {
		if (sev === 'ERROR') return 'var(--error)';
		if (sev === 'WARN') return '#765b00';
		return 'var(--on-surface-variant)';
	}

	function actionIcon(action) {
		if (action.includes('created')) return 'add_circle';
		if (action.includes('destroyed') || action.includes('expired')) return 'remove_circle';
		if (action.includes('exec')) return 'terminal';
		if (action.includes('file')) return 'description';
		return 'info';
	}

	function formatTime(ts) {
		return new Date(ts).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
	}

	function formatDate(ts) {
		return new Date(ts).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	}

	async function refresh() {
		try {
			const res = await apiFetch('/audit?limit=200');
			if (res.ok) entries = await res.json();
			error = '';
		} catch (e) { error = e.message; }
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

	if (authenticated) {
		refresh();
		connectSSE();
		setInterval(refresh, 15000); // Fallback polling (SSE handles real-time)
	}
</script>

<div class="page-header">
	<div>
		<h1>System Logs</h1>
		<p class="subtitle">Audit trail and event log</p>
	</div>
	<div class="live-indicator">
		<span class="status-dot running pulse"></span>
		Live
	</div>
</div>

{#if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
{/if}

{#if !authenticated}
<div class="empty-state card">
	<span class="material-symbols-outlined">key</span>
	<h3>Connect to view logs</h3>
	<p>Enter your API key on the <a href="/">Fleet</a> page</p>
</div>
{:else}
<div class="controls">
	<div class="filters">
		{#each ['ALL', 'INFO', 'WARN', 'ERROR'] as sev}
			<button
				class="filter-btn"
				class:active={severityFilter === sev}
				onclick={() => severityFilter = sev}
			>
				{sev}
				{#if sev === 'ERROR' && stats.errors > 0}
					<span class="filter-count error">{stats.errors}</span>
				{:else if sev === 'WARN' && stats.warns > 0}
					<span class="filter-count warn">{stats.warns}</span>
				{:else if sev === 'ALL'}
					<span class="filter-count">{stats.total}</span>
				{/if}
			</button>
		{/each}
	</div>
</div>

<div class="log-layout">
	<div class="log-viewer card" style="padding:0; overflow:hidden;">
		<div class="log-header">
			<span class="mono" style="font-size:0.75rem;">audit.log</span>
			<span class="log-count">{filtered.length} entries</span>
		</div>
		<div class="log-content">
			{#if filtered.length === 0}
				<div class="empty-state" style="padding:2rem;">
					<span class="material-symbols-outlined">receipt_long</span>
					<h3>No audit entries yet</h3>
					<p>Activity will appear here as sandboxes are created and used</p>
				</div>
			{:else}
				{#each filtered as entry}
					<div class="log-line">
						<span class="log-time mono">{formatDate(entry.timestamp)} {formatTime(entry.timestamp)}</span>
						<span class="log-severity" style="color: {severityColor(severity(entry.action))}">
							{severity(entry.action)}
						</span>
						<span class="material-symbols-outlined log-icon" style="font-size:0.9rem; color: {severityColor(severity(entry.action))}">
							{actionIcon(entry.action)}
						</span>
						<span class="log-action">{entry.action}</span>
						{#if entry.sandbox_id}
							<a href="/sandboxes/{entry.sandbox_id}" class="log-id mono">{entry.sandbox_id.slice(0, 8)}</a>
						{/if}
						{#if entry.detail}
							<span class="log-detail">{entry.detail}</span>
						{/if}
					</div>
				{/each}
			{/if}
		</div>
	</div>
</div>
{/if}

<style>
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 1.25rem; }
	h1 { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.8rem; margin-top: 0.15rem; }
	.live-indicator { display: flex; align-items: center; gap: 0.4rem; font-size: 0.75rem; font-weight: 600; color: #0a6b2a; }

	.controls { margin-bottom: 1rem; }
	.filters { display: flex; gap: 0.25rem; }
	.filter-btn {
		display: flex; align-items: center; gap: 0.3rem;
		padding: 0.35rem 0.75rem; border-radius: var(--radius-sm);
		font-size: 0.75rem; font-weight: 600;
		background: var(--surface-container); color: var(--on-surface-variant);
		cursor: pointer; border: 1px solid transparent;
	}
	.filter-btn:hover { background: var(--surface-container-high); }
	.filter-btn.active { background: var(--primary); color: #ffffff; }
	.filter-count { font-size: 0.65rem; background: rgba(0,0,0,0.1); padding: 0.05rem 0.35rem; border-radius: 9999px; }
	.filter-count.error { background: rgba(186,26,26,0.15); color: var(--error); }
	.filter-count.warn { background: rgba(255,199,3,0.2); color: #765b00; }

	.log-header {
		display: flex; align-items: center; justify-content: space-between;
		padding: 0.5rem 0.75rem; background: var(--surface-container-high);
		border-bottom: 1px solid var(--surface-container);
	}
	.log-count { font-size: 0.7rem; color: var(--on-surface-variant); }

	.log-content {
		max-height: 600px; overflow-y: auto;
		font-family: var(--font-mono); font-size: 0.78rem; line-height: 1.6;
	}

	.log-line {
		display: flex; align-items: center; gap: 0.5rem;
		padding: 0.3rem 0.75rem; border-bottom: 1px solid var(--surface-container-low);
	}
	.log-line:hover { background: var(--surface-container-low); }
	.log-time { color: var(--outline); font-size: 0.72rem; white-space: nowrap; }
	.log-severity { font-size: 0.65rem; font-weight: 700; width: 38px; text-align: center; font-family: var(--font-mono); }
	.log-action { font-weight: 500; color: var(--on-surface); }
	.log-id { font-size: 0.72rem; color: var(--primary); }
	.log-detail { color: var(--on-surface-variant); font-size: 0.72rem; }
</style>
