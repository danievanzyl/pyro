<script>
	import { onMount } from 'svelte';
	import { apiFetch } from '$lib/auth.svelte.js';

	let entries = $state([]);
	let error = $state('');
	let severityFilter = $state('ALL');

	async function refresh() {
		try {
			const res = await apiFetch('/audit?limit=200');
			if (res.ok) entries = await res.json();
		} catch (e) { error = e.message; }
	}

	onMount(() => { refresh(); const i = setInterval(refresh, 5000); return () => clearInterval(i); });

	function actionSeverity(action) {
		if (action.includes('error') || action.includes('expired')) return 'ERROR';
		if (action.includes('destroyed') || action.includes('file')) return 'WARN';
		return 'INFO';
	}

	function severityColor(sev) {
		if (sev === 'ERROR') return 'var(--error)';
		if (sev === 'WARN') return 'var(--secondary)';
		return 'var(--on-surface-variant)';
	}

	function actionIcon(action) {
		if (action.includes('created')) return 'add_circle';
		if (action.includes('destroyed') || action.includes('expired')) return 'remove_circle';
		if (action.includes('exec')) return 'terminal';
		if (action.includes('file')) return 'description';
		return 'info';
	}

	let filtered = $derived(
		severityFilter === 'ALL' ? entries
		: entries.filter(e => actionSeverity(e.action) === severityFilter)
	);

	let stats = $derived({
		total: entries.length,
		errors: entries.filter(e => actionSeverity(e.action) === 'ERROR').length,
		warns: entries.filter(e => actionSeverity(e.action) === 'WARN').length,
	});
</script>

<div class="page-header">
	<div>
		<h1>System Logs</h1>
		<p class="subtitle">Real-time observability and event correlation for Firecracker microVMs</p>
	</div>
	<div class="live-badge">
		<span class="live-dot"></span>
		Live Monitoring Active
	</div>
</div>

