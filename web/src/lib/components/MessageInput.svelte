<script lang="ts">
	const { agentId, disabled = false, onsend }: {
		agentId: string;
		disabled?: boolean;
		onsend: (detail: { agentId: string; content: string }) => void;
	} = $props();

	let message = $state('');

	function send() {
		if (!message.trim() || disabled) return;
		onsend({ agentId, content: message });
		message = '';
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			send();
		}
	}
</script>

<div class="flex gap-2 p-2 border-t border-zinc-700">
	<input
		bind:value={message}
		onkeydown={handleKeydown}
		placeholder="Send message to agent..."
		class="flex-1 bg-zinc-800 text-zinc-100 px-3 py-2 rounded text-sm border border-zinc-700 focus:border-zinc-500 focus:outline-none"
		{disabled}
	/>
	<button
		onclick={send}
		class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded text-sm transition-colors"
		{disabled}
	>
		Send
	</button>
</div>
