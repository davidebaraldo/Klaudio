import { writable } from 'svelte/store';

export interface StreamEvent {
	type: string;
	event?: string;
	data?: Record<string, unknown>;
}

export function createTaskStream(taskId: string, agentId?: string) {
	const events = writable<StreamEvent[]>([]);
	const connected = writable(false);
	const terminalData = writable<Uint8Array | null>(null);

	let ws: WebSocket | null = null;
	let shouldReconnect = true;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

	function connect() {
		if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
			return;
		}

		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		let url = `${proto}//${location.host}/ws/tasks/${taskId}/stream`;
		if (agentId) {
			url += `?agent=${agentId}`;
		}
		ws = new WebSocket(url);
		ws.binaryType = 'arraybuffer';

		ws.onopen = () => {
			connected.set(true);
		};

		ws.onclose = () => {
			connected.set(false);
			if (shouldReconnect) {
				reconnectTimer = setTimeout(connect, 2000);
			}
		};

		ws.onerror = () => {
			// onclose will fire after this
		};

		ws.onmessage = (e: MessageEvent) => {
			if (e.data instanceof ArrayBuffer) {
				terminalData.set(new Uint8Array(e.data));
			} else if (typeof e.data === 'string') {
				try {
					const evt: StreamEvent = JSON.parse(e.data);
					events.update((list) => [...list, evt]);
				} catch {
					// ignore malformed JSON
				}
			}
		};
	}

	function sendMessage(agentId: string, content: string) {
		if (ws && ws.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify({ type: 'message', agent_id: agentId, content }));
		}
	}

	function disconnect() {
		shouldReconnect = false;
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		ws?.close();
		ws = null;
	}

	return { events, connected, terminalData, connect, sendMessage, disconnect };
}
