import os from 'node:os'
import {setTimeout} from 'node:timers/promises'

import {TRPCError} from '@trpc/server'
import {z} from 'zod'
import {$} from 'execa'
import fse from 'fs-extra'
import stripAnsi from 'strip-ansi'

import {performReset} from './factory-reset.js'
import {OWNER_USER_ID} from '../user/constants.js'
import type Titand from '../../index.js'
import {getUpdateStatus, performUpdate, getLatestRelease, getRecoveryStatus, performRollback} from './update.js'
import {
	getCpuTemperature,
	getSystemDiskUsage,
	getDiskUsage,
	getMemoryUsage,
	getCpuUsage,
	getGpuUsage,
	reboot,
	shutdown,
	detectDevice,
	getSystemMemoryUsage,
	getIpAddresses,
	getNetworkInterfaces,
	getHostname,
	setHostname,
	setStaticIp,
	confirmStaticIp,
	clearStaticIp,
	syncDns,
} from './system.js'
import {getTailscaleBrowserHostname} from './tailscale.js'

import {
	privateProcedure,
	publicProcedure,
	publicProcedureWhenNoUserExists,
	router,
	privateProcedureWithMembers,
} from '../server/trpc/trpc.js'
import {runAfterResponse} from '../server/run-after-response.js'

type SystemStatus = 'running' | 'updating' | 'shutting-down' | 'restarting' | 'migrating' | 'resetting' | 'restoring'
let systemStatus: SystemStatus = 'running'

// Quick hack so we can set system status from migration module until we refactor this
export function setSystemStatus(status: SystemStatus) {
	systemStatus = status
}

function assertSystemIdle() {
	if (systemStatus !== 'running' || getUpdateStatus().running) {
		throw new TRPCError({code: 'CONFLICT', message: 'Eine Systemaktion läuft bereits. Bitte warten.'})
	}
}

function startPowerAction(
	titand: Titand,
	response: Parameters<typeof runAfterResponse>[0],
	status: Extract<SystemStatus, 'restarting' | 'shutting-down'>,
	action: () => Promise<unknown>,
) {
	assertSystemIdle()
	systemStatus = status
	runAfterResponse(response, () => {
		void (async () => {
			try {
				await titand.stop()
				await action()
			} catch (error) {
				systemStatus = 'running'
				titand.logger.error(`Failed while ${status}`, error)
			}
		})()
	})
}

async function getSystemLogs(type: 'titanos' | 'system', lines = 1500, maxOutputBytes?: number) {
	if (!maxOutputBytes) {
		const process =
			type === 'titanos'
				? await $`journalctl --unit titan --unit titand-production --unit titand --unit ui --lines ${lines}`
				: await $`journalctl --lines ${lines}`
		return stripAnsi(process.stdout)
	}

	const journal =
		type === 'titanos'
			? $({buffer: false})`journalctl --unit titan --unit titand-production --unit titand --unit ui --lines ${lines}`
			: $({buffer: false})`journalctl --lines ${lines}`
	// titand has no inherited stdin under systemd, so execa's default stdin is
	// unavailable. The second process needs an explicit pipe for journalctl.
	const tail = $({stdin: 'pipe'})`tail --bytes ${maxOutputBytes}`
	journal.pipeStdout!(tail)
	const [, process] = await Promise.all([journal, tail])
	return stripAnsi(process.stdout)
}

// Fold device-wide usage a member can't inspect into a single 'other' entry.
// Machines are owner-only, while installed apps remain visible only when the
// owner shared them with this member.
async function scopeUsageAppsForMember<
	T extends {apps: {id: string; used: number}[]; machines: {id: string; used: number}[]},
>(titand: Titand, usage: T, userId: string): Promise<T> {
	if (userId === OWNER_USER_ID) return usage
	const sharedAppIds = await titand.apps.sharedAppIdsForUser(userId)
	const visibleApps = usage.apps.filter((app) => sharedAppIds.includes(app.id))
	const hiddenApps = usage.apps.filter((app) => !sharedAppIds.includes(app.id))
	const otherUsed = [...hiddenApps, ...usage.machines].reduce((total, item) => total + item.used, 0)
	const apps = otherUsed > 0 ? [...visibleApps, {id: 'other', used: otherUsed}] : visibleApps
	return {...usage, apps, machines: []}
}

