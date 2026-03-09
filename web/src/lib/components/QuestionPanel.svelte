<script lang="ts">
	import { onMount } from 'svelte';
	import { getQuestions, answerQuestion, type Question } from '$lib/api';

	const { taskId, polling = true }: { taskId: string; polling?: boolean } = $props();

	let questions = $state<Question[]>([]);
	let answers = $state<Record<string, string>>({});
	let submitting = $state<Record<string, boolean>>({});
	let error = $state('');

	async function loadQuestions() {
		try {
			const result = await getQuestions(taskId);
			questions = result.questions ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load questions';
		}
	}

	async function submitAnswer(questionId: string) {
		const answer = answers[questionId]?.trim();
		if (!answer) return;

		submitting = { ...submitting, [questionId]: true };
		error = '';
		try {
			await answerQuestion(taskId, questionId, answer);
			questions = questions.map((q) =>
				q.id === questionId ? { ...q, status: 'answered', answer } : q
			);
			delete answers[questionId];
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to submit answer';
		} finally {
			submitting = { ...submitting, [questionId]: false };
		}
	}

	function selectSuggestion(questionId: string, suggestion: string) {
		answers[questionId] = suggestion;
	}

	function selectOption(questionId: string, option: string) {
		answers[questionId] = option;
	}

	function formatTime(ts: string): string {
		try {
			return new Date(ts).toLocaleString();
		} catch {
			return ts;
		}
	}

	onMount(() => {
		loadQuestions();

		// Poll for new questions every 3s while panel is visible
		const interval = polling
			? setInterval(() => {
					// Only poll if there are no questions yet, or some are still pending
					const hasPending = questions.length === 0 || questions.some((q) => q.status === 'pending');
					if (hasPending) loadQuestions();
				}, 3000)
			: null;

		return () => {
			if (interval) clearInterval(interval);
		};
	});
</script>

<div class="space-y-4">
	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm">{error}</div>
	{/if}

	{#if questions.length === 0}
		<div class="p-4 text-center text-zinc-500 text-sm">No questions from the planner.</div>
	{:else}
		{#each questions as question}
			<div class="p-4 bg-zinc-800/50 rounded border border-zinc-700">
				<div class="flex items-start justify-between mb-2">
					<p class="text-sm text-zinc-200">{question.text}</p>
					<span class="text-xs text-zinc-500 shrink-0 ml-4">{formatTime(question.asked_at)}</span>
				</div>

				{#if question.status === 'pending'}
					<!-- Multiple choice options -->
					{#if question.options && question.options.length > 0}
						<div class="flex flex-wrap gap-2 mt-3 mb-3">
							{#each question.options as option}
								<button
									onclick={() => selectOption(question.id, option)}
									class="px-3 py-1.5 text-sm rounded border transition-colors {answers[question.id] === option
										? 'bg-blue-600 border-blue-500 text-white'
										: 'bg-zinc-900 border-zinc-600 text-zinc-300 hover:border-zinc-500 hover:text-zinc-200'}"
								>
									{option}
								</button>
							{/each}
						</div>
					{/if}

					<!-- Suggestion chips -->
					{#if question.suggestions && question.suggestions.length > 0}
						<div class="flex flex-wrap gap-1.5 mt-2 mb-2">
							{#each question.suggestions as suggestion}
								<button
									onclick={() => selectSuggestion(question.id, suggestion)}
									class="px-2.5 py-1 text-xs rounded-full border transition-colors {answers[question.id] === suggestion
										? 'bg-violet-600 border-violet-500 text-white'
										: 'bg-zinc-900/60 border-zinc-700 text-zinc-400 hover:border-violet-500/50 hover:text-zinc-300'}"
								>
									{suggestion}
								</button>
							{/each}
						</div>
					{/if}

					<!-- Text input + submit -->
					<div class="flex gap-2 mt-3">
						<input
							bind:value={answers[question.id]}
							placeholder={question.options?.length ? 'Or type a custom answer...' : question.suggestions?.length ? 'Type or pick a suggestion...' : 'Type your answer...'}
							class="flex-1 bg-zinc-900 text-zinc-200 text-sm px-3 py-2 rounded border border-zinc-600 focus:border-zinc-500 focus:outline-none"
							onkeydown={(e) => { if (e.key === 'Enter') submitAnswer(question.id); }}
						/>
						<button
							onclick={() => submitAnswer(question.id)}
							disabled={submitting[question.id] || !answers[question.id]?.trim()}
							class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 disabled:text-zinc-500 text-white text-sm rounded transition-colors"
						>
							{submitting[question.id] ? '...' : 'Answer'}
						</button>
					</div>
				{:else if question.status === 'answered'}
					<div class="mt-2 p-2 bg-zinc-900 rounded text-sm text-zinc-400">
						Answer: <span class="text-zinc-300">{question.answer}</span>
					</div>
				{/if}
			</div>
		{/each}
	{/if}
</div>
