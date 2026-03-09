<script lang="ts">
	import { onMount } from 'svelte';
	import { getAgentMessages, type AgentMessage, type Subtask } from '$lib/api';

	interface Props {
		taskId: string;
		subtasks: Subtask[];
	}
	let { taskId, subtasks }: Props = $props();

	let messages = $state<AgentMessage[]>([]);
	let loading = $state(true);
	let pollTimer: ReturnType<typeof setInterval> | null = null;

	// Build lookup maps
	let subtaskMap = $derived(
		Object.fromEntries(subtasks.map((s) => [s.id, s]))
	);
	let agentToSubtask = $derived(
		Object.fromEntries(subtasks.filter((s) => s.agent_id).map((s) => [s.agent_id!, s.id]))
	);

	// Split messages by type
	let contextMessages = $derived(messages.filter((m) => m.msg_type === 'context'));
	let broadcastMessages = $derived(messages.filter((m) => m.msg_type === 'message'));

	// Build interaction graph: edges between subtasks
	let interactions = $derived.by(() => {
		const edges: Array<{
			from: string;
			to: string;
			type: 'dependency' | 'context' | 'message';
			label: string;
		}> = [];

		// Dependency edges from plan
		for (const st of subtasks) {
			for (const depId of st.depends_on ?? []) {
				edges.push({ from: depId, to: st.id, type: 'dependency', label: 'depends on' });
			}
		}

		// Context passing edges
		for (const msg of contextMessages) {
			if (!msg.from_subtask_id) continue;
			// Find who received this context (subtasks that depend on the sender)
			for (const st of subtasks) {
				if (st.depends_on?.includes(msg.from_subtask_id)) {
					edges.push({
						from: msg.from_subtask_id,
						to: st.id,
						type: 'context',
						label: 'context passed'
					});
				}
			}
		}

		// Broadcast message edges
		for (const msg of broadcastMessages) {
			if (!msg.from_subtask_id) continue;
			edges.push({
				from: msg.from_subtask_id,
				to: '*',
				type: 'message',
				label: msg.content.slice(0, 60) + (msg.content.length > 60 ? '...' : '')
			});
		}

		return edges;
	});

	function subtaskLabel(id: string): string {
		const st = subtaskMap[id];
		if (!st) return id.slice(0, 8);
		return st.name || id.slice(0, 8);
	}

	function subtaskRole(id: string): string {
		return subtaskMap[id]?.agent_role || 'agent';
	}

	function subtaskStatus(id: string): string {
		return subtaskMap[id]?.status || 'pending';
	}

	function roleColor(role: string): string {
		switch (role) {
			case 'developer':
				return 'bg-blue-900/40 text-blue-400 border-blue-800';
			case 'tester':
				return 'bg-green-900/40 text-green-400 border-green-800';
			case 'reviewer':
				return 'bg-purple-900/40 text-purple-400 border-purple-800';
			default:
				return 'bg-zinc-800/40 text-zinc-400 border-zinc-700';
		}
	}

	function statusDot(status: string): string {
		switch (status) {
			case 'completed':
				return 'bg-green-500';
			case 'running':
				return 'bg-blue-500 animate-pulse';
			case 'failed':
				return 'bg-red-500';
			default:
				return 'bg-zinc-600';
		}
	}

	function formatTime(iso: string): string {
		try {
			const d = new Date(iso);
			return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
		} catch {
			return '';
		}
	}

	async function loadMessages() {
		try {
			const result = await getAgentMessages(taskId);
			messages = result.messages ?? [];
		} catch {
			// ignore
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		loadMessages();
		pollTimer = setInterval(loadMessages, 5000);
		return () => {
			if (pollTimer) clearInterval(pollTimer);
		};
	});
</script>

