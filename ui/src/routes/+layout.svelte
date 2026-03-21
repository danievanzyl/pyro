<script>
	let { children } = $props();
</script>

<svelte:head>
	<style>
		:global(*) { box-sizing: border-box; margin: 0; padding: 0; }
		:global(body) {
			font-family: 'SF Mono', 'Cascadia Code', 'Fira Code', monospace;
			background: #0a0a0a;
			color: #e0e0e0;
			line-height: 1.5;
		}
		:global(a) { color: #ff6b35; text-decoration: none; }
		:global(a:hover) { text-decoration: underline; }
	</style>
</svelte:head>

<div class="shell">
	<nav>
		<div class="logo">🔥 firecrackerlacker</div>
		<div class="links">
			<a href="/">Dashboard</a>
			<a href="/sandboxes">Sandboxes</a>
			<a href="/images">Images</a>
			<a href="/audit">Audit</a>
		</div>
	</nav>
	<main>
		{@render children()}
	</main>
</div>

<style>
	.shell {
		max-width: 1200px;
		margin: 0 auto;
		padding: 1rem;
	}
	nav {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 1rem 0;
		border-bottom: 1px solid #222;
		margin-bottom: 2rem;
	}
	.logo {
		font-size: 1.1rem;
		font-weight: 700;
		color: #ff6b35;
	}
	.links {
		display: flex;
		gap: 1.5rem;
		font-size: 0.85rem;
	}
	main {
		min-height: 80vh;
	}
</style>
