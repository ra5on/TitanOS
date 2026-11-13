<script lang="ts">
	// PLAN 82-3: the main switch of the cameras' messages and, per camera,
	// who gets what – kinds, recipients with their ways, the pause and the
	// window – with a way to the camera's own rows. The NAS checks the
	// accounts again for every message; a chosen account that would get
	// nothing now is said here plainly.
	import { onMount } from 'svelte';
	import { api } from '../api.ts';
	import SettingGroup from '../components/settings/SettingGroup.svelte';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import { t, type MessageKey } from '../i18n/index.svelte.ts';
	import {
		kindPillsOf,
		missesNames,
		pauseText,
		recipientText,
		staleReason,
		windowText,
		type RecipientAccount,
		type RecipientsAnswer,
	} from './notify.ts';
	import type { Camera } from './types.ts';

	let {
		cameras,
		off,
		busy = false,
		onswitch,
	}: {
		/** The cameras as administrators get them (removed ones are left out here). */
		cameras: Camera[];
		/** The main switch is off (Settings.notifyOff). */
		off: boolean;
		busy?: boolean;
		onswitch: (on: boolean) => void;
	} = $props();

	let accounts = $state<RecipientAccount[] | null>(null);
	onMount(() => {
		api
			.get<RecipientsAnswer>('/nvr/recipients')
			.then((answer) => (accounts = answer.accounts))
			.catch(() => {
				// Without the accounts the overview names the chosen ones only.
			});
	});

	const shown = $derived(cameras.filter((c) => !c.deleted && c.notify));

	function lines(camera: Camera): string[] {
		const n = camera.notify!;
		const out: string[] = [];
		const kinds = [
			...kindPillsOf(n.kinds).map((pill) => t(`nvr.notify.kind.${pill}` as MessageKey)),
			...(n.faceUnknown ? [t('nvr.notify.kind.faceUnknown')] : []),
		];
		out.push(
			kinds.length
				? t('nvr.notify.overviewKinds', { kinds: kinds.join(', ') })
				: t('nvr.notify.overviewNoKinds'),
		);
		// PLAN 88: messages limited to zones name them.
		const zoneNames = (n.zones ?? [])
			.map((id) => camera.zones?.find((z) => z.id === id)?.name)
			.filter(Boolean);
		if (zoneNames.length) out.push(t('nvr.notify.overviewZones', { zones: zoneNames.join(', ') }));
		if (n.recipients == null) {
			// A camera of 0.78: its old ways to every administrator.
			const ways = [
				...(n.titan ? [t('nvr.notify.way.bell')] : []),
				...(n.push ? [t('nvr.notify.way.push')] : []),
				...(n.email ? [t('nvr.notify.overviewLegacyEmail')] : []),
			];
			out.push(
				ways.length
					? t('nvr.notify.overviewLegacy', { ways: ways.join(', ') })
					: t('nvr.notify.overviewNobody'),
			);
		} else if (n.recipients.length === 0) {
			out.push(t('nvr.notify.overviewNobody'));
		} else {
			out.push(
				t('nvr.notify.overviewRecipients', { list: n.recipients.map(recipientText).join(', ') }),
			);
			if (accounts)
				for (const entry of n.recipients) {
					const why = staleReason(entry.user, accounts, camera.id);
					const account = accounts.find((a) => a.name === entry.user);
					if (why)
						out.push(
							t('nvr.notify.overviewNothing', {
								name: entry.user,
								why: t(why === 'gone' ? 'nvr.notify.overviewGone' : 'nvr.notify.overviewNoRight'),
							}),
						);
					else if (account && missesNames(account, camera.id, n.kinds, n.faceUnknown))
						out.push(t('nvr.notify.overviewNoNames', { name: entry.user }));
				}
		}
		const also = [
			...(n.homeAssistant ? [t('nvr.notify.homeAssistant')] : []),
			...(n.agent ? [t('nvr.notify.agent')] : []),
		];
		if (also.length) out.push(t('nvr.notify.overviewAlso', { list: also.join(', ') }));
		out.push(
			`${t('nvr.notify.overviewPause', { pause: pauseText(n.pauseSeconds) })} · ${windowText(n.window)}`,
		);
		return out;
	}
</script>

<SettingGroup title={t('nvr.settings.notify')} id="nvr-notify">
	<SettingRow
		icon="bell"
		title={t('nvr.notify.main')}
		desc={off ? t('nvr.notify.mainOffHint') : t('nvr.notify.mainOnHint')}
		id="nvr-notify-main"
	>
		{#snippet control()}
			<input
				type="checkbox"
				role="switch"
				class="switch"
				aria-label={t('nvr.notify.main')}
				checked={!off}
				disabled={busy}
				onchange={(e) => onswitch(e.currentTarget.checked)}
			/>
		{/snippet}
	</SettingRow>
	{#each shown as camera (camera.id)}
		<SettingRow icon="camera" title={camera.name} id="nvr-notify-{camera.id}">
			{#snippet description()}
				{#each lines(camera) as line, i (i)}<p>{line}</p>{/each}
			{/snippet}
			{#snippet control()}
				<a
					class="btn"
					href="/nvr/cameras?edit={encodeURIComponent(camera.id)}#nvr-notify-recipients"
					aria-label={t('nvr.notify.overviewEditLabel', { camera: camera.name })}
					>{t('nvr.notify.overviewEdit')}</a
				>
			{/snippet}
		</SettingRow>
	{:else}
		<p class="muted none">{t('nvr.notify.overviewNone')}</p>
	{/each}
</SettingGroup>

<style>
	p {
		margin: 0;
	}
	.none {
		padding: var(--space-2) 0;
	}
</style>
