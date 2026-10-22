import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { actionPayload, completeEditor, errorFieldPath, parseDraft, scaffoldDraft, validateEditor } from './editor-model.ts';
import { json, wireJSON } from './model.ts';
import { resources, type FieldSpec } from './resources.ts';
const fields: FieldSpec[] = [
  { name: 'attributes', required: true, fields: [{ name: 'name', required: true }, { name: 'maxMemory', kind: 'int64', nullable: true }, { name: 'enabled', kind: 'bool' }, { name: 'metadata', kind: 'json' }] },
  { name: 'role', choices: ['admin', 'user'], nullable: true }, { name: 'productId', required: true },
];
test('descriptor scaffold fills required nested fields without overriding existing values or optional server defaults', () => {
  const initial = { attributes: { maxMemory: 9223372036854775807n, metadata: { existing: true } }, role: null };
  const result = scaffoldDraft(fields, initial);
  assert.deepEqual(result, { attributes: { name: '', maxMemory: 9223372036854775807n, metadata: { existing: true } }, role: null, productId: '' });
  assert.ok(!Object.hasOwn(result.attributes!, 'enabled'));
  assert.ok(!Object.hasOwn(initial.attributes, 'name'));
  assert.deepEqual(scaffoldDraft(fields, result), result);
  assert.equal((parseDraft(json(result)).attributes as Record<string, unknown>).maxMemory, 9223372036854775807n);
});
test('advanced completion suggests missing descriptor properties in incomplete nested JSON', () => {
  const text = '{"attributes":{"na';
  const result = completeEditor(fields, text, text.length)!;
  assert.equal(result.from, text.length - 2);
  assert.deepEqual(result.options.map((option) => option.label), ['name']);
  assert.equal(result.options[0].apply, 'name": ""');
  const existing = '{"attributes":{"name":"a",';
  const nested = completeEditor(fields, existing, existing.length)!;
  assert.ok(!nested.options.some((option) => option.label === 'name'));
  assert.ok(nested.options.some((option) => option.label === 'maxMemory'));
  assert.equal(completeEditor(fields, '', 0)!.options[0].apply, json(scaffoldDraft(fields)));
});
test('completion respects string escapes, braces in values, existing closing quotes, and nested object paths', () => {
  const text = '{"attributes":{"name":"} \\" {", "ma"}}';
  const position = text.indexOf('ma"') + 2;
  const result = completeEditor(fields, text, position)!;
  assert.equal(result.options[0].label, 'maxMemory');
  assert.equal(result.options[0].apply, 'maxMemory');
  const freeJSON = '{"attributes":{"metadata":{';
  assert.deepEqual(completeEditor(fields, freeJSON, freeJSON.length)!.options, []);
});
test('enum and nullable-value completions never mutate the descriptor', () => {
  const text = '{"role":"ad';
  assert.deepEqual(completeEditor(fields, text, text.length)!.options.map((option) => option.apply), ['admin"']);
  const emptyValue = '{"role":';
  assert.ok(completeEditor(fields, emptyValue, emptyValue.length)!.options.some((option) => option.apply === 'null'));
  completeEditor(fields, emptyValue, emptyValue.length);
  assert.deepEqual(fields[1].choices, ['admin', 'user']);
});
test('advanced diagnostics cover malformed JSON, required fields, unknown fields, types and enum constraints', () => {
  assert.ok(validateEditor(fields, '{').length);
  assert.ok(validateEditor(fields, '[]').length);
  assert.ok(validateEditor(fields, '{}').length);
  const invalid = json({ attributes: { name: 'x', enabled: 'yes', created: 'readonly' }, role: 'root', productId: 'p' });
  const errors = validateEditor(fields, invalid);
  assert.ok(errors.some((error) => error.message.includes('unknown field')));
  assert.ok(errors.some((error) => error.message.includes('enabled')));
  assert.ok(errors.some((error) => error.message.includes('role')));
  assert.ok(errors.every((error) => error.from >= 0 && error.to <= invalid.length));
  assert.deepEqual(validateEditor(fields, json({ attributes: { name: 'x', enabled: false, maxMemory: 9223372036854775807n }, role: 'admin', productId: 'p' })), []);
});
test('server pointers map to native schema attributes, relationship ID controls and action metadata', () => {
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/attributes/maxMemory' } }, fields), ['attributes', 'maxMemory']);
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/attributes/max_memory' } }, fields), ['attributes', 'maxMemory']);
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/relationships/product/data/id' } }, fields), ['productId']);
  assert.deepEqual(errorFieldPath({ source: { pointer: '/meta/legacy_encrypted' } }, [{ name: 'legacyEncrypted', kind: 'bool' }]), ['legacyEncrypted']);
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/attributes/literal~1key' } }, [{ name: 'attributes', fields: [{ name: 'literal/key' }] }]), ['attributes', 'literal/key']);
  assert.equal(errorFieldPath({ source: { pointer: '/missing' } }, fields), undefined);
});
test('validate-key serializes legacy_encrypted exactly and does not mutate form values', () => {
  const action = resources.find((resource) => resource.name === 'licenses')!.actions!.find((action) => action.name === 'validateKey')!;
  const values = { key: 'encrypted-key', legacyEncrypted: true, nonce: 9223372036854775807n };
  assert.deepEqual(actionPayload(action, values), { meta: { key: 'encrypted-key', legacy_encrypted: true, nonce: 9223372036854775807n } });
  assert.match(wireJSON(actionPayload(action, values)), /"legacy_encrypted":true/);
  assert.ok(!wireJSON(actionPayload(action, values)).includes('legacyEncrypted'));
  assert.equal(values.legacyEncrypted, true);
  assert.deepEqual(actionPayload({ ...action, name: 'validate' }, { nonce: 1n }), { meta: { nonce: 1n } });
});

test('both checkout forms expose exactly the algorithm combinations accepted by backend constants', () => {
  const file = readFileSync(new URL('../../internal/domain/crypto/licensefile.go', import.meta.url), 'utf8');
  const signatures = readFileSync(new URL('../../internal/domain/crypto/signature.go', import.meta.url), 'utf8');
  const constant = (source: string, name: string) => source.match(new RegExp(name + '\\s*=\\s*"([^"]+)"'))![1];
  const encodings = ['EncodingBase64', 'EncodingAES256GCM'].map((name) => constant(file, name));
  const signing = ['AlgEd25519', 'AlgRSAPSSSHA256', 'AlgRSASHA256', 'AlgECDSAP256'].map((name) => constant(signatures, name));
  const expected = encodings.flatMap((encoding) => signing.map((algorithm) => `${encoding}+${algorithm}`)).sort();
  for (const name of ['checkout', 'downloadLicense']) {
    const action = resources.find((resource) => resource.name === 'licenses')!.actions!.find((action) => action.name === name)!;
    assert.deepEqual([...action.fields!.find((field) => field.name === 'algorithm')!.choices!].sort(), expected);
    assert.deepEqual(validateEditor(action.fields!, '{}'), []); // Omission keeps the backend-derived default.
    for (const algorithm of expected) assert.deepEqual(validateEditor(action.fields!, json({ algorithm })), []);
    assert.ok(validateEditor(action.fields!, json({ algorithm: 'ed25519' })).length);
  }
});
