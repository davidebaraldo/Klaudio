<script lang="ts">
	import { onMount } from 'svelte';
	import {
		getConfig,
		updateAuth,
		getRepoTemplates,
		createRepoTemplate,
		getTeamTemplates,
		createTeamTemplate,
		deleteTeamTemplate,
		type AppConfig,
		type RepoTemplate,
		type TeamTemplate,
		type TeamRole
	} from '$lib/api';

	let config = $state<AppConfig | null>(null);
	let templates = $state<RepoTemplate[]>([]);
	let teamTemplates = $state<TeamTemplate[]>([]);
	let error = $state('');
	let success = $state('');

	// Auth settings
	let authMode = $state('host');
	let sessionKey = $state('');
	let savingAuth = $state(false);

	// New template
	let newTplName = $state('');
	let newTplUrl = $state('');
	let newTplBranch = $state('');
	let newTplToken = $state('');
	let newTplAutoBranch = $state(false);
	let newTplAutoCommit = $state(false);
	let newTplAutoPush = $state(false);
	let newTplAutoPr = $state(false);
	let newTplPrTarget = $state('');
	let savingTpl = $state(false);

	// New team template
	let newTeamName = $state('');
	let newTeamDesc = $state('');
	let newTeamMaxAgents = $state(3);
	let newTeamReview = $state(false);
	let newTeamMode = $state<'sequential' | 'collaborative'>('sequential');
	let newTeamRoles = $state<TeamRole[]>([
		{ name: 'developer', description: 'Writes production code', prompt_hint: '', max_instances: 2, run_last: false }
	]);
	let savingTeam = $state(false);

	async function loadConfig() {
		try {
			config = await getConfig();
			authMode = config.claude?.auth_mode ?? 'host';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load config';
		}
	}

	async function loadTemplates() {
		try {
			const result = await getRepoTemplates();
			templates = result.templates ?? [];
		} catch {
			// ignore
		}
	}

	async function saveAuth() {
		savingAuth = true;
		error = '';
		success = '';
		try {
			await updateAuth(authMode, authMode === 'env' ? sessionKey : undefined);
			success = 'Auth settings saved.';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save';
		} finally {
			savingAuth = false;
		}
	}

	async function addTemplate() {
		if (!newTplName.trim() || !newTplUrl.trim()) {
			error = 'Template name and URL are required.';
			return;
		}
		savingTpl = true;
		error = '';
		success = '';
		try {
			await createRepoTemplate({
				name: newTplName.trim(),
				url: newTplUrl.trim(),
				default_branch: newTplBranch.trim() || undefined,
				access_token: newTplToken.trim() || undefined,
				auto_branch: newTplAutoBranch,
				auto_commit: newTplAutoCommit,
				auto_push: newTplAutoPush,
				auto_pr: newTplAutoPr,
				pr_target: newTplPrTarget.trim() || undefined
			});
			newTplName = '';
			newTplUrl = '';
			newTplBranch = '';
			newTplToken = '';
			newTplAutoBranch = false;
			newTplAutoCommit = false;
			newTplAutoPush = false;
			newTplAutoPr = false;
			newTplPrTarget = '';
			success = 'Template created.';
			await loadTemplates();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create template';
		} finally {
			savingTpl = false;
		}
	}

	async function loadTeamTemplates() {
		try {
			const result = await getTeamTemplates();
			teamTemplates = result.templates ?? [];
		} catch {
			// ignore
		}
	}

	function addRole() {
		newTeamRoles = [...newTeamRoles, { name: '', description: '', prompt_hint: '', max_instances: 1, run_last: false }];
	}

	function removeRole(index: number) {
		newTeamRoles = newTeamRoles.filter((_, i) => i !== index);
	}

	async function addTeamTemplate() {
		if (!newTeamName.trim()) {
			error = 'Team template name is required.';
			return;
		}
		if (newTeamRoles.length === 0 || !newTeamRoles[0].name.trim()) {
			error = 'At least one role with a name is required.';
			return;
		}
		savingTeam = true;
		error = '';
		success = '';
		try {
			await createTeamTemplate({
				name: newTeamName.trim(),
				description: newTeamDesc.trim(),
				max_agents: newTeamMaxAgents,
				review: newTeamReview,
				mode: newTeamMode,
				roles: newTeamRoles.filter(r => r.name.trim())
			});
			newTeamName = '';
			newTeamDesc = '';
			newTeamMaxAgents = 3;
			newTeamReview = false;
			newTeamMode = 'sequential';
			newTeamRoles = [{ name: 'developer', description: 'Writes production code', prompt_hint: '', max_instances: 2, run_last: false }];
			success = 'Team template created.';
			await loadTeamTemplates();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create team template';
		} finally {
			savingTeam = false;
		}
	}

	async function handleDeleteTeam(id: string) {
		if (!confirm('Delete this team template?')) return;
		try {
			await deleteTeamTemplate(id);
			await loadTeamTemplates();
			success = 'Team template deleted.';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete';
		}
	}

	onMount(() => {
		loadConfig();
		loadTemplates();
		loadTeamTemplates();
	});
