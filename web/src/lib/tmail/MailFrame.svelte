<script lang="ts">
	// The HTML of a message in a sandboxed frame (docs/TMAIL.md): titand's
	// whole document unchanged in srcdoc – with its own CSP meta and the
	// embedded pictures as data: URLs – and sandbox "allow-popups
	// allow-popups-to-escape-sandbox": no scripts, no same origin.
	// 1.13: with one style that fits it to the frame's width (fitMailHtml).
	import { fitMailHtml, MAIL_SANDBOX, mailLinks, type MailLink } from './html.ts';

	let {
		html,
		title,
		onlinks,
	}: {
		html: string;
		title: string;
		onlinks?: (links: MailLink[]) => void;
	} = $props();

	$effect(() => onlinks?.(mailLinks(html)));
</script>

<!-- No scripts and no same origin: the page cannot reach into it, nor it into the page. -->
<iframe class="frame" {title} sandbox={MAIL_SANDBOX} srcdoc={fitMailHtml(html)} data-no-edge-swipe
></iframe>

<style>
	.frame {
		display: block;
		width: 100%;
		flex: 1 1 auto;
		min-height: 360px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		/* Mails are written for a light page: the frame is one in every theme
		   (system colour of the light scheme, not a theme colour). */
		color-scheme: light;
		background: Canvas;
	}
	/* The phone and narrow windows: most of the screen's height for the
	   mail (it scrolls inside the frame). */
	@media (max-width: 899px) {
		.frame {
			min-height: max(360px, 70dvh);
		}
	}
</style>
