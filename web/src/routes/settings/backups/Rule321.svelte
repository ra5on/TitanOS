<script lang="ts">
	// The 3-2-1 overview of the backups overview page: worked out from the
	// backup jobs (lib/backup-321.ts), nothing is asked of the NAS for it.
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { rule321, type BackupJobLike } from '#lib/backup-321.ts';

	let { jobs }: { jobs: BackupJobLike[] | undefined } = $props();

	const rule = $derived(jobs ? rule321(jobs) : null);
	const all = $derived(!!rule && rule.three && rule.two && rule.one);
	const rows = $derived(
		rule
			? [
					{
						ok: rule.three,
						title: t('rule321.three'),
						detail: t('rule321.threeDetail', { count: rule.copies, backups: rule.copies - 1 }),
					},
					{
						ok: rule.two,
						title: t('rule321.two'),
						detail: t('rule321.twoDetail', {
							count: rule.media.length,
							media: rule.media.map((m) => t(`rule321.medium.${m}`)).join(', '),
						}),
					},
					{
						ok: rule.one,
						title: t('rule321.one'),
						detail: t('rule321.oneDetail', { count: rule.offsite }),
					},
				]
			: [],
	);
</script>

{#if rule}
	<!-- PLAN 78-1: a group of three rows; each says in words whether it is met. -->
	<SettingGroup id="rule321" title={t('rule321.title')} desc={t('rule321.intro')}>
		{#snippet tools()}<span class="chip {all ? 'ok' : 'warn'}"
				>{t(all ? 'rule321.met' : 'rule321.notMet')}</span
			>{/snippet}
		{#each rows as row (row.title)}
			<SettingRow icon={row.ok ? 'check' : 'info'} title={row.title} desc={row.detail}>
				{#snippet control()}<span class="chip {row.ok ? 'ok' : 'warn'}"
						>{t(row.ok ? 'rule321.yes' : 'rule321.no')}</span
					>{/snippet}
			</SettingRow>
		{/each}
		<p class="muted small">
			{t('rule321.note')}
			<a href="/settings/backups/coverage">{t('coverage.title')}</a>.
		</p>
	</SettingGroup>
{/if}

<style>
	.small {
		font-size: 0.8125rem;
	}
</style>
