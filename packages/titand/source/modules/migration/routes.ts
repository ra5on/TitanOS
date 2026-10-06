import {router, privateProcedure, publicProcedureWhenNoUserExists} from '../server/trpc/trpc.js'

import {
	findExternalTitanInstall,
	runPreMigrationChecks,
	migrateData,
	getMigrationStatus,
	unmountExternalDrives,
} from './migration.js'
import isTitanHome from '../is-titan-home.js'

export default router({
	isTitanHome: privateProcedure.query(() => isTitanHome()),
	// TODO: Implement
	isMigratingFromTitanHome: privateProcedure.query(() => false),

	canMigrate: privateProcedure.query(async ({ctx}) => {
		const currentInstall = ctx.titand.dataDirectory
		const externalTitanInstall = await findExternalTitanInstall()
		await runPreMigrationChecks(currentInstall, externalTitanInstall as string, ctx.titand)
		await unmountExternalDrives()

		return true
	}),

	// TODO: Refactor this into a subscription
	migrationStatus: publicProcedureWhenNoUserExists.query(() => getMigrationStatus()),

	migrate: privateProcedure.mutation(async ({ctx}) => {
		const currentInstall = ctx.titand.dataDirectory
		const externalTitanInstall = await findExternalTitanInstall()
		await runPreMigrationChecks(currentInstall, externalTitanInstall as string, ctx.titand)

		void migrateData(currentInstall, externalTitanInstall as string, ctx.titand)

		return true
	}),
})
