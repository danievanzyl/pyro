<script>
	import { onMount } from 'svelte';

	let health = $state(null);
	let sandboxes = $state([]);
	let error = $state('');
	let apiKeyInput = $state('');
	let refreshInterval;

	const getKey = () => typeof localStorage !== 'undefined' ? localStorage.getItem('fclk_api_key') || '' : '';
	let apiKey = $state(getKey());

	function saveKey() {
		if (apiKeyInput.trim()) {
			localStorage.setItem('fclk_api_key', apiKeyInput.trim());
			apiKey = apiKeyInput.trim();
			fetchData();
		}
	}

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

	function formatTTL(expiresAt) {
		const remaining = new Date(expiresAt) - new Date();
		if (remaining <= 0) return 'expired';
		const mins = Math.floor(remaining / 60000);
		const secs = Math.floor((remaining % 60000) / 1000);
		if (mins > 60) return `${Math.floor(mins / 60)}h ${mins % 60}m`;
		return `${mins}m ${secs}s`;
	}

	function stateColor(state) {
		if (state === 'running') return 'var(--tertiary)';
		if (state === 'creating') return 'var(--primary)';
		if (state === 'stopping') return 'var(--secondary)';
		return 'var(--on-surface-variant)';
	}

	onMount(() => {
		fetchData();
		refreshInterval = setInterval(fetchData, 3000);
		return () => clearInterval(refreshInterval);
	});
</script>

