<script lang="ts">
	import { onMount } from 'svelte';
	import {
		getConfig,
		updateConfig,
		getRepoTemplates,
		createRepoTemplate,
		updateRepoTemplate,
		deleteRepoTemplate,
		getTeamTemplates,
		createTeamTemplate,
		updateTeamTemplate,
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
	let activeTab = $state<'general' | 'repos' | 'teams'>('general');

	// --- General config form state ---
	let serverPort = $state(8080);
	let serverHost = $state('0.0.0.0');
	let dockerHost = $state('');
	let dockerImageName = $state('klaudio-agent');
	let dockerNetwork = $state('klaudio-net');
	let dockerMaxAgents = $state(5);
	let dockerMaxAgentsPerTask = $state(3);
	let claudeAuthMode = $state('host');
	let claudeHostDir = $state('');
	let claudeSessionKey = $state('');
	let storageDataDir = $state('data');
	let storageStatesDir = $state('data/states');
	let storageFilesDir = $state('data/files');
	let stateAutoSaveEnabled = $state(true);
	let stateAutoSaveInterval = $state('5m');
	let stateMaxCheckpoints = $state(3);
	let stateRetentionDays = $state(7);
	let savingConfig = $state(false);
	let hasSessionKey = $state(false);

	// --- Repo template form ---
	let editingRepoId = $state<string | null>(null);
	let repoForm = $state({
		name: '', url: '', default_branch: '', access_token: '',
		auto_branch: false, auto_commit: false, auto_push: false, auto_pr: false,
		pr_target: '', enable_memory: false
	});
	let savingRepo = $state(false);

	// --- Team template form ---
	let editingTeamId = $state<string | null>(null);
	let teamForm = $state({
		name: '', description: '', max_agents: 3, review: false,
		mode: 'sequential' as 'sequential' | 'collaborative',
		roles: [{ name: 'developer', description: 'Writes production code', prompt_hint: '', max_instances: 2, run_last: false }] as TeamRole[]
	});
	let savingTeam = $state(false);

	function clearMessages() { error = ''; success = ''; }

	function showSuccess(msg: string) {
		success = msg;
		setTimeout(() => { if (success === msg) success = ''; }, 3000);
	}

	async function loadConfig() {
		try {
			config = await getConfig();
			serverPort = config.server.port;
			serverHost = config.server.host;
			dockerHost = config.docker.host;
			dockerImageName = config.docker.image_name;
			dockerNetwork = config.docker.network;
			dockerMaxAgents = config.docker.max_agents;
			dockerMaxAgentsPerTask = config.docker.max_agents_per_task;
			claudeAuthMode = config.claude.auth_mode;
			claudeHostDir = config.claude.host_dir;
			hasSessionKey = config.claude.has_session_key;
			storageDataDir = config.storage.data_dir;
			storageStatesDir = config.storage.states_dir;
			storageFilesDir = config.storage.files_dir;
			stateAutoSaveEnabled = config.state.auto_save_enabled;
			stateAutoSaveInterval = config.state.auto_save_interval;
			stateMaxCheckpoints = config.state.max_checkpoints;
			stateRetentionDays = config.state.retention_days;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load config';
		}
	}

	async function saveConfig() {
		clearMessages();
		savingConfig = true;
		try {
			const payload: Record<string, unknown> = {
				server: { port: serverPort, host: serverHost },
				docker: {
					host: dockerHost, image_name: dockerImageName, network: dockerNetwork,
					max_agents: dockerMaxAgents, max_agents_per_task: dockerMaxAgentsPerTask
				},
				claude: {
					auth_mode: claudeAuthMode, host_dir: claudeHostDir,
					...(claudeSessionKey ? { session_key: claudeSessionKey } : {})
				},
				storage: { data_dir: storageDataDir, states_dir: storageStatesDir, files_dir: storageFilesDir },
				state: {
					auto_save_enabled: stateAutoSaveEnabled, auto_save_interval: stateAutoSaveInterval,
					max_checkpoints: stateMaxCheckpoints, retention_days: stateRetentionDays
				}
			};
			await updateConfig(payload);
			showSuccess('Configuration saved. Some changes (port, host) require a restart.');
			claudeSessionKey = '';
			await loadConfig();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save config';
		} finally {
			savingConfig = false;
		}
	}

	// --- Repo Templates ---
	async function loadRepoTemplates() {
		try {
			const result = await getRepoTemplates();
			templates = result.templates ?? [];
		} catch { /* ignore */ }
	}

	function resetRepoForm() {
		editingRepoId = null;
		repoForm = {
			name: '', url: '', default_branch: '', access_token: '',
			auto_branch: false, auto_commit: false, auto_push: false, auto_pr: false,
			pr_target: '', enable_memory: false
		};
	}

	function editRepo(tpl: RepoTemplate) {
		editingRepoId = tpl.id;
		repoForm = {
			name: tpl.name, url: tpl.url, default_branch: tpl.default_branch,
			access_token: '', auto_branch: tpl.auto_branch, auto_commit: tpl.auto_commit,
			auto_push: tpl.auto_push, auto_pr: tpl.auto_pr, pr_target: tpl.pr_target,
			enable_memory: tpl.enable_memory
		};
	}

	async function saveRepo() {
		if (!repoForm.name.trim() || !repoForm.url.trim()) {
			error = 'Template name and URL are required.'; return;
		}
		clearMessages();
		savingRepo = true;
		try {
			const data = {
				name: repoForm.name.trim(), url: repoForm.url.trim(),
				default_branch: repoForm.default_branch.trim() || undefined,
				access_token: repoForm.access_token.trim() || undefined,
				auto_branch: repoForm.auto_branch, auto_commit: repoForm.auto_commit,
				auto_push: repoForm.auto_push, auto_pr: repoForm.auto_pr,
				pr_target: repoForm.pr_target.trim() || undefined,
				enable_memory: repoForm.enable_memory
			};
			if (editingRepoId) {
				await updateRepoTemplate(editingRepoId, data);
				showSuccess('Template updated.');
			} else {
				await createRepoTemplate(data);
				showSuccess('Template created.');
			}
			resetRepoForm();
			await loadRepoTemplates();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save template';
		} finally {
			savingRepo = false;
		}
	}

	async function handleDeleteRepo(id: string) {
		if (!confirm('Delete this repository template?')) return;
		clearMessages();
		try {
			await deleteRepoTemplate(id);
			if (editingRepoId === id) resetRepoForm();
			await loadRepoTemplates();
			showSuccess('Template deleted.');
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete';
		}
	}

	// --- Team Templates ---
	async function loadTeamTemplates() {
		try {
			const result = await getTeamTemplates();
			teamTemplates = result.templates ?? [];
		} catch { /* ignore */ }
	}

	function resetTeamForm() {
		editingTeamId = null;
		teamForm = {
			name: '', description: '', max_agents: 3, review: false,
			mode: 'sequential',
			roles: [{ name: 'developer', description: 'Writes production code', prompt_hint: '', max_instances: 2, run_last: false }]
		};
	}

	function editTeam(tt: TeamTemplate) {
		editingTeamId = tt.id;
		teamForm = {
			name: tt.name, description: tt.description, max_agents: tt.max_agents,
			review: tt.review, mode: tt.mode,
			roles: tt.roles.map(r => ({ ...r }))
		};
	}

	function addRole() {
		teamForm.roles = [...teamForm.roles, { name: '', description: '', prompt_hint: '', max_instances: 1, run_last: false }];
	}

	function removeRole(index: number) {
		teamForm.roles = teamForm.roles.filter((_, i) => i !== index);
	}

	async function saveTeam() {
		if (!teamForm.name.trim()) { error = 'Team template name is required.'; return; }
		if (teamForm.roles.length === 0 || !teamForm.roles[0].name.trim()) {
			error = 'At least one role with a name is required.'; return;
		}
		clearMessages();
		savingTeam = true;
		try {
			const data = {
				name: teamForm.name.trim(), description: teamForm.description.trim(),
				max_agents: teamForm.max_agents, review: teamForm.review, mode: teamForm.mode,
				roles: teamForm.roles.filter(r => r.name.trim())
			};
			if (editingTeamId) {
				await updateTeamTemplate(editingTeamId, data);
				showSuccess('Team template updated.');
			} else {
				await createTeamTemplate(data);
				showSuccess('Team template created.');
			}
			resetTeamForm();
			await loadTeamTemplates();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save team template';
		} finally {
			savingTeam = false;
		}
	}

	async function handleDeleteTeam(id: string) {
		if (!confirm('Delete this team template?')) return;
		clearMessages();
		try {
			await deleteTeamTemplate(id);
			if (editingTeamId === id) resetTeamForm();
			await loadTeamTemplates();
			showSuccess('Team template deleted.');
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete';
		}
	}

	onMount(() => {
		loadConfig();
		loadRepoTemplates();
		loadTeamTemplates();
	});
</script>

<div class="p-6 max-w-3xl mx-auto">
	<h1 class="text-2xl font-semibold text-zinc-100 mb-6">Settings</h1>

	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm mb-4">{error}</div>
	{/if}
	{#if success}
		<div class="p-3 bg-green-900/20 border border-green-700 rounded text-green-400 text-sm mb-4">{success}</div>
	{/if}

	<!-- Tab bar -->
	<div class="flex border-b border-zinc-700 mb-6">
		{#each [
			{ key: 'general', label: 'General' },
			{ key: 'repos', label: 'Repository Templates' },
			{ key: 'teams', label: 'Team Templates' }
		] as tab}
			<button
				onclick={() => { activeTab = tab.key as typeof activeTab; clearMessages(); }}
				class="px-4 py-2.5 text-sm font-medium border-b-2 transition-colors -mb-px
					{activeTab === tab.key
						? 'text-blue-400 border-blue-400'
						: 'text-zinc-400 border-transparent hover:text-zinc-200 hover:border-zinc-500'}"
			>{tab.label}</button>
		{/each}
	</div>

	<!-- ==================== GENERAL TAB ==================== -->
	{#if activeTab === 'general'}
		<div class="space-y-6">

			<!-- Server -->
			<section class="p-4 bg-zinc-800/40 rounded-lg border border-zinc-700">
				<h2 class="text-sm font-semibold text-zinc-200 mb-3 flex items-center gap-2">
					<span class="w-2 h-2 rounded-full bg-blue-400"></span> Server
				</h2>
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label for="srv-host" class="block text-xs text-zinc-500 mb-1">Bind Address</label>
						<input id="srv-host" bind:value={serverHost} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
					</div>
					<div>
						<label for="srv-port" class="block text-xs text-zinc-500 mb-1">Port</label>
						<input id="srv-port" type="number" min="1" max="65535" bind:value={serverPort} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
					</div>
				</div>
				<p class="text-[11px] text-zinc-500 mt-2">Changes to server address/port require a restart to take effect.</p>
			</section>

			<!-- Docker -->
			<section class="p-4 bg-zinc-800/40 rounded-lg border border-zinc-700">
				<h2 class="text-sm font-semibold text-zinc-200 mb-3 flex items-center gap-2">
					<span class="w-2 h-2 rounded-full bg-cyan-400"></span> Docker
				</h2>
				<div class="space-y-3">
					<div>
						<label for="dk-host" class="block text-xs text-zinc-500 mb-1">Docker Host</label>
						<input id="dk-host" bind:value={dockerHost} placeholder="unix:///var/run/docker.sock" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
					</div>
					<div class="grid grid-cols-3 gap-3">
						<div>
							<label for="dk-image" class="block text-xs text-zinc-500 mb-1">Image Name</label>
							<input id="dk-image" bind:value={dockerImageName} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
						</div>
						<div>
							<label for="dk-network" class="block text-xs text-zinc-500 mb-1">Network</label>
							<input id="dk-network" bind:value={dockerNetwork} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
						</div>
						<div class="grid grid-cols-2 gap-2">
							<div>
								<label for="dk-max" class="block text-xs text-zinc-500 mb-1">Max Agents</label>
								<input id="dk-max" type="number" min="1" max="20" bind:value={dockerMaxAgents} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
							</div>
							<div>
								<label for="dk-max-task" class="block text-xs text-zinc-500 mb-1">Per Task</label>
								<input id="dk-max-task" type="number" min="1" max="10" bind:value={dockerMaxAgentsPerTask} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
							</div>
						</div>
					</div>
				</div>
			</section>

			<!-- Claude Auth -->
			<section class="p-4 bg-zinc-800/40 rounded-lg border border-zinc-700">
				<h2 class="text-sm font-semibold text-zinc-200 mb-3 flex items-center gap-2">
					<span class="w-2 h-2 rounded-full bg-violet-400"></span> Claude Authentication
				</h2>
				<div class="space-y-3">
					<div>
						<label for="claude-mode" class="block text-xs text-zinc-500 mb-1">Auth Mode</label>
						<select id="claude-mode" bind:value={claudeAuthMode} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm">
							<option value="host">Host (mount ~/.claude/ from host)</option>
							<option value="env">Environment (session key)</option>
						</select>
					</div>
					{#if claudeAuthMode === 'host'}
						<div>
							<label for="claude-dir" class="block text-xs text-zinc-500 mb-1">Host Directory (~/.claude)</label>
							<input id="claude-dir" bind:value={claudeHostDir} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
						</div>
					{:else}
						<div>
							<label for="claude-key" class="block text-xs text-zinc-500 mb-1">
								Session Key
								{#if hasSessionKey}
									<span class="text-green-400 ml-1">(set)</span>
								{/if}
							</label>
							<input id="claude-key" type="password" bind:value={claudeSessionKey} placeholder={hasSessionKey ? '(unchanged — enter new value to update)' : 'sk-...'} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
						</div>
					{/if}
				</div>
			</section>

			<!-- Storage -->
			<section class="p-4 bg-zinc-800/40 rounded-lg border border-zinc-700">
				<h2 class="text-sm font-semibold text-zinc-200 mb-3 flex items-center gap-2">
					<span class="w-2 h-2 rounded-full bg-amber-400"></span> Storage Paths
				</h2>
				<div class="grid grid-cols-3 gap-3">
					<div>
						<label for="st-data" class="block text-xs text-zinc-500 mb-1">Data Dir</label>
						<input id="st-data" bind:value={storageDataDir} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
					</div>
					<div>
						<label for="st-states" class="block text-xs text-zinc-500 mb-1">States Dir</label>
						<input id="st-states" bind:value={storageStatesDir} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
					</div>
					<div>
						<label for="st-files" class="block text-xs text-zinc-500 mb-1">Files Dir</label>
						<input id="st-files" bind:value={storageFilesDir} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
					</div>
				</div>
				<p class="text-[11px] text-zinc-500 mt-2">Path changes require a restart. Existing data is not moved automatically.</p>
			</section>

			<!-- State / Checkpoints -->
			<section class="p-4 bg-zinc-800/40 rounded-lg border border-zinc-700">
				<h2 class="text-sm font-semibold text-zinc-200 mb-3 flex items-center gap-2">
					<span class="w-2 h-2 rounded-full bg-green-400"></span> Checkpoints & Auto-Save
				</h2>
				<div class="space-y-3">
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={stateAutoSaveEnabled} class="rounded bg-zinc-800 border-zinc-600 text-blue-500" />
						Enable auto-save
					</label>
					<div class="grid grid-cols-3 gap-3">
						<div>
							<label for="cp-interval" class="block text-xs text-zinc-500 mb-1">Save Interval</label>
							<input id="cp-interval" bind:value={stateAutoSaveInterval} placeholder="5m" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
						</div>
						<div>
							<label for="cp-max" class="block text-xs text-zinc-500 mb-1">Max Checkpoints</label>
							<input id="cp-max" type="number" min="1" max="50" bind:value={stateMaxCheckpoints} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
						</div>
						<div>
							<label for="cp-retention" class="block text-xs text-zinc-500 mb-1">Retention (days)</label>
							<input id="cp-retention" type="number" min="1" max="365" bind:value={stateRetentionDays} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
						</div>
					</div>
				</div>
			</section>

			<div class="flex justify-end">
				<button
					onclick={saveConfig}
					disabled={savingConfig}
					class="px-6 py-2.5 text-sm font-medium bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded-lg transition-colors"
				>
					{savingConfig ? 'Saving...' : 'Save Configuration'}
				</button>
			</div>
		</div>
	{/if}

	<!-- ==================== REPOS TAB ==================== -->
	{#if activeTab === 'repos'}
		<!-- Existing templates list -->
		{#if templates.length > 0}
			<div class="space-y-2 mb-6">
				{#each templates as tpl}
					<div class="p-3 bg-zinc-800/50 rounded-lg border border-zinc-700 {editingRepoId === tpl.id ? 'ring-1 ring-blue-500' : ''}">
						<div class="flex items-center justify-between">
							<div>
								<span class="text-sm font-medium text-zinc-200">{tpl.name}</span>
								<span class="text-xs text-zinc-500 font-mono ml-2">{tpl.url}</span>
							</div>
							<div class="flex items-center gap-2">
								<button onclick={() => editRepo(tpl)} class="text-xs text-blue-400/70 hover:text-blue-400 transition-colors">edit</button>
								<button onclick={() => handleDeleteRepo(tpl.id)} class="text-xs text-red-400/60 hover:text-red-400 transition-colors">delete</button>
							</div>
						</div>
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
							{#if tpl.enable_memory}
								<span class="px-1.5 py-0.5 text-[10px] rounded bg-cyan-900/40 text-cyan-400 border border-cyan-800">memory</span>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{:else}
			<p class="text-sm text-zinc-500 mb-6">No repository templates yet.</p>
		{/if}

		<!-- Add/Edit form -->
		<div class="p-4 bg-zinc-800/30 rounded-lg border border-zinc-700 space-y-3">
			<div class="flex items-center justify-between">
				<h3 class="text-sm font-medium text-zinc-300">{editingRepoId ? 'Edit Template' : 'Add Template'}</h3>
				{#if editingRepoId}
					<button onclick={resetRepoForm} class="text-xs text-zinc-400 hover:text-zinc-200 transition-colors">Cancel</button>
				{/if}
			</div>
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label for="rp-name" class="block text-xs text-zinc-500 mb-1">Name</label>
					<input id="rp-name" bind:value={repoForm.name} placeholder="My Project" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
				</div>
				<div>
					<label for="rp-branch" class="block text-xs text-zinc-500 mb-1">Default Branch</label>
					<input id="rp-branch" bind:value={repoForm.default_branch} placeholder="main" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
				</div>
			</div>
			<div>
				<label for="rp-url" class="block text-xs text-zinc-500 mb-1">URL</label>
				<input id="rp-url" bind:value={repoForm.url} placeholder="https://github.com/org/repo.git" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
			</div>
			<div>
				<label for="rp-token" class="block text-xs text-zinc-500 mb-1">Access Token {editingRepoId ? '(leave empty to keep current)' : '(optional)'}</label>
				<input id="rp-token" type="password" bind:value={repoForm.access_token} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm font-mono" />
			</div>
			<div class="space-y-2">
				<h4 class="text-xs text-zinc-500">Git Automation</h4>
				<div class="flex flex-wrap gap-x-6 gap-y-2">
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={repoForm.auto_branch} class="rounded bg-zinc-800 border-zinc-600" />
						Create branch
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={repoForm.auto_commit} class="rounded bg-zinc-800 border-zinc-600" />
						Commit
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={repoForm.auto_push} class="rounded bg-zinc-800 border-zinc-600" />
						Push
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
						<input type="checkbox" bind:checked={repoForm.auto_pr} class="rounded bg-zinc-800 border-zinc-600" />
						Create PR
					</label>
				</div>
				{#if repoForm.auto_pr}
					<input bind:value={repoForm.pr_target} placeholder="PR target branch" class="bg-zinc-900 text-zinc-100 px-3 py-1.5 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
				{/if}
				<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer mt-2">
					<input type="checkbox" bind:checked={repoForm.enable_memory} class="rounded bg-zinc-800 border-zinc-600" />
					Enable repo memory (cache codebase analysis per commit)
				</label>
			</div>
			<button
				onclick={saveRepo}
				disabled={savingRepo}
				class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded-lg transition-colors"
			>
				{savingRepo ? 'Saving...' : editingRepoId ? 'Update Template' : 'Add Template'}
			</button>
		</div>
	{/if}

	<!-- ==================== TEAMS TAB ==================== -->
	{#if activeTab === 'teams'}
		<!-- Existing team templates -->
		{#if teamTemplates.length > 0}
			<div class="space-y-2 mb-6">
				{#each teamTemplates as tt}
					<div class="p-3 bg-zinc-800/50 rounded-lg border border-zinc-700 {editingTeamId === tt.id ? 'ring-1 ring-blue-500' : ''}">
						<div class="flex items-center justify-between">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-zinc-200">{tt.name}</span>
								{#if tt.is_default}
									<span class="px-1.5 py-0.5 text-[10px] rounded bg-blue-900/40 text-blue-400 border border-blue-800">default</span>
								{/if}
							</div>
							<div class="flex items-center gap-2">
								<button onclick={() => editTeam(tt)} class="text-xs text-blue-400/70 hover:text-blue-400 transition-colors">edit</button>
								<button onclick={() => handleDeleteTeam(tt.id)} class="text-xs text-red-400/60 hover:text-red-400 transition-colors">delete</button>
							</div>
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
		{:else}
			<p class="text-sm text-zinc-500 mb-6">No team templates yet.</p>
		{/if}

		<!-- Add/Edit form -->
		<div class="p-4 bg-zinc-800/30 rounded-lg border border-zinc-700 space-y-3">
			<div class="flex items-center justify-between">
				<h3 class="text-sm font-medium text-zinc-300">{editingTeamId ? 'Edit Team Template' : 'Add Team Template'}</h3>
				{#if editingTeamId}
					<button onclick={resetTeamForm} class="text-xs text-zinc-400 hover:text-zinc-200 transition-colors">Cancel</button>
				{/if}
			</div>
			<div class="grid grid-cols-2 gap-3">
				<div>
					<label class="block text-xs text-zinc-500 mb-1">Name</label>
					<input bind:value={teamForm.name} placeholder="e.g. Full Team" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
				</div>
				<div>
					<label class="block text-xs text-zinc-500 mb-1">Max Agents</label>
					<input type="number" min="1" max="10" bind:value={teamForm.max_agents} class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
				</div>
			</div>
			<div>
				<label class="block text-xs text-zinc-500 mb-1">Description</label>
				<input bind:value={teamForm.description} placeholder="Team with developers and reviewer" class="w-full bg-zinc-900 text-zinc-100 px-3 py-2 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-sm" />
			</div>

			<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
				<input type="checkbox" bind:checked={teamForm.review} class="rounded bg-zinc-800 border-zinc-600" />
				Auto-review (run reviewer agent after all subtasks complete)
			</label>

			<!-- Execution Mode -->
			<div>
				<label class="block text-xs text-zinc-500 mb-1">Execution Mode</label>
				<div class="flex gap-3">
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer px-3 py-2 rounded border transition-colors {teamForm.mode === 'sequential' ? 'bg-zinc-700 border-zinc-500' : 'bg-zinc-800/30 border-zinc-700'}">
						<input type="radio" bind:group={teamForm.mode} value="sequential" class="text-blue-500" />
						<div>
							<div class="font-medium">Sequential (DAG)</div>
							<div class="text-[10px] text-zinc-500">Agents run in dependency order</div>
						</div>
					</label>
					<label class="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer px-3 py-2 rounded border transition-colors {teamForm.mode === 'collaborative' ? 'bg-orange-900/20 border-orange-700' : 'bg-zinc-800/30 border-zinc-700'}">
						<input type="radio" bind:group={teamForm.mode} value="collaborative" class="text-orange-500" />
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
					<button onclick={addRole} class="text-xs text-blue-400 hover:text-blue-300 transition-colors">+ Add role</button>
				</div>
				<div class="space-y-2">
					{#each teamForm.roles as role, i}
						<div class="p-3 bg-zinc-900/50 rounded border border-zinc-700 space-y-2">
							<div class="flex items-center justify-between">
								<span class="text-[10px] text-zinc-500">Role {i + 1}</span>
								{#if teamForm.roles.length > 1}
									<button onclick={() => removeRole(i)} class="text-[10px] text-red-400/60 hover:text-red-400 transition-colors">remove</button>
								{/if}
							</div>
							<div class="grid grid-cols-3 gap-2">
								<input bind:value={role.name} placeholder="developer" class="bg-zinc-800 text-zinc-100 px-2 py-1.5 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-xs" />
								<input bind:value={role.description} placeholder="What this role does" class="col-span-2 bg-zinc-800 text-zinc-100 px-2 py-1.5 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-xs" />
							</div>
							<input bind:value={role.prompt_hint} placeholder="Prompt hint (instructions for agents with this role)" class="w-full bg-zinc-800 text-zinc-100 px-2 py-1.5 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-xs" />
							<div class="flex items-center gap-4">
								<label class="flex items-center gap-1 text-xs text-zinc-400">
									Max instances:
									<input type="number" min="1" max="5" bind:value={role.max_instances} class="w-14 bg-zinc-800 text-zinc-100 px-2 py-1 rounded border border-zinc-700 focus:border-blue-500 focus:outline-none text-xs" />
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
				onclick={saveTeam}
				disabled={savingTeam}
				class="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-zinc-700 text-white rounded-lg transition-colors"
			>
				{savingTeam ? 'Saving...' : editingTeamId ? 'Update Team Template' : 'Add Team Template'}
			</button>
		</div>
	{/if}
</div>
