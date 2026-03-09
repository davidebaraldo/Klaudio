<script lang="ts">
	const { status }: { status: string } = $props();

	const colorMap: Record<string, string> = {
		running: 'bg-green-600/20 text-green-400 border-green-600/30',
		completed: 'bg-green-600/20 text-green-400 border-green-600/30',
		planning: 'bg-yellow-600/20 text-yellow-400 border-yellow-600/30',
		planned: 'bg-yellow-600/20 text-yellow-400 border-yellow-600/30',
		approved: 'bg-blue-600/20 text-blue-400 border-blue-600/30',
		failed: 'bg-red-600/20 text-red-400 border-red-600/30',
		error: 'bg-red-600/20 text-red-400 border-red-600/30',
		paused: 'bg-zinc-600/20 text-zinc-400 border-zinc-600/30',
		created: 'bg-zinc-600/20 text-zinc-400 border-zinc-600/30',
		pending: 'bg-zinc-600/20 text-zinc-400 border-zinc-600/30',
		modified: 'bg-blue-600/20 text-blue-400 border-blue-600/30'
	};

	const classes = $derived(colorMap[status] || 'bg-zinc-600/20 text-zinc-400 border-zinc-600/30');
</script>

<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border {classes}">
	{status}
</span>
