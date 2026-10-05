#!/usr/bin/env python3
"""Remove upstream OAuth registrations before the imported source is committed.

The fork must use its own runtime OAuth registrations and redirect service.
This transformation deliberately contains none of the inherited credentials.
"""

from pathlib import Path
import re
import sys


MARKER = "// Titan: OAuth registrations are supplied only at runtime."


def replace_between(text, start, end, replacement):
    first = text.index(start)
    last = text.index(end, first)
    return text[:first] + replacement + text[last:]


def prepare(root):
    source_path = root / "packages/umbreld/source/modules/files/cloud-auth.ts"
    tests_path = root / "packages/umbreld/source/modules/files/cloud-auth.unit.test.ts"
    source = source_path.read_text()
    tests = tests_path.read_text()
    if MARKER in source:
        if "DEFAULT_OAUTH_CLIENTS" in source or "cloudoauth.umbrel.com" in source:
            raise ValueError("OAuth sanitization marker exists but inherited defaults remain")
        print("Cloud OAuth already requires fork-owned runtime configuration.")
        return

    # Remember only in memory, so we can fail closed if an inherited registration
    # has another occurrence outside the two known public-source files.
    defaults_start = source.index("const DEFAULT_OAUTH_CLIENTS:")
    defaults_end = source.index("const OAUTH_ENV_PREFIXES:", defaults_start)
    inherited = re.findall(r"client(?:Id|Secret):\s*'([^']+)'", source[defaults_start:defaults_end])
    if len(inherited) != 5:
        raise ValueError("Pinned OAuth registration block changed; review the new source")

    source = replace_between(
        source,
        "// The production bouncer",
        "export const CLOUD_OAUTH_SCOPES",
        "// Titan requires an independently configured OAuth redirect service.\n\n",
    )
    source = replace_between(
        source,
        "// OAuth client identifiers",
        "const OAUTH_ENV_PREFIXES:",
        MARKER + "\n// There are no inherited provider client IDs or client secrets.\n\n",
    )
    source = replace_between(
        source,
        "const oauthClientsFromEnvironment =",
        "const appendOptionalClientSecret =",
        """const oauthClientsFromEnvironment = (environment: Environment): Partial<Record<OAuthProvider, OAuthClient>> => {
	const clients: Partial<Record<OAuthProvider, OAuthClient>> = {}
	for (const provider of OAUTH_PROVIDERS) {
		const prefix = OAUTH_ENV_PREFIXES[provider]
		const titanPrefix = prefix.replace(/^UMBREL_/, 'TITAN_')
		const clientId =
			valueFromEnvironment(environment, `${titanPrefix}_CLIENT_ID`) ??
			valueFromEnvironment(environment, `${prefix}_CLIENT_ID`)
		if (!clientId) continue
		const clientSecret =
			valueFromEnvironment(environment, `${titanPrefix}_CLIENT_SECRET`) ??
			valueFromEnvironment(environment, `${prefix}_CLIENT_SECRET`)
		clients[provider] = {clientId, ...(clientSecret ? {clientSecret} : {})}
	}
	return clients
}

""",
    )
    source = source.replace(
        "readonly oauthClients: Record<OAuthProvider, OAuthClient>",
        "readonly oauthClients: Partial<Record<OAuthProvider, OAuthClient>>",
    )
    old_constructor = """		this.redirectUrl = new URL('/callback', environment.UMBREL_OAUTH_PROXY_URL ?? DEFAULT_OAUTH_PROXY_URL).toString()
		this.oauthClients = oauthClientsFromEnvironment(environment)"""
    new_constructor = """		const proxyUrl =
			valueFromEnvironment(environment, 'TITAN_OAUTH_PROXY_URL') ??
			valueFromEnvironment(environment, 'UMBREL_OAUTH_PROXY_URL')
		this.redirectUrl = proxyUrl ? new URL('/callback', proxyUrl).toString() : ''
		// A client ID without our own redirect service must not advertise a working integration.
		this.oauthClients = this.redirectUrl ? oauthClientsFromEnvironment(environment) : {}"""
    if source.count(old_constructor) != 1:
        raise ValueError("Pinned OAuth constructor changed; review the new source")
    source = source.replace(old_constructor, new_constructor)

    tests = replace_between(
        tests,
        "\ttest('uses the production OAuth registrations",
        "\ttest('creates local ten-minute PKCE sessions",
        """	test('disables inherited OAuth registrations and requires runtime client IDs plus an own redirect service', () => {
		const rclone = {
			async browse() {
				return {entries: [], truncated: false}
			},
			getAccountPaths(accountId: string) {
				return {config: `/tmp/${accountId}.conf`}
			},
		}
		const defaults = new CloudAuth({rclone, environment: {}})
		expect(defaults.oauthClients).toEqual({})
		expect(defaults.redirectUrl).toBe('')
		expect(defaults.getAvailableProviders()).toEqual(['webdav', 'icloud'])
		for (const provider of ['google-drive', 'dropbox', 'onedrive'] as const) {
			expect(() => defaults.beginOAuth(ACCOUNT_ID, provider)).toThrow('[cloud-provider-unavailable]')
		}

		const noRedirect = new CloudAuth({rclone, environment: {TITAN_CLOUD_GOOGLE_CLIENT_ID: 'test-google-client'}})
		expect(noRedirect.getAvailableProviders()).toEqual(['webdav', 'icloud'])
		const noClient = new CloudAuth({rclone, environment: {TITAN_OAUTH_PROXY_URL: 'https://proxy.example'}})
		expect(noClient.getAvailableProviders()).toEqual(['webdav', 'icloud'])
		const secretOnly = new CloudAuth({
			rclone,
			environment: {TITAN_OAUTH_PROXY_URL: 'https://proxy.example', TITAN_CLOUD_GOOGLE_CLIENT_SECRET: 'test-only-secret'},
		})
		expect(secretOnly.getAvailableProviders()).toEqual(['webdav', 'icloud'])

		const configured = new CloudAuth({rclone, environment: ENVIRONMENT})
		expect(configured.oauthClients).toEqual({
			'google-drive': {clientId: 'google-client', clientSecret: 'google-secret'},
			dropbox: {clientId: 'dropbox-client', clientSecret: 'dropbox-secret'},
			onedrive: {clientId: 'onedrive-client', clientSecret: 'test-onedrive-secret'},
		})
		expect(configured.redirectUrl).toBe('https://proxy.example/callback')
		const titanConfigured = new CloudAuth({
			rclone,
			environment: {
				TITAN_OAUTH_PROXY_URL: 'https://titan-proxy.example',
				TITAN_CLOUD_GOOGLE_CLIENT_ID: 'test-google-client',
				TITAN_CLOUD_GOOGLE_CLIENT_SECRET: 'test-google-secret',
			},
		})
		expect(titanConfigured.oauthClients).toEqual({
			'google-drive': {clientId: 'test-google-client', clientSecret: 'test-google-secret'},
		})
		expect(titanConfigured.getAvailableProviders()).toEqual(['google-drive', 'webdav', 'icloud'])
		expect(titanConfigured.redirectUrl).toBe('https://titan-proxy.example/callback')
	})

""",
    )
    tests = tests.replace("'ignored-onedrive-secret'", "'test-onedrive-secret'")
    for credential in inherited:
        if credential in source or credential in tests:
            raise ValueError("An inherited registration remains in a transformed OAuth file")
    for path in root.rglob('*'):
        if not path.is_file() or path.is_symlink() or path in {source_path, tests_path}:
            continue
        relative = path.relative_to(root)
        if '.git' in relative.parts or 'node_modules' in relative.parts or path.suffix not in {'.ts', '.tsx', '.js', '.json', '.md', '.yml', '.yaml', '.sh', '.py'}:
            continue
        content = path.read_text(errors='replace')
        if any(credential in content for credential in inherited):
            raise ValueError(f"Inherited OAuth registration found outside approved transform: {relative}")
    source_path.write_text(source)
    tests_path.write_text(tests)
    print("Removed inherited OAuth registrations and hosted redirect default; runtime configuration is required.")


if __name__ == '__main__':
    try:
        prepare(Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent)
    except (OSError, ValueError) as error:
        raise SystemExit(f"Cloud OAuth preparation failed: {error}") from error
