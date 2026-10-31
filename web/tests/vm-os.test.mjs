import { test } from 'node:test';
import assert from 'node:assert/strict';
import { vmOsIcon } from '../src/lib/vm-os.ts';
const vm = (template, name, iso = '', description = '') => ({ template, name, iso, description });
test('VM marks follow the template, then names and the installation medium', () => {
	assert.equal(vmOsIcon(vm('windows', 'office')), 'windows');
	assert.equal(vmOsIcon(vm('linux', 'box')), 'linux');
	assert.equal(vmOsIcon(vm('other', 'haos', 'haos_ova-16.qcow2')), 'home');
	assert.equal(vmOsIcon(vm('other', 'router', 'OPNsense-25.1-dvd-amd64.iso')), 'shield');
	assert.equal(vmOsIcon(vm('other', 'test', 'Win11_24H2_German_x64.iso')), 'windows');
	assert.equal(vmOsIcon(vm('other', 'srv', 'debian-13.1.0-amd64-netinst.iso')), 'linux');
	assert.equal(vmOsIcon(vm('other', 'mystery')), 'monitor');
	// "darwin" or similar words must not pass for Windows or Linux by accident.
	assert.equal(vmOsIcon(vm('other', 'twins')), 'monitor');
});
