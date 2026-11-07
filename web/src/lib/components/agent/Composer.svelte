<script lang="ts">
	// The input of the KI-Agent (PLAN 48, card of PLAN 68): the clip, the
	// task, the microphone and the send button in the accent. Enter sends,
	// Shift+Enter a new line; while a run works the round button stops it.
	// Model and thinking level sit at the top of the chat (ModelPicker).
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { apiFetch, responseError } from '#lib/api.ts';

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
	}: {
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
					const res = await apiFetch(`/agent/transcribe?lang=${i18n.locale}`, {
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
	function send() {
		const value = text.trim() || (attachments.length ? t('agent.attachDefault') : '');
		if (!value || busy || disabled) return;
		onsend(value);
		text = '';
		queueMicrotask(grow);
	}
	function key(event: KeyboardEvent) {
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
	/* PLAN 68: a card at the bottom – 📎, text, 🎙 and the send button in
	   the accent, like the rest of TitanOS (no glass, no glow). */
	.composer {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 8px 8px 8px 10px;
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
		padding: 5px 4px;
	}
	textarea::placeholder {
		color: var(--muted);
	}
	.tools {
		display: flex;
		align-items: flex-end;
		gap: 6px;
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
		gap: 6px;
		padding: 2px 2px 0;
	}
	.file {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		padding: 2px 4px 2px 8px;
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
