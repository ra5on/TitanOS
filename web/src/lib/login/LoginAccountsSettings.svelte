<script lang="ts">
	// PLAN 67: Systemsteuerung › Darstellung › Anmeldeseite › Konten. How the
	// login page offers accounts: as circles or a list; all accounts of the
	// NAS (on by default) or only those of this browser; and whether the NAS
	// also shows them to someone outside the home network (off by default).
	// The fields belong to the login look: the editor saves them with it.
	import Segments from '#lib/components/Segments.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { LoginLook } from './look.ts';

	let { look = $bindable() }: { look: LoginLook } = $props();

	const styles = $derived<{ value: LoginLook['accountsStyle']; label: string }[]>([
		{ value: 'circles', label: t('loginPick.circles') },
		{ value: 'list', label: t('loginPick.list') },
	]);
</script>

<section class="tile ui-card" aria-labelledby="h-login-accounts" data-testid="login-accounts">
	<div class="card-head"><h2 id="h-login-accounts">{t('loginPick.title')}</h2></div>
	<div class="set-row">
		<span class="set-label"
			>{t('loginPick.style')}<span class="set-hint">{t('loginPick.styleHint')}</span></span
		>
		<span class="set-control">
			<span class="previews" aria-hidden="true">
				<span class="mini circles" class:on={look.accountsStyle === 'circles'}
					><i></i><i></i><i></i></span
				>
				<span class="mini list" class:on={look.accountsStyle === 'list'}><i></i><i></i></span>
			</span>
			<Segments
				label={t('loginPick.style')}
				options={styles}
				value={look.accountsStyle}
				onchange={(v) => (look.accountsStyle = v)}
			/>
		</span>
	</div>
	<label class="set-row">
		<span class="set-label"
			>{t('loginPick.showAll')}<span class="set-hint">{t('loginPick.showAllHint')}</span></span
		>
		<span class="set-control end">
			<input type="checkbox" role="switch" class="switch" bind:checked={look.showAccounts} />
		</span>
	</label>
	<label class="set-row" class:off={!look.showAccounts}>
		<span class="set-label"
			>{t('loginPick.external')}<span class="set-hint">{t('loginPick.externalHint')}</span></span
		>
		<span class="set-control end">
			<input
				type="checkbox"
				role="switch"
				class="switch"
				disabled={!look.showAccounts}
				bind:checked={look.showAccountsExternal}
			/>
		</span>
	</label>
	<p class="card-note">{t('loginPick.note')}</p>
</section>

<style>
	.previews {
		display: inline-flex;
		gap: 6px;
	}
	.mini {
		width: 46px;
		height: 30px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 3px;
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--bg);
		opacity: 0.6;
	}
	.mini.on {
		border-color: var(--accent);
		opacity: 1;
	}
	.mini i {
		display: block;
		background: var(--muted);
	}
	.circles i {
		width: 9px;
		height: 9px;
		border-radius: 50%;
	}
	.list {
		flex-direction: column;
	}
	.list i {
		width: 30px;
		height: 6px;
		border-radius: 2px;
	}
	.off {
		opacity: 0.6;
	}
	.card-note {
		margin: 0;
		padding: 10px 16px 14px;
		border-top: 1px solid var(--panel-border, var(--border));
		color: var(--muted);
		font-size: var(--text-sm);
	}
</style>
