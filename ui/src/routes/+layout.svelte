<script>
	import '../app.css';
	let { children } = $props();

	const navItems = [
		{ href: '/', icon: 'dashboard', label: 'Fleet' },
		{ href: '/images', icon: 'inventory_2', label: 'Images' },
		{ href: '/network', icon: 'lan', label: 'Network' },
		{ href: '/audit', icon: 'receipt_long', label: 'Logs' },
	];

	let currentPath = $state('/');

	// Direct call — Svelte 5 tree-shakes afterNavigate in production
	if (typeof window !== 'undefined') {
		currentPath = window.location.pathname;
		// Update on popstate (back/forward nav)
		window.addEventListener('popstate', () => {
			currentPath = window.location.pathname;
		});
	}

	function isActive(href) {
		if (href === '/') return currentPath === '/';
		return currentPath.startsWith(href);
	}

	// Update currentPath on click navigation
	function handleNavClick() {
		setTimeout(() => {
			if (typeof window !== 'undefined') currentPath = window.location.pathname;
		}, 0);
	}
</script>

<div class="shell">
	<header class="topbar">
		<div class="topbar-left">
			<a href="/" class="brand" onclick={handleNavClick}>
				<span class="material-symbols-outlined brand-icon">local_fire_department</span>
				<span class="brand-name">Pyro</span>
			</a>
			<nav class="nav-tabs">
				{#each navItems as item}
					<a
						href={item.href}
						class="nav-tab"
						class:active={isActive(item.href)}
						onclick={handleNavClick}
					>
						<span class="material-symbols-outlined">{item.icon}</span>
						{item.label}
					</a>
				{/each}
			</nav>
		</div>
		<div class="topbar-right">
			<a href="/sandboxes" class="btn-primary" onclick={handleNavClick}>
				<span class="material-symbols-outlined" style="font-size:1rem;">add</span>
				New VM
			</a>
		</div>
	</header>

	<main class="content">
		{@render children()}
	</main>
</div>

<style>
	.shell {
		display: flex;
		flex-direction: column;
		min-height: 100vh;
	}

	.topbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0 1.5rem;
		height: 56px;
		border-bottom: 1px solid var(--surface-container);
		background: #ffffff;
		position: sticky;
		top: 0;
		z-index: 10;
	}

	.topbar-left {
		display: flex;
		align-items: center;
		gap: 2rem;
	}
	.topbar-right {
		display: flex;
		align-items: center;
		gap: 0.75rem;
	}

	.brand {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		text-decoration: none;
		color: var(--on-surface);
	}
	.brand:hover { text-decoration: none; }
	.brand-icon { font-size: 1.5rem; color: var(--primary); }
	.brand-name {
		font-family: var(--font-headline);
		font-weight: 700;
		font-size: 0.95rem;
	}

	.nav-tabs {
		display: flex;
		align-items: center;
		gap: 0;
		height: 56px;
	}
	.nav-tab {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0 1rem;
		height: 100%;
		font-size: 0.8rem;
		font-weight: 500;
		color: var(--on-surface-variant);
		text-decoration: none;
		border-bottom: 2px solid transparent;
		transition: all 0.15s;
	}
	.nav-tab:hover {
		color: var(--on-surface);
		background: var(--surface-container-low);
		text-decoration: none;
	}
	.nav-tab.active {
		color: var(--primary);
		border-bottom-color: var(--primary);
	}
	.nav-tab :global(.material-symbols-outlined) { font-size: 1.15rem; }

	.content {
		flex: 1;
		padding: 1.5rem 2rem;
		max-width: 1400px;
		margin: 0 auto;
		width: 100%;
	}
</style>
