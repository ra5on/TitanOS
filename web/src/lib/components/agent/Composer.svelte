<script lang="ts" module>
	import type { IconName } from '#lib/components/Icon.svelte';
	/** A "/" command of the input (PLAN 90-4); `arg`: words follow it. */
	export type Command = { name: string; label: string; icon: IconName; arg?: boolean };
</script>

<script lang="ts">
	// The input of the KI-Agent (PLAN 48, card of PLAN 68): the clip, the
	// task, the microphone and the send button in the accent. Enter sends,
	// Shift+Enter a new line; while a run works the round button stops it.
	// Model and thinking level sit at the top of the chat (ModelPicker).
	// PLAN 90-4: "/" at the start opens the commands (arrow keys, Enter or a
	// tap choose one); the field grows with its text up to 220 px.
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { apiFetch, responseError } from '#lib/api.ts';
	import { toWav16k } from '#lib/speech-audio.ts';

	let {
		micHold = false,
		busy = false,
		disabled = false,
		placeholder = '',
		attachments = $bindable([]),
		onsend,
		onstop,
		onpicknas,
		onerror,
		voice = false,
		voiceNas = false,
		commands = [],
		oncommand,
	}: {
		/** The "/" commands on offer and what runs them (name, words after it). */
		commands?: Command[];
		oncommand?: (name: string, arg: string) => void;
		/** PLAN 53: record while the button is held instead of tap on / tap off. */
		micHold?: boolean;
		busy?: boolean;
		disabled?: boolean;
		placeholder?: string;
		/** Files to send with the message: uploads or files of the NAS. */
		attachments?: { name: string; file?: File; ref?: string }[];
		onsend: (text: string) => void;
		onstop?: () => void;
		/** Opens the choice of files on the NAS (shares of the account). */
		onpicknas?: () => void;
		/** A file could not be taken, e.g. too large. */
		onerror?: (message: string) => void;
		/** Speech input through Whisper is switched on (PLAN 49). */
		voice?: boolean;
		/** Speech input goes to the speech services of the NAS (PLAN 92). */
		voiceNas?: boolean;
	} = $props();

	// Speech input: record in the browser, the NAS passes it to Whisper and
	// the text lands in the field (not sent on its own).
	let recording = $state(false);
	let transcribing = $state(false);
	let recorder: MediaRecorder | null = null;
	let chunks: Blob[] = [];
	async function toggleVoice() {
		if (recording) {
			recorder?.stop();
			return;
		}
		try {
			const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
			const type = ['audio/webm', 'audio/mp4', 'audio/ogg'].find(
				(t) => typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported?.(t),
			);
			recorder = new MediaRecorder(stream, type ? { mimeType: type } : undefined);
			chunks = [];
			recorder.ondataavailable = (e) => e.data.size && chunks.push(e.data);
			recorder.onstop = async () => {
				stream.getTracks().forEach((t) => t.stop());
				recording = false;
				const blob = new Blob(chunks, { type: recorder?.mimeType || type || 'audio/webm' });
				if (!blob.size) return;
				transcribing = true;
				try {
					// PLAN 92: the NAS's own speech to text takes a WAV file; the
					// Whisper service set up by address takes the recording.
					const res = voiceNas
						? await apiFetch(`/agent/speech/hear?lang=${i18n.locale}`, {
								method: 'POST',
								headers: { 'Content-Type': 'audio/wav' },
								body: await toWav16k(blob),
							})
						: await apiFetch(`/agent/transcribe?lang=${i18n.locale}`, {
								method: 'POST',
								headers: { 'Content-Type': blob.type },
								body: blob,
							});
					if (!res.ok) throw await responseError(res);
					const { text: said } = (await res.json()) as { text: string };
					if (said) fill(text.trim() ? `${text.trim()} ${said}` : said);
				} catch (e) {
					onerror?.(apiErrorText(e));
				} finally {
					transcribing = false;
				}
			};
			recorder.start();
			recording = true;
		} catch {
			onerror?.(t('agent.voiceNoMic'));
		}
	}

	let text = $state('');
	let area = $state<HTMLTextAreaElement>();

	function grow() {
		if (!area) return;
		area.style.height = 'auto';
		area.style.height = Math.min(area.scrollHeight, 220) + 'px';
	}
	let picker = $state<HTMLInputElement>();
	let photoPicker = $state<HTMLInputElement>();
	function picked(event: Event) {
		const files = (event.currentTarget as HTMLInputElement).files;
		const big: string[] = [];
		for (const file of files ?? []) {
			if (file.size > 20 * 1024 * 1024) big.push(file.name);
			else if (!attachments.some((a) => a.name === file.name))
				attachments = [...attachments, { name: file.name, file }];
		}
		// Larger files are chosen from the NAS instead (nothing is copied).
		if (big.length) onerror?.(t('agent.attachTooBig', { names: big.join(', ') }));
		(event.currentTarget as HTMLInputElement).value = '';
	}
	// ----- "/" commands -----
	const typed = $derived(oncommand ? /^\/(\S*)$/.exec(text)?.[1]?.toLowerCase() : undefined);
	let dismissed = $state('');
	const offered = $derived(
		typed === undefined || dismissed === text
			? []
			: commands.filter((c) => c.name.startsWith(typed)),
	);
	let pickedCmd = $state(0);
	$effect(() => {
		void offered.length;
		pickedCmd = 0;
	});
	function choose(c: Command) {
		if (c.arg) fill(`/${c.name} `);
		else {
			text = '';
			queueMicrotask(grow);
			oncommand?.(c.name, '');
		}
	}
	function send() {
		const value = text.trim() || (attachments.length ? t('agent.attachDefault') : '');
		const cmd = oncommand ? /^\/(\S+)(?:\s+([\s\S]*))?$/.exec(text.trim()) : null;
		const known = cmd && commands.find((c) => c.name === cmd[1].toLowerCase());
		if (known) {
			text = '';
			queueMicrotask(grow);
			oncommand?.(known.name, (cmd[2] ?? '').trim());
			return;
		}
		if (!value || busy || disabled) return;
		onsend(value);
		text = '';
		queueMicrotask(grow);
	}
	function key(event: KeyboardEvent) {
		if (offered.length) {
			if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
				event.preventDefault();
				pickedCmd =
					(pickedCmd + (event.key === 'ArrowDown' ? 1 : -1) + offered.length) % offered.length;
				return;
			}
			if ((event.key === 'Enter' && !event.isComposing) || event.key === 'Tab') {
				event.preventDefault();
				choose(offered[pickedCmd]);
				return;
			}
			if (event.key === 'Escape') {
				event.stopPropagation();
				dismissed = text;
				return;
			}
		}
		if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
			event.preventDefault();
			send();
		}
	}
	/** Puts a text into the field (suggestions). */
	export function fill(value: string) {
		text = value;
		queueMicrotask(() => {
			grow();
			area?.focus();
		});
	}
