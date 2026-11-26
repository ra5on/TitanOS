<script lang="ts">
	import { onMount } from 'svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api, type User } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		agentState,
		levels,
		capabilityLimits,
		saveLimits,
		type Level,
		type Capability,
	} from './api.ts';
	import { msg } from './labels.ts';
	import './personal.css';
	let users = $state<User[]>([]),
		caps = $state<Capability[]>([]),
		account = $state(''),
		rights = $state<Record<string, Level>>({}),
		defaults = $state<Record<string, Level>>({});
	let busy = $state(false),
		error = $state(''),
		dirty = $state(false),
		saved = $state(false),
		ready = $state(false);
	async function select(name: string) {
		account = name;
		ready = false;
		dirty = false;
		saved = false;
		error = '';
		rights = {};
		if (!name) return;
		try {
			const data = await capabilityLimits(name);
			if (account === name) {
				rights = data.rights ?? {};
				defaults = data.defaults ?? {};
				ready = true;
			}
		} catch (e) {
			if (account === name) error = apiErrorText(e);
		}
	}
	onMount(() => {
		void Promise.all([api.get<User[]>('/users'), agentState()])
			.then(async ([list, state]) => {
				users = list.filter((u) => u.role !== 'owner' || session.data?.user?.role === 'owner');
				caps = state.capabilities;
				await select(users[0]?.name ?? '');
			})
			.catch((e) => (error = apiErrorText(e)));
	});
	function choose(id: string, level: string) {
		const next = { ...rights };
		if (!level) delete next[id];
		else next[id] = level as Level;
		rights = next;
		dirty = true;
		saved = false;
	}
	async function save() {
		if (!account || !ready || busy) return;
		busy = true;
		error = '';
		try {
			await saveLimits(account, rights);
			dirty = false;
			saved = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={msg('admin')} />
<section class="pa-ui pa-admin">
	<SettingGroup id="users" title={t('pa.userLimits')} desc={msg('limitsNote')}>
		<label class="pa-field"
			>{msg('account')}<select
				value={account}
				disabled={busy || dirty}
				onchange={(e) => void select(e.currentTarget.value)}
				>{#each users as u (u.name)}<option value={u.name}>{u.name}</option>{/each}</select
			></label
		>
	</SettingGroup>
	{#if error}<p role="alert" class="pa-warning"><Icon name="info" />{error}</p>{/if}
	{#if ready}{#each [...new Set(caps.map((c) => c.app))] as group}<div class="pa-settings-card">
				<h2>{group}</h2>
				<div class="pa-rights">
					{#each caps.filter((c) => c.app === group) as c (c.id)}
						<label
							><span
								><b>{c.title}</b>{#if c.sensitive}<small
										><Icon name="lock" size={14} />{msg('sensitive')}</small
									>{/if}</span
							>
							<select
								aria-label={c.title}
								value={rights[c.id] ?? ''}
								onchange={(e) => choose(c.id, e.currentTarget.value)}
								><option value="">{msg('default')} · {msg(defaults[c.id] ?? 'never')}</option
								>{#each levels as level}<option value={level}>{msg(level)}</option>{/each}</select
							></label
						>
					{/each}
				</div>
			</div>{/each}<button class="pa-primary" disabled={!dirty || busy} onclick={save}
			>{msg('save')}</button
		>{#if dirty}<button class="pa-button" disabled={busy} onclick={() => select(account)}
				>{msg('cancel')}</button
			>{/if}{#if saved}<small role="status">{msg('saved')}</small>{/if}{/if}
</section>
