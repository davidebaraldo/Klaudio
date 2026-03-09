<script lang="ts">
	import { goto } from '$app/navigation';
	import {
		createTask,
		uploadFiles,
		getRepoTemplates,
		createRepoTemplate,
		getTeamTemplates,
		type CreateTaskRequest,
		type RepoTemplate,
		type CreateRepoTemplateRequest,
		type RepoOverrides,
		type TeamTemplate
	} from '$lib/api';
	import { loadTasks } from '$lib/stores/tasks';

	let step = $state(1);
	let error = $state('');
	let creating = $state(false);

	// Step 1: Basics
	let name = $state('');
	let prompt = $state('');

	// Step 2: Repository Template
	let templates = $state<RepoTemplate[]>([]);
	let selectedTemplateId = $state('');
	let loadingTemplates = $state(false);
	let showNewTemplate = $state(false);
	let creatingTemplate = $state(false);

	// Per-task repo permission overrides (initialized from selected template)
	let repoOverrides = $state<RepoOverrides>({
		auto_branch: false,
		auto_commit: false,
		auto_push: false,
		auto_pr: false
	});

	// New template form
	let newTpl = $state<CreateRepoTemplateRequest>({
		name: '',
		url: '',
		default_branch: 'main',
		access_token: '',
		auto_branch: false,
		auto_commit: false,
		auto_push: false,
		auto_pr: false,
		pr_target: 'main'
	});

	// Team template
	let teamTemplates = $state<TeamTemplate[]>([]);
	let selectedTeamId = $state('');

	// Step 3: Files
	let inputFiles = $state<File[]>([]);
	let outputFilePaths = $state('');
	let dragOver = $state(false);

	function selectTemplate(id: string) {
		selectedTemplateId = id;
		showNewTemplate = false;
		const tpl = templates.find((t) => t.id === id);
		if (tpl) {
			repoOverrides = {
				auto_branch: tpl.auto_branch,
				auto_commit: tpl.auto_commit,
				auto_push: tpl.auto_push,
				auto_pr: tpl.auto_pr
			};
		}
	}

	async function loadTemplates() {
		loadingTemplates = true;
		try {
			const res = await getRepoTemplates();
			templates = res.templates ?? [];
		} catch {
			templates = [];
		} finally {
			loadingTemplates = false;
		}
	}

	function nextStep() {
		if (step === 1 && (!name.trim() || !prompt.trim())) {
			error = 'Name and prompt are required.';
			return;
		}
		error = '';
		if (step === 2 && !loadingTemplates && templates.length === 0) {
			loadTemplates();
		}
		step = Math.min(step + 1, 3);
	}

	function prevStep() {
		error = '';
		step = Math.max(step - 1, 1);
	}

	function handleDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;
		if (e.dataTransfer?.files) {
			inputFiles = [...inputFiles, ...Array.from(e.dataTransfer.files)];
		}
	}

	function handleFileInput(e: Event) {
		const input = e.target as HTMLInputElement;
		if (input.files) {
			inputFiles = [...inputFiles, ...Array.from(input.files)];
		}
	}

	function removeFile(index: number) {
		inputFiles = inputFiles.filter((_, i) => i !== index);
	}

	async function handleCreateTemplate() {
		if (!newTpl.name?.trim() || !newTpl.url?.trim()) {
			error = 'Template name and URL are required.';
			return;
		}
		creatingTemplate = true;
		error = '';
		try {
			const created = await createRepoTemplate(newTpl);
			templates = [...templates, created];
			selectedTemplateId = created.id;
			showNewTemplate = false;
			newTpl = { name: '', url: '', default_branch: 'main', access_token: '', auto_branch: false, auto_commit: false, auto_push: false, auto_pr: false, pr_target: 'main' };
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create template';
		} finally {
			creatingTemplate = false;
		}
	}

	async function handleCreate() {
		if (!name.trim() || !prompt.trim()) {
			error = 'Name and prompt are required.';
			return;
		}

		creating = true;
		error = '';

		try {
			const data: CreateTaskRequest = {
				name: name.trim(),
				prompt: prompt.trim()
			};

			if (selectedTemplateId) {
				data.repo_template_id = selectedTemplateId;
				data.repo_overrides = repoOverrides;
			}

			if (selectedTeamId) {
				data.team_template = selectedTeamId;
			}

			const outPaths = outputFilePaths
				.split('\n')
				.map((p) => p.trim())
				.filter(Boolean);
			if (outPaths.length > 0) {
				data.output_files = outPaths;
			}

			const task = await createTask(data);

			// Upload files if any
			if (inputFiles.length > 0) {
				await uploadFiles(task.id, inputFiles);
			}

			await loadTasks();
			goto(`/tasks/${task.id}`);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create task';
		} finally {
			creating = false;
		}
	}

	async function loadTeamTemplates() {
		try {
			const res = await getTeamTemplates();
			teamTemplates = res.templates ?? [];
		} catch {
			teamTemplates = [];
		}
	}

	// Load templates when entering step 2
	$effect(() => {
		if (step === 2 && templates.length === 0 && !loadingTemplates) {
			loadTemplates();
			loadTeamTemplates();
		}
	});
