<script lang="ts">
	import { getEvents, type TaskEvent } from '$lib/api';

	const { taskId, wsEvents = [] }: { taskId: string; wsEvents?: Array<{ type: string; event?: string; data?: Record<string, unknown> }> } = $props();

	let events = $state<TaskEvent[]>([]);
	let error = $state('');
	let container: HTMLDivElement;

	const eventLabels: Record<string, string> = {
		'task.created': 'Task created',
		'task.started': 'Task started',
		'task.completed': 'Task completed',
		'task.failed': 'Task failed',
		'task.paused': 'Task paused',
		'task.resumed': 'Task resumed',
		'plan.generated': 'Plan generated',
		'plan.approved': 'Plan approved',
		'plan.modified': 'Plan modified',
		'agent.started': 'Agent started',
		'agent.completed': 'Agent completed',
		'agent.failed': 'Agent failed',
		'subtask.started': 'Subtask started',
		'subtask.completed': 'Subtask completed',
		'subtask.failed': 'Subtask failed',
		'planner.question': 'Planner question',
		'message.sent': 'Message sent'
	};

	const eventColors: Record<string, string> = {
		'task.created': 'bg-zinc-500',
		'task.started': 'bg-green-500',
		'task.completed': 'bg-green-500',
		'task.failed': 'bg-red-500',
		'task.paused': 'bg-zinc-500',
		'task.resumed': 'bg-green-500',
		'plan.generated': 'bg-yellow-500',
		'plan.approved': 'bg-blue-500',
		'agent.started': 'bg-green-500',
		'agent.completed': 'bg-green-500',
		'agent.failed': 'bg-red-500',
		'subtask.completed': 'bg-green-500',
		'subtask.failed': 'bg-red-500',
		'planner.question': 'bg-yellow-500'
	};

	async function loadEvents() {
		try {
			const result = await getEvents(taskId);
			events = result.events ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load events';
		}
	}

	function formatTime(ts: string): string {
		try {
			return new Date(ts).toLocaleTimeString();
		} catch {
			return ts;
		}
	}

	function formatDate(ts: string): string {
		try {
			return new Date(ts).toLocaleDateString();
		} catch {
			return '';
		}
	}

	// Auto-scroll
	$effect(() => {
		if (events.length && container) {
			container.scrollTop = container.scrollHeight;
		}
	});

	// Append WS events
	$effect(() => {
		if (wsEvents.length > 0) {
			const latest = wsEvents[wsEvents.length - 1];
			if (latest.type === 'event' && latest.event) {
				events = [...events, {
					id: Date.now(),
					type: latest.event,
					data: (latest.data ?? {}) as Record<string, unknown>,
					created_at: new Date().toISOString()
				}];
			}
		}
	});

	$effect(() => {
		loadEvents();
	});
</script>

<div bind:this={container} class="space-y-1 max-h-[600px] overflow-y-auto">
	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm">{error}</div>
	{/if}

	{#if events.length === 0}
		<div class="p-8 text-center text-zinc-500 text-sm">No events yet.</div>
	{:else}
		{#each events as event}
			<div class="flex items-start gap-3 p-2 rounded hover:bg-zinc-800/50">
				<div class="mt-1.5 w-2 h-2 rounded-full shrink-0 {eventColors[event.type] || 'bg-zinc-500'}"></div>
				<div class="flex-1 min-w-0">
					<div class="flex items-center gap-2">
						<span class="text-sm text-zinc-200">{eventLabels[event.type] || event.type}</span>
						{#if event.data && Object.keys(event.data).length > 0}
							<span class="text-xs text-zinc-500 font-mono truncate">
								{Object.entries(event.data).map(([k, v]) => `${k}=${v}`).join(' ')}
							</span>
						{/if}
					</div>
				</div>
				<span class="text-xs text-zinc-500 shrink-0">{formatTime(event.created_at)}</span>
			</div>
		{/each}
	{/if}
</div>
