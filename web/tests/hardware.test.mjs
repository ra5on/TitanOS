import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseDocument } from 'yaml';
import { readService, writeService } from '../src/lib/compose/service.ts';
import { hardwareSelections, withHardware } from '../src/lib/compose/hardware.ts';

test('hardware selection survives form/Compose round trip and keeps unknown devices', () => {
 const doc = parseDocument('services:\n  media:\n    image: example # preserve\n    devices: ["vendor.example/device=custom"]\n    x-titanos-hardware: ["0000:01:00.0"]\n');
 const form = readService(doc, 'media');
 assert.deepEqual(form.hardware, ['0000:01:00.0']);
 form.hardware = ['0000:00:02.0'];
 writeService(doc, 'media', form);
 assert.deepEqual(doc.toJS().services.media.devices, ['vendor.example/device=custom']);
 assert.match(doc.toString(), /# preserve/);
 assert.deepEqual(readService(doc, 'media').hardware, ['0000:00:02.0']);
});

test('template requests can be explicitly cleared without being re-enabled', () => {
 const input = 'x-titanos:\n  hardware: [gpu-nvidia]\nservices:\n  app:\n    image: example\n  db:\n    image: postgres\n';
 assert.deepEqual(hardwareSelections(input), {app:['gpu-nvidia'],db:[]});
 const cleared = withHardware(input,{app:[],db:[]});
 assert.deepEqual(hardwareSelections(cleared),{app:[],db:[]});
});