</script>

<div class="p-6 max-w-2xl mx-auto">
	<h1 class="text-2xl font-semibold text-zinc-100 mb-6">New Task</h1>

	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm mb-4">{error}</div>
	{/if}

	<!-- Step indicator -->
	<div class="flex items-center gap-2 mb-8">
		{#each [1, 2, 3] as s}
			<button
				onclick={() => { if (s < step) { step = s; error = ''; } }}
				class="flex items-center gap-2 text-sm {s === step ? 'text-zinc-100' : s < step ? 'text-zinc-400' : 'text-zinc-600'}"
			>
				<span class="w-6 h-6 rounded-full flex items-center justify-center text-xs border
					{s === step ? 'bg-blue-600 border-blue-600 text-white' : s < step ? 'border-zinc-500 text-zinc-400' : 'border-zinc-700 text-zinc-600'}">
					{s}
				</span>
				<span>{s === 1 ? 'Basics' : s === 2 ? 'Repository' : 'Files'}</span>
			</button>
			{#if s < 3}
				<div class="flex-1 h-px bg-zinc-800"></div>
			{/if}
		{/each}
	</div>

	<!-- Step 1: Basics -->
	{#if step === 1}
		<div class="space-y-4">
			<div>
				<label for="name" class="block text-sm text-zinc-400 mb-1">Task Name</label>
				<input
					id="name"
					bind:value={name}
					placeholder="e.g., Refactor API layer"
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
				/>
			</div>
			<div>
				<label for="prompt" class="block text-sm text-zinc-400 mb-1">Prompt</label>
				<textarea
					id="prompt"
					bind:value={prompt}
					placeholder="Describe what you want the agents to do..."
					rows="10"
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm resize-y"
				></textarea>
			</div>
		</div>
	{/if}

	<!-- Step 2: Repository Template -->
	{#if step === 2}
		<div class="space-y-4">
			<p class="text-sm text-zinc-500">Optional: select a repository template to work from. You can create one if needed.</p>

			{#if loadingTemplates}
				<div class="p-4 text-center text-zinc-500 text-sm">Loading templates...</div>
			{:else}
				<!-- Template list -->
				<div class="space-y-2">
					<!-- No repo option -->
					<button
						onclick={() => { selectedTemplateId = ''; showNewTemplate = false; }}
						class="w-full text-left p-3 rounded border transition-colors {selectedTemplateId === '' && !showNewTemplate
							? 'border-blue-500 bg-blue-500/10'
							: 'border-zinc-700 bg-zinc-800/50 hover:border-zinc-600'}"
					>
						<div class="text-sm text-zinc-300 font-medium">No repository</div>
						<div class="text-xs text-zinc-500 mt-0.5">The task will run without a Git repository</div>
					</button>

					{#each templates as tpl}
						<button
							onclick={() => selectTemplate(tpl.id)}
							class="w-full text-left p-3 rounded border transition-colors {selectedTemplateId === tpl.id
								? 'border-blue-500 bg-blue-500/10'
								: 'border-zinc-700 bg-zinc-800/50 hover:border-zinc-600'}"
						>
							<div class="flex items-center justify-between">
								<span class="text-sm text-zinc-200 font-medium">{tpl.name}</span>
								<div class="flex gap-1.5">
									{#if tpl.auto_branch}
										<span class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-700 text-zinc-400">branch</span>
									{/if}
									{#if tpl.auto_commit}
										<span class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-700 text-zinc-400">commit</span>
									{/if}
									{#if tpl.auto_push}
										<span class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-700 text-zinc-400">push</span>
									{/if}
									{#if tpl.auto_pr}
										<span class="text-[10px] px-1.5 py-0.5 rounded bg-emerald-900/50 text-emerald-400">PR</span>
									{/if}
								</div>
							</div>
							<div class="text-xs text-zinc-500 mt-0.5 font-mono truncate">{tpl.url}</div>
							<div class="text-xs text-zinc-600 mt-0.5">branch: {tpl.default_branch}{tpl.auto_pr ? ` → ${tpl.pr_target}` : ''}</div>
						</button>
					{/each}
				</div>

				<!-- Per-task permission overrides (shown when a template is selected) -->
				{#if selectedTemplateId}
					<div class="p-3 rounded border border-zinc-700 bg-zinc-800/30">
						<h4 class="text-xs font-medium text-zinc-400 mb-2">Permissions for this task</h4>
						<div class="flex flex-wrap gap-x-6 gap-y-2">
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={repoOverrides.auto_branch} class="rounded bg-zinc-800 border-zinc-600" />
								Create branch
							</label>
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={repoOverrides.auto_commit} class="rounded bg-zinc-800 border-zinc-600" />
								Commit
							</label>
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={repoOverrides.auto_push} class="rounded bg-zinc-800 border-zinc-600" />
								Push
							</label>
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={repoOverrides.auto_pr} class="rounded bg-zinc-800 border-zinc-600" />
								Create PR
							</label>
						</div>
					</div>
				{/if}

				<!-- Create new template toggle -->
				{#if !showNewTemplate}
					<button
						onclick={() => { showNewTemplate = true; selectedTemplateId = ''; }}
						class="w-full p-3 rounded border border-dashed border-zinc-700 hover:border-zinc-500 text-sm text-zinc-400 hover:text-zinc-300 transition-colors"
					>
						+ Create New Repository Template
					</button>
				{/if}

				<!-- New template form -->
				{#if showNewTemplate}
					<div class="p-4 rounded border border-zinc-600 bg-zinc-800/80 space-y-3">
						<h3 class="text-sm font-medium text-zinc-200">New Repository Template</h3>

						<div class="grid grid-cols-2 gap-3">
							<div>
								<label class="block text-xs text-zinc-400 mb-1">Template Name *</label>
								<input
									bind:value={newTpl.name}
									placeholder="e.g., My Project"
									class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
								/>
							</div>
							<div>
								<label class="block text-xs text-zinc-400 mb-1">Repository URL *</label>
								<input
									bind:value={newTpl.url}
									placeholder="https://github.com/org/repo.git"
									class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
								/>
							</div>
						</div>

						<div class="grid grid-cols-2 gap-3">
							<div>
								<label class="block text-xs text-zinc-400 mb-1">Default Branch</label>
								<input
									bind:value={newTpl.default_branch}
									placeholder="main"
									class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
								/>
							</div>
							<div>
								<label class="block text-xs text-zinc-400 mb-1">PR Target Branch</label>
								<input
									bind:value={newTpl.pr_target}
									placeholder="main"
									class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
								/>
							</div>
						</div>

						<div>
							<label class="block text-xs text-zinc-400 mb-1">Access Token</label>
							<input
								type="password"
								bind:value={newTpl.access_token}
								placeholder="Token (optional)"
								class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
							/>
						</div>

						<div class="flex flex-wrap gap-x-6 gap-y-2">
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={newTpl.auto_branch} class="rounded bg-zinc-800 border-zinc-600" />
								Create branch
							</label>
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={newTpl.auto_commit} class="rounded bg-zinc-800 border-zinc-600" />
								Commit
							</label>
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={newTpl.auto_push} class="rounded bg-zinc-800 border-zinc-600" />
								Push
							</label>
							<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
								<input type="checkbox" bind:checked={newTpl.auto_pr} class="rounded bg-zinc-800 border-zinc-600" />
								Create PR
							</label>
						</div>

						<div class="flex gap-2 pt-1">
							<button
								onclick={handleCreateTemplate}
								disabled={creatingTemplate}
								class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded transition-colors"
							>
								{creatingTemplate ? 'Creating...' : 'Save Template'}
							</button>
							<button
								onclick={() => { showNewTemplate = false; }}
								class="px-4 py-2 text-sm bg-zinc-700 hover:bg-zinc-600 text-zinc-300 rounded transition-colors"
							>
								Cancel
							</button>
						</div>
					</div>
				{/if}
			{/if}

			<!-- Team Template -->
			<div class="mt-6 pt-6 border-t border-zinc-800">
				<h3 class="text-sm font-medium text-zinc-300 mb-2">Team Template (optional)</h3>
				<p class="text-xs text-zinc-500 mb-3">Choose a team composition for the planner. If not set, the planner decides freely.</p>
				<div class="space-y-2">
					<button
						onclick={() => { selectedTeamId = ''; }}
						class="w-full text-left p-3 rounded border transition-colors {selectedTeamId === ''
							? 'border-blue-500 bg-blue-500/10'
							: 'border-zinc-700 bg-zinc-800/50 hover:border-zinc-600'}"
					>
						<div class="text-sm text-zinc-300 font-medium">Default (no template)</div>
						<div class="text-xs text-zinc-500 mt-0.5">The planner decides the team composition</div>
					</button>

					{#each teamTemplates as tt}
						<button
							onclick={() => { selectedTeamId = tt.id; }}
							class="w-full text-left p-3 rounded border transition-colors {selectedTeamId === tt.id
								? 'border-blue-500 bg-blue-500/10'
								: 'border-zinc-700 bg-zinc-800/50 hover:border-zinc-600'}"
						>
							<div class="flex items-center justify-between">
								<span class="text-sm text-zinc-200 font-medium">{tt.name}</span>
								<div class="flex gap-1.5">
									<span class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-700 text-zinc-400">max {tt.max_agents}</span>
									{#if tt.review}
										<span class="text-[10px] px-1.5 py-0.5 rounded bg-purple-900/50 text-purple-400">review</span>
									{/if}
								</div>
							</div>
							{#if tt.description}
								<div class="text-xs text-zinc-500 mt-0.5">{tt.description}</div>
							{/if}
							<div class="flex gap-1.5 mt-1.5">
								{#each tt.roles as role}
									<span class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-700/50 text-zinc-400 border border-zinc-700">
										{role.name}{role.max_instances > 1 ? ` x${role.max_instances}` : ''}
									</span>
								{/each}
							</div>
						</button>
					{/each}
				</div>
			</div>
		</div>
	{/if}

	<!-- Step 3: Files -->
	{#if step === 3}
		<div class="space-y-4">
			<div>
				<label for="file-upload" class="block text-sm text-zinc-400 mb-2">Input Files (optional)</label>
				<div
					role="button"
					tabindex="0"
					class="border-2 border-dashed rounded-lg p-6 text-center transition-colors
						{dragOver ? 'border-blue-500 bg-blue-500/10' : 'border-zinc-700 hover:border-zinc-500'}"
					ondrop={handleDrop}
					ondragover={(e) => { e.preventDefault(); dragOver = true; }}
					ondragleave={() => { dragOver = false; }}
				>
					<p class="text-sm text-zinc-400 mb-2">Drag and drop files here</p>
					<label class="inline-block px-4 py-2 bg-zinc-700 hover:bg-zinc-600 text-zinc-200 text-sm rounded cursor-pointer transition-colors">
						Browse files
						<input id="file-upload" type="file" multiple class="hidden" onchange={handleFileInput} />
					</label>
				</div>

				{#if inputFiles.length > 0}
					<div class="mt-2 space-y-1">
						{#each inputFiles as file, i}
							<div class="flex items-center justify-between p-2 bg-zinc-800/50 rounded text-sm">
								<span class="text-zinc-300">{file.name}</span>
								<button onclick={() => removeFile(i)} class="text-xs text-red-400 hover:text-red-300">Remove</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>

			<div>
				<label for="output-paths" class="block text-sm text-zinc-400 mb-1">Expected Output Files (one per line)</label>
				<textarea
					id="output-paths"
					bind:value={outputFilePaths}
					placeholder="api/handler.go&#10;api/service.go"
					rows="4"
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm font-mono resize-y"
				></textarea>
			</div>
		</div>
	{/if}

	<!-- Navigation -->
	<div class="flex items-center justify-between mt-8">
		<div>
			{#if step > 1}
				<button onclick={prevStep} class="px-4 py-2 text-sm bg-zinc-800 hover:bg-zinc-700 text-zinc-300 rounded transition-colors">
					Back
				</button>
			{/if}
		</div>
		<div class="flex gap-2">
			{#if step < 3}
				<button onclick={nextStep} class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded transition-colors">
					Next
				</button>
			{/if}
			{#if step === 3}
				<button
					onclick={handleCreate}
					disabled={creating}
					class="px-6 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded transition-colors"
				>
					{creating ? 'Creating...' : 'Create Task'}
				</button>
			{/if}
		</div>
	</div>
</div>
