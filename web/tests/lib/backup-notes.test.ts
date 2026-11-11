import { describe, expect, it } from 'vitest';
import de from '../../src/lib/i18n/de.json';
import en from '../../src/lib/i18n/en.json';
import {
	NOTICE_CODES,
	PROBLEM_CODES,
	baseName,
	hasNotes,
	legacyWarning,
	noteGroups,
} from '../../src/lib/backup-notes.ts';

describe('backup notes', () => {
	it('groups problems before notices and counts what is not listed', () => {
		const groups = noteGroups({
			warning: '2 files that changed while being copied: a, b',
			notices: [{ code: 'vm_disk_skipped', count: 1, paths: ['vms/win11/system.img'] }],
			problems: [
				{ code: 'link_unsupported', count: 25, paths: ['Medien/aktuell', 'Dokumente/neu'] },
				{ code: 'made_up_later', count: 1, paths: null },
			],
		});
		expect(groups.map((g) => [g.kind, g.code, g.count, g.more, g.choice])).toEqual([
			['problem', 'link_unsupported', 25, 23, false],
			['problem', 'other', 1, 1, false],
			['notice', 'vm_disk_skipped', 1, 0, true],
		]);
	});

	it('falls back to the English sentence only for a state without codes', () => {
		const old = { warning: '192 links or special files: a, b' };
		expect(noteGroups(old)).toEqual([]);
		expect(legacyWarning(old)).toBe(old.warning);
		expect(hasNotes(old)).toBe(true);
		expect(legacyWarning({ ...old, problems: [{ code: 'changed', count: 1, paths: ['a'] }] })).toBe(
			'',
		);
		expect(hasNotes({ warning: '', problems: null, notices: [] })).toBe(false);
	});

	it('has German and English words for every code the NAS can send', () => {
		for (const table of [de, en] as Record<string, string>[]) {
			for (const code of [...PROBLEM_CODES, ...NOTICE_CODES]) {
				for (const part of ['one', 'many', 'text'])
					expect(table[`backup.note.${code}.${part}`], `${code}.${part}`).toBeTruthy();
			}
			for (const key of [
				'notify.kind.vm_backup_undo_failed.title',
				'notify.kind.vm_backup_undo_failed.text',
			])
				expect(table[key], key).toBeTruthy();
		}
	});

	it('names a file by its last part', () => {
		expect(baseName('Volume-nvme/vms/debian/vdisk1.qcow2')).toBe('vdisk1.qcow2');
		expect(baseName('plain')).toBe('plain');
	});
});
