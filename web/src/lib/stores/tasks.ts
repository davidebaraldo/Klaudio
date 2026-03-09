import { writable } from 'svelte/store';
import { getTasks, getTask, type Task } from '$lib/api';

export const tasksStore = writable<Task[]>([]);
export const tasksLoading = writable(false);
export const tasksError = writable<string | null>(null);

export async function loadTasks() {
	tasksLoading.set(true);
	tasksError.set(null);
	try {
		const result = await getTasks({ limit: 100 });
		tasksStore.set(result.tasks ?? []);
	} catch (err) {
		tasksError.set(err instanceof Error ? err.message : 'Failed to load tasks');
		tasksStore.set([]);
	} finally {
		tasksLoading.set(false);
	}
}

export async function refreshTask(taskId: string) {
	try {
		const task = await getTask(taskId);
		tasksStore.update((tasks) => {
			const idx = tasks.findIndex((t) => t.id === taskId);
			if (idx >= 0) {
				tasks[idx] = task;
				return [...tasks];
			}
			return [task, ...tasks];
		});
		return task;
	} catch {
		return null;
	}
}
