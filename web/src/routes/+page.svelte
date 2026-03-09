<script lang="ts">
	import { tasksStore, tasksLoading, tasksError, loadTasks } from '$lib/stores/tasks';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import logoIcon from '$lib/assets/klaudio-logo-icon.svg';

	const tasks = $derived($tasksStore);
	const loading = $derived($tasksLoading);
	const error = $derived($tasksError);

	function formatDate(ts: string): string {
		try {
			return new Date(ts).toLocaleDateString(undefined, {
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit'
			});
		} catch {
			return ts;
		}
	}
</script>

<div class="p-6 max-w-5xl mx-auto">
	<div class="flex items-center justify-between mb-6">
		<h1 class="text-2xl font-semibold text-zinc-100">Dashboard</h1>
		<div class="flex gap-2">
			<button
				onclick={() => loadTasks()}
				class="px-3 py-2 text-sm bg-zinc-800 hover:bg-zinc-700 text-zinc-300 rounded transition-colors"
			>
				Refresh
			</button>
			<a
				href="/tasks/new"
				class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded transition-colors"
			>
				New Task
			</a>
		</div>
	</div>

	{#if error}
		<div class="p-4 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm mb-4">{error}</div>
	{/if}

	{#if loading && tasks.length === 0}
		<div class="p-8 text-center text-zinc-500">Loading tasks...</div>
	{:else if tasks.length === 0}
		<div class="p-12 text-center">
			<img src={logoIcon} alt="Klaudio" class="h-16 w-16 mx-auto mb-4 opacity-30" />
			<p class="text-zinc-500 mb-4">No tasks yet. Create your first task to get started.</p>
			<a
				href="/tasks/new"
				class="inline-block px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm rounded transition-colors"
			>
				New Task
			</a>
		</div>
	{:else}
		<div class="space-y-2">
			{#each tasks as task}
				<a
					href="/tasks/{task.id}"
					class="block p-4 bg-zinc-800/50 hover:bg-zinc-800 border border-zinc-700/50 hover:border-zinc-700 rounded-lg transition-colors"
				>
					<div class="flex items-center justify-between">
						<div class="flex items-center gap-3">
							<span class="text-sm font-medium text-zinc-200">{task.name}</span>
							<StatusBadge status={task.status} />
						</div>
						<span class="text-xs text-zinc-500">{formatDate(task.created_at)}</span>
					</div>
					{#if task.prompt}
						<p class="mt-1 text-xs text-zinc-500 truncate max-w-xl">{task.prompt}</p>
					{/if}
				</a>
			{/each}
		</div>
	{/if}
</div>
