<script lang="ts" module>
	import { session } from '#lib/session.svelte.ts';

	/** Whether the fields hold what the NAS asks for: the password and, with two-factor login on, a code. */
	export function reconfirmComplete(password: string, code: string): boolean {
		return !!password && (!session.data?.user?.twoFactor || !!code.trim());
	}
</script>

<script lang="ts">
	// Password and, with two-factor login on, a code: the proof the NAS asks for
	// before a change that opens another way into the account (API tokens, SSH
	// keys). Same fields and names as switching off two-factor login.
	import { t } from '#lib/i18n/index.svelte.ts';

	let { password = $bindable(''), code = $bindable('') }: { password?: string; code?: string } =
		$props();
	const twoFactor = $derived(!!session.data?.user?.twoFactor);
</script>

<p class="muted reconfirm-hint">{t(twoFactor ? 'reconfirm.hintCode' : 'reconfirm.hint')}</p>
<label class="reconfirm-field"
	><span>{t('auth.password')}</span><input
		class="field"
		type="password"
		bind:value={password}
		autocomplete="current-password"
		required
	/></label
>
{#if twoFactor}<label class="reconfirm-field"
		><span>{t('auth.code')}</span><input
			class="field mono"
			type="text"
			bind:value={code}
			inputmode="numeric"
			autocomplete="one-time-code"
			spellcheck="false"
			required
		/></label
	>{/if}

<style>
	.reconfirm-hint {
		margin: 0;
	}
	.reconfirm-field {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
</style>
