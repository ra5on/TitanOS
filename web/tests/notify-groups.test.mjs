import { test } from 'node:test';
import assert from 'node:assert/strict';
import { bellGroup, bellShows, sanitizeBell } from '../src/lib/notify-groups.ts';

test('kinds map to groups, unknown ones to system', () => {
	assert.equal(bellGroup('volume_degraded'), 'storage');
	assert.equal(bellGroup('config_backup_failed'), 'backups');
	assert.equal(bellGroup('assistant_done'), 'assistant');
	assert.equal(bellGroup('certificate_expiring'), 'system');
});

test('defaults show everything; malformed settings are bounded', () => {
	const settings = sanitizeBell({ minSeverity: 'loud', groups: { usb: 'no', assistant: false, evil: false } });
	assert.equal(settings.minSeverity, 'info');
	assert.equal(settings.groups.usb, true);
	assert.equal(settings.groups.assistant, false);
	assert.equal('evil' in settings.groups, false);
});

test('filters by group and severity, but never hides storage errors', () => {
	const settings = sanitizeBell({ minSeverity: 'warning', groups: { storage: false, usb: false } });
	assert.equal(bellShows(settings, { kind: 'usb_plugged', severity: 'info' }), false);
	assert.equal(bellShows(settings, { kind: 'backup_failed', severity: 'info' }), false);
	assert.equal(bellShows(settings, { kind: 'backup_failed', severity: 'warning' }), true);
	assert.equal(bellShows(settings, { kind: 'volume_degraded', severity: 'warning' }), false);
	assert.equal(bellShows(settings, { kind: 'volume_failed', severity: 'error' }), true);
	assert.equal(bellShows(sanitizeBell({ minSeverity: 'error', groups: { disks: false } }), { kind: 'disk_unhealthy', severity: 'error' }), true);
});
