import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	TOKEN_PLACEHOLDER,
	claudeCodeCommand,
	desktopBridgeConfig,
	desktopRemoteConfig,
	mcpEndpoint,
	mcpTokenScopes,
} from '../src/lib/mcp.ts';

test('the endpoint is the origin of the web UI plus /mcp', () => {
	assert.equal(mcpEndpoint('https://nas.local'), 'https://nas.local/mcp');
	assert.equal(mcpEndpoint('https://nas.local/', '/mcp'), 'https://nas.local/mcp');
});

test('new MCP tokens only read, Docker and VMs only for administrators', () => {
	assert.deepEqual(mcpTokenScopes(false), ['mcp', 'system:read', 'files:read']);
	assert.deepEqual(mcpTokenScopes(true), [
		'mcp',
		'system:read',
		'files:read',
		'docker:read',
		'vms:read',
	]);
	for (const scope of [...mcpTokenScopes(true)]) assert.ok(!scope.endsWith(':write'), scope);
});

test('Claude Code gets the HTTP transport with a Bearer header', () => {
	assert.equal(
		claudeCodeCommand('https://nas.local/mcp', 'titanos_ab_cd'),
		'claude mcp add --transport http titanos https://nas.local/mcp --header "Authorization: Bearer titanos_ab_cd"',
	);
	assert.ok(claudeCodeCommand('https://nas.local/mcp').includes(TOKEN_PLACEHOLDER));
});

test('Claude Desktop configurations are valid JSON without the token in an argument', () => {
	const remote = JSON.parse(desktopRemoteConfig('https://nas.local/mcp', 'titanos_x_y'));
	const server = remote.mcpServers.titanos;
	assert.equal(server.command, 'npx');
	assert.deepEqual(server.args.slice(0, 3), ['-y', 'mcp-remote', 'https://nas.local/mcp']);
	assert.ok(!server.args.join(' ').includes('titanos_x_y'));
	assert.equal(server.env.TITANOS_AUTH, 'Bearer titanos_x_y');

	const bridge = JSON.parse(
		desktopBridgeConfig('https://nas.local/', '/home/me/.config/titanos-token'),
	);
	assert.deepEqual(bridge.mcpServers.titanos, {
		command: 'titanosctl',
		args: ['--url', 'https://nas.local', '--token-file', '/home/me/.config/titanos-token', 'mcp'],
	});
});
