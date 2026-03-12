<script lang="ts">
	import type { Subtask } from '$lib/api';

	const { subtasks }: { subtasks: Subtask[] } = $props();

	// Layout constants
	const NODE_W = 160;
	const NODE_H = 48;
	const GAP_X = 60;
	const GAP_Y = 32;
	const PAD = 24;

	// Topological sort into layers (BFS by depth)
	let layout = $derived.by(() => {
		if (subtasks.length === 0) return { nodes: [], edges: [], width: 0, height: 0 };

		const idMap = new Map(subtasks.map((s) => [s.id, s]));

		// Compute depth for each node
		const depth = new Map<string, number>();
		const queue: string[] = [];

		// Find roots (no dependencies)
		for (const s of subtasks) {
			if (!s.depends_on || s.depends_on.length === 0) {
				depth.set(s.id, 0);
				queue.push(s.id);
			}
		}

		// BFS
		while (queue.length > 0) {
			const id = queue.shift()!;
			const d = depth.get(id)!;
			for (const s of subtasks) {
				if (s.depends_on?.includes(id)) {
					const prev = depth.get(s.id) ?? -1;
					if (d + 1 > prev) {
						depth.set(s.id, d + 1);
						queue.push(s.id);
					}
				}
			}
		}

		// Assign depth 0 to any unvisited nodes
		for (const s of subtasks) {
			if (!depth.has(s.id)) depth.set(s.id, 0);
		}

		// Group by layer
		const layers = new Map<number, Subtask[]>();
		for (const s of subtasks) {
			const d = depth.get(s.id) ?? 0;
			if (!layers.has(d)) layers.set(d, []);
			layers.get(d)!.push(s);
		}

		const maxLayer = Math.max(...layers.keys(), 0);
		const maxPerLayer = Math.max(...[...layers.values()].map((l) => l.length), 1);

		// Position nodes
		const nodes: Array<{ id: string; x: number; y: number; subtask: Subtask }> = [];

		for (let col = 0; col <= maxLayer; col++) {
			const layerNodes = layers.get(col) ?? [];
			const totalHeight = layerNodes.length * NODE_H + (layerNodes.length - 1) * GAP_Y;
			const maxTotalHeight = maxPerLayer * NODE_H + (maxPerLayer - 1) * GAP_Y;
			const offsetY = (maxTotalHeight - totalHeight) / 2;

			for (let row = 0; row < layerNodes.length; row++) {
				nodes.push({
					id: layerNodes[row].id,
					x: PAD + col * (NODE_W + GAP_X),
					y: PAD + offsetY + row * (NODE_H + GAP_Y),
					subtask: layerNodes[row]
				});
			}
		}

		// Build edges
		const posMap = new Map(nodes.map((n) => [n.id, n]));
		const edges: Array<{ x1: number; y1: number; x2: number; y2: number; from: string; to: string }> = [];

		for (const s of subtasks) {
			for (const depId of s.depends_on ?? []) {
				const from = posMap.get(depId);
				const to = posMap.get(s.id);
				if (from && to) {
					edges.push({
						x1: from.x + NODE_W,
						y1: from.y + NODE_H / 2,
						x2: to.x,
						y2: to.y + NODE_H / 2,
						from: depId,
						to: s.id
					});
				}
			}
		}

		const width = PAD * 2 + (maxLayer + 1) * NODE_W + maxLayer * GAP_X;
		const height = PAD * 2 + maxPerLayer * NODE_H + (maxPerLayer - 1) * GAP_Y;

		return { nodes, edges, width, height };
	});

	function statusFill(status: string): string {
		switch (status) {
			case 'completed': return '#059669';
			case 'running': return '#2563eb';
			case 'failed': return '#dc2626';
			default: return '#3f3f46';
		}
	}

	function statusStroke(status: string): string {
		switch (status) {
			case 'completed': return '#10b981';
			case 'running': return '#3b82f6';
			case 'failed': return '#ef4444';
			default: return '#52525b';
		}
	}

	function roleAbbr(role?: string): string {
		switch (role) {
			case 'developer': return 'DEV';
			case 'tester': return 'TEST';
			case 'reviewer': return 'REV';
			case 'manager': return 'MGR';
			default: return role?.slice(0, 4).toUpperCase() ?? '';
		}
	}

	// Edge path with a smooth curve
	function edgePath(e: { x1: number; y1: number; x2: number; y2: number }): string {
		const dx = e.x2 - e.x1;
		const cp = dx * 0.4;
		return `M${e.x1},${e.y1} C${e.x1 + cp},${e.y1} ${e.x2 - cp},${e.y2} ${e.x2},${e.y2}`;
	}
</script>

{#if subtasks.length === 0}
	<div class="p-8 text-center text-zinc-500 text-sm">No subtasks in the plan.</div>
{:else}
	<div class="overflow-auto rounded border border-zinc-800 bg-zinc-900/50">
		<svg
			width={layout.width}
			height={layout.height}
			viewBox="0 0 {layout.width} {layout.height}"
			class="min-w-full"
		>
			<defs>
				<marker id="arrow" viewBox="0 0 10 7" refX="9" refY="3.5" markerWidth="8" markerHeight="6" orient="auto-start-reverse">
					<polygon points="0 0, 10 3.5, 0 7" fill="#52525b" />
				</marker>
			</defs>

			<!-- Edges -->
			{#each layout.edges as edge}
				<path
					d={edgePath(edge)}
					fill="none"
					stroke="#52525b"
					stroke-width="1.5"
					marker-end="url(#arrow)"
				/>
			{/each}

			<!-- Nodes -->
			{#each layout.nodes as node}
				<g>
					<rect
						x={node.x}
						y={node.y}
						width={NODE_W}
						height={NODE_H}
						rx="6"
						fill={statusFill(node.subtask.status)}
						fill-opacity="0.15"
						stroke={statusStroke(node.subtask.status)}
						stroke-width="1.5"
					/>
					<!-- Status dot -->
					<circle
						cx={node.x + 14}
						cy={node.y + NODE_H / 2}
						r="4"
						fill={statusStroke(node.subtask.status)}
					>
						{#if node.subtask.status === 'running'}
							<animate attributeName="opacity" values="1;0.3;1" dur="1.5s" repeatCount="indefinite" />
						{/if}
					</circle>
					<!-- Name -->
					<text
						x={node.x + 26}
						y={node.y + 20}
						fill="#e4e4e7"
						font-size="12"
						font-weight="500"
						class="select-none"
					>
						{node.subtask.name.length > 16 ? node.subtask.name.slice(0, 15) + '...' : node.subtask.name}
					</text>
					<!-- Role badge -->
					<text
						x={node.x + 26}
						y={node.y + 36}
						fill="#71717a"
						font-size="10"
						class="select-none"
					>
						{roleAbbr(node.subtask.agent_role)}
					</text>
					<!-- Status text -->
					<text
						x={node.x + NODE_W - 8}
						y={node.y + 36}
						fill="#71717a"
						font-size="9"
						text-anchor="end"
						class="select-none"
					>
						{node.subtask.status}
					</text>
				</g>
			{/each}
		</svg>
	</div>
{/if}
