import {expect, beforeAll, beforeEach, afterAll, afterEach, test} from 'vitest'
import got from 'got'

import * as totp from '../utilities/totp.js'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {resolveWallpaperAppearance} from './wallpapers.js'

let titand: Awaited<ReturnType<typeof createTestVm>>
let httpsPort = 0
let failed = false
let sessionToken = ''

beforeAll(async () => {
	// Forward the VM's HTTPS port so proxy cookie behavior can be tested over real TLS.
	titand = await createTestVm({
		device: 'titan-home',
		forwardPorts: [{guestPort: 443}],
	})
	await titand.vm.powerOn()
	httpsPort = titand.vm.getHostPort(443)
})

afterAll(async () => {
	await titand.cleanup()
})

// The tests are stateful steps of one scenario, skip the rest after a failure
afterEach(({task}) => {
	if (task.result?.state === 'fail') failed = true
})

beforeEach(({skip}) => {
	if (failed) skip()
})

const testUserCredentials = {
	name: 'satoshi',
	password: 'moneyprintergobrrr',
}

const testUserLanguage = 'ja'

const testTotpUri =
	'otpauth://totp/Titan?secret=63AU7PMWJX6EQJR6G3KTQFG5RDZ2UE3WVUMP3VFJWHSWJ7MMHTIQ&period=30&digits=6&algorithm=SHA1&issuer=titan.local'

function trpcResult<T>(body: unknown) {
	return (body as {result?: {data?: T}}).result?.data
}

// The following tests are stateful and must be run in order

test.sequential('exists() returns false when no user is registered', async () => {
	await expect(titand.client.user.exists.query()).resolves.toBe(false)
})

test.sequential('login() throws invalid error when no user is registered', async () => {
	await expect(titand.client.user.login.mutate({password: testUserCredentials.password})).rejects.toThrow(
		'Incorrect password',
	)
})

test.sequential('register() throws error if username is not supplied', async () => {
	await expect(
		titand.client.user.register.mutate({
			password: testUserCredentials.password,
		} as any),
	).rejects.toThrow(/invalid_type.*name/s)
})

test.sequential('register() throws error if password is not supplied', async () => {
	await expect(
		titand.client.user.register.mutate({
			name: testUserCredentials.name,
		} as any),
	).rejects.toThrow(/invalid_type.*password/s)
})

test.sequential('register() throws error if password is below min length', async () => {
	await expect(
		titand.client.user.register.mutate({
			name: testUserCredentials.name,
			password: 'rekt',
		}),
	).rejects.toThrow('Password must be at least 6 characters')
})

test.sequential('register() creates a user', async () => {
	await expect(titand.client.user.register.mutate(testUserCredentials)).resolves.toBe(true)
})

test.sequential('exists() returns true when a user is registered', async () => {
	await expect(titand.client.user.exists.query()).resolves.toBe(true)
})

test.sequential('register() throws an error if the user is already registered', async () => {
	await expect(titand.client.user.register.mutate(testUserCredentials)).rejects.toThrow(
		'Attempted to register when user is already registered',
	)
})

test.sequential('login() throws an error for invalid credentials', async () => {
	await expect(titand.client.user.login.mutate({password: 'usdtothemoon'})).rejects.toThrow('Incorrect password')
})

test.sequential('login() throws an error if password is not supplied', async () => {
	await expect(titand.client.user.login.mutate({} as any)).rejects.toThrow(/invalid_type.*password/s)
})

test.sequential("renewToken() throws if we're not logged in", async () => {
	await expect(titand.client.user.renewToken.mutate()).rejects.toThrow('Invalid token')
})

test.sequential("isLoggedIn() returns false if we're not logged in", async () => {
	await expect(titand.client.user.isLoggedIn.query()).resolves.toBe(false)
})

test.sequential('login() returns a token', async () => {
	const token = await titand.client.user.login.mutate(testUserCredentials)
	expect(token).toMatch(/^titan_[0-9a-f]{32}_[0-9a-f]{64}$/)
	sessionToken = token
	titand.setAuthToken(token)
})

test.sequential('dashboard tRPC rejects either half of the browser credential pair on its own', async () => {
	const tokenOnly = await titand.unauthenticatedApi
		.get('../trpc/user.get', {headers: {Authorization: `Bearer ${sessionToken}`}})
		.catch((error) => error)
	expect(tokenOnly.response.statusCode).toBe(401)

	const cookieOnly = await titand.browserApi.get('../trpc/user.get').catch((error) => error)
	expect(cookieOnly.response.statusCode).toBe(401)
})

