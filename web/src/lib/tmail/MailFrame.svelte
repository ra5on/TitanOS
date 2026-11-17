<script lang="ts">
	// The HTML of a message in a sandboxed frame (docs/TMAIL.md): srcdoc,
	// sandbox "allow-popups allow-popups-to-escape-sandbox" – no scripts, no
	// same origin – and the CSP meta first in the document. Embedded pictures
	// are fetched by the page (with its login) and go in as data: URLs; the
	// frame itself loads nothing from TitanOS.
	import { apiFetch } from '../api.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { mailApi } from './api.ts';
	import { cidsIn, MAIL_SANDBOX, prepareHtml, type MailLink } from './html.ts';

	let {
		html,
		accountId,
		folder,
		uid,
		images,
		title,
		onlinks,
	}: {
		html: string;
		accountId: string;
		folder: string;
		uid: number;
		images: boolean;
		title: string;
		onlinks?: (links: MailLink[]) => void;
	} = $props();

	let cids = $state<Map<string, string>>(new Map());

	// The embedded pictures of this message, read once per message.
	$effect(() => {
		const wanted = cidsIn(html);
		const key = `${accountId}\n${folder}\n${uid}`;
		cids = new Map();
		if (!wanted.length) return;
		let gone = false;
		void (async () => {
			const found = new Map<string, string>();
			for (const cid of wanted.slice(0, 50)) {
				try {
					const res = await apiFetch(mailApi.cidPath(accountId, folder, uid, cid));
					if (!res.ok) continue;
					const blob = await res.blob();
					if (!/^image\/(png|jpeg|gif|webp)$/.test(blob.type)) continue;
					found.set(cid, await dataUrl(blob));
				} catch {
					// Left as the picture's note.
				}
			}
			if (!gone && key === `${accountId}\n${folder}\n${uid}`) cids = found;
		})();
		return () => {
			gone = true;
		};
	});

	function dataUrl(blob: Blob): Promise<string> {
		return new Promise((resolve, reject) => {
			const reader = new FileReader();
			reader.onload = () => resolve(String(reader.result));
			reader.onerror = () => reject(reader.error);
			reader.readAsDataURL(blob);
		});
	}

	const prepared = $derived(
		prepareHtml(html, {
			images,
			cids,
			warnText: (host) => t('mail.link.warnInline', { host }),
			missing: t('mail.picture'),
		}),
	);
	$effect(() => onlinks?.(prepared.links));
</script>

<!-- No scripts and no same origin: the page cannot reach into it, nor it into the page. -->
<iframe class="frame" {title} sandbox={MAIL_SANDBOX} srcdoc={prepared.srcdoc} data-no-edge-swipe
></iframe>

<style>
	.frame {
		display: block;
		width: 100%;
		flex: 1 1 auto;
		min-height: 360px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--surface);
	}
</style>
