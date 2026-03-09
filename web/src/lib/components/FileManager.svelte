<script lang="ts">
	import { onMount } from 'svelte';
	import {
		uploadFiles,
		getFiles,
		downloadFile,
		getFileContent,
		updateFileContent,
		deleteFile,
		type FileInfo
	} from '$lib/api';

	const { taskId }: { taskId: string } = $props();

	let inputFiles = $state<FileInfo[]>([]);
	let outputFiles = $state<FileInfo[]>([]);
	let workspaceFiles = $state<FileInfo[]>([]);
	let uploading = $state(false);
	let dragOver = $state(false);
	let error = $state('');
	let uploadProgress = $state('');

	// Collapsible .klaudio section
	let klaudioExpanded = $state(false);

	// File viewer modal
	let viewerOpen = $state(false);
	let viewerFile = $state<{ name: string; path: string; type: 'input' | 'output' | 'workspace' } | null>(null);
	let viewerContent = $state('');
	let viewerLoading = $state(false);
	let viewerEditing = $state(false);
	let viewerEditContent = $state('');
	let viewerSaving = $state(false);
	let viewerError = $state('');
	let copySuccess = $state(false);

	// Split workspace files into .klaudio and regular
	let klaudioFiles = $derived(workspaceFiles.filter((f) => f.name.startsWith('.klaudio/')));
	let regularWorkspaceFiles = $derived(workspaceFiles.filter((f) => !f.name.startsWith('.klaudio/')));

	async function loadFiles() {
		try {
			const result = await getFiles(taskId);
			inputFiles = result.input ?? [];
			outputFiles = result.output ?? [];
			workspaceFiles = result.workspace ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load files';
		}
	}

	async function handleUpload(files: FileList | File[]) {
		if (!files.length) return;
		uploading = true;
		error = '';
		uploadProgress = `Uploading ${files.length} file(s)...`;
		try {
			const fileArray = Array.from(files);
			await uploadFiles(taskId, fileArray);
			uploadProgress = '';
			await loadFiles();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Upload failed';
			uploadProgress = '';
		} finally {
			uploading = false;
		}
	}

	function handleDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;
		if (e.dataTransfer?.files) {
			handleUpload(e.dataTransfer.files);
		}
	}

	function handleDragOver(e: DragEvent) {
		e.preventDefault();
		dragOver = true;
	}

	function handleDragLeave() {
		dragOver = false;
	}

	function handleFileInput(e: Event) {
		const input = e.target as HTMLInputElement;
		if (input.files) {
			handleUpload(input.files);
		}
	}

	function formatSize(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}

	function fileIcon(name: string): string {
		const ext = name.split('.').pop()?.toLowerCase() ?? '';
		if (['go', 'ts', 'js', 'py', 'rs', 'java', 'c', 'cpp', 'h'].includes(ext)) return '{}';
		if (['md', 'txt', 'log', 'csv'].includes(ext)) return '\u2261';
		if (['json', 'yaml', 'yml', 'toml'].includes(ext)) return '\u2699';
		if (['mod', 'sum'].includes(ext)) return '\u25ce';
		return '\u25a1';
	}

	function langFromExt(name: string): string {
		const ext = name.split('.').pop()?.toLowerCase() ?? '';
		const map: Record<string, string> = {
			go: 'go',
			ts: 'typescript',
			js: 'javascript',
			py: 'python',
			rs: 'rust',
			json: 'json',
			yaml: 'yaml',
			yml: 'yaml',
			md: 'markdown',
			sql: 'sql',
			sh: 'bash',
			bash: 'bash',
			toml: 'toml'
		};
		return map[ext] ?? '';
	}

	async function openFile(name: string, type: 'input' | 'output' | 'workspace') {
		viewerFile = { name, path: name, type };
		viewerContent = '';
		viewerEditing = false;
		viewerError = '';
		viewerOpen = true;
		viewerLoading = true;
		copySuccess = false;
		try {
			const result = await getFileContent(taskId, name, type);
			viewerContent = result.content;
		} catch (e) {
			viewerError = e instanceof Error ? e.message : 'Failed to load file content';
		} finally {
			viewerLoading = false;
		}
	}

	function closeViewer() {
		viewerOpen = false;
		viewerFile = null;
		viewerEditing = false;
	}

	async function handleCopy() {
		try {
			await navigator.clipboard.writeText(viewerContent);
			copySuccess = true;
			setTimeout(() => (copySuccess = false), 2000);
		} catch {
			viewerError = 'Failed to copy to clipboard';
		}
	}

	function startEdit() {
		viewerEditContent = viewerContent;
		viewerEditing = true;
	}

	async function saveEdit() {
		if (!viewerFile) return;
		viewerSaving = true;
		viewerError = '';
		try {
			await updateFileContent(taskId, viewerFile.name, viewerEditContent, viewerFile.type);
			viewerContent = viewerEditContent;
			viewerEditing = false;
			await loadFiles();
		} catch (e) {
			viewerError = e instanceof Error ? e.message : 'Failed to save file';
		} finally {
			viewerSaving = false;
		}
	}

	async function handleDelete() {
		if (!viewerFile) return;
		if (!confirm(`Delete ${viewerFile.name}?`)) return;
		viewerError = '';
		try {
			await deleteFile(taskId, viewerFile.name, viewerFile.type);
			closeViewer();
			await loadFiles();
		} catch (e) {
			viewerError = e instanceof Error ? e.message : 'Failed to delete file';
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && viewerOpen) {
			closeViewer();
		}
	}

	onMount(() => {
		loadFiles();
	});
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="space-y-6">
	{#if error}
		<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm">{error}</div>
	{/if}

	<!-- Upload area -->
	<div>
		<h3 class="text-sm font-medium text-zinc-300 mb-3">Upload Input Files</h3>
		<div
			role="button"
			tabindex="0"
			class="border-2 border-dashed rounded-lg p-6 text-center transition-colors
				{dragOver ? 'border-blue-500 bg-blue-500/10' : 'border-zinc-700 hover:border-zinc-500'}"
			ondrop={handleDrop}
			ondragover={handleDragOver}
			ondragleave={handleDragLeave}
		>
			{#if uploading}
				<p class="text-sm text-zinc-400">{uploadProgress}</p>
			{:else}
				<p class="text-sm text-zinc-400 mb-2">Drag and drop files here</p>
				<label
					class="inline-block px-4 py-2 bg-zinc-700 hover:bg-zinc-600 text-zinc-200 text-sm rounded cursor-pointer transition-colors"
				>
					Browse files
					<input type="file" multiple class="hidden" onchange={handleFileInput} />
				</label>
			{/if}
		</div>
	</div>

	<!-- Input files -->
	{#if inputFiles.length > 0}
		<div>
			<h3 class="text-sm font-medium text-zinc-300 mb-2">
				Input Files
				<span class="text-xs text-zinc-500 font-normal">({inputFiles.length})</span>
			</h3>
			<div class="space-y-0.5">
				{#each inputFiles as file}
					<button
						class="w-full flex items-center justify-between p-2 bg-zinc-800/50 hover:bg-zinc-700/50 rounded cursor-pointer transition-colors text-left"
						onclick={() => openFile(file.name, 'input')}
					>
						<div class="flex items-center gap-2 min-w-0">
							<span class="text-xs text-zinc-500 font-mono w-4 text-center flex-shrink-0"
								>{fileIcon(file.name)}</span
							>
							<span class="text-sm text-zinc-300 font-mono truncate">{file.name}</span>
						</div>
						<span class="text-xs text-zinc-500 flex-shrink-0 ml-2">{formatSize(file.size)}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Output files -->
	{#if outputFiles.length > 0}
		<div>
			<h3 class="text-sm font-medium text-zinc-300 mb-2">
				Output Files
				<span class="text-xs text-zinc-500 font-normal">({outputFiles.length})</span>
			</h3>
			<div class="space-y-0.5">
				{#each outputFiles as file}
					<button
						class="w-full flex items-center justify-between p-2 bg-zinc-800/50 hover:bg-zinc-700/50 rounded cursor-pointer transition-colors text-left"
						onclick={() => openFile(file.name, 'output')}
					>
						<div class="flex items-center gap-2 min-w-0">
							<span class="text-xs text-zinc-500 font-mono w-4 text-center flex-shrink-0"
								>{fileIcon(file.name)}</span
							>
							<span class="text-sm text-zinc-300 font-mono truncate">{file.name}</span>
						</div>
						<span class="text-xs text-zinc-500 flex-shrink-0 ml-2">{formatSize(file.size)}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Workspace files (regular) -->
	{#if regularWorkspaceFiles.length > 0}
		<div>
			<h3 class="text-sm font-medium text-zinc-300 mb-2">
				Workspace Files
				<span class="text-xs text-zinc-500 font-normal">({regularWorkspaceFiles.length})</span>
			</h3>
			<div class="space-y-0.5">
				{#each regularWorkspaceFiles as file}
					<button
						class="w-full flex items-center justify-between p-2 bg-zinc-800/50 hover:bg-zinc-700/50 rounded cursor-pointer transition-colors text-left"
						onclick={() => openFile(file.name, 'workspace')}
					>
						<div class="flex items-center gap-2 min-w-0">
							<span class="text-xs text-zinc-500 font-mono w-4 text-center flex-shrink-0"
								>{fileIcon(file.name)}</span
							>
							<span class="text-sm text-zinc-300 font-mono truncate">{file.name}</span>
						</div>
						<span class="text-xs text-zinc-500 flex-shrink-0 ml-2">{formatSize(file.size)}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<!-- .klaudio internal files (collapsible) -->
	{#if klaudioFiles.length > 0}
		<div>
			<button
				class="flex items-center gap-2 text-sm font-medium text-zinc-400 hover:text-zinc-300 transition-colors mb-2"
				onclick={() => (klaudioExpanded = !klaudioExpanded)}
			>
				<span
					class="text-xs transition-transform duration-150"
					class:rotate-90={klaudioExpanded}>{'\u25b6'}</span
				>
				.klaudio
				<span class="text-xs text-zinc-500 font-normal">({klaudioFiles.length} files)</span>
			</button>
			{#if klaudioExpanded}
				<div class="space-y-0.5 ml-4 border-l border-zinc-700/50 pl-3">
					{#each klaudioFiles as file}
						{@const shortName = file.name.replace('.klaudio/', '')}
						<button
							class="w-full flex items-center justify-between p-1.5 bg-zinc-800/30 hover:bg-zinc-700/40 rounded cursor-pointer transition-colors text-left"
							onclick={() => openFile(file.name, 'workspace')}
						>
							<div class="flex items-center gap-2 min-w-0">
								<span class="text-xs text-zinc-500 font-mono w-4 text-center flex-shrink-0"
									>{fileIcon(file.name)}</span
								>
								<span class="text-xs text-zinc-400 font-mono truncate">{shortName}</span>
							</div>
							<span class="text-xs text-zinc-600 flex-shrink-0 ml-2">{formatSize(file.size)}</span>
						</button>
					{/each}
				</div>
			{/if}
		</div>
	{/if}

	{#if !inputFiles.length && !outputFiles.length && !workspaceFiles.length}
		<div class="text-sm text-zinc-500">No files yet.</div>
	{/if}
</div>

<!-- File Viewer Modal -->
{#if viewerOpen && viewerFile}
	<!-- Backdrop -->
	<!-- svelte-ignore a11y_interactive_supports_focus -->
	<div
		class="fixed inset-0 bg-black/70 z-50 flex items-center justify-center p-4"
		role="dialog"
		aria-modal="true"
		onclick={(e) => {
			if (e.target === e.currentTarget) closeViewer();
		}}
		onkeydown={(e) => {
			if (e.key === 'Escape') closeViewer();
		}}
	>
		<!-- Modal -->
		<div
			class="bg-zinc-900 border border-zinc-700 rounded-lg shadow-2xl w-full max-w-4xl max-h-[85vh] flex flex-col"
		>
			<!-- Header -->
			<div class="flex items-center justify-between px-4 py-3 border-b border-zinc-700 flex-shrink-0">
				<div class="flex items-center gap-2 min-w-0">
					<span class="text-xs text-zinc-500 font-mono">{fileIcon(viewerFile.name)}</span>
					<span class="text-sm font-mono text-zinc-200 truncate">{viewerFile.name}</span>
					{#if langFromExt(viewerFile.name)}
						<span
							class="text-[10px] px-1.5 py-0.5 bg-zinc-700 text-zinc-400 rounded font-mono flex-shrink-0"
							>{langFromExt(viewerFile.name)}</span
						>
					{/if}
				</div>
				<button
					class="text-zinc-400 hover:text-zinc-200 text-lg leading-none px-1"
					onclick={closeViewer}>&times;</button
				>
			</div>

			<!-- Toolbar -->
			<div class="flex items-center gap-1 px-4 py-2 border-b border-zinc-800 flex-shrink-0">
				<button
					class="px-3 py-1.5 text-xs rounded transition-colors
						{copySuccess
						? 'bg-green-700/30 text-green-400'
						: 'bg-zinc-700 hover:bg-zinc-600 text-zinc-300'}"
					onclick={handleCopy}
					disabled={viewerLoading}
				>
					{copySuccess ? 'Copied!' : 'Copy'}
				</button>
				{#if !viewerEditing}
					<button
						class="px-3 py-1.5 text-xs bg-zinc-700 hover:bg-zinc-600 text-zinc-300 rounded transition-colors"
						onclick={startEdit}
						disabled={viewerLoading}
					>
						Edit
					</button>
				{:else}
					<button
						class="px-3 py-1.5 text-xs bg-blue-700 hover:bg-blue-600 text-white rounded transition-colors"
						onclick={saveEdit}
						disabled={viewerSaving}
					>
						{viewerSaving ? 'Saving...' : 'Save'}
					</button>
					<button
						class="px-3 py-1.5 text-xs bg-zinc-700 hover:bg-zinc-600 text-zinc-300 rounded transition-colors"
						onclick={() => (viewerEditing = false)}
					>
						Cancel
					</button>
				{/if}
				<a
					href={downloadFile(taskId, viewerFile.name, viewerFile.type)}
					download
					class="px-3 py-1.5 text-xs bg-zinc-700 hover:bg-zinc-600 text-zinc-300 rounded transition-colors inline-block"
				>
					Download
				</a>
				<button
					class="px-3 py-1.5 text-xs bg-red-900/40 hover:bg-red-800/50 text-red-400 rounded transition-colors ml-auto"
					onclick={handleDelete}
				>
					Delete
				</button>
			</div>

			<!-- Content -->
			<div class="flex-1 overflow-auto min-h-0">
				{#if viewerLoading}
					<div class="flex items-center justify-center h-32">
						<span class="text-sm text-zinc-500">Loading...</span>
					</div>
				{:else if viewerError}
					<div class="p-4">
						<div class="p-3 bg-red-900/20 border border-red-700 rounded text-red-400 text-sm">
							{viewerError}
						</div>
					</div>
				{:else if viewerEditing}
					<textarea
						class="w-full h-full min-h-[400px] bg-zinc-950 text-zinc-200 font-mono text-sm p-4 resize-none focus:outline-none"
						bind:value={viewerEditContent}
						spellcheck="false"
					></textarea>
				{:else}
					<pre
						class="text-sm font-mono text-zinc-300 p-4 whitespace-pre-wrap break-words">{viewerContent}</pre>
				{/if}
			</div>
		</div>
	</div>
{/if}
