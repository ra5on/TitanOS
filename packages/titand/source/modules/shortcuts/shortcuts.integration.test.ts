import {createServer, type Server} from 'node:http'
import type {AddressInfo} from 'node:net'

import {afterAll, beforeAll, expect, test} from 'vitest'

import createTestTitand from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestTitand>>
let webServer: Server
const WEB_SERVER_PORT = 6969

const webServerShortcut = {
	url: `http://localhost:${WEB_SERVER_PORT}`,
	title: 'Fake Website',
	icon: `http://localhost:${WEB_SERVER_PORT}/icon.png`,
}
const titanPortShortcut = {
	url: `titan:${WEB_SERVER_PORT}`,
	title: 'Fake Website',
	icon: `http://localhost:${WEB_SERVER_PORT}/icon.png`,
}

beforeAll(async () => {
	webServer = await createWebServer()
	titand = await createTestTitand()
})

afterAll(async () => {
	await Promise.all([closeServer(webServer), titand.cleanup()])
})

async function createWebServer() {
	const server = createServer((request, response) => {
		if (request.url === '/') {
			response.writeHead(200, {'content-type': 'text/html'})
			response.end(`
				<html>
					<head>
						<title>Fake Website</title>
						<link rel="icon" href="/icon.png">
					</head>
				</html>
				`)
			return
		}

		response.writeHead(404)
		response.end()
	})

	await new Promise<void>((resolve, reject) => {
		server.once('error', reject)
		server.listen(WEB_SERVER_PORT, () => {
			server.off('error', reject)
			resolve()
		})
	})

	return server
}

async function closeServer(server: Server | undefined) {
	if (!server) return

	await new Promise<void>((resolve, reject) => {
		server.close((error) => {
			if (error) reject(error)
			else resolve()
		})
	})
}

// The following tests are stateful and must be run in order

test.sequential('shortcuts.fetchPageMetadata() throws invalid error without auth token', async () => {
	await expect(
		titand.unauthenticatedClient.shortcuts.fetchPageMetadata.query({url: webServerShortcut.url}),
	).rejects.toThrow('Invalid token')
})

test.sequential('shortcuts.list() throws invalid error without auth token', async () => {
	await expect(titand.unauthenticatedClient.shortcuts.list.query()).rejects.toThrow('Invalid token')
})

test.sequential('shortcuts.add() throws invalid error without auth token', async () => {
	await expect(titand.unauthenticatedClient.shortcuts.add.mutate(webServerShortcut)).rejects.toThrow('Invalid token')
})

test.sequential('shortcuts.remove() throws invalid error without auth token', async () => {
	await expect(titand.unauthenticatedClient.shortcuts.remove.mutate({url: webServerShortcut.url})).rejects.toThrow(
		'Invalid token',
	)
})

test.sequential('login', async () => {
	await expect(titand.registerAndLogin()).resolves.toBe(true)
})

test.sequential('shortcuts.fetchPageMetadata() returns metadata for an external URL', async () => {
	await expect(titand.client.shortcuts.fetchPageMetadata.query({url: webServerShortcut.url})).resolves.toStrictEqual({
		title: webServerShortcut.title,
		icon: webServerShortcut.icon,
	})
})

test.sequential('shortcuts.fetchPageMetadata() works for an internal port', async () => {
	await expect(titand.client.shortcuts.fetchPageMetadata.query({url: titanPortShortcut.url})).resolves.toStrictEqual({
		title: titanPortShortcut.title,
		icon: titanPortShortcut.icon,
	})
})

test.sequential('shortcuts.add() adds the provided shortcut', async () => {
	await expect(titand.client.shortcuts.add.mutate(webServerShortcut)).resolves.toBe(true)
	await expect(titand.client.shortcuts.list.query()).resolves.toStrictEqual([webServerShortcut])
})

test.sequential('shortcuts.add() does not add a duplicate shortcut', async () => {
	await expect(titand.client.shortcuts.add.mutate(webServerShortcut)).rejects.toThrow('[shortcut-already-exists]')
	await expect(titand.client.shortcuts.list.query()).resolves.toStrictEqual([webServerShortcut])
})

test.sequential('shortcuts.list() shows the added shortcuts', async () => {
	// add another shortcut
	await expect(titand.client.shortcuts.add.mutate(titanPortShortcut)).resolves.toBe(true)
	// list the shortcuts
	await expect(titand.client.shortcuts.list.query()).resolves.toStrictEqual([webServerShortcut, titanPortShortcut])
})

test.sequential('shortcuts.remove() removes the shortcut', async () => {
	await expect(titand.client.shortcuts.remove.mutate({url: webServerShortcut.url})).resolves.toBe(true)
	await expect(titand.client.shortcuts.list.query()).not.toContain([titanPortShortcut])
})
