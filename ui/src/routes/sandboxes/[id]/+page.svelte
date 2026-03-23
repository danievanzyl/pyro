<script>
	import { page } from '$app/state';
	import { apiFetch, getApiKey } from '$lib/auth.svelte.js';

	let sandbox = $state(null);
	let error = $state('');
	let consoleLines = $state([]);
	let wsStatus = $state('disconnected');
	let ws = null;
	let consoleEl = $state(null);
	let autoScroll = $state(true);
	let activeTab = $state('console');
	let cmdInput = $state('');
	let cmdRunning = $state(false);
	let cmdHistory = $state([]);
	let historyIdx = $state(-1);

	const id = page.params.id;

	async function fetchSandbox() {
		try {
			const res = await apiFetch(`/sandboxes/${id}`);
			if (res.ok) {
				sandbox = await res.json();
				error = '';
			} else if (res.status === 404) {
				error = 'Sandbox not found. It may have expired.';
			} else {
				error = `Error ${res.status}`;
			}
		} catch (e) { error = e.message; }
	}

	function connectWS() {
		const key = getApiKey();
		if (!key || !sandbox || sandbox.state !== 'running') return;

		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		ws = new WebSocket(`${proto}//${location.host}/api/sandboxes/${id}/ws?api_key=${key}`);
		wsStatus = 'connecting';

		ws.onopen = () => { wsStatus = 'connected'; };
		ws.onmessage = (e) => {
			try {
				const data = JSON.parse(e.data);
				if (data.type === 'stdout' && data.data) {
					consoleLines = [...consoleLines, ...data.data.split('\n').filter(l => l)];
				} else if (data.type === 'stderr' && data.data) {
					consoleLines = [...consoleLines, ...data.data.split('\n').filter(l => l).map(l => `[stderr] ${l}`)];
				} else if (data.type === 'exit') {
					if (data.exit_code !== 0) {
						consoleLines = [...consoleLines, `exit ${data.exit_code}`];
					}
					cmdRunning = false;
				} else if (data.type === 'error' && data.data) {
					consoleLines = [...consoleLines, `error: ${data.data}`];
					cmdRunning = false;
				}
				if (autoScroll && consoleEl) {
					setTimeout(() => consoleEl.scrollTop = consoleEl.scrollHeight, 10);
				}
			} catch {}
		};
		ws.onclose = () => { wsStatus = 'disconnected'; ws = null; };
		ws.onerror = () => { wsStatus = 'error'; ws = null; };
	}

	function sendCommand() {
		if (!cmdInput.trim() || !ws || ws.readyState !== WebSocket.OPEN) return;
		const cmd = cmdInput.trim();
		consoleLines = [...consoleLines, `$ ${cmd}`];
		cmdHistory = [...cmdHistory, cmd];
		historyIdx = -1;
		cmdRunning = true;
		ws.send(JSON.stringify({ type: 'exec', command: ['sh', '-c', cmd] }));
		cmdInput = '';
		if (autoScroll && consoleEl) {
			setTimeout(() => consoleEl.scrollTop = consoleEl.scrollHeight, 10);
		}
	}

	function handleKeydown(e) {
		if (e.key === 'Enter') {
			e.preventDefault();
			sendCommand();
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			if (cmdHistory.length > 0) {
				historyIdx = historyIdx < 0 ? cmdHistory.length - 1 : Math.max(0, historyIdx - 1);
				cmdInput = cmdHistory[historyIdx];
			}
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			if (historyIdx >= 0) {
				historyIdx = historyIdx + 1;
				cmdInput = historyIdx < cmdHistory.length ? cmdHistory[historyIdx] : '';
				if (historyIdx >= cmdHistory.length) historyIdx = -1;
			}
		}
	}

	function clearConsole() { consoleLines = []; }

	function downloadLog() {
		const blob = new Blob([consoleLines.join('\n')], { type: 'text/plain' });
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = `${id.slice(0, 12)}-console.log`;
		a.click();
	}

	async function destroySandbox() {
		await apiFetch(`/sandboxes/${id}`, { method: 'DELETE' });
		window.location.href = '/sandboxes';
	}

	function formatDate(d) {
		return new Date(d).toLocaleString();
	}

	function formatTTL(expiresAt) {
		const remaining = new Date(expiresAt) - new Date();
		if (remaining <= 0) return 'expired';
		const mins = Math.floor(remaining / 60000);
		if (mins > 60) return `${Math.floor(mins / 60)}h ${mins % 60}m`;
		return `${mins}m`;
	}

	let pollInterval = null;

	async function init() {
		await fetchSandbox();
		if (sandbox?.state === 'running' && wsStatus === 'disconnected') connectWS();
		pollInterval = setInterval(async () => {
			await fetchSandbox();
			if (sandbox?.state === 'running' && wsStatus === 'disconnected' && !ws) connectWS();
		}, 5000);
	}

	if (typeof window !== 'undefined') init();
