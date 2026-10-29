import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseDocument } from 'yaml';
import { ollamaService, prepareOllama, validOllamaModel } from '../src/lib/ollama-setup.ts';
import { readService } from '../src/lib/compose/service.ts';

// Shaped like the template in TitanOS-Apps: Ollama without a host port,
// Open WebUI on 3000 with a generated secret.
const compose = `x-titanos:
  name: Ollama
  variables:
    WEBUI_SECRET_KEY:
      secret: generate
  ports:
    "8080":
      webui: true
services:
  ollama:
    image: ollama/ollama:0.40.5
    volumes:
      - ./models:/root/.ollama
    restart: unless-stopped
  open-webui:
    image: ghcr.io/open-webui/open-webui:v0.12.2
    ports:
      - "3000:8080"
    environment:
      - OLLAMA_BASE_URL=http://ollama:11434
      - WEBUI_SECRET_KEY=
    volumes:
      - ./webui:/app/backend/data
`;
const tpl = { compose, timezone: 'Europe/Berlin', uid: 1000, gid: 100, appdata: '/mnt/Data/appdata', listening: [], missingDevices: [] };

test('the Ollama template gets a loopback port, the GPU and its secret apart', () => {
	const result = prepareOllama(tpl, [], new Map(), '0000:01:00.0');
	assert.equal(result.name, 'ollama');
	assert.equal(result.address, 'http://127.0.0.1:11434');
	const doc = parseDocument(result.compose, { merge: true });
	assert.deepEqual(doc.getIn(['services', 'ollama', 'ports']).toJSON(), ['127.0.0.1:11434:11434']);
	assert.deepEqual(readService(doc, 'ollama').hardware, ['0000:01:00.0']);
	assert.deepEqual(readService(doc, 'open-webui').hardware, []);
	assert.match(result.env.WEBUI_SECRET_KEY, /^.{16,}$/);
	assert.ok(!result.compose.includes(result.env.WEBUI_SECRET_KEY));
});

test('a second Ollama gets its own name and a free port; without a GPU it runs on the CPU', () => {
	const used = new Map([
		['11434/tcp', 'ollama'],
		['3000/tcp', 'other'],
	]);
	const result = prepareOllama(tpl, ['ollama'], used, null);
	assert.equal(result.name, 'ollama-2');
	assert.equal(result.port, 11435);
	const doc = parseDocument(result.compose, { merge: true });
	assert.deepEqual(readService(doc, 'ollama').hardware, []);
	// The web interface moved away from the busy port.
	assert.notDeepEqual(doc.getIn(['services', 'open-webui', 'ports']).toJSON(), ['3000:8080']);
});

test('a template without the Ollama image is refused', () => {
	assert.equal(ollamaService(compose), 'ollama');
	assert.throws(() => prepareOllama({ ...tpl, compose: 'services:\n  web:\n    image: nginx\n' }, [], new Map(), null), /template_without_ollama/);
});

test('own model names must be Ollama model references', () => {
	for (const name of ['qwen3:8b', 'llama3.1', 'library/llama3.1:8b', 'hf.co/bartowski/Llama-3.2-3B-Instruct-GGUF:Q4_K_M', 'registry.local:5000/team/model:v1'])
		assert.ok(validOllamaModel(name), name);
	for (const name of ['', 'qwen3 8b', '-x', 'a/../b', 'model@sha256:abc', 'x:', 'https://ollama.com/qwen3', 'a'.repeat(201)])
		assert.ok(!validOllamaModel(name), name);
});
