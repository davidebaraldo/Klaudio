<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		getTask,
		getPlan,
		startTask,
		stopTask,
		resumeTask,
		relaunchTask,
		deleteTask,
		sendMessage,
		type Task,
		type Plan
	} from '$lib/api';
	import { createTaskStream, type StreamEvent } from '$lib/stores/websocket';
	import { refreshTask, loadTasks } from '$lib/stores/tasks';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import PlanViewer from '$lib/components/PlanViewer.svelte';
	import FileManager from '$lib/components/FileManager.svelte';
	import MessageInput from '$lib/components/MessageInput.svelte';
	import QuestionPanel from '$lib/components/QuestionPanel.svelte';
	import AgentComms from '$lib/components/AgentComms.svelte';

	const { data } = $props();

	let task = $state<Task | null>(null);

	// Sync task from page data
	$effect(() => {
		if (data.task) task = data.task;
	});
	let plan = $state<Plan | null>(null);
	let activeTab = $state<'plan' | 'agents' | 'comms' | 'files'>('agents');
	let error = $state('');
	let actionLoading = $state('');
	let wsEvents = $state<StreamEvent[]>([]);

	let stream: ReturnType<typeof createTaskStream> | null = null;
	let TerminalComponent: typeof import('$lib/components/Terminal.svelte').default | null = $state(null);

	// Relaunch
	let showRelaunch = $state(false);
	let relaunchPrompt = $state('');
	let relaunchKeepContext = $state(true);

	async function refresh() {
		try {
			task = await getTask(data.taskId);
			refreshTask(data.taskId);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to refresh';
		}
	}

	async function loadPlan() {
		try {
			plan = await getPlan(data.taskId);
		} catch {
			plan = null;
		}
	}

	async function handleStart() {
		actionLoading = 'start';
		error = '';
		try {
			await startTask(data.taskId);
			await refresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to start';
		} finally {
			actionLoading = '';
		}
	}

	async function handleStop() {
		actionLoading = 'stop';
		error = '';
		try {
			await stopTask(data.taskId);
			await refresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to stop';
		} finally {
			actionLoading = '';
		}
	}

	async function handleResume() {
		actionLoading = 'resume';
		error = '';
		try {
			await resumeTask(data.taskId);
			await refresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to resume';
		} finally {
			actionLoading = '';
		}
	}

	async function handleDelete() {
		if (!confirm('Delete this task? This cannot be undone.')) return;
		actionLoading = 'delete';
		error = '';
		try {
			await deleteTask(data.taskId);
			await loadTasks();
			goto('/');
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete';
		} finally {
			actionLoading = '';
		}
	}

	async function handleRelaunch(autoStart: boolean) {
		actionLoading = 'relaunch';
		error = '';
		try {
			const newTask = await relaunchTask(data.taskId, {
				prompt: relaunchPrompt.trim() || undefined,
				autoStart,
				keepContext: relaunchKeepContext
			});
			await loadTasks();
			goto(`/tasks/${newTask.id}`);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to relaunch';
		} finally {
			actionLoading = '';
		}
	}

	function handleSendMessage(detail: { agentId: string; content: string }) {
		if (stream) {
			stream.sendMessage(detail.agentId, detail.content);
		} else {
			sendMessage(data.taskId, detail.agentId, detail.content).catch((e) => {
				error = e instanceof Error ? e.message : 'Failed to send message';
			});
		}
	}

	onMount(() => {
		let cleanup: (() => void) | undefined;

		(async () => {
		await loadPlan();

		// Dynamic import for Terminal (browser-only)
		const mod = await import('$lib/components/Terminal.svelte');
		TerminalComponent = mod.default;

		// Connect WebSocket
		stream = createTaskStream(data.taskId);

		const unsubEvents = stream.events.subscribe((evts) => {
			wsEvents = evts;
			// Auto-refresh task on relevant events
			const last = evts[evts.length - 1];
			if (last?.event?.startsWith('task.') || last?.event?.startsWith('plan.') || last?.event?.startsWith('agent.')) {
				refresh();
				if (last.event?.startsWith('plan.')) {
					loadPlan();
				}
			}
		});

		stream.connect();

		// Auto-refresh every 3s while planning/running to pick up new agents
		const pollInterval = setInterval(() => {
			if (task?.status === 'planning' || task?.status === 'running') {
				refresh();
			}
		}, 3000);

		cleanup = () => {
			unsubEvents();
			stream?.disconnect();
			clearInterval(pollInterval);
		};
		})();

		return () => cleanup?.();
	});

	const canStart = $derived(task?.status === 'created');
	const canStop = $derived(task?.status === 'running' || task?.status === 'planning');
	const canResume = $derived(task?.status === 'paused');
	const canRelaunch = $derived(task?.status === 'completed' || task?.status === 'failed' || task?.status === 'cancelled');
	const hasAgents = $derived((task?.agents?.length ?? 0) > 0);
