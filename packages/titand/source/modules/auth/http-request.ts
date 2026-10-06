import type express from 'express'

import type Titand from '../../index.js'
import type {HttpApiScope} from './auth.js'
import {browserSessionTokenFromRequest} from './browser-session-cookie.js'

function bearerToken(request: express.Request) {
	const [scheme, token] = request.get('Authorization')?.split(' ') ?? []
	return scheme?.toLowerCase() === 'bearer' && token ? token : undefined
}

export async function authorizePhotoBackupRequest(titand: Titand, request: express.Request) {
	const token = bearerToken(request)
	if (!token) throw new Error('Missing photo backup credential')
	return titand.auth.authenticatePhotoBackupGrant(token)
}

export async function authorizeDashboardRequest(titand: Titand, request: express.Request) {
	const token = bearerToken(request)
	if (!token) throw new Error('Missing dashboard credential')
	return titand.auth.authenticateDashboardCredentials(token, browserSessionTokenFromRequest(request))
}

export async function authorizeHttpRequest(
	titand: Titand,
	request: express.Request,
	scope: HttpApiScope,
	resource?: string,
) {
	const dashboardToken = bearerToken(request)
	if (dashboardToken) {
		const principal = await titand.auth.authenticate(dashboardToken, 'dashboard')
		if (principal.actor !== 'system') throw new Error('Dashboard credentials cannot authorize HTTP API URLs')
		return titand.auth.authorizeHttpApi(principal, scope, resource)
	}

	const browserSessionToken = browserSessionTokenFromRequest(request)
	const urlToken = request.query.token
	if (typeof browserSessionToken !== 'string' || typeof urlToken !== 'string') {
		throw new Error('Missing HTTP API credentials')
	}
	return titand.auth.authorizeHttpApiCredentials(browserSessionToken, urlToken, scope, resource)
}
