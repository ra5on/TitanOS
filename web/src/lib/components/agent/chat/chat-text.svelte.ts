// The parts of a chat of the KI-Agent (ChatView and its pieces, 1.01): the
// words for steps and notes, what a step got and gave, and copying.
import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
import { approvalTitle, stepWord, type Entry, type Pending } from '#lib/agent.ts';

// Steps TitanOS can take back (agent_extra.go undoActions).
export const UNDOABLE = ['file_write', 'files_delete', 'ha_call'];

/** The steps and thoughts of one turn, shown as one line. */
export type StepsBlock = { steps: Entry[]; thinking: string };

export function stepLabel(e: Entry): string {
	if (e.role === 'note')
		return t(
			e.note === 'verify'
				? 'agent.noteVerify'
				: e.note === 'approved'
					? 'agent.noteApproved'
					: 'agent.noteDeclined',
		);
	const word = stepWord(e.tool);
	return word ? t(`agent.step.${word}` as MessageKey) : (e.tool ?? '').replaceAll('_', ' ');
}

export function seconds(steps: Entry[]): string {
	const ms = steps.reduce((sum, s) => sum + (s.durationMs ?? 0), 0);
	// "0,0 s" says nothing: below a tenth of a second no time is shown.
	if (ms < 100) return '';
	const n = (ms / 1000).toLocaleString(i18n.locale, {
		minimumFractionDigits: 1,
		maximumFractionDigits: 1,
	});
	return ` · ${n} s`;
}

/** What the steps did, in short ("Docker-Apps gelesen, VMs gelesen"). More
 *  than three end in "und 2 weitere", never in "…" (1.07: no cut-off text). */
export function stepsGist(steps: Entry[]): string {
	const words = [...new Set(steps.filter((s) => s.role === 'tool').map(stepLabel))];
	const rest = words.length - 3;
	if (rest <= 0) return words.join(', ');
	return (
		words.slice(0, 3).join(', ') +
		' ' +
		(rest === 1 ? t('agent.stepsMoreOne') : t('agent.stepsMore', { count: rest }))
	);
}

export function stepOutput(step: Entry): string {
	const text = step.result || step.summary || '';
	return text.length > 6000 ? text.slice(0, 5999) + '…' : text;
}

export function argLines(args: unknown): [string, string][] {
	if (!args || typeof args !== 'object') return [];
	return Object.entries(args as Record<string, unknown>)
		.filter(([k]) => k !== 'content' && k !== 'sheets' && k !== 'code')
		.map(([k, v]) => [k, typeof v === 'string' ? v : JSON.stringify(v)] as [string, string])
		.map(([k, v]) => [k, v.length > 160 ? v.slice(0, 159) + '…' : v]);
}

export function preview(args: unknown): string {
	if (!args || typeof args !== 'object') return '';
	// A program the agent wants to run is shown whole, however long:
	// nobody approves lines they could not read.
	const code = (args as Record<string, unknown>).code;
	if (typeof code === 'string') return code;
	const c = (args as Record<string, unknown>).content;
	return typeof c === 'string' ? (c.length > 600 ? c.slice(0, 599) + '…' : c) : '';
}

export const noteText: Record<string, MessageKey> = {
	memory: 'agent.noteMemory',
	note: 'agent.noteNote',
	skill: 'agent.noteSkill',
	schedule: 'agent.noteSchedule',
	homeassistant: 'agent.noteHA',
	album: 'agent.noteAlbum',
	stopped: 'agent.noteStopped',
	interrupted: 'agent.noteInterrupted',
	error: 'agent.noteError',
	limit: 'agent.noteLimit',
	timelimit: 'agent.noteTimeLimit',
	loop: 'agent.noteLoop',
	undone: 'agent.noteUndone',
};

export function askTitle(p: Pending): string {
	const plain = approvalTitle(p);
	const what = plain
		? t(`agent.ask.${plain.key}` as MessageKey, { name: plain.name })
		: p.summary || p.tool.replaceAll('_', ' ');
	return t('agent.ask.title', { what });
}

export const levelText: Record<string, MessageKey> = {
	normal: 'agent.levelChange',
	danger: 'agent.levelDanger',
	destroy: 'agent.levelDestroy',
};

/** Kopieren: the clipboard API needs HTTPS; on plain http in the home
 *  network an invisible text field does it. True when it worked. */
export async function copyText(text: string): Promise<boolean> {
	try {
		await navigator.clipboard.writeText(text);
		return true;
	} catch {
		const area = document.createElement('textarea');
		area.value = text;
		area.setAttribute('readonly', '');
		area.style.position = 'fixed';
		area.style.opacity = '0';
		document.body.appendChild(area);
		area.select();
		let ok = false;
		try {
			ok = document.execCommand('copy');
		} catch {
			ok = false;
		}
		area.remove();
		return ok;
	}
}

/** The feedback of Kopieren for a moment: which text, and whether it worked. */
export class Copied {
	shown = $state<{ id: string; ok: boolean } | null>(null);
	private timer: ReturnType<typeof setTimeout> | undefined;
	copy = async (id: string, text: string) => {
		const ok = await copyText(text);
		this.shown = { id, ok };
		clearTimeout(this.timer);
		this.timer = setTimeout(() => (this.shown = null), 1800);
	};
	of = (id: string) => (this.shown?.id === id ? this.shown : null);
}
