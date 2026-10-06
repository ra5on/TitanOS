import {expect, beforeAll, afterAll, test} from 'vitest'

import createTestTitand from '../test-utilities/create-test-titand.js'
import runGitServer from '../test-utilities/run-git-server.js'

let titand: Awaited<ReturnType<typeof createTestTitand>>
let communityAppStoreGitServer: Awaited<ReturnType<typeof runGitServer>>

beforeAll(async () => {
	;[titand, communityAppStoreGitServer] = await Promise.all([createTestTitand(), runGitServer()])
})

afterAll(async () => {
	await Promise.all([communityAppStoreGitServer.close(), titand.cleanup()])
})

// The following tests are stateful and must be run in order

test.sequential('registry() throws invalid error when no user is registered', async () => {
	await expect(titand.client.appStore.registry.query()).rejects.toThrow('Invalid token')
})

test.sequential('repositories() throws invalid error when no user is registered', async () => {
	await expect(titand.client.appStore.repositories.query()).rejects.toThrow('Invalid token')
})

test.sequential('addRepository() throws invalid error when no user is registered', async () => {
	await expect(titand.client.appStore.addRepository.mutate({url: communityAppStoreGitServer.url})).rejects.toThrow(
		'Invalid token',
	)
})

test.sequential('removeRepository() throws invalid error when no user is registered', async () => {
	await expect(titand.client.appStore.removeRepository.mutate({url: communityAppStoreGitServer.url})).rejects.toThrow(
		'Invalid token',
	)
})

test.sequential('login', async () => {
	await expect(titand.registerAndLogin()).resolves.toBe(true)
})

test.sequential('registry() returns app registry', async () => {
	await expect(titand.client.appStore.registry.query()).resolves.toStrictEqual([
		{
			meta: {
				id: 'sparkles',
				name: 'Sparkles',
			},
			apps: [
				{
					appStoreId: 'sparkles',
					manifestVersion: '1.0.0',
					compatible: true,
					id: 'sparkles-hello-world',
					name: 'Hello World',
					tagline: "Replace this tagline with your app's tagline",
					icon: 'https://svgur.com/i/mvA.svg',
					category: 'Development',
					version: '1.0.0',
					description: "Add your app's description here.\n\nYou can also add newlines!",
					developer: 'Titan',
					website: 'https://umbrel.com',
					submitter: 'Titan',
					submission: 'https://github.com/getumbrel/umbrel-hello-world-app',
					repo: 'https://github.com/getumbrel/umbrel-hello-world-app',
					support: 'https://github.com/getumbrel/umbrel-hello-world-app/issues',
					gallery: [
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
					],
					releaseNotes: "Add what's new in the latest version of your app here.",
					dependencies: [],
				},
			],
		},
	])
})

test.sequential('repositories() returns owner-only repository management data', async () => {
	await expect(titand.client.appStore.repositories.query()).resolves.toStrictEqual([
		{
			url: titand.instance.appStore.defaultAppStoreRepo,
			meta: {id: 'sparkles', name: 'Sparkles'},
		},
	])
})

test.sequential('addRepository() adds a second repository', async () => {
	await expect(titand.client.appStore.addRepository.mutate({url: communityAppStoreGitServer.url})).resolves.toBe(true)
})

test.sequential('registry() returns both app repositories in registry', async () => {
	await expect(titand.client.appStore.registry.query()).resolves.toStrictEqual([
		{
			meta: {
				id: 'sparkles',
				name: 'Sparkles',
			},
			apps: [
				{
					appStoreId: 'sparkles',
					manifestVersion: '1.0.0',
					compatible: true,
					id: 'sparkles-hello-world',
					name: 'Hello World',
					tagline: "Replace this tagline with your app's tagline",
					icon: 'https://svgur.com/i/mvA.svg',
					category: 'Development',
					version: '1.0.0',
					description: "Add your app's description here.\n\nYou can also add newlines!",
					developer: 'Titan',
					website: 'https://umbrel.com',
					submitter: 'Titan',
					submission: 'https://github.com/getumbrel/umbrel-hello-world-app',
					repo: 'https://github.com/getumbrel/umbrel-hello-world-app',
					support: 'https://github.com/getumbrel/umbrel-hello-world-app/issues',
					gallery: [
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
					],
					releaseNotes: "Add what's new in the latest version of your app here.",
					dependencies: [],
				},
			],
		},
		{
			meta: {
				id: 'sparkles',
				name: 'Sparkles',
			},
			apps: [
				{
					appStoreId: 'sparkles',
					manifestVersion: '1.0.0',
					compatible: true,
					id: 'sparkles-hello-world',
					name: 'Hello World',
					tagline: "Replace this tagline with your app's tagline",
					icon: 'https://svgur.com/i/mvA.svg',
					category: 'Development',
					version: '1.0.0',
					description: "Add your app's description here.\n\nYou can also add newlines!",
					developer: 'Titan',
					website: 'https://umbrel.com',
					submitter: 'Titan',
					submission: 'https://github.com/getumbrel/umbrel-hello-world-app',
					repo: 'https://github.com/getumbrel/umbrel-hello-world-app',
					support: 'https://github.com/getumbrel/umbrel-hello-world-app/issues',
					gallery: [
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
					],
					releaseNotes: "Add what's new in the latest version of your app here.",
					dependencies: [],
				},
			],
		},
	])
})

test.sequential('addRepository() throws adding a repository that has already been added', async () => {
	await expect(titand.client.appStore.addRepository.mutate({url: communityAppStoreGitServer.url})).rejects.toThrow(
		'already exists',
	)
})

test.sequential('removeRepository() removes a reposoitory', async () => {
	await expect(titand.client.appStore.removeRepository.mutate({url: communityAppStoreGitServer.url})).resolves.toBe(
		true,
	)
})

test.sequential('registry() no longer returns an app repository that has been removed', async () => {
	await expect(titand.client.appStore.registry.query()).resolves.toStrictEqual([
		{
			meta: {
				id: 'sparkles',
				name: 'Sparkles',
			},
			apps: [
				{
					appStoreId: 'sparkles',
					manifestVersion: '1.0.0',
					compatible: true,
					id: 'sparkles-hello-world',
					name: 'Hello World',
					tagline: "Replace this tagline with your app's tagline",
					icon: 'https://svgur.com/i/mvA.svg',
					category: 'Development',
					version: '1.0.0',
					description: "Add your app's description here.\n\nYou can also add newlines!",
					developer: 'Titan',
					website: 'https://umbrel.com',
					submitter: 'Titan',
					submission: 'https://github.com/getumbrel/umbrel-hello-world-app',
					repo: 'https://github.com/getumbrel/umbrel-hello-world-app',
					support: 'https://github.com/getumbrel/umbrel-hello-world-app/issues',
					gallery: [
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
						'https://i.imgur.com/yyVG0Jb.jpeg',
					],
					releaseNotes: "Add what's new in the latest version of your app here.",
					dependencies: [],
				},
			],
		},
	])
})

test.sequential('removeRepository() throws removing a reposoitory that does not exist', async () => {
	await expect(titand.client.appStore.removeRepository.mutate({url: communityAppStoreGitServer.url})).rejects.toThrow(
		'does not exist',
	)
})

test.sequential('removeRepository() throws removing the default reposoitory', async () => {
	await expect(
		titand.client.appStore.removeRepository.mutate({url: titand.instance.appStore.defaultAppStoreRepo}),
	).rejects.toThrow('Cannot remove default repository')
})
