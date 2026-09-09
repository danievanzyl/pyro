<script>
	import { apiFetch } from '$lib/auth.svelte.js';

	let { open = $bindable(false), images = [], prefill = null, onCreated = () => {} } = $props();

	let name = $state('');
	let source = $state('');
	let force = $state(false);
	let submitting = $state(false);

	// field-level errors
	let nameErr = $state('');
	let sourceErr = $state('');

	// banner-level error (409/413/500)
	let banner = $state('');

	// Collision detected client-side from already-loaded list. Force checkbox
	// only materializes when name matches an existing image — avoids
	// inviting destructive choice by default.
	let collision = $derived(!!name && images.some((x) => x.name === name));

	// Reset form whenever modal transitions to open=true. Honor prefill
	// (issue 14 retry flow) when supplied.
	let prevOpen = false;
	$effect(() => {
		if (open && !prevOpen) {
			name = prefill?.name ?? '';
			source = prefill?.source ?? '';
			force = prefill?.force ?? false;
			nameErr = '';
			sourceErr = '';
			banner = '';
			submitting = false;
		}
		prevOpen = open;
	});

	function close() {
		if (submitting) return;
		open = false;
	}

	function backdropClick(e) {
		if (e.target === e.currentTarget) close();
	}

	function onKey(e) {
		if (e.key === 'Escape') close();
	}

	async function submit(e) {
		e?.preventDefault?.();
		nameErr = '';
		sourceErr = '';
		banner = '';

		if (!name.trim()) nameErr = 'Required';
		if (!source.trim()) sourceErr = 'Required';
		if (nameErr || sourceErr) return;

		submitting = true;
		try {
			const body = { name: name.trim(), source: source.trim() };
			if (force) body.force = true;
			const res = await apiFetch('/images', {
				method: 'POST',
				body: JSON.stringify(body),
			});
			let data = {};
			try { data = await res.json(); } catch {}

			if (res.status === 200 || res.status === 202) {
				onCreated(data);
				open = false;
				return;
			}
			if (res.status === 400) {
				const msg = data.error || 'Invalid request';
				if (/name/i.test(msg)) nameErr = msg;
				else if (/source|dockerfile/i.test(msg)) sourceErr = msg;
				else banner = msg;
				return;
			}
			if (res.status === 409) {
				banner = 'Another pull for this name is in flight — wait for it to finish or use force.';
				return;
			}
			if (res.status === 413) {
				const est = data.estimated_mb ?? '?';
				const lim = data.limit_mb ?? '?';
				banner = `Image is ~${est} MB, exceeds the ${lim} MB limit. Ask an operator to raise MaxImageSizeMB.`;
				return;
			}
			banner = data.error || `Error ${res.status}`;
		} catch (err) {
			banner = err.message || 'Network error';
		} finally {
			submitting = false;
		}
	}

	let title = $derived(prefill ? 'Retry pull' : 'Create image');
</script>

<svelte:window on:keydown={onKey} />

{#if open}
	<div class="backdrop" onclick={backdropClick} onkeydown={onKey} role="dialog" aria-modal="true" aria-labelledby="cim-title" tabindex="-1">
		<div class="modal card">
			<div class="modal-header">
				<h2 id="cim-title">{title}</h2>
				<button class="btn-icon" onclick={close} disabled={submitting} title="Close" aria-label="Close">
					<span class="material-symbols-outlined" style="font-size:1rem;">close</span>
				</button>
			</div>

			{#if banner}
				<div class="error-banner">
					<span class="material-symbols-outlined">error</span>
					{banner}
				</div>
			{/if}

			<form class="form" onsubmit={submit}>
				<div class="field">
					<label for="cim-name">Name</label>
					<input
						id="cim-name"
						type="text"
						bind:value={name}
						placeholder="python-test"
						autocomplete="off"
						disabled={submitting}
					/>
					{#if nameErr}<p class="field-err">{nameErr}</p>{/if}
				</div>

				<div class="field">
					<label for="cim-source">Source</label>
					<input
						id="cim-source"
						type="text"
						bind:value={source}
						placeholder="python:3.12"
						autocomplete="off"
						disabled={submitting}
					/>
					{#if sourceErr}<p class="field-err">{sourceErr}</p>{/if}
				</div>

				{#if collision}
					<label class="check">
						<input type="checkbox" bind:checked={force} disabled={submitting} />
						<span>Replace existing image (re-pull from registry).</span>
					</label>
				{/if}

				<div class="footer">
					<p class="hint">Building from a Dockerfile? Use <code class="mono">pyro build-image</code>.</p>
					<div class="actions">
						<button type="button" class="btn-secondary" onclick={close} disabled={submitting}>Cancel</button>
						<button type="submit" class="btn-primary" disabled={submitting}>
							{#if submitting}
								<span class="material-symbols-outlined spin" style="font-size:1rem;">progress_activity</span>
								Pulling...
							{:else}
								<span class="material-symbols-outlined" style="font-size:1rem;">cloud_download</span>
								{prefill ? 'Retry' : 'Pull'}
							{/if}
						</button>
					</div>
				</div>
			</form>
		</div>
	</div>
{/if}

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.4);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 50;
	}
	.modal {
		width: min(480px, calc(100vw - 2rem));
		max-height: calc(100vh - 2rem);
		overflow: auto;
		padding: 1.25rem 1.5rem;
	}
	.modal-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 1rem;
	}
	.modal-header h2 {
		font-family: var(--font-headline);
		font-size: 1.1rem;
		font-weight: 600;
	}
	.form { display: flex; flex-direction: column; gap: 0.85rem; }
	.field { display: flex; flex-direction: column; gap: 0.3rem; }
	.field label {
		font-size: 0.7rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--on-surface-variant);
	}
	.field input { height: 2.5rem; box-sizing: border-box; }
	.field-err { font-size: 0.75rem; color: var(--error); }

	.check {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8rem;
		color: var(--on-surface);
	}

	.footer {
		display: flex;
		justify-content: space-between;
		align-items: flex-end;
		gap: 1rem;
		margin-top: 0.5rem;
		flex-wrap: wrap;
	}
	.hint {
		font-size: 0.75rem;
		color: var(--on-surface-variant);
		flex: 1 1 auto;
		min-width: 0;
	}
	.hint code {
		background: var(--surface-container);
		padding: 0.1rem 0.35rem;
		border-radius: var(--radius-xs);
		font-size: 0.75rem;
	}
	.actions { display: flex; gap: 0.5rem; }

	@keyframes spin { to { transform: rotate(360deg); } }
	.spin { animation: spin 1s linear infinite; }
</style>
