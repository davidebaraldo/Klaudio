<script lang="ts">
	import StatusBadge from './StatusBadge.svelte';
	import TruncatedText from './TruncatedText.svelte';
	import type { Plan, Subtask } from '$lib/api';
	import { updatePlan, approveTask } from '$lib/api';

	const { plan, taskId, taskStatus, onrefresh }: {
		plan: Plan | null;
		taskId: string;
		taskStatus: string;
		onrefresh: () => void;
	} = $props();

	let editMode = $state(false);
	let editedSubtasks = $state<Subtask[]>([]);
	let approving = $state(false);
	let saving = $state(false);
	let error = $state('');

	function startEdit() {
		if (plan) {
			editedSubtasks = JSON.parse(JSON.stringify(plan.subtasks));
			editMode = true;
		}
	}

	function cancelEdit() {
		editMode = false;
		editedSubtasks = [];
	}

	async function saveEdit() {
		saving = true;
		error = '';
		try {
			await updatePlan(taskId, { subtasks: editedSubtasks, strategy: plan?.strategy });
			editMode = false;
			onrefresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save';
		} finally {
			saving = false;
		}
	}

	async function handleApprove() {
		approving = true;
		error = '';
		try {
			await approveTask(taskId);
			onrefresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to approve';
		} finally {
			approving = false;
		}
	}

	const complexityColors: Record<string, string> = {
		low: 'text-green-400',
		medium: 'text-yellow-400',
		high: 'text-red-400'
	};
</script>

<div class="space-y-4">
	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm">{error}</div>
	{/if}

	{#if plan}
		{#if plan.analysis}
			<div class="p-4 bg-zinc-800 rounded border border-zinc-700">
				<h3 class="text-sm font-medium text-zinc-400 mb-2">Analysis</h3>
				<p class="text-sm text-zinc-300">{plan.analysis}</p>
			</div>
		{/if}

		<div class="flex items-center justify-between">
			<div class="flex items-center gap-3">
				<span class="text-sm text-zinc-400">Strategy: <span class="text-zinc-200">{plan.strategy || 'auto'}</span></span>
				<span class="text-sm text-zinc-400">Subtasks: <span class="text-zinc-200">{plan.subtasks.length}</span></span>
				{#if plan.estimated_agents}
					<span class="text-sm text-zinc-400">Agents: <span class="text-zinc-200">{plan.estimated_agents}</span></span>
				{/if}
			</div>
			<div class="flex gap-2">
				{#if (plan.status === 'planned' || plan.status === 'draft' || plan.status === 'modified') && !editMode}
					<button onclick={startEdit} class="px-3 py-1.5 text-sm bg-zinc-700 hover:bg-zinc-600 text-zinc-200 rounded transition-colors">
						Edit
					</button>
					<button onclick={handleApprove} disabled={approving} class="px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded transition-colors">
						{approving ? 'Approving...' : 'Approve Plan'}
					</button>
				{/if}
				{#if editMode}
					<button onclick={cancelEdit} class="px-3 py-1.5 text-sm bg-zinc-700 hover:bg-zinc-600 text-zinc-200 rounded transition-colors">
						Cancel
					</button>
					<button onclick={saveEdit} disabled={saving} class="px-3 py-1.5 text-sm bg-green-600 hover:bg-green-700 disabled:bg-zinc-700 text-white rounded transition-colors">
						{saving ? 'Saving...' : 'Save Changes'}
					</button>
				{/if}
			</div>
		</div>

		<div class="space-y-3">
			{#each editMode ? editedSubtasks : plan.subtasks as subtask, i}
				<div class="p-4 bg-zinc-800/50 rounded border border-zinc-700">
					<div class="flex items-center justify-between mb-2">
						<div class="flex items-center gap-2">
							<span class="text-sm font-medium text-zinc-200">{subtask.name}</span>
							<StatusBadge status={subtask.status} />
							{#if subtask.complexity}
								<span class="text-xs {complexityColors[subtask.complexity] || 'text-zinc-400'}">
									{subtask.complexity}
								</span>
							{/if}
							{#if subtask.agent_role}
								<span class="text-xs text-zinc-500">{subtask.agent_role}</span>
							{/if}
						</div>
						<span class="text-xs text-zinc-500 font-mono">{subtask.id}</span>
					</div>

					{#if editMode}
						<textarea
							bind:value={editedSubtasks[i].prompt}
							class="w-full bg-zinc-900 text-zinc-200 text-sm p-2 rounded border border-zinc-600 focus:border-zinc-500 focus:outline-none resize-y min-h-[60px]"
							placeholder="Subtask prompt..."
						></textarea>
					{:else if subtask.description}
						<p class="text-sm text-zinc-400">
							<TruncatedText text={subtask.description} maxLength={200} title="{subtask.name} — Description" />
						</p>
					{/if}

					{#if subtask.depends_on && subtask.depends_on.length > 0}
						<div class="mt-2 text-xs text-zinc-500">
							depends on: {subtask.depends_on.join(', ')}
						</div>
					{/if}

					{#if subtask.files_involved && subtask.files_involved.length > 0}
						<div class="mt-1 text-xs text-zinc-500 font-mono">
							files: {subtask.files_involved.join(', ')}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{:else}
		<div class="p-8 text-center text-zinc-500">
			{#if taskStatus === 'created'}
				No plan yet. Start the task to generate a plan.
			{:else if taskStatus === 'planning'}
				Generating plan...
			{:else}
				No plan available.
			{/if}
		</div>
	{/if}
</div>
