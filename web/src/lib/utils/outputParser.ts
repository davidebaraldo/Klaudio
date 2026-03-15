/**
 * Output parser: converts raw terminal text (ANSI-stripped) into structured
 * activity blocks for the parsed terminal view.
 */

// Strip ANSI escape sequences from text
// eslint-disable-next-line no-control-regex
const ANSI_RE = /\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?(?:\x07|\x1b\\)|\x1b[()][AB012]|\x1b\[[\?]?[0-9;]*[hlm]|\x0f/g;

export function stripAnsi(text: string): string {
	return text.replace(ANSI_RE, '');
}

export type ActivityType =
	| 'tool_read'
	| 'tool_write'
	| 'tool_edit'
	| 'tool_bash'
	| 'tool_grep'
	| 'tool_glob'
	| 'tool_other'
	| 'thinking'
	| 'output'
	| 'error'
	| 'status'
	| 'text';

export interface Activity {
	id: number;
	type: ActivityType;
	title: string;
	content: string;
	timestamp: number;
	file?: string;
	command?: string;
	collapsed: boolean;
}

// Tool detection patterns (Claude Code output format)
const TOOL_PATTERNS: { re: RegExp; type: ActivityType; extract: (m: RegExpMatchArray, line: string) => Partial<Activity> }[] = [
	{
		re: /⏺\s*(?:Read|Reading)\s*(?:file:?\s*)?(.+)/i,
		type: 'tool_read',
		extract: (m) => ({ title: 'Read file', file: m[1].trim() })
	},
	{
		re: /⏺\s*(?:Write|Writing|Create|Creating)\s*(?:file:?\s*|to:?\s*)?(.+)/i,
		type: 'tool_write',
		extract: (m) => ({ title: 'Write file', file: m[1].trim() })
	},
	{
		re: /⏺\s*(?:Edit|Editing|Update|Updating)\s*(?:file:?\s*)?(.+)/i,
		type: 'tool_edit',
		extract: (m) => ({ title: 'Edit file', file: m[1].trim() })
	},
	{
		re: /⏺\s*(?:Bash|Run|Running|Execute|Executing|Command)(?::?\s*)(.*)/i,
		type: 'tool_bash',
		extract: (m) => ({ title: 'Run command', command: m[1].trim() })
	},
	{
		re: /⏺\s*(?:Grep|Search|Searching)(?::?\s*)(.*)/i,
		type: 'tool_grep',
		extract: (m) => ({ title: 'Search', command: m[1].trim() })
	},
	{
		re: /⏺\s*(?:Glob|Find|Finding)(?::?\s*)(.*)/i,
		type: 'tool_glob',
		extract: (m) => ({ title: 'Find files', command: m[1].trim() })
	},
	{
		re: /⏺\s*(.+)/,
		type: 'tool_other',
		extract: (m) => ({ title: m[1].trim() })
	}
];

const THINKING_RE = /(?:Thinking|Analyzing|Planning|Reasoning|Processing)\s*\.{0,3}/i;
const ERROR_RE = /(?:Error|FAILED|panic|FATAL|exception|traceback)/i;
const SEPARATOR_RE = /^[\s─━═╌╍┄┅┈┉-]{5,}$/;
const STATUS_RE = /^(?:✓|✗|✔|✘|→|←|⚡|📝|🔍|📁|💡|⚠️|❌|✅)/;

let idCounter = 0;

/**
 * Parse raw terminal text into structured activity blocks.
 * Incrementally callable: pass new text chunks and get new activities.
 */
export function parseOutput(text: string): Activity[] {
	const clean = stripAnsi(text);
	const lines = clean.split('\n');
	const activities: Activity[] = [];
	let current: Activity | null = null;
	let contentLines: string[] = [];

	function flush() {
		if (current) {
			current.content = contentLines.join('\n').trim();
			if (current.content || current.title) {
				activities.push(current);
			}
			current = null;
			contentLines = [];
		}
	}

	for (const line of lines) {
		const trimmed = line.trim();

		// Skip empty separator lines
		if (SEPARATOR_RE.test(trimmed)) {
			continue;
		}

		// Skip completely empty lines (but allow them inside content blocks)
		if (!trimmed && !current) {
			continue;
		}

		// Check for tool patterns (lines starting with ⏺)
		let matched = false;
		for (const pat of TOOL_PATTERNS) {
			const m = trimmed.match(pat.re);
			if (m) {
				flush();
				const extracted = pat.extract(m, trimmed);
				current = {
					id: idCounter++,
					type: pat.type,
					title: extracted.title || trimmed,
					content: '',
					timestamp: Date.now(),
					file: extracted.file,
					command: extracted.command,
					collapsed: true
				};
				contentLines = [];
				matched = true;
				break;
			}
		}
		if (matched) continue;

		// Check for thinking
		if (THINKING_RE.test(trimmed) && !current) {
			flush();
			current = {
				id: idCounter++,
				type: 'thinking',
				title: trimmed,
				content: '',
				timestamp: Date.now(),
				collapsed: true
			};
			contentLines = [];
			continue;
		}

		// Check for errors
		if (ERROR_RE.test(trimmed) && !current) {
			flush();
			current = {
				id: idCounter++,
				type: 'error',
				title: trimmed.slice(0, 120),
				content: '',
				timestamp: Date.now(),
				collapsed: false
			};
			contentLines = [];
			continue;
		}

		// Check for status lines
		if (STATUS_RE.test(trimmed) && !current) {
			flush();
			activities.push({
				id: idCounter++,
				type: 'status',
				title: trimmed,
				content: '',
				timestamp: Date.now(),
				collapsed: true
			});
			continue;
		}

		// Accumulate content into current block, or create a text block
		if (current) {
			contentLines.push(line);
		} else {
			// Start a new text block for unmatched content
			current = {
				id: idCounter++,
				type: 'text',
				title: '',
				content: '',
				timestamp: Date.now(),
				collapsed: true
			};
			contentLines = [line];
		}
	}

	flush();
	return activities;
}

/** Icon and color mapping for activity types */
export function getActivityMeta(type: ActivityType): { icon: string; color: string; bg: string; border: string } {
	switch (type) {
		case 'tool_read':
			return { icon: '📖', color: 'text-sky-400', bg: 'bg-sky-950/40', border: 'border-sky-800/50' };
		case 'tool_write':
			return { icon: '✏️', color: 'text-emerald-400', bg: 'bg-emerald-950/40', border: 'border-emerald-800/50' };
		case 'tool_edit':
			return { icon: '🔧', color: 'text-amber-400', bg: 'bg-amber-950/40', border: 'border-amber-800/50' };
		case 'tool_bash':
			return { icon: '💻', color: 'text-violet-400', bg: 'bg-violet-950/40', border: 'border-violet-800/50' };
		case 'tool_grep':
			return { icon: '🔍', color: 'text-cyan-400', bg: 'bg-cyan-950/40', border: 'border-cyan-800/50' };
		case 'tool_glob':
			return { icon: '📁', color: 'text-orange-400', bg: 'bg-orange-950/40', border: 'border-orange-800/50' };
		case 'tool_other':
			return { icon: '🔹', color: 'text-blue-400', bg: 'bg-blue-950/40', border: 'border-blue-800/50' };
		case 'thinking':
			return { icon: '💭', color: 'text-purple-400', bg: 'bg-purple-950/40', border: 'border-purple-800/50' };
		case 'error':
			return { icon: '❌', color: 'text-red-400', bg: 'bg-red-950/40', border: 'border-red-800/50' };
		case 'status':
			return { icon: '→', color: 'text-zinc-400', bg: 'bg-zinc-800/40', border: 'border-zinc-700/50' };
		case 'text':
		default:
			return { icon: '📝', color: 'text-zinc-400', bg: 'bg-zinc-800/30', border: 'border-zinc-700/30' };
	}
}
