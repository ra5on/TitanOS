import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from 'vitest';
import { copyErrorKey, isKept, problemKey } from '#lib/copy-problems.ts';
import de from '#lib/i18n/de.json';
import en from '#lib/i18n/en.json';
import { taskItems } from '#lib/operations/tasks.ts';
import type { Operations } from '#lib/operations.ts';

// Every code titand can send has a sentence in both languages.
const backend = readFileSync(
	resolve(process.cwd(), '../backend/internal/files/copy_problems.go'),
	'utf8',
);
const copyGo = readFileSync(resolve(process.cwd(), '../backend/internal/files/copy.go'), 'utf8');
const transfersGo = readFileSync(
	resolve(process.cwd(), '../backend/internal/files/transfers.go'),
	'utf8',
);
const quoted = (source: string, pattern: RegExp) =>
	[...source.matchAll(pattern)].map((match) => match[1]);

test('every problem code of the backend has a text', () => {
	const codes = new Set([
		...quoted(
			backend.slice(backend.indexOf('func problemCode'), backend.indexOf('func jobErrorCode')),
			/return "([a-z_]+)"/g,
		),
		'read_error',
		'write_error',
		...quoted(copyGo + backend, /failure\("([a-z_]+)"/g),
		...quoted(copyGo, /noteKept\([^)]*"(kept_[a-z_]+)"\)/g),
	]);
	codes.delete('');
	expect(codes.size).toBeGreaterThan(20);
	for (const code of codes) {
		const key = problemKey(code);
		expect(key, code).toBe(`files.copyProblem.${code}`);
		expect(de[key as keyof typeof de], code).toBeTruthy();
		expect(en[key as keyof typeof en], code).toBeTruthy();
	}
	expect(problemKey('something new')).toBe('files.copyProblem.other');
	expect(isKept('kept_link') && !isKept('permission')).toBe(true);
});

test('every job error code of the backend has a text', () => {
	const start = backend.indexOf('func jobErrorCode');
	const codes = new Set([
		...quoted(backend.slice(start), /return "([a-z_]+)"/g),
		...quoted(transfersGo, /ErrorCode = "([a-z_]+)"/g),
	]);
	for (const code of codes) {
		const key = copyErrorKey(code);
		expect(key, code).toBe(`files.copyError.${code}`);
		expect(de[key as keyof typeof de], code).toBeTruthy();
		expect(en[key as keyof typeof en], code).toBeTruthy();
	}
	expect(copyErrorKey(undefined)).toBe('');
});

test('a job finished with errors needs attention in the task list', () => {
	const job = {
		id: 'j',
		move: false,
		state: 'done',
		from: 'share/a',
		to: 'share/b',
		dir: '',
		names: ['x'],
		files: 2,
		done: 1,
		bytes: 2,
		copied: 1,
		current: '',
		error: '',
		failed: 1,
	};
	const [item] = taskItems({ transfers: [job] } as unknown as Operations);
	expect(item.state).toBe('failed');
	const [clean] = taskItems({ transfers: [{ ...job, failed: 0 }] } as unknown as Operations);
	expect(clean.state).toBe('done');
});