<div class="page-header">
	<div>
		<h1>Fleet Overview</h1>
		<p class="subtitle">Real-time monitoring for Firecracker microVM sandboxes</p>
	</div>
	{#if health?.status === 'ok'}
		<div class="live-badge">
			<span class="live-dot"></span>
			Live
		</div>
	{/if}
</div>

{#if error}
	<div class="error-banner glass-panel">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
{/if}

{#if !apiKey}
	<div class="setup-card glass-panel">
		<span class="material-symbols-outlined setup-icon">key</span>
		<h3>Connect to your cluster</h3>
		<p>Enter your API key to monitor sandboxes</p>
		<div class="key-input-row">
			<input type="password" bind:value={apiKeyInput} placeholder="fclk_..." />
			<button class="btn-primary" onclick={saveKey}>Connect</button>
		</div>
	</div>
{:else}
	<div class="metrics-grid">
		<div class="metric-card glass-panel">
			<div class="metric-label">System Status</div>
			<div class="metric-value" style="color: var(--tertiary)">
				{health?.status === 'ok' ? 'Healthy' : '...'}
			</div>
			<div class="metric-sparkline">
				<span class="material-symbols-outlined" style="color: var(--tertiary); font-size: 1rem;">check_circle</span>
				All systems operational
			</div>
		</div>
		<div class="metric-card glass-panel">
			<div class="metric-label">Active Micro-VMs</div>
			<div class="metric-value">{health?.active_sandboxes ?? '—'}</div>
			<div class="metric-sparkline">
				<span class="material-symbols-outlined" style="font-size: 1rem;">trending_flat</span>
				across cluster
			</div>
		</div>
		<div class="metric-card glass-panel">
			<div class="metric-label">Your Sandboxes</div>
			<div class="metric-value">{sandboxes.length}</div>
			<div class="metric-sparkline">
				<span class="material-symbols-outlined" style="font-size: 1rem;">person</span>
				owned by this key
			</div>
		</div>
		<div class="metric-card glass-panel">
			<div class="metric-label">Avg TTL Remaining</div>
			<div class="metric-value">
				{#if sandboxes.length > 0}
					{formatTTL(sandboxes.reduce((acc, s) => {
						const r = new Date(s.expires_at) - new Date();
						return r > 0 ? acc + r : acc;
					}, 0) / sandboxes.length + new Date().getTime())}
				{:else}
					—
				{/if}
			</div>
			<div class="metric-sparkline">
				<span class="material-symbols-outlined" style="font-size: 1rem;">timer</span>
				average lifetime
			</div>
		</div>
	</div>

	<div class="section-header">
		<h2>VM Instances</h2>
		<a href="/sandboxes" class="view-all">View all <span class="material-symbols-outlined" style="font-size: 1rem;">arrow_forward</span></a>
	</div>

	{#if sandboxes.length > 0}
		<div class="vm-list">
			{#each sandboxes as sb}
				<div class="vm-row glass-panel">
					<div class="vm-status">
						<span class="status-dot" style="background: {stateColor(sb.state)}"></span>
					</div>
					<div class="vm-info">
						<div class="vm-id">{sb.id.slice(0, 12)}</div>
						<div class="vm-meta">{sb.image} &middot; CID {sb.vsock_cid}</div>
					</div>
					<div class="vm-stat">
						<div class="vm-stat-label">State</div>
						<div class="vm-stat-value" style="color: {stateColor(sb.state)}">{sb.state}</div>
					</div>
					<div class="vm-stat">
						<div class="vm-stat-label">PID</div>
						<div class="vm-stat-value">{sb.pid || '—'}</div>
					</div>
					<div class="vm-stat">
						<div class="vm-stat-label">TTL</div>
						<div class="vm-stat-value">{formatTTL(sb.expires_at)}</div>
					</div>
				</div>
			{/each}
		</div>
	{:else}
		<div class="empty-state glass-panel">
			<span class="material-symbols-outlined empty-icon">cloud_off</span>
			<h3>No active sandboxes</h3>
			<p>Provision a new micro-VM to get started</p>
			<a href="/sandboxes" class="btn-primary">
				<span class="material-symbols-outlined" style="font-size: 1.1rem;">add</span>
				Create Sandbox
			</a>
		</div>
	{/if}
{/if}

<style>
	.page-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		margin-bottom: 2rem;
	}
	h1 {
		font-family: var(--font-headline);
		font-size: 1.75rem;
		font-weight: 700;
	}
	.subtitle {
		color: var(--on-surface-variant);
		font-size: 0.85rem;
		margin-top: 0.25rem;
	}
	.live-badge {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.35rem 0.75rem;
		border-radius: 9999px;
		background: rgba(181, 255, 194, 0.1);
		color: var(--tertiary);
		font-size: 0.75rem;
		font-weight: 600;
		letter-spacing: 0.05em;
	}
	.live-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--tertiary);
		animation: pulse 2s infinite;
	}
	@keyframes pulse {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.4; }
	}

	.error-banner {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.75rem 1rem;
		margin-bottom: 1.5rem;
		color: var(--error);
		font-size: 0.85rem;
	}

	.setup-card {
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 3rem;
		text-align: center;
		gap: 0.75rem;
	}
	.setup-icon { font-size: 2.5rem; color: var(--primary); }
	.setup-card h3 {
		font-family: var(--font-headline);
		font-size: 1.1rem;
	}
	.setup-card p { color: var(--on-surface-variant); font-size: 0.85rem; }
	.key-input-row {
		display: flex;
		gap: 0.5rem;
		margin-top: 0.5rem;
	}
	.key-input-row input { width: 320px; }

	.btn-primary {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.55rem 1.25rem;
		border-radius: 0.75rem;
		background: linear-gradient(135deg, var(--primary), var(--primary-dim));
		color: #4a0076;
		font-weight: 600;
		font-size: 0.85rem;
		text-decoration: none;
	}
	.btn-primary:hover { opacity: 0.9; text-decoration: none; }

	.metrics-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 1rem;
		margin-bottom: 2rem;
	}
	.metric-card {
		padding: 1.25rem;
	}
	.metric-label {
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--on-surface-variant);
		margin-bottom: 0.5rem;
	}
	.metric-value {
		font-family: var(--font-headline);
		font-size: 1.75rem;
		font-weight: 800;
		margin-bottom: 0.5rem;
	}
	.metric-sparkline {
		display: flex;
		align-items: center;
		gap: 0.35rem;
		font-size: 0.7rem;
		color: var(--on-surface-variant);
	}

	.section-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 1rem;
	}
	h2 {
		font-family: var(--font-headline);
		font-size: 1.1rem;
		font-weight: 600;
	}
	.view-all {
		display: flex;
		align-items: center;
		gap: 0.25rem;
		font-size: 0.8rem;
		color: var(--primary);
	}

	.vm-list {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}
	.vm-row {
		display: flex;
		align-items: center;
		gap: 1.25rem;
		padding: 1rem 1.25rem;
		transition: background 0.15s;
	}
	.vm-row:hover {
		background: rgba(39, 36, 49, 0.8);
	}
	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
	}
	.vm-info { flex: 1; }
	.vm-id {
		font-family: var(--font-headline);
		font-size: 0.9rem;
		font-weight: 600;
	}
	.vm-meta {
		font-size: 0.75rem;
		color: var(--on-surface-variant);
	}
	.vm-stat {
		text-align: right;
		min-width: 80px;
	}
	.vm-stat-label {
		font-size: 0.65rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--on-surface-variant);
	}
	.vm-stat-value {
		font-size: 0.85rem;
		font-weight: 600;
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 4rem;
		text-align: center;
		gap: 0.75rem;
	}
	.empty-icon {
		font-size: 3rem;
		color: var(--outline);
	}
	.empty-state h3 {
		font-family: var(--font-headline);
		font-size: 1.1rem;
	}
	.empty-state p {
		color: var(--on-surface-variant);
		font-size: 0.85rem;
		margin-bottom: 0.5rem;
	}
</style>
