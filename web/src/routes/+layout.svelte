<script lang="ts">
	import '../app.css';
	import { tasksStore, loadTasks } from '$lib/stores/tasks';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import logoIcon from '$lib/assets/klaudio-logo-icon.svg';

	let { children } = $props();

	const tasks = $derived($tasksStore);
</script>

<svelte:head>
	<title>Klaudio</title>
	<link rel="icon" type="image/svg+xml" href={logoIcon} />
</svelte:head>

<div class="flex h-screen bg-zinc-900 text-zinc-100">
	<!-- Sidebar -->
	<aside class="w-64 bg-zinc-950 border-r border-zinc-800 flex flex-col shrink-0">
		<!-- Logo -->
		<div class="p-4 border-b border-zinc-800">
			<a href="/" class="flex items-center gap-2.5">
				<img src={logoIcon} alt="Klaudio" class="h-8 w-8" />
				<span class="text-lg font-semibold text-zinc-100 tracking-tight">Klaudio</span>
			</a>
		</div>

		<!-- Task list -->
		<nav class="flex-1 overflow-y-auto p-2">
			<div class="flex items-center justify-between px-2 py-1 mb-1">
				<span class="text-xs font-medium text-zinc-500 uppercase tracking-wider">Tasks</span>
				<a
					href="/tasks/new"
					class="text-xs text-blue-400 hover:text-blue-300 transition-colors"
				>
					+ New
				</a>
			</div>

			{#if tasks.length === 0}
				<p class="px-2 py-4 text-xs text-zinc-600">No tasks yet</p>
			{:else}
				{#each tasks as task}
					<a
						href="/tasks/{task.id}"
						class="block px-3 py-2 rounded text-sm hover:bg-zinc-800 transition-colors mb-0.5"
					>
						<div class="flex items-center justify-between">
							<span class="text-zinc-300 truncate">{task.name}</span>
						</div>
						<div class="mt-1">
							<StatusBadge status={task.status} />
						</div>
					</a>
				{/each}
			{/if}
		</nav>

		<!-- Footer -->
		<div class="p-2 border-t border-zinc-800">
			<a
				href="/settings"
				class="block px-3 py-2 rounded text-sm text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 transition-colors"
			>
				Settings
			</a>
		</div>
	</aside>

	<!-- Main content -->
	<main class="flex-1 overflow-y-auto">
		{@render children()}
	</main>
</div>
