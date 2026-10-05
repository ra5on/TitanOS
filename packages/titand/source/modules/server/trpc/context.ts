import {type CreateExpressContextOptions} from '@trpc/server/adapters/express'

import type Titand from '../../../index.js'
import type {AuthenticatedWebSocketRequest} from '../index.js'

export const createContextExpress = ({req, res}: CreateExpressContextOptions) => {
	const titand = req.app.get('titand') as Titand
	const logger = req.app.get('logger') as Titand['logger']
	return {
		...createContext({titand, logger}),
		transport: 'express' as const,
		request: req,
		response: res,
	}
}

export const createContextWss = ({
	titand,
	logger,
	request,
}: {
	titand: Titand
	logger: Titand['logger']
	request: AuthenticatedWebSocketRequest
}) => {
	return {
		...createContext({titand, logger}),
		transport: 'ws' as const,
		principal: request.authPrincipal,
	}
}

const createContext = ({titand, logger}: {titand: Titand; logger: Titand['logger']}) => {
	const server = titand.server
	const user = titand.user
	const appStore = titand.appStore
	const apps = titand.apps
	return {
		titand,
		server,
		user,
		appStore,
		apps,
		logger,
		dangerouslyBypassAuthentication: false,
	}
}

// Helper that flattens the resulting intersection so the IDE shows
// a single object type instead of A & B & C …
type Simplify<T> = {[K in keyof T]: T[K]}

/**
 * Merge two object types:
 * - Keys that exist in **both** A and B are **required** and their type is `A[K] | B[K]`
 * - Keys that exist in **only one** side become **optional**
 */
type Merge<A, B> = Simplify<
	// 1. keys in both → required, union of the two property types
	{[K in keyof A & keyof B]: A[K] | B[K]} & {[K in Exclude<keyof A, keyof B>]?: A[K]} & {
		// 2. keys only in A → optional // 3. keys only in B → optional
		[K in Exclude<keyof B, keyof A>]?: B[K]
	}
>

// Combined type that satisfies both the websocket and express contexts
type ContextWss = ReturnType<typeof createContextWss>
type ContextExpress = ReturnType<typeof createContextExpress>
export type Context = Merge<ContextWss, ContextExpress>
