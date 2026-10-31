<script lang="ts">
	// The 3-2-1 overview of the backups overview page: worked out from the
	// backup jobs (lib/backup-321.ts), nothing is asked of the NAS for it.
	import Icon from '#lib/components/Icon.svelte';
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
	<section class="tile rule" aria-labelledby="rule321-title">
		<div class="head">
			<h2 class="tile-title" id="rule321-title">{t('rule321.title')}</h2>
			<span class="chip {all ? 'ok' : 'warn'}">{t(all ? 'rule321.met' : 'rule321.notMet')}</span>
		</div>
		<p class="muted">{t('rule321.intro')}</p>
		<ul>
			{#each rows as row (row.title)}
				<li class:missing={!row.ok}>
					<span class="mark" aria-hidden="true"
						>{#if row.ok}<Icon name="check" size={16} />{:else}!{/if}</span
					>
					<span class="text">
						<strong>{row.title}</strong>
						<span class="muted">{row.detail}</span>
					</span>
					<span class="state">{t(row.ok ? 'rule321.yes' : 'rule321.no')}</span>
				</li>
			{/each}
		</ul>
		<p class="muted small">
			{t('rule321.note')}
			<a href="/settings/backups/coverage">{t('coverage.title')}</a>.
		</p>
	</section>
{/if}

<style>
	.rule {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: calc(var(--tile-pad) + 2px);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 12px;
	}
	h2,
	p {
		margin: 0;
	}
	ul {
		display: grid;
		gap: 0;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	li {
		display: grid;
		grid-template-columns: 24px minmax(0, 1fr) auto;
		align-items: center;
		gap: 12px;
		padding: 10px 0;
		border-top: 1px solid var(--panel-border);
	}
	.mark {
		display: inline-grid;
		place-items: center;
		width: 24px;
		height: 24px;
		border-radius: 50%;
		background: color-mix(in srgb, var(--status-ok) 18%, transparent);
		color: var(--text);
		font-weight: 700;
	}
	li.missing .mark {
		background: color-mix(in srgb, var(--status-warn) 22%, transparent);
	}
	.text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	strong {
		font-weight: 500;
	}
	.state {
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.small {
		font-size: 0.8125rem;
	}
</style>