async function scopeGpuUsageAppsForMember(
	titand: Titand,
	usage: Awaited<ReturnType<typeof getGpuUsage>>,
	userId: string,
): Promise<Awaited<ReturnType<typeof getGpuUsage>>> {
	if (userId === OWNER_USER_ID) return usage
	const sharedAppIds = await titand.apps.sharedAppIdsForUser(userId)
	const visibleApps = usage.apps.filter((app) => sharedAppIds.includes(app.id))
	const hiddenApps = usage.apps.filter((app) => !sharedAppIds.includes(app.id))
	const other = hiddenApps.reduce(
		(total, app) => ({id: 'other', used: total.used + app.used, memoryUsed: total.memoryUsed + app.memoryUsed}),
		{id: 'other', used: 0, memoryUsed: 0},
	)
	return {
		...usage,
		apps: other.used > 0 || other.memoryUsed > 0 ? [...visibleApps, other] : visibleApps,
	}
}

export default router({
	online: publicProcedure.query(() => true),
	version: publicProcedure.query(async ({ctx}) => {
		return {
			version: ctx.titand.version,
			name: ctx.titand.versionName,
			previousVersion: await ctx.titand.store.get('previousVersion'),
		}
	}),
	status: publicProcedure.query(() => systemStatus),
	// Public so discovery clients can identify a device before they can authenticate with it
	discoveryInfo: publicProcedure.query(async ({ctx}) => {
		const [id, {device}, onboarded] = await Promise.all([
			ctx.titand.systemNg.device.getDiscoveryId(),
			detectDevice(),
			ctx.user.exists(),
		])
		return {id, device, version: ctx.titand.version, onboarded}
	}),
	// Public for first-use trust: native clients validate HTTPS with this CA before
	// persisting it or sending credentials. The CA private key stays in LAN ingress.
	localHttpsIdentity: publicProcedure.query(async ({ctx}) => {
		if (!ctx.response) {
			throw new TRPCError({code: 'METHOD_NOT_SUPPORTED', message: 'HTTP transport required'})
		}
		ctx.response.set('Cache-Control', 'no-store')
		const [id, caCertificate] = await Promise.all([
			ctx.titand.systemNg.device.getDiscoveryId(),
			ctx.titand.lanIngress.getCaCertificate(),
		])
		return {id, caCertificate}
	}),
	updateStatus: privateProcedure.query(() => getUpdateStatus()),
	uptime: privateProcedureWithMembers.query(() => os.uptime()),
	checkUpdate: privateProcedure.query(async ({ctx}) => {
		let {version, name, releaseNotes} = await getLatestRelease(ctx.titand)
		// v prefix is needed in the tag name for legacy reasons, remove it before comparing to local version
		const available = version.replace('v', '') !== ctx.titand.version
		return {available, version, name, releaseNotes}
	}),
	getReleaseChannel: privateProcedure.query(async ({ctx}) => {
		return 'stable' as const
	}),
	setReleaseChannel: privateProcedure
		.input(
			z.object({
				channel: z.literal('stable'),
			}),
		)
		.mutation(async ({ctx, input}) => {
			return ctx.titand.store.set('settings.releaseChannel', input.channel)
		}),
	isExternalDns: privateProcedure.query(async ({ctx}) => {
		return await ctx.titand.store.get('settings.externalDns', true)
	}),
	setExternalDns: privateProcedure.input(z.boolean()).mutation(async ({ctx, input}) => {
		const previousExternalDns = await ctx.titand.store.get('settings.externalDns', true)
		if (previousExternalDns === input) return true
		await ctx.titand.store.set('settings.externalDns', input)
		try {
			const success = await syncDns()
			if (!success) throw new Error('Failed to synchronize external DNS setting')
			return true
		} catch (error) {
			await ctx.titand.store.set('settings.externalDns', previousExternalDns)
			throw error
		}
	}),
	update: privateProcedure.mutation(async ({ctx}) => {
		assertSystemIdle()
		systemStatus = 'updating'
		let success = false
		try {
			success = await performUpdate(ctx.titand)
			if (success) {
				// The HTTP acknowledgement must leave the server before stop()
				// closes its own listener. Otherwise this very request can deadlock.
				runAfterResponse(ctx.response, () => {
					void (async () => {
						try {
							await setTimeout(1000)
							await ctx.titand.stop()
							await reboot()
						} catch (error) {
							systemStatus = 'running'
							ctx.titand.logger.error('Failed to reboot after installing the system update', error)
						}
					})()
				})
			}
		} finally {
			if (!success) systemStatus = 'running'
		}
		return success
	}),
	recoveryStatus: privateProcedure.query(({ctx}) => getRecoveryStatus(ctx.titand)),
	rollback: privateProcedure
		.input(z.object({selection: z.string().regex(/^[a-f0-9]{64}$/)}))
		.mutation(async ({ctx, input}) => {
			assertSystemIdle()
			// Reserve before the asynchronous check, so power and update actions
			// cannot race this selection. The helper rechecks under its OS lock.
			systemStatus = 'restarting'
			try {
				const state = await getRecoveryStatus(ctx.titand)
				if (!state.previous.some((item) => item.selection === input.selection)) {
					throw new TRPCError({
						code: 'CONFLICT',
						message: 'Dieser Systemstand ist nicht mehr verfügbar. Bitte neu laden.',
					})
				}
				runAfterResponse(ctx.response, () => {
					void performRollback(ctx.titand, input.selection)
						.then((success) => {
							if (!success) systemStatus = 'running'
						})
						.catch((error) => {
							systemStatus = 'running'
							ctx.titand.logger.error('TitanOS rollback failed', error)
						})
				})
				return true
			} catch (error) {
				systemStatus = 'running'
				throw error
			}
		}),
	hiddenService: privateProcedure.query(async ({ctx}) => {
		try {
			return await fse.readFile(`${ctx.titand.dataDirectory}/tor/data/web/hostname`, 'utf-8')
		} catch (error) {
			ctx.titand.logger.error(`Failed to read hidden service for ui`, error)
			return ''
		}
	}),
	// Public during onboarding to show device-specific UI (Pro/Home images, video background)
	device: publicProcedureWhenNoUserExists.query(() => detectDevice()),
	// Read-only device metadata shown in every account's settings summary.
	deviceName: privateProcedureWithMembers.query(async () => (await detectDevice()).device),
	// Usage stats are visible to members so they get the normal settings and
	// live usage experience. Device-wide breakdowns compute the full result as
	// normal and are then post-processed to fold owner-only resources and apps
	// the member can't see into a single 'other' entry.
	cpuTemperature: privateProcedureWithMembers.query(() => getCpuTemperature()),
	systemDiskUsage: privateProcedureWithMembers.query(({ctx}) => getSystemDiskUsage(ctx.titand)),
	diskUsage: privateProcedureWithMembers.query(async ({ctx}) =>
		scopeUsageAppsForMember(ctx.titand, await getDiskUsage(ctx.titand), ctx.principal?.accountId ?? OWNER_USER_ID),
	),
	// Total physical memory. Public during onboarding so the SSD acceleration step can
	// recommend a drive size (~32x RAM) before a user exists.
	memorySize: publicProcedureWhenNoUserExists.query(async () => (await getSystemMemoryUsage()).size),
	systemMemoryUsage: privateProcedureWithMembers.query(({ctx}) => getSystemMemoryUsage()),
	memoryUsage: privateProcedureWithMembers.query(async ({ctx}) =>
		scopeUsageAppsForMember(ctx.titand, await getMemoryUsage(ctx.titand), ctx.principal?.accountId ?? OWNER_USER_ID),
	),
	cpuUsage: privateProcedureWithMembers.query(async ({ctx}) =>
		scopeUsageAppsForMember(ctx.titand, await getCpuUsage(ctx.titand), ctx.principal?.accountId ?? OWNER_USER_ID),
	),
	gpuUsage: privateProcedureWithMembers.query(async ({ctx}) =>
		scopeGpuUsageAppsForMember(ctx.titand, await getGpuUsage(ctx.titand), ctx.principal?.accountId ?? OWNER_USER_ID),
	),
	getIpAddresses: privateProcedureWithMembers.query(() => getIpAddresses()),
	// Optional browser metadata. Native clients keep literal, identity-verified IPs for
	// API and background traffic; the Tailscale hostname is only a friendlier browser destination.
	getTailscaleBrowserHostname: privateProcedureWithMembers.query(({ctx}) => getTailscaleBrowserHostname(ctx.titand)),
	getHostname: privateProcedure.query(() => getHostname()),
	getNetworkInterfaces: privateProcedure.query(({ctx}) => getNetworkInterfaces(ctx.titand)),
	setHostname: privateProcedure
		.input(
			z.object({
				hostname: z
					.string()
					.trim()
					.toLowerCase()
					.regex(/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/, 'Invalid hostname'),
			}),
		)
		.mutation(async ({ctx, input}) => setHostname(ctx.titand, input.hostname)),
	setStaticIp: privateProcedure
		.input(
			z.object({
				mac: z.string().regex(/^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i, 'Invalid MAC address'),
				ip: z.string().ip({version: 'v4', message: 'Invalid IPv4 address'}),
				subnetPrefix: z.number().int().min(0).max(32),
				gateway: z.string().ip({version: 'v4', message: 'Invalid IPv4 gateway'}),
				dns: z.array(z.string().ip({version: 'v4', message: 'Invalid IPv4 DNS address'})).min(1),
			}),
		)
		.mutation(async ({ctx, input}) => setStaticIp(ctx.titand, input)),
	// Public so it can be called from a new origin after an IP change, where no browser credential is available.
	confirmStaticIp: publicProcedure
		.input(
			z.object({
				ip: z.string().ip({version: 'v4', message: 'Invalid IPv4 address'}),
			}),
		)
		.mutation(async ({input}) => confirmStaticIp(input.ip)),
	clearStaticIp: privateProcedure
		.input(
			z.object({
				mac: z.string().regex(/^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i, 'Invalid MAC address'),
			}),
		)
		.mutation(async ({ctx, input}) => clearStaticIp(ctx.titand, input)),
	// Public during onboarding and recovery mode so users can shut down during RAID setup or mount failure
	shutdown: publicProcedureWhenNoUserExists.mutation(({ctx}) => {
		startPowerAction(ctx.titand, ctx.response, 'shutting-down', shutdown)
		return true
	}),
	// Public during onboarding and recovery mode
	restart: publicProcedureWhenNoUserExists.mutation(({ctx}) => {
		startPowerAction(ctx.titand, ctx.response, 'restarting', reboot)
		return true
	}),
	logs: privateProcedure
		.input(
			z.object({
				type: z.enum(['titanos', 'system']),
				lines: z.number().int().min(1).max(1500).default(1500),
				maxOutputBytes: z.number().int().positive().max(1_000_000).optional(),
			}),
		)
		.query(async ({input}) => getSystemLogs(input.type, input.lines, input.maxOutputBytes)),
	//
	// Public during onboarding and recovery mode - password required unless in recovery mode
	factoryReset: publicProcedureWhenNoUserExists
		.input(
			z.object({
				password: z.string().optional(),
			}),
		)
		.mutation(async ({ctx, input}) => {
			// Skip password validation in recovery mode (RAID mount failure) since user data is inaccessible
			const raidMountFailure = await ctx.titand.hardware.raid.checkRaidMountFailure()
			if (!raidMountFailure) {
				if (!input.password || !(await ctx.user.validatePassword(input.password))) {
					throw new TRPCError({code: 'UNAUTHORIZED', message: 'Invalid password'})
				}
			}
			assertSystemIdle()
			systemStatus = 'resetting'
			try {
				// Wait for UI to poll status (polls every 10s) and see we're resetting
				await setTimeout(11000)
				// Triggers an immediate reboot via Rugix or the RAID-safe reset path
				await performReset(ctx.titand)
			} catch (error) {
				systemStatus = 'running'
				throw error
			}
		}),
})