test.sequential('login() sets only the HTTP app and browser session cookies on HTTP requests', async () => {
	const response = await titand.unauthenticatedApi.post('../trpc/user.login', {json: testUserCredentials})
	const setCookies = response.headers['set-cookie'] ?? []
	const appCookie = setCookies.find((cookie) => /^TITAN_APP_SESSION=[^;]/.test(cookie))
	const browserCookie = setCookies.find((cookie) => /^TITAN_BROWSER_SESSION=[^;]/.test(cookie))
	for (const cookie of [appCookie, browserCookie]) {
		expect(cookie).toContain('; Path=/')
		expect(cookie).toContain('; Expires=')
		expect(cookie).toContain('; HttpOnly')
		expect(cookie).toContain('; SameSite=Lax')
		expect(cookie).not.toContain('; Secure')
		expect(cookie).not.toContain('Domain=')
	}
	expect(setCookies.some((cookie) => /^__Host-TITAN_APP_SESSION_HTTPS=[^;]/.test(cookie))).toBe(false)
	expect(setCookies.some((cookie) => /^__Host-TITAN_BROWSER_SESSION_HTTPS=[^;]/.test(cookie))).toBe(false)
})

test.sequential('login() sets only the HTTPS app and browser session cookies on HTTPS requests', async () => {
	// A real TLS request through LAN ingress. Client-supplied forwarded headers are
	// overwritten at the ingress boundary, so the scheme cannot be spoofed here the
	// way the previous non-VM version of this test did.
	const {caCertificate} = await titand.client.lanIngress.getCertificateStatus.query()
	const response = await got.post(`https://127.0.0.1:${httpsPort}/trpc/user.login`, {
		json: testUserCredentials,
		https: {certificateAuthority: caCertificate},
		responseType: 'json',
	})
	const setCookies = response.headers['set-cookie'] ?? []
	const httpsProxyCookie = setCookies.find((cookie) => /^__Host-TITAN_APP_SESSION_HTTPS=[^;]/.test(cookie))
	const httpsBrowserCookie = setCookies.find((cookie) => /^__Host-TITAN_BROWSER_SESSION_HTTPS=[^;]/.test(cookie))
	expect(httpsProxyCookie).toContain('; Path=/')
	expect(httpsProxyCookie).toContain('; Secure')
	expect(httpsProxyCookie).toContain('; HttpOnly')
	expect(httpsProxyCookie).toContain('; SameSite=Lax')
	expect(httpsProxyCookie).toContain('; Expires=')
	expect(httpsProxyCookie).not.toContain('Domain=')
	expect(httpsBrowserCookie).toContain('; Path=/')
	expect(httpsBrowserCookie).toContain('; Secure')
	expect(httpsBrowserCookie).toContain('; HttpOnly')
	expect(httpsBrowserCookie).toContain('; SameSite=Lax')
	expect(httpsBrowserCookie).toContain('; Expires=')
	expect(httpsBrowserCookie).not.toContain('Domain=')
	expect(setCookies.some((cookie) => /^TITAN_APP_SESSION=[^;]/.test(cookie))).toBe(false)
	expect(setCookies.some((cookie) => /^TITAN_BROWSER_SESSION=[^;]/.test(cookie))).toBe(false)

	const dashboardToken = (response.body as {result?: {data?: unknown}}).result?.data
	if (typeof dashboardToken !== 'string' || !httpsProxyCookie || !httpsBrowserCookie) {
		throw new Error('HTTPS login did not return all credentials')
	}
	const renewed = await got.post(`https://127.0.0.1:${httpsPort}/trpc/user.renewToken`, {
		json: null,
		headers: {
			authorization: `Bearer ${dashboardToken}`,
			cookie: `${httpsProxyCookie.split(';')[0]}; ${httpsBrowserCookie.split(';')[0]}`,
		},
		https: {certificateAuthority: caCertificate},
		responseType: 'json',
	})
	expect((renewed.body as {result?: {data?: unknown}}).result?.data).toBe(dashboardToken)
	const renewedCookies = renewed.headers['set-cookie'] ?? []
	expect(renewedCookies.find((cookie) => cookie.startsWith('__Host-TITAN_APP_SESSION_HTTPS='))?.split(';')[0]).toBe(
		httpsProxyCookie.split(';')[0],
	)
	expect(
		renewedCookies.find((cookie) => cookie.startsWith('__Host-TITAN_BROWSER_SESSION_HTTPS='))?.split(';')[0],
	).toBe(httpsBrowserCookie.split(';')[0])
	expect(renewedCookies.some((cookie) => cookie.startsWith('TITAN_APP_SESSION='))).toBe(false)
	expect(renewedCookies.some((cookie) => cookie.startsWith('TITAN_BROWSER_SESSION='))).toBe(false)
})

