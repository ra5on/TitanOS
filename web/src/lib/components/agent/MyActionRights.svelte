<script lang="ts">
	// 1.13 (Q29): what the agent may do with this account's mail, events,
	// contacts and reminders. The NAS (Einstellungen › KI-Agent › Rechte)
	// sets the most; here every account may be stricter for itself – "Wie
	// vom NAS" or a stricter value (GET/PUT /agent/rights/me, kept on the
	// NAS for the account, never in the agent's look).
	import { onMount } from 'svelte';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		ACTION_GROUPS,
		getMyRights,
		ownChoices,
		setMyRights,
		type ActionKey,
		type ActionMode,
		type ActionsView,
	} from '#lib/agent-actions.ts';

	let view = $state<ActionsView | null>(null);
	let error = $state('');
	let saving = $state(false);
	onMount(async () => {
		try {
			view = await getMyRights();
		} catch (e) {
			error = apiErrorText(e);
		}
	});
	const modeLabel = (m: string) => t(`agentpim.mode.${m}` as MessageKey);
	async function choose(key: ActionKey, value: '' | ActionMode) {
		if (!view) return;
		const own = { ...view.own };
		if (value) own[key] = value;
		else delete own[key];
		saving = true;
		error = '';
		try {
			view = await setMyRights(own);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			saving = false;
		}
	}
</script>

<p class="intro">{t('agentpim.mine.intro')}</p>
<p class="notice-warn" role="note">{t('agentpim.cloud.settings')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if !view && !error}<p class="muted" role="status">{t('common.loading')}</p>{/if}
{#if view}
	{#each ACTION_GROUPS as g (g.id)}
		<h3>{t(`agentpim.group.${g.id}` as MessageKey)}</h3>
		<div class="grp">
			{#each g.keys as key (key)}
				{@const nas = view.nas[key]}
				{@const choices = ownChoices(key, nas)}
				<div class="row">
					<span class="t"
						><strong>{t(`agentpim.action.${key}` as MessageKey)}</strong><small
							>{t('agentpim.mine.now', { mode: modeLabel(view.effective[key]) })} · {t(
								'agentpim.mine.nas',
								{ mode: modeLabel(nas) },
							)}</small
						></span
					>
					{#if choices.length > 1}
						<select
							aria-label={t(`agentpim.action.${key}` as MessageKey)}
							value={view.own[key] ?? ''}
							disabled={saving}
							onchange={(e) => void choose(key, e.currentTarget.value as '' | ActionMode)}
						>
							{#each choices as c (c)}
								<option value={c}>{c ? modeLabel(c) : t('agentpim.mine.asNas')}</option>
							{/each}
						</select>
					{/if}
				</div>
			{/each}
		</div>
	{/each}
{/if}

<style>
	.intro {
		margin: 0 0 var(--space-2);
		color: var(--muted);
	}
	h3 {
		margin: var(--space-4) 0 var(--space-1-5);
		font-size: var(--text-sm);
		color: var(--muted);
		font-weight: 600;
	}
	.grp {
		display: flex;
		flex-direction: column;
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-2-5) var(--space-3);
		min-height: 48px;
	}
	.row + .row {
		border-top: 1px solid var(--border);
	}
	.t {
		flex: 1 1 14rem;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.t small {
		color: var(--muted);
	}
	select {
		min-height: 40px;
		max-width: 100%;
	}
	.muted {
		color: var(--muted);
	}
</style>
