import {z} from 'zod'

import {privateProcedure, router} from '../server/trpc/trpc.js'

const permissions = z
	.object({
		apps: z.union([z.literal('all'), z.array(z.string())]),
		appStore: z.boolean(),
		files: z.union([z.literal('all'), z.array(z.string())]),
		manageSystem: z.boolean(),
		machines: z.union([z.literal('all'), z.array(z.string())]),
		createMachines: z.boolean(),
	})
	.strict()

const tokenMetadata = z
	.object({
		label: z.string().trim().min(1).max(80),
		agentType: z.string().trim().min(1).max(80).optional(),
	})
	.strict()

export default router({
	getSettings: privateProcedure.query(({ctx}) => ctx.titand.mcp.getSettings()),
	listTokens: privateProcedure.query(({ctx}) => ctx.titand.mcp.listTokens()),
	enable: privateProcedure
		.input(tokenMetadata.optional())
		.mutation(({ctx, input}) => (input ? ctx.titand.mcp.enable(input) : ctx.titand.mcp.enable())),
	createToken: privateProcedure.input(tokenMetadata).mutation(({ctx, input}) => ctx.titand.mcp.createToken(input)),
	revokeToken: privateProcedure
		.input(z.object({id: z.string().regex(/^[0-9a-f]{32}$/)}).strict())
		.mutation(({ctx, input}) => ctx.titand.mcp.revokeToken(input.id)),
	disable: privateProcedure.mutation(({ctx}) => ctx.titand.mcp.disable()),
	setPermissions: privateProcedure.input(permissions).mutation(({ctx, input}) => ctx.titand.mcp.setPermissions(input)),
})