test.sequential('renewToken() extends the session without rotating its credentials', async () => {
	const previousToken = sessionToken
	const previousHttpApiToken = await titand.client.user.getHttpApiToken.query()
	const token = await titand.client.user.renewToken.mutate()
	expect(token).toMatch(/^titan_[0-9a-f]{32}_[0-9a-f]{64}$/)
	expect(token).toBe(previousToken)
	sessionToken = token
	titand.setAuthToken(token)

	await expect(titand.client.user.get.query()).resolves.toMatchObject({name: testUserCredentials.name})
	expect(await titand.client.user.getHttpApiToken.query()).toBe(previousHttpApiToken)
})

test.sequential("isLoggedIn() returns true when we're logged in", async () => {
	await expect(titand.client.user.isLoggedIn.query()).resolves.toBe(true)
})

test.sequential('sessions survive an titand restart', async () => {
	await titand.vm.sshAsRoot('systemctl restart titan')
	await titand.waitForStartup({waitForUser: true})
	titand.setAuthToken(sessionToken)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({name: testUserCredentials.name})
})

test.sequential('logout revokes only the session making the request', async () => {
	const login = async () => {
		const response = await titand.unauthenticatedApi.post('../trpc/user.login', {json: testUserCredentials})
		const token = trpcResult<string>(response.body)
		const browserCookie = (response.headers['set-cookie'] ?? [])
			.find((cookie) => cookie.startsWith('TITAN_BROWSER_SESSION='))
			?.split(';')[0]
		if (!token || !browserCookie) throw new Error('Login did not return all credentials')
		return {token, browserCookie}
	}
	const preservedSession = await login()
	const loggedOutSession = await login()

	await expect(
		titand.unauthenticatedApi.post('../trpc/user.logout', {
			json: null,
			headers: {
				Authorization: `Bearer ${loggedOutSession.token}`,
				cookie: loggedOutSession.browserCookie,
			},
		}),
	).resolves.toMatchObject({statusCode: 200})

	const loggedOutRequest = await titand.unauthenticatedApi.get('../trpc/user.get', {
		headers: {
			Authorization: `Bearer ${loggedOutSession.token}`,
			cookie: loggedOutSession.browserCookie,
		},
		throwHttpErrors: false,
	})
	expect(loggedOutRequest.statusCode).toBe(401)

	const preservedRequest = await titand.unauthenticatedApi.get('../trpc/user.get', {
		headers: {
			Authorization: `Bearer ${preservedSession.token}`,
			cookie: preservedSession.browserCookie,
		},
	})
	expect(trpcResult<{name: string}>(preservedRequest.body)).toMatchObject({name: testUserCredentials.name})
})

test.sequential('generateTotpUri() returns a 2FA URI', async () => {
	await expect(titand.client.user.generateTotpUri.query()).resolves.toContain('otpauth://totp/Titan?secret=')
})

test.sequential('generateTotpUri() returns a unique 2FA URI each time', async () => {
	const firstUri = await titand.client.user.generateTotpUri.query()
	const secondUri = await titand.client.user.generateTotpUri.query()
	expect(firstUri).not.toBe(secondUri)
})

test.sequential('enable2fa() throws error on invalid token', async () => {
	const totpUri = await titand.client.user.generateTotpUri.query()
	await expect(
		titand.client.user.enable2fa.mutate({
			totpToken: '1234356',
			totpUri,
		}),
	).rejects.toThrow('Incorrect 2FA code')
})

test.sequential('enable2fa() enables 2FA on login', async () => {
	const currentSessionId = (await titand.client.user.listSessions.query()).find((session) => session.current)?.id
	const totpToken = totp.generateToken(testTotpUri)
	await expect(
		titand.client.user.enable2fa.mutate({
			totpToken,
			totpUri: testTotpUri,
		}),
	).resolves.toBe(true)
	expect((await titand.client.user.listSessions.query()).find((session) => session.current)?.id).toBe(currentSessionId)
})

test.sequential('login() requires 2FA token if enabled', async () => {
	await expect(titand.client.user.login.mutate(testUserCredentials)).rejects.toThrow('Missing 2FA code')

	const totpToken = totp.generateToken(testTotpUri)
	await expect(
		titand.unauthenticatedClient.user.login.mutate({
			...testUserCredentials,
			totpToken,
		}),
	).resolves.toBeTypeOf('string')
})

test.sequential('disable2fa() throws error on invalid token', async () => {
	await expect(
		titand.client.user.disable2fa.mutate({
			totpToken: '000000',
		}),
	).rejects.toThrow('Incorrect 2FA code')
})

