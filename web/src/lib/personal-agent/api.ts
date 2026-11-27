import { api } from '../api.ts';

export type Level = 'never' | 'ask' | 'auto';
export const levels: Level[] = ['never', 'ask', 'auto'];
export type Source = { kind: string; id: string; label?: string; ref?: Record<string, string> };
export type Handoff = { sources: Source[]; tool?: string; prompt?: string };
export type JobRequest = {
	tool: string;
	requestId: string;
	prompt?: string;
	chatId?: string;
	runAt?: string;
	sources?: Source[];
	args?: Record<string, unknown>;
};
export type Preview = { title: string; text?: string; sources?: Source[]; data?: unknown };
export type Result = Preview & { link?: string };
export type JobStatus =
	'planned' | 'running' | 'waiting' | 'paused' | 'done' | 'failed' | 'canceled';
export type Job = {
	currentTool?: string;
	id: string;
	request: JobRequest;
	status: JobStatus;
	phase: string;
	preview?: Preview;
	token?: string;
	result?: Result;
	error?: string;
	created: string;
	updated: string;
	seen: boolean;
	completedSteps?: number;
	totalSteps?: number;
	audit?: { time: string; state: string; tool: string }[];
	receipts?: { step: number; tool: string; title: string; at: string }[];
};
export type Chat = { id: string; title: string; created: string };
export type Capability = {
	id: string;
	title: string;
	app: string;
	sensitive: boolean;
	effective: Level;
	ceiling: Level;
	reason?: string;
};
export type Settings = {
	rights: Record<string, Level>;
	notifications?: { result: boolean; blocker: boolean; approval: boolean };
	paused: boolean;
	theme: 'system' | 'light' | 'dark';
	motion: 'full' | 'subtle' | 'off';
	dotSize: number;
	colors: Record<string, string>;
};
export type State = { chats: Chat[]; jobs: Job[]; capabilities: Capability[]; settings: Settings };
export type Action = 'approve' | 'pause' | 'resume' | 'seen' | 'cancel';
export type Skill = {
	id: string;
	title: string;
	steps: string[];
	trigger: 'manual' | 'schedule' | 'mail';
	runAt?: string;
	sources?: Source[];
	approval: 'ask' | 'inherit';
	builtIn: boolean;
	expected?: string;
	sender?: string;
	pdfName?: string;
	watchState?: 'active' | 'paused' | 'retry' | 'revoked';
	prompt?: string;
	stepArgs?: Record<string, Record<string, unknown>>;
};
export type SkillInput = Omit<Skill, 'builtIn' | 'watchState'>;

export const base = '/personal-agent';
const esc = encodeURIComponent;
export function agentState() {
	return api.get<State>(base + '/state');
}
export function createJob(input: JobRequest) {
	return api.post<Job>(base + '/jobs', { ...input });
}
export type SearchResult = { chats: Chat[]; jobs: { id: string; chatId: string; title: string }[] };
export function searchAgent(query: string, signal: AbortSignal) {
	return api.get<SearchResult>(base + '/search?q=' + esc(query), signal);
}
export function changeJob(id: string, action: Action, token?: string) {
	return api.post<Job>(
		base + '/jobs/' + esc(id) + '/' + action,
		action === 'approve' ? { token } : {},
	);
}
export function saveSettings(settings: Partial<Settings>) {
	return api.put<State>(base + '/settings', settings);
}
export function capabilityLimits(account: string) {
	return api.get<{ rights: Record<string, Level>; defaults?: Record<string, Level> }>(
		base + '/limits/' + esc(account),
	);
}
export function saveLimits(account: string, rights: Record<string, Level>) {
	return api.put<{ rights: Record<string, Level>; defaults?: Record<string, Level> }>(
		base + '/limits/' + esc(account),
		{ rights },
	);
}
export function getMemory() {
	return api.get<{ items: string[] }>(base + '/memory');
}
export function saveMemory(items: string[]) {
	return api.put<{ items: string[] }>(base + '/memory', { items });
}
export function listSkills() {
	return api.get<{ skills: Skill[] }>(base + '/skills');
}
export function addSkill(skill: SkillInput) {
	return api.post(base + '/skills', { ...skill });
}
export function deleteSkill(id: string) {
	return api.delete(base + '/skills/' + esc(id));
}
export function runSkill(id: string, request: Omit<JobRequest, 'tool'>) {
	return api.post<Job>(base + '/skills/' + esc(id) + '/run', { ...request });
}

// UUIDs also work on NAS addresses served over plain HTTP, where randomUUID is absent.
export function requestId() {
	const bytes = crypto.getRandomValues(new Uint8Array(16));
	return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}
