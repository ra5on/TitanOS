import assert from 'node:assert/strict';
import { test } from 'node:test';
import { agentAppMode } from '../src/lib/agent-app.ts';

function storage() {
	const m = new Map();
	return {
		getItem: (k) => m.get(k) ?? null,
		setItem: (k, v) => m.set(k, String(v)),
		clear: () => m.clear(),
	};
}
const url = (s) => new URL(s, 'https://nas.local');

test('the agent app stays an app when a notification opens it without ?app=1', () => {
	globalThis.sessionStorage = storage();
	globalThis.localStorage = storage();
	const standalone = () => true;
	assert.equal(agentAppMode(url('/agent?app=1'), standalone), true);
	// iOS ended the app; a notification opens /agent?chat=… in a new session.
	sessionStorage.clear();
	assert.equal(agentAppMode(url('/agent?chat=abc'), standalone), true);
	// A browser tab (not standalone) is no app.
	sessionStorage.clear();
	assert.equal(
		agentAppMode(url('/agent?chat=abc'), () => false),
		false,
	);
	// Outside /agent never.
	assert.equal(agentAppMode(url('/settings'), standalone), false);
});

test('a standalone TitanOS app that never was the agent app shows the frame', () => {
	globalThis.sessionStorage = storage();
	globalThis.localStorage = storage();
	assert.equal(
		agentAppMode(url('/agent'), () => true),
		false,
	);
	// Opened with ?app=1 in a browser tab: the tab only, not stored for later.
	assert.equal(
		agentAppMode(url('/agent?app=1'), () => false),
		true,
	);
	sessionStorage.clear();
	assert.equal(
		agentAppMode(url('/agent'), () => true),
		false,
	);
});
