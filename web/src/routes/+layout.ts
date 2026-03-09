import { loadTasks } from '$lib/stores/tasks';

export const ssr = false;
export const prerender = false;

export async function load() {
	await loadTasks().catch(() => {});
	return {};
}
