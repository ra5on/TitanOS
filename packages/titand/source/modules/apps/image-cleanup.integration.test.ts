import {afterAll, afterEach, beforeAll, beforeEach, describe, expect, test, vi} from 'vitest'
import {$, execa} from 'execa'
import Dockerode from 'dockerode'
import fse from 'fs-extra'
import yaml from 'js-yaml'
import pRetry from 'p-retry'

import createTestTitand from '../test-utilities/create-test-titand.js'
import runGitServer from '../test-utilities/run-git-server.js'
import appEnvironment from './legacy-compat/app-environment.js'

// Like other app integration tests, run inside an isolated titan-dev instance:
// the feature deliberately cleans all unused images in its Docker daemon.
describe.sequential('Docker image cleanup through app lifecycle operations', () => {
	const firstApp = 'sparkles-image-cleanup'
	const sharedApp = 'sparkles-image-shared'
	const failedApp = 'sparkles-image-failed'
	const docker = new Dockerode()
	let titand: Awaited<ReturnType<typeof createTestTitand>>
	let repository: Awaited<ReturnType<typeof runGitServer>>
	let pins: string[]
	let failed = false
	const containers: string[] = []

	const settleCleanup = () => titand.instance.apps.imageCleanup.runOperation(async () => {})
	const installedDirectory = (appId: string) => `${titand.instance.dataDirectory}/app-data/${appId}`
	const compose = (image: string) => ({
		services: {
			app_proxy: {environment: {APP_HOST: 'server', APP_PORT: '80'}},
			server: {image, command: ['sleep', 'infinity']},
		},
	})
	const hasImage = async (reference: string) => {
		try {
			await docker.getImage(reference).inspect()
			return true
		} catch (error) {
			if ((error as {statusCode?: number}).statusCode === 404) return false
			throw error
		}
	}
	const createLeftover = async (name: string) => {
		const tag = `image-cleanup-fixture:${name}`
		await execa('docker', ['build', '--tag', tag, '-'], {
			input: `FROM ${pins[0]}\nLABEL image-cleanup-fixture=${name}\nCMD ["sleep", "infinity"]\n`,
		})
		return (await docker.getImage(tag).inspect()).Id
	}
	const setUpdateImage = async (appId: string, image: string) => {
		const directory = await titand.instance.appStore.getAppTemplateFilePath(appId)
		await fse.writeFile(`${directory}/docker-compose.yml`, yaml.dump(compose(image)))
	}

	beforeAll(async () => {
		titand = await createTestTitand({autoLogin: true})
		// Timer behavior is covered with a fake clock in the coordination tests.
		titand.instance.apps.imageCleanup.schedule()
		await settleCleanup()
		// Use immutable references in installed fixtures, including when simulating
		// skipped versions. The tags are only used to discover these test pins.
		pins = []
		for (const tag of ['alpine:3.20', 'alpine:3.21', 'alpine:3.22']) {
			await $`docker pull ${tag}`
			pins.push((await docker.getImage(tag).inspect()).RepoDigests[0])
		}
		repository = await runGitServer()
		for (const [index, appId] of [firstApp, sharedApp, failedApp].entries()) {
			const directory = `${repository.directory}/${appId}`
			await fse.copy(`${repository.directory}/sparkles-hello-world`, directory)
			const manifest = yaml.load(await fse.readFile(`${directory}/umbrel-app.yml`, 'utf8')) as Record<string, unknown>
			await fse.writeFile(`${directory}/umbrel-app.yml`, yaml.dump({...manifest, id: appId, port: 18090 + index}))
			const image = appId === failedApp ? '127.0.0.1:9/unavailable:latest' : pins[0]
			await fse.writeFile(`${directory}/docker-compose.yml`, yaml.dump(compose(image)))
			await fse.writeFile(`${directory}/exports.sh`, '')
		}
		await $({cwd: repository.directory})`git add .`
		await repository.addNewCommit()
		await titand.client.appStore.addRepository.mutate({url: repository.url})
		await pRetry(
			async () => {
				const stores = await titand.client.appStore.registry.query()
				expect(stores.some((store) => store.apps.some((app) => app.id === firstApp))).toBe(true)
			},
			{retries: 30, minTimeout: 100, maxTimeout: 1000},
		)
	})

	afterAll(async () => {
		for (const container of containers)
			await docker
				.getContainer(container)
				.remove({force: true})
				.catch(() => {})
		await titand?.cleanup()
		await repository?.close()
	})
	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
		vi.restoreAllMocks()
	})
	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('installs apps sharing a pinned image and preserves it when one is stopped', async () => {
		await titand.client.apps.install.mutate({appId: firstApp})
		await titand.client.apps.install.mutate({appId: sharedApp})
		await titand.client.apps.stop.mutate({appId: sharedApp})
		await settleCleanup()
		expect(
			await docker.listContainers({
				all: true,
				filters: JSON.stringify({label: [`com.docker.compose.project=${sharedApp}`]}),
			}),
		).toEqual([])
		expect(await hasImage(pins[0])).toBe(true)
		await fse.outputFile(`${installedDirectory(sharedApp)}/data/persistent.txt`, 'keep app data')
	})

	test('cleans up after a failed install without dropping already installed apps from the keep set', async () => {
		const leftover = await createLeftover('failed-install')
		await expect(titand.client.apps.install.mutate({appId: failedApp})).rejects.toThrow()
		await settleCleanup()
		expect(await titand.instance.apps.isInstalled(failedApp)).toBe(false)
		expect(await hasImage(leftover)).toBe(false)
		expect(await hasImage(pins[0])).toBe(true)
	})

	test('removes skipped-version, aliased and dangling leftovers after an update while preserving containers and stopped apps', async () => {
		const old = await createLeftover('old')
		await docker.getImage(old).tag({repo: 'image-cleanup-alias', tag: 'old'})
		const dangling = await createLeftover('dangling')
		await docker.getImage(old).tag({repo: 'image-cleanup-fixture', tag: 'dangling'})
		const running = await createLeftover('running')
		const stopped = await createLeftover('stopped')
		for (const [index, image] of [running, stopped].entries()) {
			const container = await docker.createContainer({Image: image})
			containers.push(container.id)
			if (index === 0) await container.start()
		}
		await setUpdateImage(firstApp, pins[1])
		await titand.client.apps.update.mutate({appId: firstApp})
		await settleCleanup()
		for (const image of [old, dangling]) expect(await hasImage(image)).toBe(false)
		for (const image of [pins[0], pins[1], running, stopped]) expect(await hasImage(image)).toBe(true)
		for (const image of (await appEnvironment(titand.instance, 'images'))!) expect(await hasImage(image)).toBe(true)
		expect(await fse.readFile(`${installedDirectory(sharedApp)}/data/persistent.txt`, 'utf8')).toBe('keep app data')
	})

	test('resolves installed exports, overrides, profiles and build-only services without rewriting runtime configuration', async () => {
		const directory = installedDirectory(sharedApp)
		const original = await fse.readFile(`${directory}/docker-compose.yml`, 'utf8')
		const runtime = await fse.readFile(`${directory}/docker-compose.titand.yml`, 'utf8')
		const gateway = await fse.readFile(`${directory}/app-gateway.json`, 'utf8')
		try {
			await fse.writeFile(`${directory}/exports.sh`, `echo 'exports diagnostic'\nexport APP_IMAGE='${pins[0]}'\n`)
			await fse.writeFile(`${directory}/docker-compose.yml`, yaml.dump(compose('${APP_IMAGE}')))
			expect(await titand.instance.apps.getApp(sharedApp).getExpectedImages()).toEqual([pins[0]])
			await fse.writeFile(
				`${directory}/docker-compose.titan-user-settings.yml`,
				yaml.dump({
					services: {
						server: {image: pins[1]},
						optional: {image: pins[2], profiles: ['optional']},
						built: {build: '.'},
					},
				}),
			)
			const images = await titand.instance.apps.getApp(sharedApp).getExpectedImages()
			expect(images.sort()).toEqual([pins[1], pins[2], `${sharedApp}-built`].sort())
			expect(await fse.readFile(`${directory}/docker-compose.titand.yml`, 'utf8')).toBe(runtime)
			expect(await fse.readFile(`${directory}/app-gateway.json`, 'utf8')).toBe(gateway)
			expect((await fse.readdir(directory)).some((name) => name.startsWith('docker-compose.images.'))).toBe(false)
		} finally {
			await fse.writeFile(`${directory}/docker-compose.yml`, original)
			await fse.writeFile(`${directory}/exports.sh`, '')
			await fse.remove(`${directory}/docker-compose.titan-user-settings.yml`)
		}
	})

	test('cleans up after a failed update and allows retrying with a different pinned image', async () => {
		const leftover = await createLeftover('failed-update')
		await setUpdateImage(firstApp, '127.0.0.1:9/unavailable:latest')
		await expect(titand.client.apps.update.mutate({appId: firstApp})).rejects.toThrow()
		await settleCleanup()
		expect(await hasImage(leftover)).toBe(false)
		expect(await hasImage(pins[0])).toBe(true)
		expect(await hasImage(pins[1])).toBe(false)
		await setUpdateImage(firstApp, pins[2])
		await expect(titand.client.apps.update.mutate({appId: firstApp})).resolves.toBe(true)
		await settleCleanup()
		expect(await hasImage(pins[2])).toBe(true)
	})

	test('skips the whole sweep for unreadable installed configuration, then recovers on the next operation', async () => {
		const leftover = await createLeftover('invalid-compose')
		const composePath = `${installedDirectory(sharedApp)}/docker-compose.yml`
		const original = await fse.readFile(composePath, 'utf8')
		try {
			await fse.writeFile(composePath, 'services: [invalid')
			await titand.client.apps.update.mutate({appId: firstApp})
			await settleCleanup()
			expect(await hasImage(leftover)).toBe(true)
			expect(await hasImage(pins[0])).toBe(true)
		} finally {
			await fse.writeFile(composePath, original)
		}
		await titand.client.apps.update.mutate({appId: firstApp})
		await settleCleanup()
		expect(await hasImage(leftover)).toBe(false)
	})

	test('defers cleanup until overlapping updates finish and performs one sweep', async () => {
		const apps = titand.instance.apps
		const log = vi.spyOn(apps.logger, 'log')
		const leftover = await createLeftover('batch')
		const gates = [firstApp, sharedApp].map((appId) => `${installedDirectory(appId)}/hold-update`)
		const hook = (gate: string) => `#!/bin/sh\ntouch '${gate}.entered'\nwhile test -f '${gate}'; do sleep 0.1; done\n`
		for (const [index, appId] of [firstApp, sharedApp].entries()) {
			await fse.outputFile(gates[index], '')
			await fse.outputFile(`${installedDirectory(appId)}/hooks/pre-update`, hook(gates[index]), {mode: 0o755})
		}
		// Call the lifecycle entrypoints directly: HTTP batching delivers both
		// results together and would hide the first operation finishing here.
		const first = apps.update(firstApp)
		const second = apps.update(sharedApp)
		const results = Promise.allSettled([first, second])
		try {
			await pRetry(
				async () => {
					for (const gate of gates) expect(await fse.pathExists(`${gate}.entered`)).toBe(true)
				},
				{retries: 60, minTimeout: 100, maxTimeout: 1000},
			)
			await fse.remove(gates[0])
			await first
			expect(await hasImage(leftover)).toBe(true)
			expect(log.mock.calls.filter(([message]) => message === 'Cleaning up unused Docker images')).toHaveLength(0)
		} finally {
			await Promise.all(gates.map((gate) => fse.remove(gate)))
			await results
		}
		await second
		await settleCleanup()
		expect(await hasImage(leftover)).toBe(false)
		expect(log.mock.calls.filter(([message]) => message === 'Cleaning up unused Docker images')).toHaveLength(1)
	})

	test('starts apps before scheduling cleanup and retains a manually stopped installed app', async () => {
		await titand.client.apps.stop.mutate({appId: sharedApp})
		await titand.instance.apps.stop()
		const leftover = await createLeftover('startup')
		const schedule = vi.spyOn(titand.instance.apps.imageCleanup, 'schedule')
		await titand.instance.apps.start()
		expect(schedule).toHaveBeenCalledWith(10 * 60 * 1000)
		expect(titand.instance.apps.getApp(firstApp).state).toBe('ready')
		expect(titand.instance.apps.getApp(sharedApp).state).toBe('stopped')
		expect(await hasImage(leftover)).toBe(true)
		titand.instance.apps.imageCleanup.schedule()
		await settleCleanup()
		expect(await hasImage(leftover)).toBe(false)
		expect(await hasImage(pins[0])).toBe(true)
		expect(titand.instance.apps.getApp(sharedApp).state).toBe('stopped')
	})

	test('uninstalls through centralized cleanup without deleting another stopped app image or data', async () => {
		// Put both apps back on the same pin to exercise the old --rmi all bug.
		await setUpdateImage(firstApp, pins[0])
		await titand.client.apps.update.mutate({appId: firstApp})
		await titand.client.apps.uninstall.mutate({appId: firstApp})
		await settleCleanup()
		expect(await hasImage(pins[0])).toBe(true)
		expect(await fse.readFile(`${installedDirectory(sharedApp)}/data/persistent.txt`, 'utf8')).toBe('keep app data')
		await titand.client.apps.uninstall.mutate({appId: sharedApp})
		await settleCleanup()
		expect(await fse.pathExists(installedDirectory(sharedApp))).toBe(false)
	})
})
