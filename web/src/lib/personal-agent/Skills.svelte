<script lang="ts">
	import { onMount } from 'svelte';
	import { mailApi } from '#lib/tmail/api.ts';
	import type { MailAccount } from '#lib/tmail/types.ts';
	import FileDialog from '#lib/webapps/FileDialog.svelte';
	import { activeNode } from '#lib/cluster-node.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { apiErrorText } from '#lib/i18n/index.svelte.ts';
	import {
		type Capability,
		type Source,
		type Skill,
		type Job,
		addSkill,
		deleteSkill,
		listSkills,
		runSkill,
		requestId,
	} from './api.ts';
	import { msg, sourceTitle } from './labels.ts';
	let { caps, sources, onjob }: { caps: Capability[]; sources: Source[]; onjob: (j: Job) => void } =
		$props();
	let skills = $state<Skill[]>([]),
		title = $state(''),
		steps = $state<string[]>(['']),
		trigger = $state<'manual' | 'schedule' | 'mail'>('manual'),
		runAt = $state(''),
		approval = $state<'ask' | 'inherit'>('ask');
	let error = $state(''),
		busy = $state(false),
		editor = $state(false),
		confirm = $state(''),
		selectedSources = $state<Source[]>([]);
	let sender = $state(''),
		expected = $state(''),
		pdfName = $state(''),
		prompt = $state(''),
		outputName = $state(''),
		mailbox = $state('');
	let accounts = $state<MailAccount[]>([]),
		folderPick = $state(false);
	const watch = $derived(caps.find((c) => c.id === 'mail.watch')?.effective === 'auto');
	const officeOutput = $derived(steps.find((s) => s === 'office.table' || s === 'office.document'));
	$effect(() => {
		if (!editor || trigger !== 'mail' || !watch || activeNode()) return;
		const controller = new AbortController();
		mailApi
			.accounts(controller.signal)
			.then((list) => {
				if (!controller.signal.aborted) accounts = list;
			})
			.catch((e) => {
				if (!controller.signal.aborted) error = apiErrorText(e);
			});
		return () => controller.abort();
	});
	let creation = '',
		creationPayload = '',
		runAttempt: { id: string; nonce: string } | null = null;
	const tools = $derived(
		caps.filter(
			(c) => c.effective !== 'never' && c.id !== 'speech.transcribe' && c.id !== 'mail.watch',
		),
	);
	async function refresh() {
		skills = (await listSkills()).skills ?? [];
	}
	onMount(() => {
		void refresh().catch((e) => (error = apiErrorText(e)));
	});
	async function action(run: () => Promise<unknown>) {
		if (busy) return;
		busy = true;
		error = '';
		try {
			await run();
			await refresh();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	function edit() {
		editor = true;
		title = '';
		steps = [''];
		runAt = '';
		trigger = 'manual';
		sender = '';
		expected = '';
		pdfName = '';
		prompt = '';
		outputName = '';
		mailbox = '';
		approval = 'ask';
		selectedSources = [...sources];
		creation = '';
	}
	function save() {
		return action(async () => {
			const input = {
				title: title.trim(),
				steps,
				trigger,
				runAt: trigger === 'schedule' ? new Date(runAt).toISOString() : undefined,
				sources:
					trigger === 'mail'
						? [
								...selectedSources.filter((s) => !['mail', 'mailbox', 'mail.pdf'].includes(s.kind)),
								{
									kind: 'mailbox',
									id: mailbox,
									label: accounts.find((a) => a.id === mailbox)?.name,
									ref: { account: mailbox, folder: 'INBOX' },
								},
							]
						: selectedSources,
				pdfName: trigger === 'mail' && pdfName.trim() ? pdfName.trim() : undefined,
				prompt: prompt.trim() || undefined,
				stepArgs:
					officeOutput && outputName.trim()
						? { [officeOutput]: { name: outputName.trim() } }
						: undefined,
				sender: trigger === 'mail' ? sender.trim() : undefined,
				expected: expected.trim() || undefined,
				approval,
			};
			const payload = JSON.stringify(input);
			if (creationPayload !== payload || !creation) {
				creation = requestId();
				creationPayload = payload;
			}
			await addSkill({ ...input, id: creation });
			editor = false;
			creation = '';
		});
	}
	function start(skill: Skill) {
		return action(async () => {
			if (runAttempt?.id !== skill.id) runAttempt = { id: skill.id, nonce: requestId() };
			const job = await runSkill(skill.id, {
				requestId: runAttempt.nonce,
				sources: sources.length ? sources : undefined,
			});
			runAttempt = null;
			onjob(job);
		});
	}
</script>

<div class="pa-title-row">
	<h3>{msg('skills')}</h3>
	<button class="pa-icon" aria-label={msg('newSkill')} onclick={edit}><Icon name="plus" /></button>
</div>
{#if error}<p role="alert" class="pa-warning">{error}</p>{/if}
<div class="pa-row-list">
	{#each skills as sk (sk.id)}<div class="pa-skill">
			<strong>{sk.title}</strong>{#if sk.trigger === 'mail' && sk.watchState}<small
					>{msg('watch.' + sk.watchState)}{sk.watchState === 'revoked'
						? ' · ' + msg('newRule')
						: ''}</small
				>{/if}<small
				>{sk.steps.map((id) => caps.find((c) => c.id === id)?.title ?? id).join(' → ')}</small
			>
			<div class="pa-actions">
				{#if sk.trigger !== 'mail'}<button
						class="pa-button"
						disabled={busy || sk.steps.some((id) => !tools.some((t) => t.id === id))}
						onclick={() => start(sk)}><Icon name="play" />{msg('skillStart')}</button
					>{/if}
				{#if !sk.builtIn}<button
						class="pa-icon"
						aria-label={msg('delete') + ' ' + sk.title}
						onclick={() => (confirm = sk.id)}><Icon name="trash" /></button
					>{/if}
			</div>
			{#if confirm === sk.id}<div class="pa-approval">
					<strong>{msg('confirmDelete')}</strong><button
						class="pa-button"
						onclick={() => (confirm = '')}>{msg('cancel')}</button
					><button
						class="pa-button"
						disabled={busy}
						onclick={() =>
							action(async () => {
								await deleteSkill(sk.id);
								confirm = '';
							})}>{msg('delete')}</button
					>
				</div>{/if}
		</div>{/each}
</div>
{#if editor}<form
		class="pa-formgrid"
		onsubmit={(e) => {
			e.preventDefault();
			void save();
		}}
	>
		<label>{msg('title')}<input required maxlength="96" bind:value={title} /></label>
		<label
			>{msg('trigger')}<select bind:value={trigger}>
				<option value="manual">{msg('manual')}</option><option value="schedule"
					>{msg('schedule')}</option
				>
				<option value="mail" disabled={!watch || !!activeNode()}>{msg('incomingMail')}</option>
			</select></label
		>
		{#if trigger === 'mail'}
			<label
				>{msg('mailbox')}<select bind:value={mailbox} required
					><option value="" disabled>{msg('chooseMailbox')}</option>{#each accounts as a}<option
							value={a.id}>{a.name} · {a.email}</option
						>{/each}</select
				></label
			>
			<label>{msg('from')}<input required type="email" bind:value={sender} /></label>
			<label
				>{msg('pdfName')}<input
					bind:value={pdfName}
					pattern=".*\.[Pp][Dd][Ff]"
					placeholder="Rechnung.pdf"
				/></label
			>
		{/if}
		<label
			>{msg('skillPrompt')}<textarea bind:value={prompt} maxlength="16384" rows="2"
			></textarea></label
		>
		<label>{msg('expected')}<input bind:value={expected} maxlength="256" /></label>
		{#if trigger === 'schedule'}<label
				>{msg('runAt')}<input required type="datetime-local" bind:value={runAt} /></label
			>{/if}
		{#each steps as step, index}<label
				>{msg(index === steps.length - 1 ? 'output' : 'steps')} · {index + 1}<select
					required
					value={step}
					onchange={(e) => (steps[index] = e.currentTarget.value)}
					><option value="" disabled>{msg('tool')}</option>{#each tools as tool (tool.id)}<option
							value={tool.id}>{tool.title}</option
						>{/each}</select
				></label
			>{/each}
		{#if steps.length < 8}<button
				type="button"
				class="pa-button"
				onclick={() => (steps = [...steps, ''])}><Icon name="plus" />{msg('addStep')}</button
			>{/if}
		{#if officeOutput}
			<label
				>{msg(trigger === 'mail' ? 'mailOutputName' : 'outputName')}<input
					required
					bind:value={outputName}
					placeholder={officeOutput === 'office.table' ? 'Entwurf.xlsx' : 'Entwurf.docx'}
				/>{#if trigger === 'mail'}<small>{msg('mailOutputSuffix')}</small>{/if}</label
			>
			<button
				type="button"
				class="pa-button"
				disabled={!!activeNode()}
				onclick={() => (folderPick = true)}><Icon name="folder" />{msg('outputFolder')}</button
			>
		{/if}
		{#if selectedSources.length}<div class="pa-chips">
				{#each selectedSources as src, i}<button
						type="button"
						onclick={() => (selectedSources = selectedSources.filter((_, n) => i !== n))}
						aria-label={msg('remove') + ' ' + sourceTitle(src)}
						>{sourceTitle(src)}<Icon name="close" size={14} /></button
					>{/each}
			</div>{/if}
		<label
			>{msg('approval')}<select bind:value={approval}
				><option value="ask">{msg('alwaysAsk')}</option><option value="inherit"
					>{msg('inherit')}</option
				></select
			></label
		>
		<div class="pa-actions">
			<button type="button" class="pa-button" onclick={() => (editor = false)}
				>{msg('cancel')}</button
			><button
				class="pa-primary"
				disabled={busy ||
					!title.trim() ||
					steps.some((s) => !s) ||
					(!!officeOutput && !selectedSources.some((s) => s.kind === 'folder')) ||
					(trigger === 'mail' && (!watch || !mailbox || !steps[0]?.startsWith('mail.')))}
				>{msg('save')}</button
			>
		</div>
	</form>{/if}

{#if folderPick}<FileDialog
		mode="folder"
		title={msg('outputFolder')}
		onclose={() => (folderPick = false)}
		onpick={(f) => {
			selectedSources = [
				...selectedSources.filter((s) => s.kind !== 'folder'),
				{ kind: 'folder', id: f.path || '.', label: f.path || f.root, ref: { root: f.root } },
			];
			folderPick = false;
		}}
	/>{/if}