<div class="space-y-6">
	<!-- Agent Overview Grid -->
	<div>
		<h3 class="text-sm font-medium text-zinc-400 mb-3">Agents</h3>
		<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
			{#each subtasks as st}
				<div class="p-3 rounded border border-zinc-700 bg-zinc-800/30">
					<div class="flex items-center gap-2 mb-1">
						<span class="w-2 h-2 rounded-full {statusDot(st.status)}"></span>
						<span class="text-sm font-medium text-zinc-200 truncate">{st.name}</span>
					</div>
					<div class="flex items-center gap-2">
						<span
							class="px-1.5 py-0.5 text-[10px] rounded border {roleColor(
								st.agent_role || 'agent'
							)}"
						>
							{st.agent_role || 'agent'}
						</span>
						<span class="text-[10px] text-zinc-500">{st.id.slice(0, 8)}</span>
					</div>
					{#if st.depends_on && st.depends_on.length > 0}
						<div class="mt-1.5 text-[10px] text-zinc-500">
							needs: {st.depends_on.map((d) => subtaskLabel(d)).join(', ')}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	</div>

	<!-- Interaction Flow -->
	<div>
		<h3 class="text-sm font-medium text-zinc-400 mb-3">Interaction Flow</h3>
		{#if loading}
			<div class="text-sm text-zinc-500">Loading...</div>
		{:else if interactions.length === 0 && messages.length === 0}
			<div class="text-sm text-zinc-500 p-4 text-center border border-zinc-800 rounded">
				No interactions yet. Agents will communicate via shared context and messages as they work.
			</div>
		{:else}
			<div class="space-y-2">
				{#each interactions as edge}
					<div
						class="flex items-center gap-3 p-2 rounded text-sm
						{edge.type === 'context'
							? 'bg-amber-900/10 border border-amber-900/30'
							: edge.type === 'message'
								? 'bg-blue-900/10 border border-blue-900/30'
								: 'bg-zinc-800/30 border border-zinc-800'}"
					>
						<!-- From -->
						<div class="flex items-center gap-1.5 min-w-0 shrink-0">
							<span
								class="w-1.5 h-1.5 rounded-full {statusDot(subtaskStatus(edge.from))}"
							></span>
							<span class="text-zinc-300 truncate text-xs font-medium"
								>{subtaskLabel(edge.from)}</span
							>
						</div>

						<!-- Arrow with label -->
						<div class="flex items-center gap-1 text-zinc-500 shrink-0">
							{#if edge.type === 'context'}
								<svg class="w-4 h-4 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7l5 5m0 0l-5 5m5-5H6" />
								</svg>
								<span class="text-[10px] text-amber-500/80">context</span>
							{:else if edge.type === 'message'}
								<svg class="w-4 h-4 text-blue-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
								</svg>
								<span class="text-[10px] text-blue-500/80">msg</span>
							{:else}
								<svg class="w-4 h-4 text-zinc-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 8l4 4m0 0l-4 4m4-4H3" />
								</svg>
								<span class="text-[10px] text-zinc-600">dep</span>
							{/if}
						</div>

						<!-- To -->
						<div class="flex items-center gap-1.5 min-w-0 shrink-0">
							{#if edge.to === '*'}
								<span class="text-zinc-400 text-xs">all agents</span>
							{:else}
								<span
									class="w-1.5 h-1.5 rounded-full {statusDot(subtaskStatus(edge.to))}"
								></span>
								<span class="text-zinc-300 truncate text-xs font-medium"
									>{subtaskLabel(edge.to)}</span
								>
							{/if}
						</div>

						<!-- Message preview for broadcasts -->
						{#if edge.type === 'message'}
							<span class="text-[10px] text-zinc-500 truncate ml-auto"
								>{edge.label}</span
							>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<!-- Context Summaries -->
	{#if contextMessages.length > 0}
		<div>
			<h3 class="text-sm font-medium text-zinc-400 mb-3">Context Summaries</h3>
			<div class="space-y-3">
				{#each contextMessages as msg}
					<div class="p-3 rounded border border-amber-900/30 bg-amber-900/10">
						<div class="flex items-center justify-between mb-2">
							<div class="flex items-center gap-2">
								<span class="px-1.5 py-0.5 text-[10px] rounded border {roleColor(subtaskRole(msg.from_subtask_id || ''))}">
									{subtaskRole(msg.from_subtask_id || '')}
								</span>
								<span class="text-xs font-medium text-zinc-300"
									>{subtaskLabel(msg.from_subtask_id || '')}</span
								>
								<span class="text-[10px] text-amber-500/70">completed and shared context</span>
							</div>
							<span class="text-[10px] text-zinc-500">{formatTime(msg.created_at)}</span>
						</div>
						<pre
							class="text-xs text-zinc-400 whitespace-pre-wrap font-mono max-h-40 overflow-y-auto">{msg.content.slice(0, 500)}{msg.content.length > 500 ? '...' : ''}</pre>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Broadcast Messages -->
	{#if broadcastMessages.length > 0}
		<div>
			<h3 class="text-sm font-medium text-zinc-400 mb-3">Agent Messages</h3>
			<div class="space-y-2">
				{#each broadcastMessages as msg}
					<div class="flex items-start gap-3 p-2 rounded border border-zinc-800 bg-zinc-800/20">
						<div class="shrink-0 mt-0.5">
							<span class="px-1.5 py-0.5 text-[10px] rounded border {roleColor(subtaskRole(msg.from_subtask_id || ''))}">
								{subtaskRole(msg.from_subtask_id || '')}
							</span>
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2 mb-0.5">
								<span class="text-xs font-medium text-zinc-300">{subtaskLabel(msg.from_subtask_id || '')}</span>
								{#if msg.to_subtask_id}
									<span class="text-[10px] text-zinc-500">to {subtaskLabel(msg.to_subtask_id)}</span>
								{:else}
									<span class="text-[10px] text-zinc-500">to all</span>
								{/if}
								<span class="text-[10px] text-zinc-600 ml-auto">{formatTime(msg.created_at)}</span>
							</div>
							<p class="text-xs text-zinc-400">{msg.content}</p>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}
</div>