</script>

{#if error}
	<div class="error-banner">
		<span class="material-symbols-outlined">error</span>
		{error}
	</div>
	<a href="/sandboxes" class="back-link">&larr; Back to VM Instances</a>
{:else if !sandbox}
	<div class="loading">Loading...</div>
{:else}
	<div class="detail-header">
		<div class="detail-title">
			<a href="/sandboxes" class="back-link">&larr;</a>
			<code class="mono id">{sandbox.id.slice(0, 12)}</code>
			<span class="badge badge-{sandbox.state === 'running' ? 'running' : sandbox.state === 'creating' ? 'creating' : 'stopped'}">
				<span class="status-dot {sandbox.state}" class:pulse={sandbox.state === 'running'}></span>
				{sandbox.state.toUpperCase()}
			</span>
			<span class="detail-meta">{sandbox.image} &middot; Up {formatTTL(sandbox.expires_at)} remaining</span>
		</div>
		<div class="detail-actions">
			<button class="btn-danger" onclick={destroySandbox}>
				<span class="material-symbols-outlined" style="font-size:0.9rem;">delete</span>
				Terminate
			</button>
		</div>
	</div>

	<div class="tabs">
		<button class="tab" class:active={activeTab === 'console'} onclick={() => { activeTab = 'console'; }}>
			<span class="material-symbols-outlined" style="font-size:1rem;">terminal</span> Console
		</button>
		<button class="tab" class:active={activeTab === 'config'} onclick={() => { activeTab = 'config'; }}>
			<span class="material-symbols-outlined" style="font-size:1rem;">settings</span> Config
		</button>
		<button class="tab" class:active={activeTab === 'network'} onclick={() => { activeTab = 'network'; }}>
			<span class="material-symbols-outlined" style="font-size:1rem;">lan</span> Network
		</button>
	</div>

	{#if activeTab === 'console'}
		<div class="console-panel">
			<div class="console-toolbar">
				<div class="ws-status">
					<span class="status-dot" class:running={wsStatus === 'connected'} class:stopped={wsStatus !== 'connected'}></span>
					{wsStatus === 'connected' ? 'Connected' : wsStatus === 'connecting' ? 'Connecting...' : 'Disconnected'}
				</div>
				<div class="console-actions">
					{#if wsStatus === 'disconnected'}
						<button class="btn-secondary" onclick={connectWS}>Reconnect</button>
					{/if}
					<button class="btn-secondary" onclick={clearConsole}>Clear</button>
					<button class="btn-secondary" onclick={downloadLog}>
						<span class="material-symbols-outlined" style="font-size:0.9rem;">download</span>
						Log
					</button>
				</div>
			</div>
			<div class="console-output" bind:this={consoleEl} role="log" aria-live="polite">
				{#if consoleLines.length === 0}
					<span class="console-empty">Waiting for output...</span>
				{:else}
					{#each consoleLines as line}
						<div class="console-line">{line}</div>
					{/each}
				{/if}
				<span class="cursor">_</span>
			</div>
			<div class="console-input-bar">
				<span class="console-prompt">$</span>
				<input
					class="console-input"
					type="text"
					bind:value={cmdInput}
					onkeydown={handleKeydown}
					placeholder={wsStatus === 'connected' ? 'Type a command...' : 'Connecting...'}
					disabled={wsStatus !== 'connected'}
					autocomplete="off"
					spellcheck="false"
				/>
				<button class="console-send" onclick={sendCommand} disabled={wsStatus !== 'connected' || !cmdInput.trim() || cmdRunning}>
					{#if cmdRunning}
						<span class="material-symbols-outlined spin" style="font-size:1rem;">progress_activity</span>
					{:else}
						<span class="material-symbols-outlined" style="font-size:1rem;">send</span>
					{/if}
				</button>
			</div>
		</div>
	{:else if activeTab === 'config'}
		<div class="config-grid">
			<div class="config-section card">
				<h4>Compute</h4>
				<dl>
					<dt>Image</dt><dd>{sandbox.image}</dd>
					<dt>PID</dt><dd class="mono">{sandbox.pid || '—'}</dd>
					<dt>vSock CID</dt><dd class="mono">{sandbox.vsock_cid}</dd>
				</dl>
			</div>
			<div class="config-section card">
				<h4>Lifecycle</h4>
				<dl>
					<dt>Created</dt><dd>{formatDate(sandbox.created_at)}</dd>
					<dt>Expires</dt><dd>{formatDate(sandbox.expires_at)}</dd>
					<dt>TTL Remaining</dt><dd>{formatTTL(sandbox.expires_at)}</dd>
				</dl>
			</div>
			<div class="config-section card">
				<h4>Identity</h4>
				<dl>
					<dt>Sandbox ID</dt><dd class="mono" style="font-size:0.75rem;">{sandbox.id}</dd>
					<dt>API Key ID</dt><dd class="mono" style="font-size:0.75rem;">{sandbox.api_key_id}</dd>
					<dt>State</dt><dd>{sandbox.state}</dd>
				</dl>
			</div>
		</div>
	{:else if activeTab === 'network'}
		<div class="config-grid">
			<div class="config-section card">
				<h4>Network</h4>
				<dl>
					<dt>TAP Device</dt><dd class="mono">{sandbox.tap_device || '—'}</dd>
					<dt>Bridge</dt><dd class="mono">fcbr0</dd>
					<dt>vSock CID</dt><dd class="mono">{sandbox.vsock_cid}</dd>
				</dl>
			</div>
		</div>
	{/if}
{/if}

<style>
	.detail-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
	.detail-title { display: flex; align-items: center; gap: 0.75rem; }
	.back-link { color: var(--on-surface-variant); text-decoration: none; font-size: 1.1rem; }
	.back-link:hover { color: var(--on-surface); text-decoration: none; }
	.id { font-size: 1.1rem; font-weight: 600; }
	.detail-meta { color: var(--on-surface-variant); font-size: 0.8rem; }
	.detail-actions { display: flex; gap: 0.5rem; }

	.loading { color: var(--on-surface-variant); padding: 2rem; text-align: center; }

	.tabs { display: flex; gap: 0; border-bottom: 1px solid var(--surface-container); margin-bottom: 1rem; }
	.tab {
		display: flex; align-items: center; gap: 0.35rem;
		padding: 0.6rem 1rem; font-size: 0.8rem; font-weight: 500;
		color: var(--on-surface-variant); background: none;
		border-bottom: 2px solid transparent; cursor: pointer;
	}
	.tab:hover { color: var(--on-surface); }
	.tab.active { color: var(--primary); border-bottom-color: var(--primary); }

	.console-panel { border: 1px solid var(--surface-container); border-radius: var(--radius-md); overflow: hidden; }
	.console-toolbar {
		display: flex; align-items: center; justify-content: space-between;
		padding: 0.5rem 0.75rem; background: var(--surface-container-high);
		border-bottom: 1px solid var(--surface-container);
	}
	.ws-status { display: flex; align-items: center; gap: 0.4rem; font-size: 0.75rem; color: var(--on-surface-variant); }
	.console-actions { display: flex; gap: 0.35rem; }

	.console-output {
		background: #1b1b1b; color: #e2e2e2;
		font-family: var(--font-mono); font-size: 13px; line-height: 1.5;
		padding: 1rem; min-height: 400px; max-height: 600px;
		overflow-y: auto; white-space: pre-wrap; word-break: break-all;
	}
	.console-output::-webkit-scrollbar { width: 4px; }
	.console-output::-webkit-scrollbar-track { background: #1b1b1b; }
	.console-output::-webkit-scrollbar-thumb { background: #4c4546; border-radius: 2px; }
	.console-empty { color: #5e5e5e; }
	.console-line { min-height: 1.5em; }
	.cursor { animation: blink 1s step-end infinite; color: #4ae176; }
	@keyframes blink { 0%, 100% { opacity: 1; } 50% { opacity: 0; } }

	.console-input-bar {
		display: flex; align-items: center; gap: 0;
		background: #252525; border-top: 1px solid #333;
		padding: 0.5rem 0.75rem;
	}
	.console-prompt {
		color: #4ae176; font-family: var(--font-mono); font-size: 13px;
		font-weight: 600; margin-right: 0.5rem; user-select: none;
	}
	.console-input {
		flex: 1; background: transparent; border: none; outline: none;
		color: #e2e2e2; font-family: var(--font-mono); font-size: 13px;
		caret-color: #4ae176;
	}
	.console-input::placeholder { color: #555; }
	.console-input:disabled { opacity: 0.4; }
	.console-send {
		background: none; border: none; color: #5e5e5e; cursor: pointer;
		padding: 0.25rem; display: flex; align-items: center;
	}
	.console-send:hover:not(:disabled) { color: #4ae176; }
	.console-send:disabled { opacity: 0.3; cursor: not-allowed; }

	.config-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; }
	.config-section h4 { font-family: var(--font-headline); font-size: 0.85rem; font-weight: 600; margin-bottom: 0.75rem; }
	dl { display: grid; grid-template-columns: auto 1fr; gap: 0.4rem 1rem; font-size: 0.8rem; }
	dt { color: var(--on-surface-variant); font-weight: 500; }
	dd { color: var(--on-surface); }
</style>
