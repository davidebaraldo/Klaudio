<script lang="ts">
	import { onMount } from 'svelte';
	import { createTaskStream } from '$lib/stores/websocket';
	import { parseOutput, type Activity } from '$lib/utils/outputParser';
	import ParsedOutput from './ParsedOutput.svelte';

	const { taskId, agentId = '' }: { taskId: string; agentId?: string } = $props();

	let terminalEl: HTMLDivElement;
	let stream: ReturnType<typeof createTaskStream> | null = null;
	let mode: 'raw' | 'parsed' = $state('raw');
	let activities: Activity[] = $state([]);
	let rawText = $state('');
	let parseTimer: ReturnType<typeof setTimeout> | null = null;

	const decoder = new TextDecoder();

	function scheduleReparse() {
		if (parseTimer) return;
		parseTimer = setTimeout(() => {
			parseTimer = null;
			activities = parseOutput(rawText);
		}, 300);
	}

	onMount(() => {
		let cleanup: (() => void) | undefined;

		(async () => {
		const { Terminal } = await import('@xterm/xterm');
		const { FitAddon } = await import('@xterm/addon-fit');
		const { WebLinksAddon } = await import('@xterm/addon-web-links');

		// Import xterm CSS
		await import('@xterm/xterm/css/xterm.css');

		const term = new Terminal({
			theme: {
				background: '#1a1b26',
				foreground: '#a9b1d6',
				cursor: '#c0caf5',
				selectionBackground: '#33467c',
				black: '#15161e',
				red: '#f7768e',
				green: '#9ece6a',
				yellow: '#e0af68',
				blue: '#7aa2f7',
				magenta: '#bb9af7',
				cyan: '#7dcfff',
				white: '#a9b1d6'
			},
			fontSize: 13,
			fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Consolas', monospace",
			cursorBlink: false,
			disableStdin: true,
			scrollback: 10000,
			convertEol: true
		});

		const fitAddon = new FitAddon();
		term.loadAddon(fitAddon);
		term.loadAddon(new WebLinksAddon());
		term.open(terminalEl);
		fitAddon.fit();

		// Connect to WebSocket filtered by agentId
		stream = createTaskStream(taskId, agentId || undefined);

		// Subscribe to terminal data — write to xterm AND accumulate for parsed view
		const unsubTerminal = stream.terminalData.subscribe((data) => {
			if (data) {
				term.write(data);
				rawText += decoder.decode(data, { stream: true });
				scheduleReparse();
			}
		});

		stream.connect();

		// Handle resize
		const resizeObserver = new ResizeObserver(() => {
			fitAddon.fit();
		});
		resizeObserver.observe(terminalEl);

		cleanup = () => {
			unsubTerminal();
			stream?.disconnect();
			resizeObserver.disconnect();
			term.dispose();
			if (parseTimer) clearTimeout(parseTimer);
		};
		})();

		return () => cleanup?.();
	});
</script>

<div class="h-full w-full flex flex-col min-h-[200px]">
	<!-- Mode toggle bar -->
	<div class="flex items-center gap-1 px-2 py-1 bg-zinc-900 border-b border-zinc-800 shrink-0">
		<button
			class="px-2.5 py-0.5 text-xs font-medium rounded transition-colors {mode === 'raw'
				? 'bg-zinc-700 text-zinc-100'
				: 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800'}"
			onclick={() => (mode = 'raw')}
			type="button"
		>
			Raw
		</button>
		<button
			class="px-2.5 py-0.5 text-xs font-medium rounded transition-colors {mode === 'parsed'
				? 'bg-zinc-700 text-zinc-100'
				: 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800'}"
			onclick={() => (mode = 'parsed')}
			type="button"
		>
			Parsed
		</button>
		{#if mode === 'parsed'}
			<span class="text-zinc-600 text-xs ml-auto">
				{activities.filter(a => a.type.startsWith('tool_')).length} tools
				· {activities.filter(a => a.type === 'error').length} errors
			</span>
		{/if}
	</div>

	<!-- Terminal views -->
	<div class="flex-1 relative overflow-hidden">
		<div
			bind:this={terminalEl}
			class="absolute inset-0"
			style="background: #1a1b26;"
			class:hidden={mode !== 'raw'}
		></div>
		{#if mode === 'parsed'}
			<div class="absolute inset-0">
				<ParsedOutput {activities} />
			</div>
		{/if}
	</div>
</div>
