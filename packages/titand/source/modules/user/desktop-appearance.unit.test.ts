import {describe, expect, test, vi} from 'vitest'

import type {Context} from '../server/trpc/context.js'
import routes from './routes.js'

function accountCaller(accountId: string) {
	const profile: Record<string, unknown> = {name: accountId, language: 'de'}
	const setAccountDesktopTransparency = vi.fn(async (_id: string, value: number) => {
		profile.desktopTransparency = value
		return true
	})
	const context = {
		transport: 'ws',
		principal: {sessionId: 'session', accountId, actor: 'account'},
		logger: {verbose: vi.fn(), error: vi.fn()},
		titand: {auth: {validatePrincipal: vi.fn(async () => {})}, files: {samba: {getShareUsername: () => accountId}}},
		user: {get: async () => profile, getMember: async () => profile, setAccountDesktopTransparency},
	} as unknown as Context
	return {caller: routes.createCaller(context), setAccountDesktopTransparency, context}
}

describe('account desktop appearance', () => {
	test.each(['0', 'Alice'])('allows %s to save the full slider range on their own profile', async (accountId) => {
		const {caller, setAccountDesktopTransparency} = accountCaller(accountId)
		expect((await caller.get()).desktopTransparency).toBe(75)
		for (const value of [0, 100, 75]) {
			await expect(caller.set({desktopTransparency: value})).resolves.toBe(true)
			expect(setAccountDesktopTransparency).toHaveBeenLastCalledWith(accountId, value)
			expect((await caller.get()).desktopTransparency).toBe(value)
		}
	})

	test.each([-1, 101, 12.5, NaN, Infinity])('rejects invalid preference %s', async (desktopTransparency) => {
		const {caller, setAccountDesktopTransparency} = accountCaller('Alice')
		await expect(caller.set({desktopTransparency})).rejects.toMatchObject({code: 'BAD_REQUEST'})
		expect(setAccountDesktopTransparency).not.toHaveBeenCalled()
	})

	test('rejects selecting another account and invalid sessions', async () => {
		const {caller, context, setAccountDesktopTransparency} = accountCaller('Alice')
		await expect(caller.set({desktopTransparency: 50, userId: '0'} as never)).rejects.toMatchObject({
			code: 'BAD_REQUEST',
		})
		vi.mocked(context.titand.auth.validatePrincipal).mockRejectedValueOnce(new Error('Session revoked'))
		await expect(caller.set({desktopTransparency: 50})).rejects.toMatchObject({code: 'UNAUTHORIZED'})
		expect(setAccountDesktopTransparency).not.toHaveBeenCalled()
	})
})
