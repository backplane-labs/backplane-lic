import { test } from 'node:test';
import assert from 'node:assert/strict';
import { accountPath, decodeDocument, humanize, json, labelOf, listData, nextPage, parseJSON, redact, RequestError, secretsOf, wireJSON, writableAttributes } from './model.ts';
const bytes = (value: unknown) => new TextEncoder().encode(JSON.stringify(value));
test('account and resource identifiers are safely addressed without losing aliases', () => {
  assert.equal(accountPath('acme/team', 'licenses/key'), '/v1/accounts/acme%2Fteam/licenses/key');
});
test('204 mutations, empty lists and JSON:API single resources are distinct', () => {
  assert.deepEqual(decodeDocument(204, new Uint8Array()), {});
  assert.deepEqual(listData({ data: null }), []);
  assert.deepEqual(listData({ data: [] }), []);
  assert.equal(listData({ data: { id: 'x', type: 'licenses' } })[0].id, 'x');
});
test('server field pointers, detail, code and status survive the RPC bridge', () => {
  assert.throws(() => decodeDocument(422, bytes({ errors: [{ detail: 'must be positive', code: 'INVALID', source: { pointer: '/data/attributes/maxUses' } }] })), (error: unknown) => error instanceof RequestError && error.status === 422 && error.message.includes('/data/attributes/maxUses') && error.message.includes('INVALID'));
  assert.throws(() => decodeDocument(502, new TextEncoder().encode('upstream unavailable')), /upstream unavailable/);
});
test('large integer values survive read, form, wire and preview without rounding', () => {
  const value = parseJSON('{"maxMemory":9223372036854775807}') as Record<string, unknown>;
  assert.equal(value.maxMemory, 9223372036854775807n);
  assert.equal(wireJSON(value), '{"maxMemory":9223372036854775807}');
  assert.match(json(value), /9223372036854775807/);
  assert.equal(wireJSON({ unset: undefined, clear: null, list: [1n, 'x'] }), '{"clear":null,"list":[1,"x"]}');
});
test('cursor links retain supported filter params without following external origins', () => {
  assert.deepEqual(nextPage({ next: 'https://elsewhere.invalid/licenses?page%5Bcursor%5D=abc&page%5Bsize%5D=25&status=ACTIVE' }), { 'page[cursor]': 'abc', 'page[size]': '25', status: 'ACTIVE' });
  assert.equal(nextPage({ next: null }), undefined);
});
test('editing excludes read-only, immutable and password fields', () => {
  assert.deepEqual(writableAttributes({ type: 'policies', id: 'x', attributes: { name: 'policy', key: 'immutable', password: 'do not prefill', created: 'now' } }, [{ name: 'name' }, { name: 'key', createOnly: true }, { name: 'password' }], true), { name: 'policy' });
});
test('one-time secrets are separated from normal views, including provisioning URIs', () => {
  const document = { data: { type: 'tokens', id: 'x', attributes: { token: 'secret-token', uri: 'otpauth://totp?secret=foo', name: 'CI' } } };
  assert.deepEqual(secretsOf(document), { token: 'secret-token', uri: 'otpauth://totp?secret=foo' });
  const display = json(redact(document));
  assert.ok(!display.includes('secret-token'));
  assert.ok(!display.includes('secret=foo'));
  assert.ok(display.includes('CI'));
});
test('untranslated attributes read as words and unnamed licenses are labelled by key', () => {
  assert.equal(humanize('distributionStrategy'), 'Distribution strategy');
  assert.equal(humanize('webhook-endpoints'), 'Webhook endpoints');
  assert.equal(humanize('maxMachines'), 'Max machines');
  assert.equal(labelOf({ type: 'licenses', id: 'id-1', attributes: { name: null, key: 'ABC-123' } }), 'ABC-123');
  assert.equal(labelOf({ type: 'webhook-events', id: 'id-2', attributes: { event: 'license.created' } }), 'license.created');
});