</script>

{#if offered.length}
	<div class="cmds" role="listbox" id="agent-cmds" aria-label={t('agent.cmd.title')}>
		<div class="cmds-head" aria-hidden="true">{t('agent.cmd.title')}</div>
		{#each offered as c, i (c.name)}
			<button
				type="button"
				class="cmd"
				class:on={i === pickedCmd}
				role="option"
				id="agent-cmd-{c.name}"
				aria-selected={i === pickedCmd}
				onmousedown={(e) => e.preventDefault()}
				onclick={() => choose(c)}
				><Icon name={c.icon} size={16} /><b>/{c.name}</b><span>{c.label}</span></button
			>
		{/each}
	</div>
{/if}
<form
	class="composer"
	onsubmit={(e) => {
		e.preventDefault();
		send();
	}}
>
	{#if attachments.length}
		<div class="files">
			{#each attachments as a (a.name)}
				<span class="file"
					><Icon name={a.file ? 'clip' : 'file'} size={13} />{a.name}<button
						type="button"
						aria-label={t('agent.attachRemove', { name: a.name })}
						onclick={() => (attachments = attachments.filter((x) => x !== a))}
						><Icon name="close" size={12} /></button
					></span
				>
			{/each}
		</div>
	{/if}

	<div class="tools">
		<input bind:this={picker} type="file" multiple hidden onchange={picked} />
		<!-- Photos: on the phone this offers the camera and the photo library. -->
		<input bind:this={photoPicker} type="file" accept="image/*" multiple hidden onchange={picked} />
		{#if onpicknas}
			<span class="attach-menu">
				<Dropdown label={t('agent.attach')} icon="clip" disabled={busy || disabled}>
					{#snippet children(close)}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								photoPicker?.click();
							}}><Icon name="camera" size={16} /><span>{t('agent.attachPhoto')}</span></button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								picker?.click();
							}}><Icon name="upload" size={16} /><span>{t('agent.attachDevice')}</span></button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								onpicknas?.();
							}}><Icon name="folder" size={16} /><span>{t('agent.attachNas')}</span></button
						>
					{/snippet}
				</Dropdown>
			</span>
		{:else}
			<button
				type="button"
				class="round attach"
				aria-label={t('agent.attach')}
				title={t('agent.attach')}
				disabled={busy || disabled}
				onclick={() => picker?.click()}><Icon name="clip" size={16} /></button
			>
		{/if}
		<textarea
			bind:this={area}
			bind:value={text}
			oninput={grow}
			onkeydown={key}
			rows="1"
			{disabled}
			aria-label={t('agent.input')}
			aria-controls={offered.length ? 'agent-cmds' : undefined}
			aria-activedescendant={offered.length ? `agent-cmd-${offered[pickedCmd]?.name}` : undefined}
			placeholder={placeholder || (busy ? t('agent.inputBusy') : t('agent.inputIdle'))}></textarea>
		{#if voice && !busy}
			<button
				type="button"
				class="round mic"
				class:on={recording}
				disabled={disabled || transcribing}
				aria-pressed={recording}
				aria-label={recording ? t('agent.voiceStop') : t('agent.voiceStart')}
				title={recording ? t('agent.voiceStop') : t('agent.voiceStart')}
				onclick={() => micHold || toggleVoice()}
				onpointerdown={() => micHold && !recording && toggleVoice()}
				onpointerup={() => micHold && recording && toggleVoice()}
				onpointerleave={() => micHold && recording && toggleVoice()}
				>{#if transcribing}<span class="dots" aria-hidden="true">…</span>{:else}<Icon
						name={recording ? 'stop' : 'mic'}
						size={17}
					/>{/if}</button
			>
		{/if}
		{#if busy}
			<button
				type="button"
				class="round stop"
				onclick={() => onstop?.()}
				aria-label={t('agent.stop')}
				title={t('agent.stop')}><Icon name="stop" size={14} /></button
			>
		{:else}
			<button
				type="submit"
				class="round send"
				disabled={disabled || (!text.trim() && !attachments.length)}
				aria-label={t('agent.send')}
				title={t('agent.send')}><Icon name="arrowUp" size={18} /></button
			>
		{/if}
	</div>
</form>

<style>
	/* PLAN 90-4: the "/" commands open above the input. */
	.cmds {
		margin-bottom: var(--space-1-5);
		padding: var(--space-1-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		box-shadow: var(--shadow-menu, 0 12px 30px rgb(0 0 0 / 0.35));
		max-height: 46dvh;
		overflow-y: auto;
	}
	.cmds-head {
		padding: var(--space-1) var(--space-2-5) var(--space-1-5);
		font-size: var(--text-xs);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--muted);
	}
	.cmd {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		width: 100%;
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.cmd :global(svg) {
		flex: none;
	}
	.cmd b {
		flex: none;
		min-width: 84px;
		color: var(--text);
		font-family: var(--font-mono, ui-monospace, monospace);
		font-weight: 500;
	}
	.cmd span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.cmd.on,
	.cmd:hover {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
	}
	@media (max-width: 760px) {
		.cmd {
			min-height: 44px;
		}
	}
	/* PLAN 68: a card at the bottom – 📎, text, 🎙 and the send button in
	   the accent, like the rest of TitanOS (no glass, no glow). */
	.composer {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		padding: var(--space-2) var(--space-2) var(--space-2) var(--space-2-5);
		border-radius: var(--radius-tile);
		border: 1px solid var(--border);
		background: var(--surface);
	}
	.composer:focus-within {
		border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
	}
	textarea {
		flex: 1;
		min-width: 0;
		align-self: center;
		resize: none;
		border: 0;
		outline: none;
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		line-height: 1.45;
		min-height: 24px;
		max-height: 220px;
		padding: 5px var(--space-1);
	}
	textarea::placeholder {
		color: var(--muted);
	}
	.tools {
		display: flex;
		align-items: flex-end;
		gap: var(--space-1-5);
	}
	.round {
		width: 32px;
		height: 32px;
		border-radius: var(--radius-ctl);
		display: grid;
		place-items: center;
		border: 0;
		cursor: pointer;
		flex: none;
	}
	.attach,
	.attach-menu :global(.btn) {
		width: 32px;
		height: 32px;
		min-height: 32px;
		padding: 0;
		border-radius: var(--radius-ctl);
		border: 0;
		background: none;
		color: var(--muted);
	}
	.attach:hover,
	.attach-menu :global(.btn:hover),
	.mic:hover {
		background: color-mix(in srgb, var(--text) 8%, transparent);
		color: var(--text);
	}
	.round:focus-visible,
	.attach-menu :global(.btn:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.files {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		padding: 2px 2px 0;
	}
	.file {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		padding: 2px var(--space-1) 2px var(--space-2);
		border-radius: 999px;
		background: var(--chip);
		font-size: var(--text-sm);
		max-width: 100%;
		overflow-wrap: anywhere;
	}
	.file button {
		border: 0;
		background: none;
		color: var(--muted);
		cursor: pointer;
		padding: 2px;
		display: grid;
	}
	.send {
		background: var(--accent);
		color: var(--on-accent);
	}
	.send:disabled {
		opacity: 0.45;
		cursor: default;
	}
	.stop {
		background: var(--text);
		color: var(--bg);
	}
	.mic {
		background: none;
		color: var(--muted);
	}
	.mic.on {
		background: var(--error-bg);
		color: var(--error-text);
		outline: 1px solid var(--error-border);
		animation: rec 1.2s ease-in-out infinite;
	}
	@keyframes rec {
		50% {
			opacity: 0.65;
		}
	}
	:global(.agent.calm) .mic.on {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.mic.on {
			animation: none;
		}
	}
	/* Under 16px iOS zooms into the field on a tap and stays zoomed. */
	@media (max-width: 760px), (pointer: coarse) {
		textarea {
			font-size: max(16px, 1rem);
		}
	}
</style>
