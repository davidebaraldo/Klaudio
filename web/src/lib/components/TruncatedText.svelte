<script lang="ts">
	interface Props {
		text: string;
		maxLength?: number;
		title?: string;
		class?: string;
		preformatted?: boolean;
	}
	let { text, maxLength = 300, title = 'Full Content', class: className = '', preformatted = false }: Props = $props();

	let modalOpen = $state(false);
	let copied = $state(false);

	let isTruncated = $derived(text.length > maxLength);
	let displayText = $derived(isTruncated ? text.slice(0, maxLength) + '...' : text);

	function openModal() {
		modalOpen = true;
	}

	function closeModal() {
		modalOpen = false;
	}

	async function copyToClipboard() {
		try {
			await navigator.clipboard.writeText(text);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			// ignore
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && modalOpen) {
			closeModal();
		}
	}
</script>

<svelte:window onkeydown={handleKeydown} />

<span class="truncated-text {className}">
	{#if preformatted}
		<pre class="whitespace-pre-wrap font-mono text-inherit">{displayText}</pre>
	{:else}
		{displayText}
	{/if}
	{#if isTruncated}
		<button
			onclick={openModal}
			class="inline-flex items-center gap-1 ml-1 px-1.5 py-0.5 text-[10px] font-medium text-blue-400 hover:text-blue-300 bg-blue-900/20 hover:bg-blue-900/40 border border-blue-800/50 rounded transition-colors cursor-pointer"
		>
			<svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
			</svg>
			View all
		</button>
	{/if}
</span>

{#if modalOpen}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- svelte-ignore a11y_interactive_supports_focus -->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-black/70 z-50 flex items-center justify-center p-4"
		role="dialog"
		aria-modal="true"
		onclick={(e) => { if (e.target === e.currentTarget) closeModal(); }}
	>
		<div class="bg-zinc-900 border border-zinc-700 rounded-lg shadow-2xl w-full max-w-4xl max-h-[85vh] flex flex-col">
			<!-- Header -->
			<div class="flex items-center justify-between px-4 py-3 border-b border-zinc-700">
				<h3 class="text-sm font-medium text-zinc-200">{title}</h3>
				<div class="flex items-center gap-2">
					<button
						onclick={copyToClipboard}
						class="px-2 py-1 text-xs text-zinc-400 hover:text-zinc-200 bg-zinc-800 hover:bg-zinc-700 rounded transition-colors"
					>
						{copied ? 'Copied!' : 'Copy'}
					</button>
					<button
						onclick={closeModal}
						class="p-1 text-zinc-400 hover:text-zinc-200 transition-colors"
						aria-label="Close"
					>
						<svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
						</svg>
					</button>
				</div>
			</div>
			<!-- Content -->
			<div class="flex-1 overflow-auto min-h-0 p-4">
				<pre class="text-sm font-mono text-zinc-300 whitespace-pre-wrap break-words">{text}</pre>
			</div>
			<!-- Footer -->
			<div class="px-4 py-2 border-t border-zinc-700 text-[10px] text-zinc-500 text-right">
				{text.length.toLocaleString()} characters
			</div>
		</div>
	</div>
{/if}