{#if error}
	<div class="error-banner glass-panel">
		<span class="material-symbols-outlined">error</span> {error}
	</div>
{/if}

<div class="controls">
	<div class="severity-filters">
		<span class="filter-label">Severity</span>
		{#each ['ALL', 'INFO', 'WARN', 'ERROR'] as sev}
			<button
				class="filter-btn"
				class:active={severityFilter === sev}
				onclick={() => severityFilter = sev}
			>{sev}</button>
		{/each}
	</div>
</div>

<div class="log-layout">
	<div class="log-stats glass-panel">
		<h3>Log Statistics</h3>
		<div class="stat-row">
			<div class="stat-item">
				<span class="stat-num">{stats.total}</span>
				<span class="stat-lbl">Total Events</span>
			</div>
			<div class="stat-item">
				<span class="stat-num" style="color: var(--error)">{stats.errors}</span>
				<span class="stat-lbl">Errors</span>
			</div>
			<div class="stat-item">
				<span class="stat-num" style="color: var(--secondary)">{stats.warns}</span>
				<span class="stat-lbl">Warnings</span>
			</div>
		</div>
	</div>

	<div class="log-viewer glass-panel">
		<div class="log-tabs">
			<button class="tab active">audit.log</button>
		</div>

		<div class="log-content">
			{#each filtered as entry}
				{@const sev = actionSeverity(entry.action)}
				<div class="log-line">
					<span class="log-time">{new Date(entry.timestamp).toLocaleTimeString()}</span>
					<span class="log-sev" style="color: {severityColor(sev)}">{sev}</span>
					<span class="material-symbols-outlined log-icon" style="color: {severityColor(sev)}; font-size: 0.9rem;">{actionIcon(entry.action)}</span>
					<span class="log-action">{entry.action}</span>
					{#if entry.sandbox_id}
						<span class="log-sandbox">[{entry.sandbox_id.slice(0, 8)}]</span>
					{/if}
					{#if entry.detail}
						<span class="log-detail">{entry.detail}</span>
					{/if}
				</div>
			{:else}
				<div class="empty-log">
					<span class="material-symbols-outlined" style="font-size: 2rem; color: var(--outline)">receipt_long</span>
					<p>No log entries yet. Sandbox activity will appear here.</p>
				</div>
			{/each}
		</div>
	</div>
</div>

<style>
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 1.5rem; }
	h1 { font-family: var(--font-headline); font-size: 1.75rem; font-weight: 700; }
	.subtitle { color: var(--on-surface-variant); font-size: 0.85rem; margin-top: 0.25rem; }
	.live-badge { display: flex; align-items: center; gap: 0.5rem; padding: 0.35rem 0.75rem; border-radius: 9999px; background: rgba(181, 255, 194, 0.1); color: var(--tertiary); font-size: 0.75rem; font-weight: 600; }
	.live-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--tertiary); animation: pulse 2s infinite; }
	@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

	.error-banner { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem; margin-bottom: 1.5rem; color: var(--error); font-size: 0.85rem; }

	.controls { margin-bottom: 1.5rem; }
	.severity-filters { display: flex; align-items: center; gap: 0.5rem; }
	.filter-label { font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.08em; color: var(--on-surface-variant); margin-right: 0.25rem; }
	.filter-btn {
		padding: 0.35rem 0.75rem; border-radius: 0.5rem; font-size: 0.75rem; font-weight: 600;
		background: var(--surface-container); color: var(--on-surface-variant);
	}
	.filter-btn:hover { background: var(--surface-container-high); }
	.filter-btn.active { background: rgba(163, 67, 231, 0.15); color: var(--primary); }

	.log-layout { display: grid; grid-template-columns: 240px 1fr; gap: 1rem; }
	.log-stats { padding: 1.25rem; height: fit-content; }
	.log-stats h3 { font-family: var(--font-headline); font-size: 0.8rem; font-weight: 600; margin-bottom: 1rem; color: var(--on-surface-variant); }
	.stat-row { display: flex; flex-direction: column; gap: 1rem; }
	.stat-item { display: flex; flex-direction: column; }
	.stat-num { font-family: var(--font-headline); font-size: 1.5rem; font-weight: 800; }
	.stat-lbl { font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.08em; color: var(--on-surface-variant); }

	.log-viewer { display: flex; flex-direction: column; min-height: 500px; }
	.log-tabs { padding: 0.75rem 1rem 0; display: flex; gap: 0.5rem; border-bottom: 1px solid rgba(73, 70, 81, 0.2); }
	.tab {
		padding: 0.5rem 1rem; border-radius: 0.5rem 0.5rem 0 0; font-size: 0.8rem; font-weight: 600;
		background: transparent; color: var(--on-surface-variant);
	}
	.tab.active { background: var(--surface-container-high); color: var(--primary); }

	.log-content { flex: 1; padding: 0.75rem 1rem; overflow-y: auto; max-height: 600px; font-size: 0.8rem; font-family: 'SF Mono', 'Fira Code', monospace; }
	.log-line {
		display: flex; align-items: center; gap: 0.5rem; padding: 0.35rem 0.5rem;
		border-radius: 0.25rem; transition: background 0.1s;
	}
	.log-line:hover { background: rgba(255, 255, 255, 0.03); }
	.log-time { color: var(--outline); min-width: 80px; font-size: 0.75rem; }
	.log-sev { min-width: 40px; font-size: 0.7rem; font-weight: 700; }
	.log-action { color: var(--on-surface); font-weight: 500; }
	.log-sandbox { color: var(--primary-dim); font-size: 0.75rem; }
	.log-detail { color: var(--on-surface-variant); font-size: 0.75rem; margin-left: auto; max-width: 250px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

	.empty-log { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 4rem; gap: 0.75rem; text-align: center; }
	.empty-log p { color: var(--on-surface-variant); font-size: 0.85rem; font-family: var(--font-body); }
</style>
