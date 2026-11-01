import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseDocument } from 'yaml';
import {
	build,
	check,
	readTemplate,
	validCpuset,
	validDevice,
	validMemLimit,
} from '../src/lib/compose/template.ts';
import { readService, writeService } from '../src/lib/compose/service.ts';

const template = (compose, extra = {}) => ({
	compose,
	timezone: 'Europe/Berlin',
	uid: 1000,
	gid: 100,
	appdata: '/mnt/Data/appdata',
	listening: [],
	missingDevices: [],
	...extra,
});

test('cpuset, memory limit and devices are read and written like other fields', () => {
	const doc = parseDocument(
		'services:\n  app:\n    image: example # keep\n    cpuset: "0,2"\n    mem_limit: 4g\n    devices: ["/dev/dri:/dev/dri", {source: /dev/x, target: /dev/x}]\n',
	);
	const form = readService(doc, 'app');
	assert.equal(form.cpuset, '0,2');
	assert.equal(form.memLimit, '4g');
	assert.equal(form.devices.length, 2);
	assert.equal(form.devices[0].value, '/dev/dri:/dev/dri');
	form.cpuset = '';
	form.memLimit = '512m';
	form.devices.push({ value: '/dev/ttyUSB0' });
	writeService(doc, 'app', form);
	const app = doc.toJS().services.app;
	assert.equal(app.cpuset, undefined);
	assert.equal(app.mem_limit, '512m');
	// The long form stays as it was, a new device is added.
	assert.deepEqual(app.devices, [
		'/dev/dri:/dev/dri',
		{ source: '/dev/x', target: '/dev/x' },
		'/dev/ttyUSB0',
	]);
	assert.match(doc.toString(), /# keep/);
});

test('the install form sets limits, own labels and devices on the main service', () => {
	const tpl = template(
		'services:\n  web:\n    image: example/web:1\n    devices: ["/dev/missing:/dev/missing", "/dev/dri:/dev/dri"]\n  db:\n    image: postgres:16\n',
		{ missingDevices: ['/dev/missing'] },
	);
	const form = readTemplate(tpl, 'demo', [], new Map());
	assert.equal(form.cpuset, '');
	form.cpuset = '8-11';
	form.memLimit = '6g';
	form.own.labels.push({ name: 'com.example.team', value: 'home' });
	form.own.devices.push('/dev/ttyUSB0:/dev/ttyUSB0');
	assert.equal(check(form, [], new Map()).blocking, false);
	const web = parseDocument(build(tpl, form).compose).toJS().services.web;
	assert.equal(web.cpuset, '8-11');
	assert.equal(web.mem_limit, '6g');
	assert.equal(web.labels['com.example.team'], 'home');
	// The missing device is left out, the own one added.
	assert.deepEqual(web.devices, ['/dev/dri:/dev/dri', '/dev/ttyUSB0:/dev/ttyUSB0']);
});

test('wrong limits and devices block the installation', () => {
	const form = readTemplate(
		template('services:\n  app:\n    image: example\n'),
		'demo',
		[],
		new Map(),
	);
	form.cpuset = '1-';
	form.own.devices.push('ttyUSB0');
	const problems = check(form, [], new Map());
	assert.equal(problems.cpuset, true);
	assert.deepEqual(problems.ownDevices, ['invalid']);
	assert.equal(problems.blocking, true);
	for (const ok of ['', '3', '8-11', '0,2,4-5']) assert.ok(validCpuset(ok), ok);
	for (const bad of ['a', '1-', ',1']) assert.ok(!validCpuset(bad), bad);
	for (const ok of ['', '512m', '4g', '1.5g', '1073741824']) assert.ok(validMemLimit(ok), ok);
	for (const bad of ['4 GB', 'g', '-1']) assert.ok(!validMemLimit(bad), bad);
	for (const ok of ['/dev/dri', '/dev/ttyUSB0:/dev/ttyACM0', '/dev/sda:/dev/xvda:rwm'])
		assert.ok(validDevice(ok), ok);
	for (const bad of ['dri', '/dev/', '/etc/passwd']) assert.ok(!validDevice(bad), bad);
});

test('a blank template asks for the image', () => {
	const tpl = template('services:\n  app:\n    image: ""\n    restart: unless-stopped\n');
	const form = readTemplate(tpl, 'mein-container', [], new Map());
	assert.deepEqual(form.images, []);
	assert.equal(form.main, 'app');
});
