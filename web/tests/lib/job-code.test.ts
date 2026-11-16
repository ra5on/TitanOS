// 1.03: known causes of Docker and LXC jobs in the page's language, and
// the bell's texts for a kind it may not know.
import { expect, test } from 'vitest';
import { dockerJobOutput, jobCodeText, lxcJobText } from '#lib/job-code.ts';
import { i18n, tOr } from '#lib/i18n/index.svelte.ts';
import { noticeText } from '#lib/notifications.svelte.ts';

const lookup = (key: string) => tOr(key, '');

test('a Docker job stopped at its time limit says so in the language of the page', () => {
	i18n.locale = 'de';
	const job = {
		output: 'Pulling immich ... \ndocker compose: stopped after 30m0s (time limit)\n',
		code: 'timeout',
	};
	expect(dockerJobOutput(job, lookup)).toBe(
		'Pulling immich ... \n' +
			'Abgebrochen: Docker Compose ist nicht innerhalb seiner Zeitgrenze fertig geworden (Laden 30 Minuten, Starten 10 Minuten).',
	);
});

test('without a code, or with one this page has no text for, the output stays', () => {
	const output = 'Error: no such image\n';
	expect(dockerJobOutput({ output }, lookup)).toBe(output);
	expect(dockerJobOutput({ output, code: 'brand_new' }, lookup)).toBe(output);
	expect(jobCodeText('docker', '../evil', lookup)).toBe('');
});

test('an interrupted LXC job names the way out instead of the English text', () => {
	i18n.locale = 'en';
	expect(lxcJobText({ text: 'interrupted: titand stopped', code: 'interrupted' }, lookup)).toBe(
		'Creating did not finish because TitanOS stopped meanwhile. Remove the container and create it again.',
	);
	expect(lxcJobText({ text: 'download failed' }, lookup)).toBe('download failed');
});

test('the bell shows the texts of share_account_skipped and the NAS text of an unknown kind', () => {
	i18n.locale = 'de';
	const base = { id: 1, time: '2026-11-16T10:00:00Z', severity: 'error' as const, note: '' };
	const known = noticeText({
		...base,
		kind: 'share_account_skipped',
		subject: 'gast',
		title: 'x',
		text: 'y',
	} as never);
	expect(known.title).toBe('Konto gast ohne Zugriff auf Freigaben');
	const unknown = noticeText({
		...base,
		kind: 'from_a_newer_nas',
		subject: 'a',
		title: 'Neu: {subject}',
		text: 'Etwas Neues mit {subject}.',
	} as never);
	expect(unknown).toEqual({ title: 'Neu: a', text: 'Etwas Neues mit a.' });
});
