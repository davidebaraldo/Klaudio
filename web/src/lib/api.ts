const BASE = '/api';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
	const res = await fetch(`${BASE}${path}`, {
		headers: { 'Content-Type': 'application/json', ...options?.headers as Record<string, string> },
		...options
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	if (res.status === 204) return undefined as T;
	return res.json();
}

// ---- Tasks ----

export interface Task {
	id: string;
	name: string;
	prompt?: string;
	status: string;
	repo?: RepoConfig;
	plan?: Plan;
	agents?: Agent[];
	input_files?: string[];
	output_files?: string[];
	has_state?: boolean;
	error?: string;
	created_at: string;
	started_at?: string;
	pr_url?: string | null;
}

export interface RepoConfig {
	url: string;
	branch?: string;
	access_token?: string;
	auto_commit?: boolean;
	auto_push?: boolean;
	auto_pr?: boolean;
	pr_target?: string;
}

export interface Agent {
	id: string;
	task_id: string;
	subtask_id: string;
	role: string;
	status: string;
	container_id?: string;
	started_at?: string;
	output_size_bytes?: number;
}

export interface RepoOverrides {
	auto_branch?: boolean;
	auto_commit?: boolean;
	auto_push?: boolean;
	auto_pr?: boolean;
}

export interface CreateTaskRequest {
	name: string;
	prompt: string;
	auto_start?: boolean;
	repo_template_id?: string;
	repo_overrides?: RepoOverrides;
	output_files?: string[];
	team_template?: string;
}

export function createTask(data: CreateTaskRequest) {
	return request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) });
}

export function getTasks(params?: { status?: string; limit?: number; offset?: number; search?: string }) {
	const qs = new URLSearchParams();
	if (params?.status) qs.set('status', params.status);
	if (params?.limit) qs.set('limit', String(params.limit));
	if (params?.offset) qs.set('offset', String(params.offset));
	if (params?.search) qs.set('search', params.search);
	const q = qs.toString();
	return request<{ tasks: Task[]; total: number }>(`/tasks${q ? '?' + q : ''}`);
}

export function getTask(id: string) {
	return request<Task>(`/tasks/${id}`);
}

export function deleteTask(id: string) {
	return request<void>(`/tasks/${id}`, { method: 'DELETE' });
}

// ---- Task Actions ----

export function startTask(id: string) {
	return request<{ status: string; message: string }>(`/tasks/${id}/start`, { method: 'POST' });
}

export function approveTask(id: string, modifiedPlan?: Plan) {
	return request<{ status: string; agents_spawned: number }>(`/tasks/${id}/approve`, {
		method: 'POST',
		body: JSON.stringify(modifiedPlan ? { modified_plan: modifiedPlan } : {})
	});
}

export function stopTask(id: string) {
	return request<{ status: string; checkpoint_id: string; state_size_mb: number }>(`/tasks/${id}/stop`, { method: 'POST' });
}

export function resumeTask(id: string) {
	return request<{ status: string; resumed_from: string; pending_subtasks: number }>(`/tasks/${id}/resume`, { method: 'POST' });
}

export function relaunchTask(id: string, opts?: { prompt?: string; autoStart?: boolean; keepContext?: boolean }) {
	return request<Task>(`/tasks/${id}/relaunch`, {
		method: 'POST',
		body: JSON.stringify({
			prompt: opts?.prompt || '',
			auto_start: opts?.autoStart ?? false,
			keep_context: opts?.keepContext ?? true
		})
	});
}

// ---- Plans ----

export interface Subtask {
	id: string;
	name: string;
	description?: string;
	prompt?: string;
	depends_on: string[];
	files_involved?: string[];
	complexity?: string;
	agent_role?: string;
	status: string;
	agent_id?: string;
}

export interface Plan {
	id: string;
	task_id: string;
	analysis?: string;
	strategy?: string;
	subtasks: Subtask[];
	questions?: Question[];
	estimated_agents?: number;
	status: string;
}

export function getPlan(taskId: string) {
	return request<Plan>(`/tasks/${taskId}/plan`);
}

export function updatePlan(taskId: string, data: { subtasks?: Subtask[]; strategy?: string }) {
	return request<{ status: string; plan: Plan }>(`/tasks/${taskId}/plan`, {
		method: 'PUT',
		body: JSON.stringify(data)
	});
}

// ---- Questions ----

export interface Question {
	id: string;
	text: string;
	status: string;
	answer?: string;
	suggestions?: string[];
	options?: string[];
	asked_at: string;
}

export function getQuestions(taskId: string) {
	return request<{ questions: Question[] }>(`/tasks/${taskId}/questions`);
}

export function answerQuestion(taskId: string, questionId: string, answer: string) {
	return request<{ status: string; question_id: string }>(`/tasks/${taskId}/questions/${questionId}/answer`, {
		method: 'POST',
		body: JSON.stringify({ answer })
	});
}

// ---- Files ----

export interface FileInfo {
	name: string;
	size: number;
	path?: string;
	uploaded_at?: string;
	created_at?: string;
}

