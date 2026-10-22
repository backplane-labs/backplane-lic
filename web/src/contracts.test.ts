import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { create, fromJson, toJson } from '@bufbuild/protobuf';
import { structToNative } from '@gopherex/schemapb';
import { formSchema, relationshipBody, validateDraft } from './schemas.ts';
import { resources, secondFactors } from './resources.ts';
import { CallRequestSchema, CallResponseSchema, InfoResponseSchema } from './gen/proto/lic/console/v1/console_pb.ts';
import english from './locales/en.ts';
import russian from './locales/ru.ts';

test('generated Call bytes use base64 JSON and response bytes decode as UTF-8', () => {
  const body = new TextEncoder().encode('{"data":{"type":"products","attributes":{"name":"Продукт"}}}');
  const message = create(CallRequestSchema, { method: 'POST', path: '/v1/accounts/acme/products', query: { 'page[number]': '2' }, body });
  const encoded = toJson(CallRequestSchema, message);
  assert.equal((encoded as Record<string, unknown>).body, Buffer.from(body).toString('base64'));
  assert.deepEqual(fromJson(CallRequestSchema, encoded).body, body);
  const response = fromJson(CallResponseSchema, { status: 200, body: Buffer.from(body).toString('base64') });
  assert.equal(new TextDecoder().decode(response.body), new TextDecoder().decode(body));
  assert.equal(fromJson(InfoResponseSchema, { accountId: 'acme', accountName: 'Acme', counts: { licenses: '9223372036854775807' } }).counts.licenses, 9223372036854775807n);
});
test('every management descriptor compiles, including all policy attrs and enums', () => {
  for (const resource of [...resources, secondFactors]) {
    assert.doesNotThrow(() => validateDraft(formSchema([{ name: 'attributes', fields: resource.attributes }]), { attributes: {} }), resource.name);
    for (const action of resource.actions ?? []) assert.doesNotThrow(() => validateDraft(formSchema(action.fields ?? []), {}), action.name);
  }
  const source = readFileSync(new URL('../../internal/infra/httpapi/policies_body.go', import.meta.url), 'utf8');
  const block = source.match(/var policyCreateAttrs = \[\]string\{([\s\S]*?)\n\}/)![1];
  const allowed = [...block.matchAll(/"([^"]+)"/g)].map((match) => match[1].replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase()));
  assert.deepEqual(resources.find((r) => r.name === 'policies')!.attributes.map((field) => field.name).sort(), allowed.sort());
});
test('schema field errors block missing required values and invalid boolean types', () => {
  const schema = formSchema([{ name: 'name', required: true }, { name: 'enabled', kind: 'bool' }]);
  assert.equal(validateDraft(schema, {}).baked, undefined);
  assert.equal(validateDraft(schema, { name: 'valid', enabled: 'yes' }).baked, undefined);
  assert.ok(validateDraft(schema, { name: 'valid', enabled: false }).baked);
});
test('int64 descriptors preserve exact native values and nullable values', () => {
  const schema = formSchema([{ name: 'maxMemory', kind: 'int64', nullable: true }]);
  const result = validateDraft(schema, { maxMemory: 9223372036854775807n });
  assert.equal(structToNative(result.baked!.values!).maxMemory, 9223372036854775807n);
  assert.ok(validateDraft(schema, { maxMemory: null }).baked);
});
test('relationship IDs map to JSON:API linkage with omission distinct from clear', () => {
  const relations = [{ name: 'policy', type: 'policies' }, { name: 'owner', type: 'users' }, { name: 'group', type: 'groups' }];
  assert.deepEqual(relationshipBody({ policyId: 'p', ownerId: null }, relations), { policy: { data: { type: 'policies', id: 'p' } }, owner: { data: null } });
});
test('all console action paths and methods exist in the actual local OpenAPI', () => {
  const yaml = readFileSync(new URL('../../openapi/openapi.yaml', import.meta.url), 'utf8');
  const paths = new Map<string, string>();
  const regex = /^  (\/v1\/accounts\/\{account\}[^\n]*):\n([\s\S]*?)(?=^  \/|^components:|$(?![\s\S]))/gm;
  for (const match of yaml.matchAll(regex)) paths.set(match[1].replace(/\{[^}]+\}/g, ':id'), match[2]);
  for (const spec of resources) {
    const root = `/v1/accounts/:id/${spec.name}`;
    assert.ok(paths.get(root)?.includes('    get:'), spec.name);
    for (const action of spec.actions ?? []) {
      const base = action.collection ? action.suffix === 'passwords' ? '/v1/accounts/:id' : root : root + '/:id';
      const path = [base, action.suffix].filter(Boolean).join('/');
      assert.ok(paths.get(path)?.includes(`    ${action.method.toLowerCase()}:`), `${action.method} ${path}`);
    }
    for (const rel of spec.relationships ?? []) {
      const path = root + '/:id/' + rel.name;
      assert.ok(paths.get(path)?.includes('    get:'), path);
      if (rel.attach) for (const method of ['post', 'delete']) assert.ok(paths.get(path)?.includes(`    ${method}:`), `${method} ${path}`);
      if (rel.update) assert.ok(paths.get(path)?.includes(`    ${rel.update.toLowerCase()}:`), path);
    }
  }
});
test('all operator copy has English and Russian translations and no machine pages', () => {
  assert.deepEqual(Object.keys(english).sort(), Object.keys(russian).sort());
  assert.ok(resources.every((resource) => !['machines', 'components', 'processes'].includes(resource.name)));
});
