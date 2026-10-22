import test from 'node:test';
import assert from 'node:assert/strict';
import { chatMarkdown } from '../src/lib/chat-markdown.ts';

test('answers become safe HTML', () => {
	assert.equal(chatMarkdown('Das Volume **tank** ist `degraded`.'), '<p>Das Volume <strong>tank</strong> ist <code>degraded</code>.</p>');
	assert.equal(chatMarkdown('- eins\n- **zwei**'), '<ul><li>eins</li><li><strong>zwei</strong></li></ul>');
	assert.equal(chatMarkdown('1. a\n2. b'), '<ul><li>a</li><li>b</li></ul>');
	assert.equal(chatMarkdown('a\nb\n\nc'), '<p>a<br>b</p><p>c</p>');
	assert.equal(chatMarkdown('```\nmdadm --detail\n```'), '<pre><code>mdadm --detail</code></pre>');
});

test('nothing from a model becomes markup of its own', () => {
	const html = chatMarkdown('<img src=x onerror=alert(1)> [link](javascript:alert(1)) <script>x</script>');
	assert.ok(!html.includes('<img'));
	assert.ok(!html.includes('<script'));
	assert.ok(!html.includes('<a '));
	assert.ok(html.includes('&lt;img'));
	// Not even inside code.
	assert.ok(!chatMarkdown('`<b>x</b>`').includes('<b>'));
});