export async function uploadFiles(taskId: string, files: File[], directory?: string) {
	const form = new FormData();
	for (const f of files) {
		form.append('files[]', f);
	}
	if (directory) form.append('directory', directory);

	const res = await fetch(`${BASE}/tasks/${taskId}/files`, { method: 'POST', body: form });
	if (!res.ok) {
		const body = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	return res.json() as Promise<{ uploaded: FileInfo[] }>;
}

export function getFiles(taskId: string) {
	return request<{ input: FileInfo[]; output: FileInfo[]; workspace: FileInfo[] }>(`/tasks/${taskId}/files`);
}

export function downloadFile(taskId: string, filename: string, type: 'input' | 'output' | 'workspace' = 'output') {
	const baseName = filename.split('/').pop() || filename;
	return `${BASE}/tasks/${taskId}/files/${encodeURIComponent(baseName)}?type=${type}&path=${encodeURIComponent(filename)}`;
}

export function getFileContent(taskId: string, filename: string, type: 'input' | 'output' | 'workspace' = 'output') {
	return request<{ name: string; path: string; size: number; content: string }>(
		`/tasks/${taskId}/file-viewer?type=${type}&path=${encodeURIComponent(filename)}`
	);
}

export function updateFileContent(taskId: string, filename: string, content: string, type: 'input' | 'output' | 'workspace' = 'output') {
	return request<{ name: string; path: string; size: number }>(
		`/tasks/${taskId}/file-viewer?type=${type}&path=${encodeURIComponent(filename)}`,
		{ method: 'PUT', body: JSON.stringify({ content }) }
	);
}

export function deleteFile(taskId: string, filename: string, type: 'input' | 'output' | 'workspace' = 'output') {
	return request<{ status: string }>(
		`/tasks/${taskId}/file-viewer?type=${type}&path=${encodeURIComponent(filename)}`,
		{ method: 'DELETE' }
	);
}

// ---- Messages ----

export function sendMessage(taskId: string, agentId: string, content: string) {
	return request<{ delivered: boolean; agent_id: string }>(`/tasks/${taskId}/message`, {
		method: 'POST',
		body: JSON.stringify({ agent_id: agentId, content })
	});
}

// ---- Events ----

export interface TaskEvent {
	id: number;
	type: string;
	data: Record<string, unknown>;
	created_at: string;
}

export function getEvents(taskId: string) {
	return request<{ events: TaskEvent[] }>(`/tasks/${taskId}/events`);
}

// ---- Config ----

export interface AppConfig {
	server: { port: number };
	docker: { max_agents: number; image_name: string };
	claude: { auth_mode: string };
	storage: { data_dir: string };
}

export function getConfig() {
	return request<AppConfig>('/config');
}

export function updateAuth(authMode: string, sessionKey?: string) {
	return request<{ updated: boolean }>('/config/auth', {
		method: 'PUT',
		body: JSON.stringify({ auth_mode: authMode, ...(sessionKey ? { session_key: sessionKey } : {}) })
	});
}

// ---- Repo Templates ----

export interface RepoTemplate {
	id: string;
	name: string;
	url: string;
	default_branch: string;
	auto_branch: boolean;
	auto_commit: boolean;
	auto_push: boolean;
	auto_pr: boolean;
	pr_target: string;
	pr_reviewers?: string[];
	enable_memory: boolean;
	created_at?: string;
	updated_at?: string;
}

export interface RepoMemory {
	id: string;
	repo_template_id: string;
	branch: string;
	commit_hash: string;
	content: string;
	file_tree?: string;
	languages?: string;
	frameworks?: string;
	key_files?: string;
	dependencies?: string;
	created_at: string;
}

export interface CreateRepoTemplateRequest {
	name: string;
	url: string;
	default_branch?: string;
	access_token?: string;
	auto_branch?: boolean;
	auto_commit?: boolean;
	auto_push?: boolean;
	auto_pr?: boolean;
	pr_target?: string;
	pr_reviewers?: string[];
	enable_memory?: boolean;
}

export function getRepoTemplates() {
	return request<{ templates: RepoTemplate[] }>('/repo-templates');
}

export function createRepoTemplate(data: CreateRepoTemplateRequest) {
	return request<RepoTemplate>('/repo-templates', { method: 'POST', body: JSON.stringify(data) });
}

export function deleteRepoTemplate(id: string) {
	return request<void>(`/repo-templates/${id}`, { method: 'DELETE' });
}

export function getRepoMemory(templateId: string, branch?: string) {
	const params = branch ? `?branch=${encodeURIComponent(branch)}` : '';
	return request<{ memory: RepoMemory | null }>(`/repo-templates/${templateId}/memory${params}`);
}

export function deleteRepoMemory(templateId: string) {
	return request<void>(`/repo-templates/${templateId}/memory`, { method: 'DELETE' });
}

// ---- Team Templates ----

export interface TeamRole {
	name: string;
	description: string;
	prompt_hint: string;
	max_instances: number;
	run_last: boolean;
}

export interface TeamTemplate {
	id: string;
	name: string;
	description: string;
	max_agents: number;
	review: boolean;
	roles: TeamRole[];
	mode: 'sequential' | 'collaborative';
	is_default: boolean;
	created_at?: string;
	updated_at?: string;
}

export interface CreateTeamTemplateRequest {
	name: string;
	description?: string;
	max_agents?: number;
	review?: boolean;
	roles: TeamRole[];
	mode?: 'sequential' | 'collaborative';
	is_default?: boolean;
}

export function getTeamTemplates() {
	return request<{ templates: TeamTemplate[] }>('/team-templates');
}

export function createTeamTemplate(data: CreateTeamTemplateRequest) {
	return request<TeamTemplate>('/team-templates', { method: 'POST', body: JSON.stringify(data) });
}

export function deleteTeamTemplate(id: string) {
	return request<void>(`/team-templates/${id}`, { method: 'DELETE' });
}

// ---- Agent Messages ----

export interface AgentMessage {
	id: number;
	task_id: string;
	from_agent_id?: string;
	from_subtask_id?: string;
	to_agent_id?: string;
	to_subtask_id?: string;
	msg_type: 'message' | 'context' | 'summary';
	content: string;
	created_at: string;
}

export function getAgentMessages(taskId: string) {
	return request<{ messages: AgentMessage[] }>(`/tasks/${taskId}/messages`);
}