test.sequential('disable2fa() disables 2fa on login', async () => {
	const currentSessionId = (await titand.client.user.listSessions.query()).find((session) => session.current)?.id
	const totpToken = totp.generateToken(testTotpUri)
	await expect(
		titand.client.user.disable2fa.mutate({
			totpToken,
		}),
	).resolves.toBe(true)
	expect((await titand.client.user.listSessions.query()).find((session) => session.current)?.id).toBe(currentSessionId)

	await expect(titand.unauthenticatedClient.user.login.mutate(testUserCredentials)).resolves.toBeTypeOf('string')
})

test.sequential('get() returns user data', async () => {
	await expect(titand.client.user.get.query()).resolves.toMatchObject({
		name: 'satoshi',
		language: 'en',
	})
})

test.sequential("set() sets the user's name", async () => {
	await expect(titand.client.user.set.mutate({name: 'Hal'})).resolves.toBe(true)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({name: 'Hal'})

	// Revert name change
	await expect(titand.client.user.set.mutate({name: testUserCredentials.name})).resolves.toBe(true)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({name: testUserCredentials.name})
})

test.sequential("set() sets the user's language", async () => {
	await expect(titand.client.user.set.mutate({language: testUserLanguage})).resolves.toBe(true)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({language: testUserLanguage})
})

test.sequential("set() sets the user's wallpaper", async () => {
	await expect(titand.client.user.set.mutate({wallpaper: '1'})).resolves.toBe(true)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({
		wallpaper: resolveWallpaperAppearance('1'),
	})

	await expect(titand.client.user.set.mutate({wallpaper: '2'})).resolves.toBe(true)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({
		wallpaper: resolveWallpaperAppearance('2'),
	})

	await expect(titand.client.user.set.mutate({wallpaper: 'unknown'})).rejects.toThrow('Unknown wallpaper')
	await expect(titand.client.user.get.query()).resolves.toMatchObject({
		wallpaper: resolveWallpaperAppearance('2'),
	})
})

test.sequential('set() throws on unknown property', async () => {
	// @ts-expect-error Testing invalid arguments
	await expect(titand.client.user.set.mutate({foo: 'bar'})).rejects.toThrow('unrecognized_keys')
})

test.sequential('language() is publically available', async () => {
	await expect(titand.unauthenticatedClient.user.language.query()).resolves.toBe(testUserLanguage)
})

test.sequential("language() returns the user's language", async () => {
	await expect(titand.client.user.language.query()).resolves.toBe(testUserLanguage)
})

test.sequential('changePassword() throws on inavlid oldPassword', async () => {
	await expect(
		titand.client.user.changePassword.mutate({oldPassword: 'fiat4lyfe', newPassword: 'usdtothemoon'}),
	).rejects.toThrow('Incorrect password')
})

test.sequential('verifyPassword() verifies without replacing the current session', async () => {
	await expect(titand.client.user.verifyPassword.mutate({password: 'wrong-password'})).rejects.toThrow(
		'Incorrect password',
	)
	await expect(titand.client.user.verifyPassword.mutate({password: testUserCredentials.password})).resolves.toBe(true)
	await expect(titand.client.user.get.query()).resolves.toMatchObject({name: testUserCredentials.name})
})

test.sequential("changePassword() changes the user's password", async () => {
	const currentSessionId = (await titand.client.user.listSessions.query()).find((session) => session.current)?.id
	await expect(
		titand.client.user.changePassword.mutate({oldPassword: testUserCredentials.password, newPassword: 'usdtothemoon'}),
	).resolves.toBe(true)
	expect((await titand.client.user.listSessions.query()).find((session) => session.current)?.id).toBe(currentSessionId)
	await expect(titand.unauthenticatedClient.user.login.mutate({password: 'usdtothemoon'})).resolves.toBeTypeOf(
		'string',
	)

	// Reset password
	await expect(
		titand.client.user.changePassword.mutate({oldPassword: 'usdtothemoon', newPassword: testUserCredentials.password}),
	).resolves.toBe(true)
	await expect(titand.unauthenticatedClient.user.login.mutate(testUserCredentials)).resolves.toBeTypeOf('string')
})

// NOTE: The test below will wipe the above state and create a new user
// We need it to test registering a user with language
test.sequential('register() creates a new user with language', async () => {
	// Create a fresh VM so we can register a new user
	await titand.cleanup()
	titand = await createTestVm({device: 'titan-home'})
	await titand.vm.powerOn()

	// Register a new user with language
	await expect(titand.client.user.register.mutate({...testUserCredentials, language: testUserLanguage})).resolves.toBe(
		true,
	)

	// Set the session credential.
	const token = await titand.client.user.login.mutate(testUserCredentials)
	titand.setAuthToken(token)

	// Check language is returned in user object
	await expect(titand.client.user.get.query()).resolves.toMatchObject({language: testUserLanguage})

	// Check language is returned in public language endpoint
	await expect(titand.client.user.language.query()).resolves.toBe(testUserLanguage)
})
