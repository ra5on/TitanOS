// 1.03: the delete dialog of an account names what goes with it of the
// KI-Agent (GET /users/{name}/agent-data).
import { expect, test } from 'vitest';
import { agentDataText, sanitizeAgentData } from '#lib/account-agent-data.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';

const full = {
	available: true,
	chats: 12,
	projects: 3,
	handedOver: 1,
	memories: 7,
	skills: 0,
	schedules: 2,
	notes: 4,
	folder: true,
};

test('the counts that are there, handed-over projects not among the deleted', () => {
	i18n.locale = 'de';
	const text = agentDataText(sanitizeAgentData(full)!);
	expect(text.deleted).toEqual([
		'Chats mit ihren Dateien: 12',
		'Eigene Projekte: 2',
		'Einträge im Gedächtnis: 7',
		'Geplante Aufträge: 2',
		'Notizen: 4',
		'Der Ordner des Kontos in der Freigabe „KI-Agent“',
	]);
	expect(text.kept).toBe('Geteilte Projekte gehen an ein Mitglied und bleiben erhalten: 1');
	expect(text.note).toBe('');
});

test('without the volume the parts are named without numbers', () => {
	i18n.locale = 'en';
	const text = agentDataText(sanitizeAgentData({ available: false })!);
	expect(text.deleted).toContain('Chats with their files');
	expect(text.deleted.join(' ')).not.toMatch(/\d/);
	expect(text.note).toMatch(/not mounted/);
	expect(text.kept).toMatch(/Shared projects go to a member/);
});

test('an account the agent keeps nothing of says so', () => {
	i18n.locale = 'de';
	const text = agentDataText(sanitizeAgentData({ available: true })!);
	expect(text.deleted).toEqual([]);
	expect(text.kept).toBe('');
	expect(text.note).toBe('Der KI-Agent hat zu diesem Konto nichts gespeichert.');
});

test('a foreign answer gives no list; odd numbers are made safe', () => {
	expect(sanitizeAgentData(null)).toBeNull();
	expect(sanitizeAgentData('x')).toBeNull();
	expect(sanitizeAgentData({ chats: 3 })).toBeNull();
	const d = sanitizeAgentData({
		available: true,
		projects: 1,
		handedOver: 5,
		chats: -2,
		notes: 2.7,
	})!;
	expect(d.handedOver).toBe(1);
	expect(d.chats).toBe(0);
	expect(d.notes).toBe(2);
});

test('1.05: the T-Mail mailboxes go with the account, with or without the volume', () => {
	i18n.locale = 'de';
	const line = 'T-Mail-Postfächer (2) mit Servern, Passwörtern, Signaturen, Regeln und Adressbuch';
	expect(agentDataText(sanitizeAgentData({ ...full, mailAccounts: 2 })!).deleted).toContain(line);
	expect(
		agentDataText(sanitizeAgentData({ available: false, mailAccounts: 2 })!).deleted,
	).toContain(line);
	// Nothing else to delete: the mailboxes alone are no "nothing".
	const only = agentDataText(
		sanitizeAgentData({ available: true, folder: false, mailAccounts: 1 })!,
	);
	expect(only.note).toBe('');
	expect(agentDataText(sanitizeAgentData(full)!).deleted.join(' ')).not.toContain('T-Mail');
});
