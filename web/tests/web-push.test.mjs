import { test } from 'node:test';
import assert from 'node:assert/strict';
import { appleMobile, deviceName, keyBytes } from '../src/lib/web-push.ts';

const iphone = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1';
const ipad = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15';
const chrome = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36';

test('iPhones and iPads need the home screen, desktops do not', () => {
	assert.ok(appleMobile(iphone, 5));
	assert.ok(appleMobile(ipad, 5), 'iPadOS reports itself as a Mac with touch');
	assert.ok(!appleMobile(ipad, 0), 'a real Mac');
	assert.ok(!appleMobile(chrome, 0));
});

test('devices get a name to tell them apart', () => {
	assert.equal(deviceName(iphone, 5), 'iPhone');
	assert.equal(deviceName(ipad, 5), 'iPad');
	assert.equal(deviceName(chrome, 0), 'Chrome (Windows)');
	assert.equal(deviceName(ipad, 0), 'Safari (macOS)');
});

test('the VAPID key becomes the bytes PushManager wants', () => {
	const bytes = keyBytes('BAEC_-8');
	assert.deepEqual([...bytes], [4, 1, 2, 255, 239]);
});
