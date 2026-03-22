<script>
	import '../app.css';
	import { afterNavigate } from '$app/navigation';
	let { children } = $props();

	const navItems = [
		{ href: '/', icon: 'dashboard', label: 'Dashboard' },
		{ href: '/sandboxes', icon: 'memory', label: 'VM Instances' },
		{ href: '/images', icon: 'collections_bookmark', label: 'Images' },
		{ href: '/audit', icon: 'description', label: 'System Logs' },
	];

	let currentPath = $state('/');

	afterNavigate(() => {
		if (typeof window !== 'undefined') currentPath = window.location.pathname;
	});
</script>

<div class="shell">
	<aside class="sidebar glass-panel">
		<div class="brand">
			<span class="material-symbols-outlined brand-icon">local_fire_department</span>
			<div>
				<div class="brand-name">firecrackerlacker</div>
				<div class="brand-sub">Celestial Observer</div>
			</div>
		</div>

		<nav class="nav-main">
			{#each navItems as item}
				<a
					href={item.href}
					class="nav-item"
					class:active={currentPath === item.href}
					>
					<span class="material-symbols-outlined">{item.icon}</span>
					{item.label}
				</a>
			{/each}
		</nav>

		<div class="nav-spacer"></div>

		<a href="/sandboxes" class="provision-btn">
			<span class="material-symbols-outlined">add</span>
			Provision VM
		</a>

		<nav class="nav-secondary">
			<a href="https://github.com/danievanzyl/firecrackerlacker" class="nav-item" target="_blank">
				<span class="material-symbols-outlined">menu_book</span>
				Documentation
			</a>
		</nav>
	</aside>

	<main class="content">
		{@render children()}
	</main>
</div>

<style>
	.shell {
		display: flex;
		min-height: 100vh;
	}

	.sidebar {
		width: 260px;
		padding: 1.5rem 1rem;
		display: flex;
		flex-direction: column;
		position: fixed;
		top: 0;
		left: 0;
		bottom: 0;
		z-index: 10;
		border-radius: 0;
		border-right: 1px solid rgba(73, 70, 81, 0.2);
		border-top: none;
		border-bottom: none;
		border-left: none;
	}

	.brand {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.5rem;
		margin-bottom: 2rem;
	}
	.brand-icon { font-size: 1.75rem; color: var(--primary); }
	.brand-name {
		font-family: var(--font-headline);
		font-weight: 700;
		font-size: 0.95rem;
	}
	.brand-sub {
		font-size: 0.7rem;
		color: var(--on-surface-variant);
		letter-spacing: 0.05em;
	}

	.nav-main, .nav-secondary {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}
	.nav-spacer { flex: 1; }

	.nav-item {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.6rem 0.75rem;
		border-radius: 0.5rem;
		font-size: 0.85rem;
		color: var(--on-surface-variant);
		text-decoration: none;
		transition: all 0.15s;
	}
	.nav-item:hover {
		background: var(--surface-container-high);
		color: var(--on-surface);
		text-decoration: none;
	}
	.nav-item.active {
		background: rgba(163, 67, 231, 0.15);
		color: var(--primary);
	}
	.nav-item :global(.material-symbols-outlined) { font-size: 1.25rem; }

	.provision-btn {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.65rem 1rem;
		margin: 1rem 0.5rem;
		border-radius: 0.75rem;
		background: linear-gradient(135deg, var(--primary), var(--primary-dim));
		color: #4a0076;
		font-weight: 600;
		font-size: 0.85rem;
	}
	.provision-btn:hover { opacity: 0.9; }

	.content {
		flex: 1;
		margin-left: 260px;
		padding: 2rem 2.5rem;
	}
</style>
