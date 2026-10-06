import {X509Certificate} from 'node:crypto'

import {expect, beforeAll, beforeEach, afterAll, afterEach, test} from 'vitest'
import getPort from 'get-port'
import got from 'got'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'

const serviceFilePath = '/etc/avahi/services/titan.service'
type TrpcResponse<T> = {result: {data: T}}

let titand: Awaited<ReturnType<typeof createTestVm>>
let failed = false
let httpsPort: number
let discoveryId = ''
let caCertificate = ''

beforeAll(async () => {
	// titan-home registers without RAID configuration (Pro requires raidDevices at signup)
	httpsPort = await getPort({host: '127.0.0.1'})
	titand = await createTestVm({
		device: 'titan-home',
		forwardPorts: [{hostPort: httpsPort, guestPort: 443}],
	})
	await titand.vm.powerOn()
})

afterAll(async () => await titand?.cleanup())

afterEach(({task}) => {
	if (task.result?.state === 'fail') failed = true
})

beforeEach(({skip}) => {
	if (failed) skip()
})

// The following tests are stateful and must be run in order

test.sequential('advertises the device and reports it as not onboarded before signup', async () => {
	const discoveryInfo = await titand.unauthenticatedClient.system.discoveryInfo.query()
	expect(discoveryInfo.id).toMatch(/^[0-9a-f]{32}$/)
	expect(discoveryInfo.onboarded).toBe(false)
	discoveryId = discoveryInfo.id

	const serviceFile = await titand.vm.ssh(`cat ${serviceFilePath}`)
	expect(serviceFile).toContain('<type>_titan._tcp</type>')
	expect(serviceFile).toContain('<port>80</port>')
	expect(serviceFile).toContain(`id=${discoveryInfo.id}`)
	expect(serviceFile).not.toContain('<txt-record>onboarded=')
})

test.sequential('publishes the discovery service through Avahi', async () => {
	const resolvedService = await pRetry(
		async () => {
			const output = await titand.vm.ssh('avahi-browse --resolve --terminate --parsable _titan._tcp')
			const resolvedService = output
				.split('\n')
				.find((line) => line.startsWith('=;') && line.includes(';_titan._tcp;'))
			if (!resolvedService) throw new Error('Avahi did not resolve the _titan._tcp service')
			return resolvedService
		},
		{retries: 20, minTimeout: 250, maxTimeout: 250},
	)

	const fields = resolvedService.split(';')
	expect(fields[8]).toBe('80')
	expect(fields.slice(9).join(';')).toContain(`id=${discoveryId}`)
	expect(fields.slice(9).join(';')).not.toContain('device=')
})

test.sequential('bootstraps and verifies local HTTPS before signup', async () => {
	// Fetch the public bootstrap over HTTP, without a session.
	const response = await titand.unauthenticatedApi.get<TrpcResponse<{id: string; caCertificate: string}>>(
		'../trpc/system.localHttpsIdentity',
		{responseType: 'json'},
	)
	expect(response.headers['cache-control']).toBe('no-store')

	const identity = response.body.result.data
	expect(identity.id).toBe(discoveryId)
	const certificate = new X509Certificate(identity.caCertificate)
	expect(certificate.ca).toBe(true)
	caCertificate = identity.caCertificate

	// The returned CA must authenticate the live HTTPS endpoint and its discovery id.
	const discoveryResponse = await got<TrpcResponse<{id: string; onboarded: boolean}>>(
		`https://127.0.0.1:${httpsPort}/trpc/system.discoveryInfo`,
		{
			https: {certificateAuthority: caCertificate},
			responseType: 'json',
		},
	)
	const discoveryInfo = discoveryResponse.body.result.data
	expect(discoveryInfo.id).toBe(identity.id)
	expect(discoveryInfo.onboarded).toBe(false)
})

test.sequential('discovery id survives an titand restart', async () => {
	const first = await titand.unauthenticatedClient.system.discoveryInfo.query()

	await titand.vm.sshAsRoot('systemctl restart titan')
	await titand.waitForStartup()

	const second = await titand.unauthenticatedClient.system.discoveryInfo.query()
	expect(second.id).toBe(first.id)
})

test.sequential('reports onboarded and keeps HTTPS identity public after signup', async () => {
	const {id: idBeforeSignup} = await titand.unauthenticatedClient.system.discoveryInfo.query()

	await titand.signup()

	const [discoveryInfo, httpsIdentity] = await Promise.all([
		titand.unauthenticatedClient.system.discoveryInfo.query(),
		titand.unauthenticatedClient.system.localHttpsIdentity.query(),
	])
	expect(discoveryInfo.onboarded).toBe(true)
	expect(discoveryInfo.id).toBe(idBeforeSignup)
	expect(httpsIdentity).toEqual({id: discoveryInfo.id, caCertificate})
})
