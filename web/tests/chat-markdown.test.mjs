import test from 'node:test';
import assert from 'node:assert/strict';
import { chatMarkdown, internalPage } from '../src/lib/chat-markdown.ts';

test('answers become safe HTML', () => {
	assert.equal(
		chatMarkdown('Das Volume **tank** ist `degraded`.'),
		'<p>Das Volume <strong>tank</strong> ist <code>degraded</code>.</p>',
	);
	assert.equal(
		chatMarkdown('- eins\n- **zwei**'),
		'<ul><li>eins</li><li><strong>zwei</strong></li></ul>',
	);
	assert.equal(chatMarkdown('1. a\n2. b'), '<ol><li>a</li><li>b</li></ol>');
	assert.equal(chatMarkdown('a\nb\n\nc'), '<p>a<br>b</p><p>c</p>');
	assert.equal(
		chatMarkdown('```\nmdadm --detail\n```'),
		'<div class="code"><div class="code-head"><span>text</span></div><pre><code>mdadm --detail</code></pre></div>',
	);
	// The language of the fence stands in the head of the block.
	assert.equal(
		chatMarkdown('So:\n\n```yaml\na: 1\n```\n\nFertig.'),
		'<p>So:</p><div class="code"><div class="code-head"><span>yaml</span></div><pre><code><span class="t-a">a</span>: <span class="t-n">1</span></code></pre></div><p>Fertig.</p>',
	);
});

test('nothing from a model becomes markup of its own', () => {
	const html = chatMarkdown(
		'<img src=x onerror=alert(1)> [link](javascript:alert(1)) <script>x</script>',
	);
	assert.ok(!html.includes('<img'));
	assert.ok(!html.includes('<script'));
	assert.ok(!html.includes('<a '));
	assert.ok(html.includes('&lt;img'));
	// Not even inside code.
	assert.ok(!chatMarkdown('`<b>x</b>`').includes('<b>'));
});

test('headings, tables and italics', () => {
	assert.equal(
		chatMarkdown('## Volumes\nalles *gut*'),
		'<h4>Volumes</h4><p>alles <em>gut</em></p>',
	);
	assert.equal(
		chatMarkdown('| Platte | Temperatur |\n|---|---:|\n| sda | **41 °C** |'),
		'<div class="table table-wrap"><table class="data-table"><thead><tr><th>Platte</th><th>Temperatur</th></tr></thead><tbody><tr><td>sda</td><td><strong>41 °C</strong></td></tr></tbody></table></div>',
	);
	assert.equal(chatMarkdown('Text\n- a\n- b'), '<p>Text</p><ul><li>a</li><li>b</li></ul>');
	assert.equal(chatMarkdown('`*nicht kursiv*`'), '<p><code>*nicht kursiv*</code></p>');
});

test('links only to pages of the web UI', () => {
	assert.equal(
		chatMarkdown('Unter [Speicher](/storage) nachsehen.'),
		'<p>Unter <a href="/storage">Speicher</a> nachsehen.</p>',
	);
	assert.equal(chatMarkdown('[VM](/vms?vm=win11)'), '<p><a href="/vms?vm=win11">VM</a></p>');
	assert.equal(
		chatMarkdown('[Netz](/settings/network)'),
		'<p><a href="/settings/network">Netz</a></p>',
	);
	for (const bad of [
		'https://evil.example',
		'//evil.example',
		'/api/v1/system/power',
		'javascript:alert(1)',
		'/settings/../api',
		'/vms?vm=a&x=1',
		'/storage"onmouseover="x',
	]) {
		assert.ok(!chatMarkdown(`[x](${bad})`).includes('<a '), bad);
		assert.ok(!internalPage(bad), bad);
	}
	assert.ok(!chatMarkdown('[<b>x</b>](/storage)').includes('<b>'));
});

test('code blocks are colored and stay safe', () => {
	const html = chatMarkdown(
		'```bash\n# <b>Kommentar</b>\ndocker compose up -d "$HOME" && echo \'<script>\'\n```',
	);
	assert.ok(html.includes('<span class="t-c"># &lt;b&gt;Kommentar&lt;/b&gt;</span>'), html);
	assert.ok(html.includes('<span class="t-k">docker</span>'), html);
	assert.ok(html.includes('<span class="t-s">&#39;&lt;script&gt;&#39;</span>'), html);
	assert.ok(!/<(?!\/?(?:div|span|pre|code)\b)/.test(html), 'only the known tags: ' + html);
	// A URL in YAML is no comment; the key and the string keep their colors.
	const yaml = chatMarkdown('```yaml\nurl: "http://nas.local" # hier\n```');
	assert.ok(yaml.includes('<span class="t-a">url</span>'), yaml);
	assert.ok(yaml.includes('<span class="t-s">&quot;http://nas.local&quot;</span>'), yaml);
	assert.ok(yaml.includes('<span class="t-c"># hier</span>'), yaml);
	// A language without rules stays plain.
	assert.ok(chatMarkdown('```brainfuck\n+[>+<-]\n```').includes('<code>+[&gt;+&lt;-]</code>'));
});