</script>

<div class="p-6 max-w-6xl mx-auto">
	{#if !task}
		<div class="p-8 text-center text-zinc-500">
			Task not found.
			<a href="/" class="text-blue-400 hover:text-blue-300 ml-2">Back to dashboard</a>
		</div>
	{:else}
		<!-- Header -->
		<div class="flex items-start justify-between mb-6">
			<div>
				<div class="flex items-center gap-3 mb-1">
					<h1 class="text-2xl font-semibold text-zinc-100">{task.name}</h1>
					<StatusBadge status={task.status} />
				</div>
				{#if task.prompt}
					<p class="text-sm text-zinc-500 max-w-2xl">{task.prompt}</p>
				{/if}
			</div>
			<div class="flex gap-2 shrink-0">
				{#if canStart}
					<button
						onclick={handleStart}
						disabled={actionLoading !== ''}
						class="px-3 py-1.5 text-sm bg-green-600 hover:bg-green-700 disabled:bg-zinc-700 text-white rounded transition-colors"
					>
						{actionLoading === 'start' ? 'Starting...' : 'Start'}
					</button>
				{/if}
				{#if canStop}
					<button
						onclick={handleStop}
						disabled={actionLoading !== ''}
						class="px-3 py-1.5 text-sm bg-yellow-600 hover:bg-yellow-700 disabled:bg-zinc-700 text-white rounded transition-colors"
					>
						{actionLoading === 'stop' ? 'Stopping...' : 'Stop'}
					</button>
				{/if}
				{#if canResume}
					<button
						onclick={handleResume}
						disabled={actionLoading !== ''}
						class="px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded transition-colors"
					>
						{actionLoading === 'resume' ? 'Resuming...' : 'Resume'}
					</button>
				{/if}
				{#if canRelaunch}
					<button
						onclick={() => { showRelaunch = !showRelaunch; relaunchPrompt = ''; relaunchKeepContext = true; }}
						disabled={actionLoading !== ''}
						class="px-3 py-1.5 text-sm bg-violet-600 hover:bg-violet-700 disabled:bg-zinc-700 text-white rounded transition-colors"
					>
						Relaunch
					</button>
				{/if}
				<button
					onclick={() => refresh()}
					class="px-3 py-1.5 text-sm bg-zinc-800 hover:bg-zinc-700 text-zinc-300 rounded transition-colors"
				>
					Refresh
				</button>
				<button
					onclick={handleDelete}
					disabled={actionLoading !== ''}
					class="px-3 py-1.5 text-sm bg-red-600/20 hover:bg-red-600/30 text-red-400 rounded transition-colors"
				>
					Delete
				</button>
			</div>
		</div>

		{#if error}
			<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm mb-4">{error}</div>
		{/if}

		<!-- Relaunch panel -->
		{#if showRelaunch}
			<div class="mb-6 p-4 bg-zinc-800/50 rounded border border-violet-700/50">
				<h2 class="text-sm font-medium text-zinc-300 mb-2">Relaunch Task</h2>
				<p class="text-xs text-zinc-500 mb-3">Creates a new task with the same workspace and configuration. You can optionally change the prompt.</p>
				<textarea
					bind:value={relaunchPrompt}
					placeholder={task?.prompt ?? 'Same prompt as original...'}
					rows="3"
					class="w-full bg-zinc-900 text-zinc-200 text-sm px-3 py-2 rounded border border-zinc-600 focus:border-zinc-500 focus:outline-none resize-y mb-3"
				></textarea>
				<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer mb-3">
					<input type="checkbox" bind:checked={relaunchKeepContext} class="rounded bg-zinc-800 border-zinc-600" />
					Keep previous context
					<span class="text-xs text-zinc-500">(injects plan summary, subtask results, and changed files into the prompt)</span>
				</label>
				<div class="flex gap-2">
					<button
						onclick={() => handleRelaunch(true)}
						disabled={actionLoading === 'relaunch'}
						class="px-4 py-2 text-sm bg-violet-600 hover:bg-violet-700 disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded transition-colors"
					>
						{actionLoading === 'relaunch' ? 'Launching...' : 'Relaunch & Start'}
					</button>
					<button
						onclick={() => handleRelaunch(false)}
						disabled={actionLoading === 'relaunch'}
						class="px-4 py-2 text-sm bg-zinc-700 hover:bg-zinc-600 disabled:bg-zinc-800 disabled:text-zinc-500 text-zinc-300 rounded transition-colors"
					>
						Create Only
					</button>
					<button
						onclick={() => { showRelaunch = false; }}
						class="px-4 py-2 text-sm text-zinc-500 hover:text-zinc-300 transition-colors"
					>
						Cancel
					</button>
				</div>
			</div>
		{/if}

		<!-- Questions panel (if any pending) -->
		{#if task.status === 'planning' || plan?.questions?.some((q) => q.status === 'pending')}
			<div class="mb-6">
				<h2 class="text-sm font-medium text-zinc-400 mb-2">Planner Questions</h2>
				<QuestionPanel taskId={data.taskId} />
			</div>
		{/if}

		<!-- Tabs -->
		<div class="flex border-b border-zinc-800 mb-4">
			{#each ['plan', 'agents', 'comms', 'files'] as tab}
				<button
					onclick={() => { activeTab = tab as typeof activeTab; }}
					class="px-4 py-2 text-sm border-b-2 transition-colors -mb-px
						{activeTab === tab ? 'border-blue-500 text-zinc-100' : 'border-transparent text-zinc-500 hover:text-zinc-300'}"
				>
					{tab.charAt(0).toUpperCase() + tab.slice(1)}
				</button>
			{/each}
		</div>

		<!-- Tab content -->
		<div>
			{#if activeTab === 'plan'}
				<PlanViewer {plan} taskId={data.taskId} taskStatus={task.status} onrefresh={() => { refresh(); loadPlan(); }} />
			{:else if activeTab === 'agents'}
				{#if hasAgents}
					<div class="space-y-4">
						{#each task.agents ?? [] as agent}
							<div class="border border-zinc-700 rounded-lg overflow-hidden">
								<div class="flex items-center justify-between p-3 bg-zinc-800/50">
									<div class="flex items-center gap-3">
										<span class="text-sm font-medium text-zinc-200">{agent.role}</span>
										<StatusBadge status={agent.status} />
										<span class="text-xs text-zinc-500 font-mono">{agent.subtask_id}</span>
									</div>
									<span class="text-xs text-zinc-500 font-mono">{agent.id}</span>
								</div>
								<div class="h-[300px]">
									{#if TerminalComponent}
										<TerminalComponent taskId={data.taskId} agentId={agent.id} />
									{:else}
										<div class="flex items-center justify-center h-full text-zinc-500 text-sm">Loading terminal...</div>
									{/if}
								</div>
								<MessageInput
									agentId={agent.id}
									disabled={agent.status !== 'running'}
									onsend={handleSendMessage}
								/>
							</div>
						{/each}
					</div>
				{:else}
					<div class="p-8 text-center text-zinc-500 text-sm">
						{#if task?.status === 'planning'}
							Planner agent starting... waiting for agent to register.
							<button onclick={refresh} class="ml-2 text-blue-400 hover:text-blue-300">Refresh</button>
						{:else if task?.status === 'created'}
							Task not started yet. Click "Start" to begin planning.
						{:else}
							No agents running. Start the task and approve the plan to spawn agents.
						{/if}
					</div>
				{/if}
			{:else if activeTab === 'comms'}
				<AgentComms taskId={data.taskId} subtasks={plan?.subtasks ?? []} />
			{:else if activeTab === 'files'}
				<FileManager taskId={data.taskId} />
			{/if}
		</div>
	{/if}
</div>
