import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

// F-12-17: errorText(code) shows the text of `error.<code>`; a code without
// that key reads "internal error" and costs the person the explanation.
// Every code the API answers with needs a text in both languages, or a
// line below that says why no person ever reads it through errorText.
const i18n = new URL('../src/lib/i18n', import.meta.url).pathname;
const api = new URL('../../backend/internal/api', import.meta.url).pathname;

/** The keys of a language across the base file and the features. */
function keys(lang) {
	const files = [
		join(i18n, `${lang}.json`),
		...readdirSync(join(i18n, 'features'))
			.filter((name) => name.endsWith(`.${lang}.json`))
			.map((name) => join(i18n, 'features', name)),
	];
	const out = new Set();
	for (const file of files)
		for (const key of Object.keys(JSON.parse(readFileSync(file, 'utf8')))) out.add(key);
	return out;
}

const errorKeys = (lang) => [...keys(lang)].filter((key) => key.startsWith('error.')).sort();

// Codes without `error.<code>`, each with the reason.
const NOT_SHOWN = new Map([
	// Answers of the MCP endpoint and of requests with an API token: the
	// reader is a client program, the browser logs in with a cookie.
	['method_not_allowed', 'MCP endpoint, not called by the UI'],
	['mcp_disabled', 'MCP endpoint, not called by the UI'],
	['token_scope_required', 'requests with an API token (MCP, scripts)'],
	// Codes of the tool catalog (catalog.go): they go into the tool result
	// of an MCP client or of the agent's model, never into an API error.
	['file_failed', 'tool result of the catalog'],
	['file_no_space', 'tool result of the catalog'],
	['file_protected', 'tool result of the catalog'],
	['file_read_only', 'tool result of the catalog'],
	['tool_timeout', 'tool result of the catalog'],
	// Home Assistant asks this endpoint, not a person.
	['assist_not_allowed', 'Ollama endpoint for Home Assistant Assist'],
	// The admin node answers another node of the cluster (agentClusterHandler);
	// the asking titand turns it into its own state.
	['agent_not_admin', 'node to node'],
	['agent_not_released', 'node to node'],
	['agent_unreachable', 'node to node'],
	// A refused WebSocket upgrade: the browser gives the page no body.
	['cross_origin', 'refused WebSocket upgrade'],
	// Thumbnails are <img src>; a failed one shows the icon instead.
	['thumb_busy', 'image request'],
	// Only the task manager asks for processes, and it looks the code up
	// under taskmanager.error.<code> first (tools/taskmanager/+page.svelte).
	['invalid_signal', 'taskmanager.error.invalid_signal'],
	['process_changed', 'taskmanager.error.process_changed'],
	['process_not_found', 'taskmanager.error.process_not_found'],
	['process_protected', 'taskmanager.error.process_protected'],
	['processes_failed', 'taskmanager.error.processes_failed'],
	// The remote view: refused over its WebSocket (viewer.err.<code>,
	// RemoteView.svelte) or as the answer to a plain download link.
	['viewer_agent', 'viewer.err.viewer_agent; download link'],
	['viewer_no_session', 'download link of the remote view'],
]);

// How a literal code reaches writeError. Codes that come out of a
// variable are found where the variable gets its literal.
const PATTERNS = [
	// writeError(w, http.StatusConflict, "agent_busy", …)
	/writeError\(\s*w,\s*[^,\n]+,\s*"([a-z0-9_]+)"/g,
	// Tables and returns: {storage.ErrBusy, http.StatusConflict, "volume_busy"}
	/\bhttp\.Status\w+,\s*"([a-z0-9_]+)"/g,
	// code = "media_busy" and code, next = "file_not_found", "…"
	/\bcode(?:,\s*\w+)?\s*:?=\s*"([a-z0-9_]+)"/g,
	// errorBody{Code: "rate_limited", …}
	/\bCode:\s*"([a-z0-9_]+)"/g,
];

/** The literal error codes of the API with the first file that has each. */
function backendCodes() {
	const codes = new Map();
	for (const name of readdirSync(api).sort()) {
		if (!name.endsWith('.go') || name.endsWith('_test.go')) continue;
		const source = readFileSync(join(api, name), 'utf8');
		for (const pattern of PATTERNS)
			for (const match of source.matchAll(pattern))
				if (!codes.has(match[1])) codes.set(match[1], name);
	}
	return codes;
}

test('every error text exists in German and English', () => {
	assert.deepEqual(errorKeys('en'), errorKeys('de'));
});

test('every error code of the API has a text or a reason to have none', () => {
	const codes = backendCodes();
	// The patterns still find the codes: a refactoring of writeError must
	// not turn this test into one that passes on nothing.
	assert.ok(codes.size > 400, `only ${codes.size} codes found in ${api}`);
	for (const code of ['agent_busy', 'volume_busy', 'media_busy'])
		assert.ok(codes.has(code), `${code} not found`);
	const known = keys('de');
	const missing = [...codes]
		.filter(([code]) => !known.has(`error.${code}`) && !NOT_SHOWN.has(code))
		.map(([code, file]) => `${code} (${file})`)
		.sort();
	assert.deepEqual(missing, []);
});

test('the codes left out are still codes of the API and have no text', () => {
	const codes = backendCodes();
	const known = keys('de');
	const stale = [...NOT_SHOWN.keys()].filter(
		(code) => !codes.has(code) || known.has(`error.${code}`),
	);
	assert.deepEqual(stale, []);
});
