<script lang="ts">
	import type { Agent } from '$lib/api';
	import StatusBadge from './StatusBadge.svelte';
	import MessageInput from './MessageInput.svelte';

	const { agents, taskId, TerminalComponent, onsend }: {
		agents: Agent[];
		taskId: string;
		TerminalComponent: typeof import('$lib/components/Terminal.svelte').default | null;
		onsend: (detail: { agentId: string; content: string }) => void;
	} = $props();

	let expandedAgent = $state<string | null>(null);

	function toggle(agentId: string) {
		expandedAgent = expandedAgent === agentId ? null : agentId;
	}

	function roleColor(role: string): string {
		switch (role) {
			case 'developer': return 'bg-blue-900/40 text-blue-400 border-blue-700';
			case 'tester': return 'bg-green-900/40 text-green-400 border-green-700';
			case 'reviewer': return 'bg-purple-900/40 text-purple-400 border-purple-700';
			case 'planner': return 'bg-amber-900/40 text-amber-400 border-amber-700';
			case 'manager': return 'bg-rose-900/40 text-rose-400 border-rose-700';
			default: return 'bg-zinc-800/40 text-zinc-400 border-zinc-700';
		}
	}

	function statusDot(status: string): string {
		switch (status) {
			case 'completed': return 'bg-green-500';
			case 'running': return 'bg-blue-500 animate-pulse';
			case 'failed': return 'bg-red-500';
			default: return 'bg-zinc-600';
		}
	}
</script>

<div class="space-y-3">
	<!-- Grid -->
	<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
		{#each agents as agent}
			<button
				onclick={() => toggle(agent.id)}
				class="p-3 rounded-lg border text-left transition-all cursor-pointer
					{expandedAgent === agent.id
						? 'border-blue-600 bg-zinc-800/80 ring-1 ring-blue-600/50'
						: 'border-zinc-700 bg-zinc-800/30 hover:bg-zinc-800/60 hover:border-zinc-600'}"
			>
				<div class="flex items-center gap-2 mb-1.5">
					<span class="w-2 h-2 rounded-full shrink-0 {statusDot(agent.status)}"></span>
					<span class="text-sm font-medium text-zinc-200 truncate">{agent.role}</span>
				</div>
				<div class="flex items-center gap-2">
					<span class="px-1.5 py-0.5 text-[10px] rounded border {roleColor(agent.role)}">
						{agent.role}
					</span>
					<StatusBadge status={agent.status} />
				</div>
				<div class="mt-1.5 text-[10px] text-zinc-500 font-mono truncate">
					{agent.subtask_id}
				</div>
				{#if agent.output_size_bytes}
					<div class="mt-0.5 text-[10px] text-zinc-600">
						{(agent.output_size_bytes / 1024).toFixed(1)} KB output
					</div>
				{/if}
			</button>
		{/each}
	</div>

	<!-- Expanded terminal -->
	{#if expandedAgent}
		{@const agent = agents.find(a => a.id === expandedAgent)}
		{#if agent}
			<div class="border border-zinc-700 rounded-lg overflow-hidden">
				<div class="flex items-center justify-between p-3 bg-zinc-800/50">
					<div class="flex items-center gap-3">
						<span class="w-2 h-2 rounded-full {statusDot(agent.status)}"></span>
						<span class="text-sm font-medium text-zinc-200">{agent.role}</span>
						<StatusBadge status={agent.status} />
						<span class="text-xs text-zinc-500 font-mono">{agent.subtask_id}</span>
					</div>
					<button
						onclick={() => { expandedAgent = null; }}
						class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors"
					>
						Close
					</button>
				</div>
				<div class="h-[400px]">
					{#if TerminalComponent}
						<TerminalComponent {taskId} agentId={agent.id} />
					{:else}
						<div class="flex items-center justify-center h-full text-zinc-500 text-sm">Loading terminal...</div>
					{/if}
				</div>
				<MessageInput
					agentId={agent.id}
					disabled={agent.status !== 'running'}
					{onsend}
				/>
			</div>
		{/if}
	{/if}
</div>
