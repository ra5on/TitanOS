import type { Spot } from '../agent-figures.ts';
export type Motif = '' | 'mail' | 'calendar' | 'document' | 'search';
export type Cue = { id: number; stops: string[]; motif: Motif; hold?: boolean };
const outlines = {
	document: [
		[-0.47, -0.7],
		[0.19, -0.7],
		[0.54, -0.34],
		[0.54, 0.7],
		[-0.47, 0.7],
		[-0.47, -0.7],
		[0.19, -0.7],
		[0.19, -0.34],
		[0.54, -0.34],
	],
	search: [
		[0.07, 0.25],
		[-0.23, 0.35],
		[-0.53, 0.2],
		[-0.65, -0.08],
		[-0.56, -0.42],
		[-0.28, -0.59],
		[0.04, -0.56],
		[0.25, -0.3],
		[0.25, -0.03],
		[0.07, 0.25],
		[0.62, 0.73],
	],

	mail: [
		[-0.69, -0.46],
		[0.69, -0.46],
		[0.69, 0.46],
		[-0.69, 0.46],
		[-0.69, -0.46],
		[0, 0.07],
		[0.69, -0.46],
	],
	calendar: [
		[-0.63, -0.46],
		[-0.36, -0.46],
		[-0.36, -0.69],
		[-0.36, -0.46],
		[0.36, -0.46],
		[0.36, -0.69],
		[0.36, -0.46],
		[0.63, -0.46],
		[0.63, 0.63],
		[-0.63, 0.63],
		[-0.63, -0.46],
		[-0.63, -0.06],
		[0.63, -0.06],
	],
};
export function motifPoints(motif: Exclude<Motif, ''>, clock: number): Spot[] {
	const path = outlines[motif];
	const lengths = path.slice(1).map(([x, y], i) => Math.hypot(x - path[i][0], y - path[i][1]));
	const total = lengths.reduce((a, b) => a + b, 0);
	return [0, 1 / 3, 2 / 3].map((phase) => {
		let distance = ((clock * 0.25 + phase) % 1) * total;
		let edge = 0;
		while (edge < lengths.length - 1 && distance > lengths[edge]) distance -= lengths[edge++];
		const p = distance / lengths[edge],
			a = path[edge],
			b = path[edge + 1];
		return { x: a[0] + (b[0] - a[0]) * p, y: a[1] + (b[1] - a[1]) * p, z: 0 };
	});
}
export function motifFor(tool: string): Motif {
	if (tool.startsWith('mail.')) return 'mail';
	if (tool.startsWith('calendar.') || tool.startsWith('reminders.')) return 'calendar';
	if (tool.startsWith('office.') || tool.includes('attachment')) return 'document';
	if (tool.startsWith('files.') || tool.includes('search')) return 'search';
	return '';
}
