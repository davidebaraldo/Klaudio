<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { RateLimitState } from '$lib/api';

	let { taskId, wsEvents = [] }: { taskId: string; wsEvents: any[] } = $props();

	let rateLimitStates = $state<RateLimitState[]>([]);
	let countdowns = $state<Record<string, number>>({});
	let countdownInterval: ReturnType<typeof setInterval> | null = null;

	// Watch for rate limit events from WebSocket
	$effect(() => {
		if (wsEvents.length === 0) return;
		const last = wsEvents[wsEvents.length - 1];
		if (!last) return;

		if (last.type === 'rate_limit' && last.data) {
			const state = last.data as RateLimitState;
			updateState(state);
		} else if (last.type === 'rate_limit_retry' && last.data) {
			const state = last.data as RateLimitState;
			updateState(state);
		}
	});

	function updateState(state: RateLimitState) {
		const idx = rateLimitStates.findIndex((s) => s.agent_id === state.agent_id);
		if (idx >= 0) {
			rateLimitStates[idx] = state;
		} else {
			rateLimitStates = [...rateLimitStates, state];
		}

		if (state.is_limited && state.retry_in_seconds > 0) {
			countdowns[state.agent_id] = state.retry_in_seconds;
		} else {
			delete countdowns[state.agent_id];
			// Remove non-limited states after a short delay
			setTimeout(() => {
				rateLimitStates = rateLimitStates.filter((s) => s.agent_id !== state.agent_id || s.is_limited);
			}, 5000);
		}
	}

	onMount(() => {
		countdownInterval = setInterval(() => {
			let changed = false;
			for (const agentId of Object.keys(countdowns)) {
				if (countdowns[agentId] > 0) {
					countdowns[agentId]--;
					changed = true;
				}
				if (countdowns[agentId] <= 0) {
					delete countdowns[agentId];
					changed = true;
				}
			}
			if (changed) {
				countdowns = { ...countdowns };
			}
		}, 1000);
	});

	onDestroy(() => {
		if (countdownInterval) clearInterval(countdownInterval);
	});

	const activeLimits = $derived(rateLimitStates.filter((s) => s.is_limited));
	const hasLimits = $derived(activeLimits.length > 0);

	function formatCountdown(seconds: number): string {
		const m = Math.floor(seconds / 60);
		const s = seconds % 60;
		if (m > 0) return `${m}m ${s}s`;
		return `${s}s`;
	}

	function progressPercent(state: RateLimitState): number {
		const remaining = countdowns[state.agent_id] ?? 0;
		if (state.retry_in_seconds <= 0) return 100;
		return ((state.retry_in_seconds - remaining) / state.retry_in_seconds) * 100;
	}
</script>

{#if hasLimits}
	<div class="mb-4 space-y-2">
		{#each activeLimits as state}
			{@const remaining = countdowns[state.agent_id] ?? 0}
			{@const progress = progressPercent(state)}
			<div class="p-3 bg-amber-900/20 border border-amber-600/50 rounded-lg">
				<div class="flex items-center justify-between mb-2">
					<div class="flex items-center gap-2">
						<svg class="w-4 h-4 text-amber-400 animate-pulse shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
						</svg>
						<span class="text-sm font-medium text-amber-300">Rate Limited</span>
						{#if state.agent_id}
							<span class="text-xs text-amber-400/60 font-mono">({state.agent_id.slice(0, 8)})</span>
						{/if}
					</div>
					<div class="flex items-center gap-3 text-xs">
						<span class="text-amber-400/80">
							Attempt {state.attempt}/{state.max_retries}
						</span>
						{#if remaining > 0}
							<span class="text-amber-300 font-mono font-semibold tabular-nums">
								{formatCountdown(remaining)}
							</span>
						{/if}
					</div>
				</div>

				<!-- Progress bar -->
				<div class="h-1.5 bg-amber-900/50 rounded-full overflow-hidden mb-1.5">
					<div
						class="h-full bg-amber-500 rounded-full transition-all duration-1000"
						style="width: {progress}%"
					></div>
				</div>

				<p class="text-xs text-amber-400/70">{state.message}</p>
			</div>
		{/each}
	</div>
{/if}

{#if rateLimitStates.some((s) => !s.is_limited && s.attempt > 0)}
	{#each rateLimitStates.filter((s) => !s.is_limited && s.attempt > 0) as state}
		<div class="mb-4 p-2 bg-emerald-900/20 border border-emerald-600/30 rounded-lg">
			<div class="flex items-center gap-2">
				<svg class="w-3.5 h-3.5 text-emerald-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
				</svg>
				<span class="text-xs text-emerald-400">
					Retrying after rate limit (attempt {state.attempt}/{state.max_retries})...
				</span>
			</div>
		</div>
	{/each}
{/if}
