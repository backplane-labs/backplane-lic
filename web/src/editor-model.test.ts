import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import {
  actionPayload,
  completeEditor,
  errorFieldPath,
  parseDraft,
  scaffoldDraft,
  validateEditor,
} from './editor-model.ts';
import { json, wireJSON } from './model.ts';
import { type FieldSpec, resources } from './resources.ts';

// The completion tests fail loudly when a fixture lookup returns nothing.
function required<T>(value: T | undefined | null, what: string): T {
  if (value === undefined || value === null) throw new Error(`Missing ${what}`);
  return value;
}

const fields: FieldSpec[] = [
  {
    name: 'attributes',
    required: true,
    fields: [
      { name: 'name', required: true },
      { name: 'maxMemory', kind: 'int64', nullable: true },
      { name: 'enabled', kind: 'bool' },
      { name: 'metadata', kind: 'json' },
    ],
  },
  { name: 'role', choices: ['admin', 'user'], nullable: true },
  { name: 'productId', required: true },
];
test('descriptor scaffold fills required nested fields without overriding existing values or optional server defaults', () => {
  const initial = { attributes: { maxMemory: 9223372036854775807n, metadata: { existing: true } }, role: null };
  const result = scaffoldDraft(fields, initial);
  assert.deepEqual(result, {
    attributes: { name: '', maxMemory: 9223372036854775807n, metadata: { existing: true } },
    role: null,
    productId: '',
  });
  assert.ok(!Object.hasOwn(required(result.attributes, 'attributes'), 'enabled'));
  assert.ok(!Object.hasOwn(initial.attributes, 'name'));
  assert.deepEqual(scaffoldDraft(fields, result), result);
  assert.deepEqual(parseDraft(json(result)).attributes, {
    name: '',
    maxMemory: 9223372036854775807n,
    metadata: { existing: true },
  });
});
test('advanced completion suggests missing descriptor properties in incomplete nested JSON', () => {
  const text = '{"attributes":{"na';
  const result = required(completeEditor(fields, text, text.length), 'completions');
  assert.equal(result.from, text.length - 2);
  assert.deepEqual(
    result.options.map((option) => option.label),
    ['name'],
  );
  assert.equal(result.options[0].apply, 'name": ""');
  const existing = '{"attributes":{"name":"a",';
  const nested = required(completeEditor(fields, existing, existing.length), 'completions');
  assert.ok(!nested.options.some((option) => option.label === 'name'));
  assert.ok(nested.options.some((option) => option.label === 'maxMemory'));
  assert.equal(required(completeEditor(fields, '', 0), 'completions').options[0].apply, json(scaffoldDraft(fields)));
});
test('completion respects string escapes, braces in values, existing closing quotes, and nested object paths', () => {
  const text = '{"attributes":{"name":"} \\" {", "ma"}}';
  const position = text.indexOf('ma"') + 2;
  const result = required(completeEditor(fields, text, position), 'completions');
  assert.equal(result.options[0].label, 'maxMemory');
  assert.equal(result.options[0].apply, 'maxMemory');
  const freeJSON = '{"attributes":{"metadata":{';
  assert.deepEqual(required(completeEditor(fields, freeJSON, freeJSON.length), 'completions').options, []);
});
test('enum and nullable-value completions never mutate the descriptor', () => {
  const text = '{"role":"ad';
  assert.deepEqual(
    required(completeEditor(fields, text, text.length), 'completions').options.map((option) => option.apply),
    ['admin"'],
  );
  const emptyValue = '{"role":';
  assert.ok(
    required(completeEditor(fields, emptyValue, emptyValue.length), 'completions').options.some(
      (option) => option.apply === 'null',
    ),
  );
  completeEditor(fields, emptyValue, emptyValue.length);
  assert.deepEqual(fields[1].choices, ['admin', 'user']);
});
test('advanced diagnostics cover malformed JSON, required fields, unknown fields, types and enum constraints', () => {
  assert.ok(validateEditor(fields, '{').length);
  assert.ok(validateEditor(fields, '[]').length);
  assert.ok(validateEditor(fields, '{}').length);
  const invalid = json({
    attributes: { name: 'x', enabled: 'yes', created: 'readonly' },
    role: 'root',
    productId: 'p',
  });
  const errors = validateEditor(fields, invalid);
  assert.ok(errors.some((error) => error.message.includes('unknown field')));
  assert.ok(errors.some((error) => error.message.includes('enabled')));
  assert.ok(errors.some((error) => error.message.includes('role')));
  assert.ok(errors.every((error) => error.from >= 0 && error.to <= invalid.length));
  assert.deepEqual(
    validateEditor(
      fields,
      json({
        attributes: { name: 'x', enabled: false, maxMemory: 9223372036854775807n },
        role: 'admin',
        productId: 'p',
      }),
    ),
    [],
  );
});
test('server pointers map to native schema attributes, relationship ID controls and action metadata', () => {
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/attributes/maxMemory' } }, fields), [
    'attributes',
    'maxMemory',
  ]);
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/attributes/max_memory' } }, fields), [
    'attributes',
    'maxMemory',
  ]);
  assert.deepEqual(errorFieldPath({ source: { pointer: '/data/relationships/product/data/id' } }, fields), [
    'productId',
  ]);
  assert.deepEqual(
    errorFieldPath({ source: { pointer: '/meta/legacy_encrypted' } }, [{ name: 'legacyEncrypted', kind: 'bool' }]),
    ['legacyEncrypted'],
  );
  assert.deepEqual(
    errorFieldPath({ source: { pointer: '/data/attributes/literal~1key' } }, [
      { name: 'attributes', fields: [{ name: 'literal/key' }] },
    ]),
    ['attributes', 'literal/key'],
  );
  assert.equal(errorFieldPath({ source: { pointer: '/missing' } }, fields), undefined);
});
test('validate-key serializes legacy_encrypted exactly and does not mutate form values', () => {
  const action = required(
    required(
      required(
        resources.find((resource) => resource.name === 'licenses'),
        'licenses',
      ).actions,
      'actions',
    ).find((candidate) => candidate.name === 'validateKey'),
    'validateKey action',
  );
  const values = { key: 'encrypted-key', legacyEncrypted: true, nonce: 9223372036854775807n };
  assert.deepEqual(actionPayload(action, values), {
    meta: { key: 'encrypted-key', legacy_encrypted: true, nonce: 9223372036854775807n },
  });
  assert.match(wireJSON(actionPayload(action, values)), /"legacy_encrypted":true/);
  assert.ok(!wireJSON(actionPayload(action, values)).includes('legacyEncrypted'));
  assert.equal(values.legacyEncrypted, true);
  assert.deepEqual(actionPayload({ ...action, name: 'validate' }, { nonce: 1n }), { meta: { nonce: 1n } });
});

