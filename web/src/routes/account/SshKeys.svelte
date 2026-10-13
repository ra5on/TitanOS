<script lang="ts">
	// The public keys this account may log in with over SSH.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Key = { id: string; type: string; fingerprint: string; comment: string };
	type Answer = { keys: Key[]; mayLogin: boolean; sshEnabled: boolean };

	let state_ = $state<Answer | null>(null);
	let adding = $state(false);
	let line = $state('');
	let error = $state('');
	let busy = $state(false);

	const problem = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	$effect(() => {
		api.get<Answer>('/account/ssh-keys').then((res) => (state_ = res), (err) => (error = problem(err)));
	});

	async function add(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			state_ = await api.post<Answer>('/account/ssh-keys', { key: line });
			line = '';
			adding = false;
		} catch (err) {
			error = problem(err);
		}
		busy = false;
	}

	async function remove(key: Key) {
		error = '';
		try {
			state_ = await api.delete<Answer>(`/account/ssh-keys/${key.id}`);
		} catch (err) {
			error = problem(err);
		}
	}

	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
</script>

{#if state_}
	<section class="tile" aria-labelledby="acc-ssh">
		<h2 class="tile-title" id="acc-ssh">{t('ssh.keys')}</h2>
		{#if !state_.mayLogin}
			<p class="muted">{t('ssh.onlyAdmins')}</p>
		{:else if !state_.sshEnabled}
			<p class="muted">{t('ssh.offHint')} <a href="/settings/access">{t('ssh.settingsLink')}</a></p>
		{:else}
			<p class="muted">{t('ssh.onHint')} <span class="mono">ssh {session.data?.user?.name}@{host}</span></p>
		{/if}
		{#each state_.keys as key (key.id)}
			<div class="key">
				<div>
					<span class="name">{key.comment || key.type}</span>
					<span class="muted mono">{key.type} · {key.fingerprint}</span>
				</div>
				<button type="button" class="btn" onclick={() => remove(key)}>{t('ssh.remove')}</button>
			</div>
		{:else}
			<p class="muted">{t('ssh.noKeys')}</p>
		{/each}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		{#if adding}
			<form onsubmit={add}>
				<label><span class="muted">{t('ssh.keyLabel')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<textarea class="field mono" bind:value={line} rows="4" placeholder="ssh-ed25519 AAAA… name@host" spellcheck="false" autocapitalize="none" required autofocus></textarea>
				</label>
				<p class="muted small">{t('ssh.keyHint')}</p>
				<div class="actions">
					<button type="button" class="btn" onclick={() => ((adding = false), (error = ''))} disabled={busy}>{t('common.cancel')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy}>{t('ssh.add')}</button>
				</div>
			</form>
		{:else}
			<div><button type="button" class="btn" onclick={() => (adding = true)}>{t('ssh.addKey')}</button></div>
		{/if}
	</section>
{/if}

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	.key {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px 16px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.key div {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.key .mono {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.name {
		font-weight: 600;
	}
	form,
	label {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	textarea {
		height: auto;
		padding: 10px 12px;
		font-size: 0.75rem;
		resize: vertical;
	}
	.small {
		font-size: 0.75rem;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
</style>
