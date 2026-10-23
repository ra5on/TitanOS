import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { automationTemplates, templatesFor } from '../src/lib/automation-templates.ts';

const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url)));
const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url)));

test('every template has its texts in both languages', () => {
	for (const tpl of automationTemplates) {
		for (const part of ['name', 'prompt']) {
			const key = `automation.tpl.${tpl.id}.${part}`;
			assert.ok(de[key] && en[key], key);
		}
	}
});

test('accounts only see what they may use', () => {
	assert.equal(templatesFor(false, true).length, 0);
	assert.ok(!templatesFor(true, false).some((tpl) => tpl.homeAssistant));
	assert.ok(templatesFor(true, true).some((tpl) => tpl.id === 'nobodyHome'));
});

test('templates that move files need a confirmation', () => {
	assert.equal(automationTemplates.find((tpl) => tpl.id === 'usbPhotos').execution, 'confirm');
});
