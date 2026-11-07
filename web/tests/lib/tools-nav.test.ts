import { existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from 'vitest';
import deBase from '#lib/i18n/de.json';
import enBase from '#lib/i18n/en.json';
import deTaskmanager from '#lib/i18n/features/taskmanager.de.json';
import enTaskmanager from '#lib/i18n/features/taskmanager.en.json';
import { toolsAnchors, toolsGroups } from '#lib/tools-nav.ts';

// Texts of a part of PLAN 61 live in its own file (i18n/features).
const de = { ...deBase, ...deTaskmanager };
const en = { ...enBase, ...enTaskmanager };
const entries = toolsGroups.flatMap((group) => group.entries);
const page = (href: string) => resolve(process.cwd(), `src/routes${href}/+page.svelte`);

test('every tools row leads to a page and has texts in both languages', () => {
	expect(entries.length).toBeGreaterThan(5);
	for (const entry of entries) {
		expect(existsSync(page(entry.href)), entry.href).toBe(true);
		for (const key of [entry.label, entry.text]) {
			expect(de[key as keyof typeof de], key).toBeTruthy();
			expect(en[key as keyof typeof en], key).toBeTruthy();
		}
	}
	expect(new Set(entries.map((entry) => entry.href)).size).toBe(entries.length);
});

test('old anchors point to existing tools pages', () => {
	for (const target of Object.values(toolsAnchors))
		expect(existsSync(page(target)), target).toBe(true);
});