test('both checkout forms expose exactly the algorithm combinations accepted by backend constants', () => {
  const file = readFileSync(new URL('../../internal/domain/crypto/licensefile.go', import.meta.url), 'utf8');
  const signatures = readFileSync(new URL('../../internal/domain/crypto/signature.go', import.meta.url), 'utf8');
  const constant = (source: string, name: string) => {
    const value = source.match(new RegExp(`${name}\\s*=\\s*"([^"]+)"`))?.[1];
    if (value === undefined) throw new Error(`Constant ${name} not found`);
    return value;
  };
  const encodings = ['EncodingBase64', 'EncodingAES256GCM'].map((name) => constant(file, name));
  const signing = ['AlgEd25519', 'AlgRSAPSSSHA256', 'AlgRSASHA256', 'AlgECDSAP256'].map((name) =>
    constant(signatures, name),
  );
  const expected = encodings.flatMap((encoding) => signing.map((algorithm) => `${encoding}+${algorithm}`)).sort();
  const licenses = required(
    resources.find((resource) => resource.name === 'licenses'),
    'licenses',
  );
  const licenseActions = required(licenses.actions, 'license actions');
  for (const name of ['checkout', 'downloadLicense']) {
    const action = required(
      licenseActions.find((candidate) => candidate.name === name),
      `${name} action`,
    );
    const actionFields = required(action.fields, `${name} fields`);
    const algorithm = required(
      actionFields.find((field) => field.name === 'algorithm'),
      'algorithm field',
    );
    assert.deepEqual([...required(algorithm.choices, 'algorithm choices')].sort(), expected);
    assert.deepEqual(validateEditor(actionFields, '{}'), []); // Omission keeps the backend-derived default.
    for (const choice of expected) assert.deepEqual(validateEditor(actionFields, json({ algorithm: choice })), []);
    assert.ok(validateEditor(actionFields, json({ algorithm: 'ed25519' })).length);
  }
});
