// PLAN 79-7: names and sentences the NVR's pages share – what a camera can
// look for, the detection devices, why the detection moved or stopped.
import { tOr, t, type MessageKey } from '../i18n/index.svelte.ts';
import type { Camera, NvrDevice } from './types.ts';

/** The friendly groups of what a camera looks for (drawing 79-nvr). */
export const OBJECT_GROUPS = {
	persons: ['person'],
	vehicles: ['car', 'motorcycle', 'bicycle', 'bus'],
	animals: ['dog', 'cat', 'bird', 'horse', 'sheep', 'cow', 'bear'],
} as const;
export type ObjectGroup = keyof typeof OBJECT_GROUPS;
export const OBJECT_GROUP_IDS = Object.keys(OBJECT_GROUPS) as ObjectGroup[];

/** The groups switched on by a list of objects (any object of the group counts). */
export function groupsOf(objects: string[]): ObjectGroup[] {
	return OBJECT_GROUP_IDS.filter((group) =>
		(OBJECT_GROUPS[group] as readonly string[]).some((o) => objects.includes(o)),
	);
}

/** Switches a whole group on or off; only objects the NAS allows are added. */
export function toggleGroup(
	objects: string[],
	group: ObjectGroup,
	on: boolean,
	allowed: string[],
): string[] {
	const members = OBJECT_GROUPS[group] as readonly string[];
	const rest = objects.filter((o) => !members.includes(o));
	if (!on) return rest;
	const add = members.filter((o) => !allowed.length || allowed.includes(o));
	// The order of the NAS's list, so the same choice is always sent the same way.
	const all = [...rest, ...add];
	return allowed.length ? allowed.filter((o) => all.includes(o)) : all;
}

/** "Person", "Auto", "Hund" – the name of something a camera saw. */
export function labelName(label: string): string {
	return tOr(`nvr.object.${label}`, label);
}

export function deviceName(kind: string, devices: NvrDevice[] = []): string {
	const found = devices.find((d) => d.kind === kind);
	const name = tOr(`nvr.device.${kind}`, kind);
	return found?.title && found.title !== name ? `${name} (${found.title})` : name;
}

/** The classified causes with texts of their own (classify.go). */
const REASONS = [
	'npu_firmware_old',
	'npu_turbo_unsupported',
	'npu_missing',
	'coral_missing',
	'config_invalid',
	'detector_unknown',
	'not_healthy',
	'engine',
];
function reasonKey(code: string): string {
	return REASONS.includes(code) ? code : 'detector_unknown';
}
/** What happened, in one sentence. */
export function reasonTitle(code: string): string {
	return t(`nvr.reason.${reasonKey(code)}.title` as MessageKey);
}
/** What it means. */
export function reasonMeaning(code: string): string {
	return t(`nvr.reason.${reasonKey(code)}.what` as MessageKey);
}
/** What to do about it. */
export function reasonAdvice(code: string): string {
	return t(`nvr.reason.${reasonKey(code)}.todo` as MessageKey);
}

/** "seit 14:02" / "seit 11.11., 14:02" for a time on another day. */
export function sinceText(iso: string | null, locale: string): string {
	if (!iso) return '';
	const at = new Date(iso);
	if (Number.isNaN(at.getTime())) return '';
	const sameDay = at.toDateString() === new Date().toDateString();
	return at.toLocaleString(locale, {
		...(sameDay ? {} : { day: '2-digit', month: '2-digit' }),
		hour: '2-digit',
		minute: '2-digit',
	});
}

export type CameraCondition = 'off' | 'online' | 'offline' | 'unknown';
/** How a camera is doing; `running` is whether the NVR runs at all. */
export function cameraCondition(camera: Camera, running: boolean): CameraCondition {
	if (!camera.enabled) return 'off';
	if (!running || !camera.state.known) return 'unknown';
	return camera.state.online ? 'online' : 'offline';
}

/** A clock time of Unix seconds. */
export function clock(seconds: number, locale: string, withSeconds = false): string {
	return new Date(seconds * 1000).toLocaleTimeString(locale, {
		hour: '2-digit',
		minute: '2-digit',
		...(withSeconds ? { second: '2-digit' } : {}),
	});
}
/** Day and clock time of Unix seconds; today without the day. */
export function dayClock(seconds: number, locale: string): string {
	const at = new Date(seconds * 1000);
	const sameDay = at.toDateString() === new Date().toDateString();
	return sameDay
		? clock(seconds, locale)
		: at.toLocaleString(locale, {
				day: '2-digit',
				month: '2-digit',
				hour: '2-digit',
				minute: '2-digit',
			});
}

/** Why a name cannot be a person of the known faces; '' when it can (nvr/faces.go). */
export type FaceNameProblem = '' | 'empty' | 'long' | 'space' | 'start' | 'chars' | 'train';
export function faceNameProblem(raw: string): FaceNameProblem {
	const name = raw.trim();
	if (!name) return 'empty';
	if (/\s/.test(name)) return 'space';
	if ([...name].length > 40) return 'long';
	if (name === 'train') return 'train';
	if (!/^[\p{L}\p{N}]/u.test(name)) return 'start';
	if (!/^[\p{L}\p{N}][\p{L}\p{N}_-]*$/u.test(name)) return 'chars';
	return '';
}
