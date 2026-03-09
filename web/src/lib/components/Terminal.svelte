<script lang="ts">
	import { onMount } from 'svelte';
	import { createTaskStream } from '$lib/stores/websocket';

	const { taskId, agentId = '' }: { taskId: string; agentId?: string } = $props();

	let terminalEl: HTMLDivElement;
	let stream: ReturnType<typeof createTaskStream> | null = null;

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

		// Subscribe to terminal data
		const unsubTerminal = stream.terminalData.subscribe((data) => {
			if (data) {
				term.write(data);
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
		};
		})();

		return () => cleanup?.();
	});
</script>

<div bind:this={terminalEl} class="h-full w-full min-h-[200px]" style="background: #1a1b26;"></div>
