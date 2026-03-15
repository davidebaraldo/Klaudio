<script lang="ts">
	import { type Activity, getActivityMeta } from '$lib/utils/outputParser';

	interface Props {
		activities: Activity[];
	}

	let { activities }: Props = $props();

	let scrollContainer: HTMLDivElement;
	let autoScroll = $state(true);

	function toggleCollapse(activity: Activity) {
		activity.collapsed = !activity.collapsed;
		// Force reactivity
		activities = [...activities];
	}

	function handleScroll() {
		if (!scrollContainer) return;
		const { scrollTop, scrollHeight, clientHeight } = scrollContainer;
		autoScroll = scrollHeight - scrollTop - clientHeight < 60;
	}

	$effect(() => {
		// Auto-scroll when new activities arrive
		if (activities.length && autoScroll && scrollContainer) {
			// Use tick-like delay
			requestAnimationFrame(() => {
				scrollContainer.scrollTop = scrollContainer.scrollHeight;
			});
		}
	});

	function contentPreview(content: string, maxLen = 200): string {
		if (!content) return '';
		if (content.length <= maxLen) return content;
		return content.slice(0, maxLen) + '...';
	}

	function lineCount(content: string): number {
		if (!content) return 0;
		return content.split('\n').length;
	}
</script>

<div
	class="h-full overflow-y-auto bg-zinc-950 p-3 space-y-1.5 font-mono text-sm"
	bind:this={scrollContainer}
	onscroll={handleScroll}
>
	{#if activities.length === 0}
		<div class="flex items-center justify-center h-full text-zinc-500">
			<span>Waiting for output...</span>
		</div>
	{:else}
		{#each activities as activity (activity.id)}
			{@const meta = getActivityMeta(activity.type)}
			<div class="rounded-md border {meta.border} {meta.bg} overflow-hidden">
				<!-- Header -->
				<button
					class="w-full flex items-center gap-2 px-3 py-1.5 text-left hover:bg-white/5 transition-colors"
					onclick={() => toggleCollapse(activity)}
					type="button"
				>
					<span class="text-base shrink-0">{meta.icon}</span>
					<span class="{meta.color} font-medium truncate flex-1">
						{activity.title || activity.type}
					</span>
					{#if activity.file}
						<span class="text-zinc-500 text-xs truncate max-w-[250px]" title={activity.file}>
							{activity.file}
						</span>
					{/if}
					{#if activity.command}
						<code class="text-zinc-500 text-xs truncate max-w-[250px] bg-zinc-800/60 px-1.5 py-0.5 rounded" title={activity.command}>
							{activity.command.length > 60 ? activity.command.slice(0, 60) + '...' : activity.command}
						</code>
					{/if}
					{#if activity.content}
						<span class="text-zinc-600 text-xs shrink-0">
							{lineCount(activity.content)} lines
						</span>
						<span class="text-zinc-600 text-xs shrink-0 transition-transform" class:rotate-180={!activity.collapsed}>
							▾
						</span>
					{/if}
				</button>

				<!-- Content (collapsible) -->
				{#if activity.content && !activity.collapsed}
					<div class="border-t border-zinc-800/60">
						<pre class="px-3 py-2 text-zinc-300 text-xs leading-relaxed overflow-x-auto max-h-[400px] overflow-y-auto whitespace-pre-wrap break-words">{activity.content}</pre>
					</div>
				{:else if activity.content && activity.collapsed && activity.type === 'error'}
					<!-- Always show preview for errors -->
					<div class="border-t border-red-900/30">
						<pre class="px-3 py-1.5 text-red-300/80 text-xs leading-relaxed overflow-x-auto whitespace-pre-wrap break-words">{contentPreview(activity.content, 300)}</pre>
					</div>
				{/if}
			</div>
		{/each}

		<!-- Stats footer -->
		<div class="text-zinc-600 text-xs text-center py-2">
			{activities.length} activities
			· {activities.filter(a => a.type.startsWith('tool_')).length} tool calls
			· {activities.filter(a => a.type === 'error').length} errors
		</div>
	{/if}
</div>
