import {expect, test, vi} from 'vitest'

import type Titand from '../../index.js'

import Server, {MAX_WEBSOCKET_PAYLOAD_BYTES} from './index.js'

test('mounted WebSocket servers reject oversized client messages', () => {
	const logger = {log: vi.fn(), error: vi.fn(), verbose: vi.fn()}
	const server = new Server({
		titand: {logger: {createChildLogger: () => logger}} as unknown as Titand,
	})

	server.mountWebSocketServer('/test', () => {})

	expect(server.webSocketRouter.get('/test')?.options.maxPayload).toBe(MAX_WEBSOCKET_PAYLOAD_BYTES)
})
