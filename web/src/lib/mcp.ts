// Settings › API und CLI › MCP: the MCP server of the NAS (backend mcp.go,
// docs/MCP.md) and the configuration snippets for AI clients.

export type MCPStatus = {
	enabled: boolean;
	path: string;
	protocolVersions: string[];
	scope: string;
};

/** Stands in for the secret in snippets until a token was just created. */
export const TOKEN_PLACEHOLDER = '<TOKEN>';

/** The address of the MCP endpoint on this NAS. */
export function mcpEndpoint(origin: string, path = '/mcp'): string {
	return origin.replace(/\/+$/, '') + path;
}

/** The scopes a new MCP token starts with: reading only, Docker/VMs for administrators. */
export function mcpTokenScopes(admin: boolean): string[] {
	return ['mcp', 'system:read', 'files:read', ...(admin ? ['docker:read', 'vms:read'] : [])];
}

/** `claude mcp add` for Claude Code with the HTTP transport. */
export function claudeCodeCommand(endpoint: string, token = TOKEN_PLACEHOLDER): string {
	return `claude mcp add --transport http titanos ${endpoint} --header "Authorization: Bearer ${token}"`;
}

/**
 * claude_desktop_config.json with mcp-remote, which turns the HTTP endpoint
 * into stdio. The header comes from the environment because Claude Desktop
 * splits arguments with spaces on some systems.
 */
export function desktopRemoteConfig(endpoint: string, token = TOKEN_PLACEHOLDER): string {
	return JSON.stringify(
		{
			mcpServers: {
				titanos: {
					command: 'npx',
					args: ['-y', 'mcp-remote', endpoint, '--header', 'Authorization:${TITANOS_AUTH}'],
					env: { TITANOS_AUTH: `Bearer ${token}` },
				},
			},
		},
		null,
		2,
	);
}

/** claude_desktop_config.json with the stdio bridge of titanosctl. */
export function desktopBridgeConfig(origin: string, tokenFile: string): string {
	return JSON.stringify(
		{
			mcpServers: {
				titanos: {
					command: 'titanosctl',
					args: ['--url', origin.replace(/\/+$/, ''), '--token-file', tokenFile, 'mcp'],
				},
			},
		},
		null,
		2,
	);
}
