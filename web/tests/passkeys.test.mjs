import { test } from 'node:test';
import assert from 'node:assert/strict';
import { passkeysPossible, toB64 } from '../src/lib/passkeys.ts';

test('toB64: base64url without padding', () => {
	assert.equal(toB64(new Uint8Array([0xfb, 0xff, 0xfe]).buffer), '-__-');
	assert.equal(toB64(new Uint8Array([1]).buffer), 'AQ');
	assert.equal(toB64(new TextEncoder().encode('titan').buffer), 'dGl0YW4');
});

test('toB64: nothing gives an empty string', () => {
	assert.equal(toB64(null), '');
	assert.equal(toB64(undefined), '');
});

test('passkeysPossible: needs WebAuthn, a secure page and a host name', () => {
	assert.equal(passkeysPossible(), false, 'no window');
	const page = (hostname, extra = {}) => {
		globalThis.window = {
			PublicKeyCredential: function () {},
			isSecureContext: true,
			location: { hostname },
			...extra,
		};
		try {
			return passkeysPossible();
		} finally {
			delete globalThis.window;
		}
	};
	assert.equal(page('nas.local'), true);
	assert.equal(page('192.168.1.20'), false);
	assert.equal(page('[fe80::1]'), false);
	assert.equal(page('fe80::1'), false);
	assert.equal(page('nas.local', { isSecureContext: false }), false);
	assert.equal(page('nas.local', { PublicKeyCredential: undefined }), false);
});
