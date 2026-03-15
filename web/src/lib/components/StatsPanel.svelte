<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { createStatsStream } from '$lib/stores/websocket';
	import type { TaskStats, AgentStats } from '$lib/api';

	let { taskId }: { taskId: string } = $props();

	let statsData = $state<TaskStats | null>(null);
	let streamConnected = $state(false);

	let stream: ReturnType<typeof createStatsStream> | null = null;

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function formatPercent(v: number): string {
		return v.toFixed(1) + '%';
	}

	function barColor(pct: number): string {
		if (pct > 80) return 'bg-red-500';
		if (pct > 60) return 'bg-amber-500';
		return 'bg-emerald-500';
	}

	function roleColor(role: string): string {
		switch (role) {
			case 'planner': return 'text-blue-400';
			case 'developer': return 'text-emerald-400';
			case 'reviewer': return 'text-purple-400';
			case 'tester': return 'text-amber-400';
			case 'manager': return 'text-orange-400';
			default: return 'text-zinc-400';
		}
	}

	function memPercent(mem: number, limit: number): number {
		return limit > 0 ? (mem / limit) * 100 : 0;
	}

	// Aggregate totals
	function totals(agents: AgentStats[]): { cpu: number; mem: number; memLimit: number; netRx: number; netTx: number; pids: number } {
		let cpu = 0, mem = 0, memLimit = 0, netRx = 0, netTx = 0, pids = 0;
		for (const a of agents) {
			cpu += a.stats.cpu_percent;
			mem += a.stats.memory_usage;
			memLimit += a.stats.memory_limit;
			netRx += a.stats.net_rx;
			netTx += a.stats.net_tx;
			pids += a.stats.pids;
		}
		return { cpu, mem, memLimit, netRx, netTx, pids };
	}

	onMount(() => {
		stream = createStatsStream(taskId, '2s');
		stream.stats.subscribe((v) => { statsData = v; });
		stream.connected.subscribe((v) => { streamConnected = v; });
		stream.connect();
	});

	onDestroy(() => {
		stream?.disconnect();
	});
</script>

{#if statsData && statsData.agents.length > 0}
	{@const t = totals(statsData.agents)}
	<div class="space-y-3">
		<!-- Aggregate summary -->
		<div class="grid grid-cols-4 gap-3">
			<!-- CPU -->
			<div class="p-3 bg-zinc-800/50 rounded-lg border border-zinc-700">
				<div class="text-[10px] text-zinc-500 uppercase tracking-wider mb-1">CPU</div>
				<div class="text-lg font-semibold text-zinc-100">{formatPercent(t.cpu)}</div>
				<div class="mt-1.5 h-1.5 bg-zinc-700 rounded-full overflow-hidden">
					<div class="h-full {barColor(t.cpu)} rounded-full transition-all duration-500" style="width: {Math.min(t.cpu, 100)}%"></div>
				</div>
			</div>
			<!-- Memory -->
			<div class="p-3 bg-zinc-800/50 rounded-lg border border-zinc-700">
				<div class="text-[10px] text-zinc-500 uppercase tracking-wider mb-1">Memory</div>
				<div class="text-lg font-semibold text-zinc-100">{formatBytes(t.mem)}</div>
				<div class="text-[10px] text-zinc-500">/ {formatBytes(t.memLimit)}</div>
				<div class="mt-1 h-1.5 bg-zinc-700 rounded-full overflow-hidden">
					<div class="h-full {barColor(memPercent(t.mem, t.memLimit))} rounded-full transition-all duration-500" style="width: {Math.min(memPercent(t.mem, t.memLimit), 100)}%"></div>
				</div>
			</div>
			<!-- Network -->
			<div class="p-3 bg-zinc-800/50 rounded-lg border border-zinc-700">
				<div class="text-[10px] text-zinc-500 uppercase tracking-wider mb-1">Network</div>
				<div class="flex items-baseline gap-2">
					<span class="text-sm text-emerald-400">&darr; {formatBytes(t.netRx)}</span>
					<span class="text-sm text-blue-400">&uarr; {formatBytes(t.netTx)}</span>
				</div>
			</div>
			<!-- PIDs -->
			<div class="p-3 bg-zinc-800/50 rounded-lg border border-zinc-700">
				<div class="text-[10px] text-zinc-500 uppercase tracking-wider mb-1">Processes</div>
				<div class="text-lg font-semibold text-zinc-100">{t.pids}</div>
				<div class="text-[10px] text-zinc-500">{statsData.agents.length} container{statsData.agents.length > 1 ? 's' : ''}</div>
			</div>
		</div>

		<!-- Per-agent breakdown -->
		{#if statsData.agents.length > 1}
			<div class="space-y-1.5">
				<h4 class="text-[10px] text-zinc-500 uppercase tracking-wider">Per Agent</h4>
				{#each statsData.agents as agent}
					<div class="flex items-center gap-3 px-3 py-2 bg-zinc-800/30 rounded border border-zinc-700/50 text-xs">
						<span class="{roleColor(agent.role)} font-medium w-20 truncate">{agent.role}</span>
						<span class="text-zinc-500 w-16 truncate font-mono" title={agent.subtask_id}>{agent.subtask_id?.slice(0, 8) || '—'}</span>
						<!-- CPU bar -->
						<div class="flex items-center gap-1.5 w-28">
							<span class="text-zinc-400 w-12 text-right">{formatPercent(agent.stats.cpu_percent)}</span>
							<div class="flex-1 h-1 bg-zinc-700 rounded-full overflow-hidden">
								<div class="h-full {barColor(agent.stats.cpu_percent)} rounded-full transition-all duration-500" style="width: {Math.min(agent.stats.cpu_percent, 100)}%"></div>
							</div>
						</div>
						<!-- Memory -->
						<span class="text-zinc-400 w-20 text-right">{formatBytes(agent.stats.memory_usage)}</span>
						<!-- Network -->
						<span class="text-emerald-400/70 w-16 text-right">&darr;{formatBytes(agent.stats.net_rx)}</span>
						<span class="text-blue-400/70 w-16 text-right">&uarr;{formatBytes(agent.stats.net_tx)}</span>
						<!-- PIDs -->
						<span class="text-zinc-500 w-8 text-right">{agent.stats.pids}</span>
					</div>
				{/each}
			</div>
		{/if}
	</div>
{:else if streamConnected}
	<div class="text-sm text-zinc-500 text-center py-8">No running containers</div>
{:else}
	<div class="text-sm text-zinc-500 text-center py-8">Connecting to stats stream...</div>
{/if}
