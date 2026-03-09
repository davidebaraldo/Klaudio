import { getTask } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params }) => {
	try {
		const task = await getTask(params.id);
		return { task, taskId: params.id };
	} catch {
		return { task: null, taskId: params.id };
	}
};