</script>

<div class="p-6 max-w-2xl mx-auto">
	<h1 class="text-2xl font-semibold text-zinc-100 mb-6">Settings</h1>

	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm mb-4">{error}</div>
	{/if}
	{#if success}
		<div class="p-3 bg-green-900/20 border border-green-700 rounded text-green-400 text-sm mb-4">{success}</div>
	{/if}

	<!-- Current config info -->
	{#if config}
		<div class="p-4 bg-zinc-800/50 rounded border border-zinc-700 mb-6">
			<h2 class="text-sm font-medium text-zinc-300 mb-2">Current Configuration</h2>
			<div class="grid grid-cols-2 gap-2 text-sm">
				<span class="text-zinc-500">Server port</span>
				<span class="text-zinc-300">{config.server.port}</span>
				<span class="text-zinc-500">Docker max agents</span>
				<span class="text-zinc-300">{config.docker.max_agents}</span>
				<span class="text-zinc-500">Docker image</span>
				<span class="text-zinc-300 font-mono">{config.docker.image_name}</span>
				<span class="text-zinc-500">Auth mode</span>
				<span class="text-zinc-300">{config.claude.auth_mode}</span>
				<span class="text-zinc-500">Data directory</span>
				<span class="text-zinc-300 font-mono">{config.storage.data_dir}</span>
			</div>
		</div>
	{/if}

	<!-- Auth settings -->
	<section class="mb-8">
		<h2 class="text-lg font-medium text-zinc-200 mb-4">Claude Authentication</h2>
		<div class="space-y-4">
			<div>
				<label for="auth-mode" class="block text-sm text-zinc-400 mb-1">Auth Mode</label>
				<select
					id="auth-mode"
					bind:value={authMode}
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
				>
					<option value="host">Host (use host machine credentials)</option>
					<option value="env">Environment (session key)</option>
				</select>
			</div>

			{#if authMode === 'env'}
				<div>
					<label for="session-key" class="block text-sm text-zinc-400 mb-1">Session Key</label>
					<input
						id="session-key"
						type="password"
						bind:value={sessionKey}
						placeholder="sk-..."
						class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
					/>
				</div>
			{/if}

			<button
				onclick={saveAuth}
				disabled={savingAuth}
				class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded transition-colors"
			>
				{savingAuth ? 'Saving...' : 'Save Auth Settings'}
			</button>
		</div>
	</section>

	<!-- Repo templates -->
	<section>
		<h2 class="text-lg font-medium text-zinc-200 mb-4">Repository Templates</h2>

		{#if templates.length > 0}
			<div class="space-y-2 mb-6">
				{#each templates as tpl}
					<div class="p-3 bg-zinc-800/50 rounded border border-zinc-700">
						<div class="flex items-center justify-between">
							<span class="text-sm font-medium text-zinc-200">{tpl.name}</span>
							<span class="text-xs text-zinc-500 font-mono">{tpl.id}</span>
						</div>
						<div class="text-xs text-zinc-400 font-mono mt-1">{tpl.url}</div>
						<div class="flex items-center gap-2 mt-1.5 flex-wrap">
							{#if tpl.default_branch}
								<span class="text-xs text-zinc-500">branch: {tpl.default_branch}</span>
							{/if}
							{#if tpl.auto_branch}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-violet-900/40 text-violet-400 border border-violet-800">branch</span>
							{/if}
							{#if tpl.auto_commit}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-blue-900/40 text-blue-400 border border-blue-800">commit</span>
							{/if}
							{#if tpl.auto_push}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-amber-900/40 text-amber-400 border border-amber-800">push</span>
							{/if}
							{#if tpl.auto_pr}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-green-900/40 text-green-400 border border-green-800">PR</span>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}

		<div class="p-4 bg-zinc-800/30 rounded border border-zinc-700 space-y-3">
			<h3 class="text-sm font-medium text-zinc-300">Add Template</h3>
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label for="tpl-name" class="block text-xs text-zinc-500 mb-1">Name</label>
					<input
						id="tpl-name"
						bind:value={newTplName}
						placeholder="My Project"
						class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
					/>
				</div>
				<div>
					<label for="tpl-branch" class="block text-xs text-zinc-500 mb-1">Default Branch</label>
					<input
						id="tpl-branch"
						bind:value={newTplBranch}
						placeholder="main"
						class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
					/>
				</div>
			</div>
			<div>
				<label for="tpl-url" class="block text-xs text-zinc-500 mb-1">URL</label>
				<input
					id="tpl-url"
					bind:value={newTplUrl}
					placeholder="https://github.com/org/repo.git"
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
				/>
			</div>
			<div>
				<label for="tpl-token" class="block text-xs text-zinc-500 mb-1">Access Token (optional)</label>
				<input
					id="tpl-token"
					type="password"
					bind:value={newTplToken}
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
				/>
			</div>
			<div class="space-y-2">
				<h4 class="text-xs text-zinc-500">Permissions</h4>
				<div class="flex flex-wrap gap-x-6 gap-y-2">
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={newTplAutoBranch} class="rounded bg-zinc-800 border-zinc-600" />
						Create branch
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={newTplAutoCommit} class="rounded bg-zinc-800 border-zinc-600" />
						Commit
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={newTplAutoPush} class="rounded bg-zinc-800 border-zinc-600" />
						Push
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={newTplAutoPr} class="rounded bg-zinc-800 border-zinc-600" />
						Create PR
					</label>
				</div>
				{#if newTplAutoPr}
					<input
						bind:value={newTplPrTarget}
						placeholder="PR target branch"
						class="bg-zinc-800 text-zinc-100 px-3 py-1.5 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
					/>
				{/if}
			</div>
			<button
				onclick={addTemplate}
				disabled={savingTpl}
				class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded transition-colors"
			>
				{savingTpl ? 'Creating...' : 'Add Template'}
			</button>
		</div>
	</section>

	<!-- Team templates -->
	<section class="mt-8">
		<h2 class="text-lg font-medium text-zinc-200 mb-4">Team Templates</h2>

		{#if teamTemplates.length > 0}
			<div class="space-y-2 mb-6">
				{#each teamTemplates as tt}
					<div class="p-3 bg-zinc-800/50 rounded border border-zinc-700">
						<div class="flex items-center justify-between">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-zinc-200">{tt.name}</span>
								{#if tt.is_default}
									<span class="px-1.5 py-0.5 text-[10px] rounded bg-blue-900/40 text-blue-400 border border-blue-800">default</span>
								{/if}
							</div>
							<button
								onclick={() => handleDeleteTeam(tt.id)}
								class="text-xs text-red-400/60 hover:text-red-400 transition-colors"
							>delete</button>
						</div>
						{#if tt.description}
							<p class="text-xs text-zinc-400 mt-1">{tt.description}</p>
						{/if}
						<div class="flex items-center gap-3 mt-2 flex-wrap">
							<span class="text-[10px] text-zinc-500">max {tt.max_agents} agents</span>
							{#if tt.mode === 'collaborative'}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-orange-900/40 text-orange-400 border border-orange-800">collaborative</span>
							{:else}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-zinc-700 text-zinc-400 border border-zinc-600">sequential</span>
							{/if}
							{#if tt.review}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-purple-900/40 text-purple-400 border border-purple-800">auto-review</span>
							{/if}
							{#each tt.roles as role}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-zinc-700 text-zinc-300 border border-zinc-600">
									{role.name}{role.max_instances > 1 ? ` x${role.max_instances}` : ''}
								</span>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		{/if}

		<div class="p-4 bg-zinc-800/30 rounded border border-zinc-700 space-y-3">
			<h3 class="text-sm font-medium text-zinc-300">Add Team Template</h3>
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label class="block text-xs text-zinc-500 mb-1">Name</label>
					<input
						bind:value={newTeamName}
						placeholder="e.g. Full Team"
						class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
					/>
				</div>
				<div>
					<label class="block text-xs text-zinc-500 mb-1">Max Agents</label>
					<input
						type="number"
						min="1"
						max="10"
						bind:value={newTeamMaxAgents}
						class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
					/>
				</div>
			</div>
			<div>
				<label class="block text-xs text-zinc-500 mb-1">Description</label>
				<input
					bind:value={newTeamDesc}
					placeholder="Team with developers and reviewer"
					class="w-full bg-zinc-800 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-sm"
				/>
			</div>

			<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
				<input type="checkbox" bind:checked={newTeamReview} class="rounded bg-zinc-800 border-zinc-600" />
				Auto-review (run reviewer agent after all subtasks complete)
			</label>

			<!-- Execution Mode -->
			<div>
				<label class="block text-xs text-zinc-500 mb-1">Execution Mode</label>
				<div class="flex gap-3">
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer px-3 py-2 rounded border transition-colors {newTeamMode === 'sequential' ? 'bg-zinc-700 border-zinc-500' : 'bg-zinc-800/30 border-zinc-700'}">
						<input type="radio" bind:group={newTeamMode} value="sequential" class="text-blue-500" />
						<div>
							<div class="font-medium">Sequential (DAG)</div>
							<div class="text-[10px] text-zinc-500">Agents run in dependency order</div>
						</div>
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer px-3 py-2 rounded border transition-colors {newTeamMode === 'collaborative' ? 'bg-orange-900/20 border-orange-700' : 'bg-zinc-800/30 border-zinc-700'}">
						<input type="radio" bind:group={newTeamMode} value="collaborative" class="text-orange-500" />
						<div>
							<div class="font-medium">Collaborative</div>
							<div class="text-[10px] text-zinc-500">Manager + all workers run simultaneously</div>
						</div>
					</label>
				</div>
			</div>

			<!-- Roles -->
			<div>
				<div class="flex items-center justify-between mb-2">
					<label class="text-xs text-zinc-500">Roles</label>
					<button
						onclick={addRole}
						class="text-xs text-blue-400 hover:text-blue-300 transition-colors"
					>+ Add role</button>
				</div>
				<div class="space-y-2">
					{#each newTeamRoles as role, i}
						<div class="p-3 bg-zinc-900/50 rounded border border-zinc-700 space-y-2">
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-zinc-500">Role {i + 1}</span>
								{#if newTeamRoles.length > 1}
									<button
										onclick={() => removeRole(i)}
										class="text-[10px] text-red-400/60 hover:text-red-400 transition-colors"
									>remove</button>
								{/if}
							</div>
							<div class="grid grid-cols-3 gap-2">
								<input
									bind:value={role.name}
									placeholder="developer"
									class="bg-zinc-800 text-zinc-100 px-2 py-1.5 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-xs"
								/>
								<input
									bind:value={role.description}
									placeholder="What this role does"
									class="col-span-2 bg-zinc-800 text-zinc-100 px-2 py-1.5 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-xs"
								/>
							</div>
							<input
								bind:value={role.prompt_hint}
								placeholder="Prompt hint (instructions for agents with this role)"
								class="w-full bg-zinc-800 text-zinc-100 px-2 py-1.5 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-xs"
							/>
							<div class="flex items-center gap-4">
								<label class="flex items-center gap-1 text-xs text-zinc-400">
									Max instances:
									<input
										type="number"
										min="1"
										max="5"
										bind:value={role.max_instances}
										class="w-14 bg-zinc-800 text-zinc-100 px-2 py-1 rounded border border-zinc-700 focus:border-zinc-500 focus:outline-none text-xs"
									/>
								</label>
								<label class="flex items-center gap-1 text-xs text-zinc-400 cursor-pointer">
									<input type="checkbox" bind:checked={role.run_last} class="rounded bg-zinc-800 border-zinc-600" />
									Run last
								</label>
							</div>
						</div>
					{/each}
				</div>
			</div>

			<button
				onclick={addTeamTemplate}
				disabled={savingTeam}
				class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded transition-colors"
			>
				{savingTeam ? 'Creating...' : 'Add Team Template'}
			</button>
		</div>
	</section>
</div>
