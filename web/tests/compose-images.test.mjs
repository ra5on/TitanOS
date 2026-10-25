import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseDocument } from 'yaml';
import { imageReference, withImageTag, validImage } from '../src/lib/compose/image.ts';
import { readTemplate, build, check } from '../src/lib/compose/template.ts';
import { readService, writeService } from '../src/lib/compose/service.ts';
import { withHardware } from '../src/lib/compose/hardware.ts';

const template = (compose) => ({
	compose,
	timezone: 'Europe/Berlin',
	uid: 1000,
	gid: 100,
	appdata: '/mnt/Data/appdata',
	listening: [],
	missingDevices: [],
});
const read = (text) => parseDocument(text, { merge: true });
test('image tags distinguish registry ports, implicit latest and opaque references', () => {
	assert.deepEqual(imageReference('registry.example:5000/team/app:stable'), {
		repository: 'registry.example:5000/team/app',
		tag: 'stable',
		simple: true,
	});
	assert.equal(imageReference('registry.example:5000/team/app').tag, 'latest');
	assert.equal(
		withImageTag('registry.example:5000/team/app', 'v2'),
		'registry.example:5000/team/app:v2',
	);
	for (const image of [
		'app@sha256:' + 'a'.repeat(64),
		'app:stable@sha256:' + 'a'.repeat(64),
		'app:${VERSION:-stable}',
		'${IMAGE}',
	]) {
		assert.equal(imageReference(image).simple, false);
		assert.equal(withImageTag(image, 'latest'), image);
	}
	for (const image of ['app:', 'app:bad tag', 'app:-bad', 'app:' + 'a'.repeat(129)])
		assert.equal(validImage(image), false, image);
	assert.equal(validImage('app:v1.2_test-3'), true);
});

test('install and reopen preserve independent tags, store pins, dependencies and comments', () => {
	const tpl = template(`# app comment
services:
  web:
    image: registry.test:5000/web # keep this comment
    depends_on: [db]
  db:
    image: postgres:17
  worker:
    image: worker:stable@sha256:${'a'.repeat(64)}
`);
	const form = readTemplate(tpl, 'test', [], new Map());
	assert.deepEqual(
		form.images.map((f) => imageReference(f.image).tag),
		['latest', '17', ''],
	);
	assert.match(build(tpl, form).compose, /image: registry.test:5000\/web # keep this comment/);
	form.images[0].image = withImageTag(form.images[0].image, 'v2');
	const output = build(tpl, form).compose,
		plain = read(output).toJS();
	assert.equal(plain.services.web.image, 'registry.test:5000/web:v2');
	assert.equal(plain.services.db.image, 'postgres:17');
	assert.equal(plain.services.worker.image, 'worker:stable@sha256:' + 'a'.repeat(64));
	assert.deepEqual(plain.services.web.depends_on, ['db']);
	assert.ok(output.includes('# keep this comment'));
	const reopened = readTemplate(template(output), 'test', [], new Map(), {});
	assert.deepEqual(reopened.images, form.images);
	reopened.images[1].image = 'postgres:';
	assert.equal(check(reopened, [], new Map()).blocking, true);
});

test('editing an anchored image or service never changes another service', () => {
	for (const text of [
		'services:\n  web:\n    image: &image app:1\n  db:\n    image: *image\n',
		'services:\n  web: &service\n    image: app:1\n    command: [serve]\n  db: *service\n',
		'x-defaults: &defaults\n  image: app:1\n  restart: always\nservices:\n  web:\n    <<: *defaults\n  db:\n    <<: *defaults\n',
	]) {
		for (const target of ['web', 'db']) {
			const doc = read(text),
				before = doc.toJS();
			const form = readService(doc, target);
			form.image = 'app:2';
			writeService(doc, target, form);
			const output = read(doc.toString()).toJS();
			assert.equal(output.services[target].image, 'app:2');
			const other = target === 'web' ? 'db' : 'web';
			assert.deepEqual(output.services[other], before.services[other]);
		}
	}
});

test('unchanged anchors survive and per-service hardware does not leak', () => {
	const text = 'services:\n  web: &service\n    image: app:1\n  worker: *service\n';
	const doc = read(text);
	writeService(doc, 'web', readService(doc, 'web'));
	assert.equal(doc.toString(), text);
	const output = read(withHardware(text, { web: ['gpu-1'], worker: [] })).toJS();
	assert.deepEqual(output.services.web['x-titanos-hardware'], ['gpu-1']);
	assert.equal(output.services.worker['x-titanos-hardware'], undefined);
});

test('YAML roundtrip keeps edits and the existing generated secret', () => {
	const tpl = template(`x-titanos:
  variables:
    PASSWORD: {secret: generate}
services:
  web:
    image: app:1
    environment: {PASSWORD: '', TZ: UTC}
    ports: ['8080:80']
    volumes: ['./data:/data']
  db:
    image: postgres:17
`);
	const form = readTemplate(tpl, 'test', [], new Map());
	const first = build(tpl, form);
	const edited = first.compose
		.replace('app:1', 'app:2')
		.replace('Europe/Berlin', 'UTC')
		.replace('8080:80', '8088:80');
	const nextTpl = template(edited);
	const next = readTemplate(nextTpl, 'test', [], new Map([['8088/tcp', 'other']]), first.env);
	const second = build(nextTpl, next),
		plain = read(second.compose).toJS();
	assert.deepEqual(second.env, first.env);
	assert.equal(plain.services.web.image, 'app:2');
	assert.equal(plain.services.web.environment.TZ, 'UTC');
	assert.equal(plain.services.web.environment.PASSWORD, '${PASSWORD}');
	assert.deepEqual(plain.services.web.ports, ['8088:80']);
	assert.deepEqual(plain.services.web.volumes, ['./data:/data']);
	assert.equal(check(next, [], new Map([['8088/tcp', 'other']])).blocking, true);
	assert.throws(() => readTemplate(template('services: ['), 'test', [], new Map(), first.env));
});
