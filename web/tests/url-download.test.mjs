import {test} from 'node:test';
import assert from 'node:assert/strict';
import {downloadName,validDigest,validDownloadName,validDownloadURL} from '../src/routes/files/url-download.ts';
import {transferProblem} from '../src/lib/transfer-progress.ts';
test('links must be http(s) with a host and no user info',()=>{
 assert.equal(validDownloadURL('https://example.org/debian.iso'),true);
 assert.equal(validDownloadURL('http://192.168.1.5:8080/a'),true);
 for(const bad of ['','ftp://example.org/a','file:///etc/passwd','javascript:alert(1)','https://user:pw@example.org/a','example.org/a'])assert.equal(validDownloadURL(bad),false,bad);
});
test('the name is prefilled from the decoded last path element',()=>{
 assert.equal(downloadName('https://example.org/dl/debian%2013.iso?token=x#y'),'debian 13.iso');
 assert.equal(downloadName('https://example.org/'),'');
 assert.equal(downloadName('https://example.org/a%2F..%2Fb'),'');
 assert.equal(downloadName('https://example.org/x.titanos-part'),'');
 assert.equal(validDownloadName('..'),false);assert.equal(validDownloadName('a\\b'),false);assert.equal(validDownloadName('ä'.repeat(128)),false);
});
test('an optional digest is 64 hex characters',()=>{
 assert.equal(validDigest(''),true);assert.equal(validDigest('A'.repeat(64)),true);assert.equal(validDigest('g'.repeat(64)),false);assert.equal(validDigest('ab'),false);
});
test('download failures get their own explanations',()=>{
 assert.equal(transferProblem('files: this address may not be reached: httpfetch: address not allowed'),'blocked');
 assert.equal(transferProblem('files: SHA-256 does not match'),'hash');
 assert.equal(transferProblem('download returned HTTP 404'),'http');
 assert.equal(transferProblem('download stalled: no data for two minutes'),'connection');
 assert.equal(transferProblem('files: not enough free space'),'space');
});
