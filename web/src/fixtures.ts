import { create } from '@bufbuild/protobuf';
import type { ClientRuntime, ClientConstructor, ClientState } from '@backplane-labs/backplane-client';
import { CallResponseSchema, InfoResponseSchema, type CallRequest } from './gen/proto/lic/console/v1/console_pb';
import { resources } from './resources';
import type { Resource } from './model';
const account = '10000000-0000-4000-8000-000000000000';
const id = (i: number) => `10000000-0000-4000-8000-${String(i + 1).padStart(12, '0')}`;
const samples: Record<string, Resource> = Object.fromEntries(resources.map((resource, i) => [resource.name, { type: resource.type, id: id(i), attributes: { name: resource.name, created: '2026-10-22T12:00:00Z', updated: '2026-10-22T12:00:00Z', metadata: { sample: true } } }]));
Object.assign(samples.products.attributes!, { name: 'Desktop SDK', code: 'desktop', distributionStrategy: 'LICENSED' });
Object.assign(samples.policies.attributes!, { name: 'Professional', duration: 31536000, strict: true, floating: false, maxMachines: 3 });
Object.assign(samples.licenses.attributes!, { name: 'Acme license', status: 'ACTIVE', key: 'SAMPLE-LICENSE-KEY', suspended: false, uses: 12, maxUses: 1000 });
Object.assign(samples.users.attributes!, { email: 'operator@example.invalid', role: 'admin', firstName: 'Sample', lastName: 'Operator' });
Object.assign(samples.groups.attributes!, { name: 'Acme', maxUsers: 50 });
Object.assign(samples.entitlements.attributes!, { name: 'Export reports', code: 'reports.export' });
Object.assign(samples.tokens.attributes!, { kind: 'admin-token', name: 'CI', expiry: null });
Object.assign(samples['webhook-endpoints'].attributes!, { url: 'https://example.invalid/webhook', subscriptions: ['license.created', 'license.revoked'] });
Object.assign(samples['webhook-events'].attributes!, { status: 'FAILED', payload: { event: 'license.created', data: samples.licenses } });
Object.assign(samples.keys.attributes!, { key: 'SAMPLE-POOL-KEY' });
const factor: Resource = { type: 'second-factors', id: id(20), attributes: { enabled: true } };
const state: ClientState = { connection: 'connected', session: null, connectionId: 1 };
export const fixtureClient: ClientRuntime = {
  getSnapshot: () => state, subscribe: () => () => {},
  client<T extends object>(constructor: ClientConstructor<T>): T {
    if (!('call' in constructor.prototype)) throw new Error(`No fixture for ${constructor.name}`);
    return {
      async info() { return create(InfoResponseSchema, { accountId: account, accountSlug: 'sample', accountName: 'Sample installation', version: 'preview', publicKeys: { ed25519: 'Sample public key — not for production' }, counts: Object.fromEntries(resources.map((r) => [r.name, 1n])) }); },
      async call(request: CallRequest) {
        if (request.method !== 'GET') throw new Error('Standalone preview is read-only');
        const segments = request.path.replace(`/v1/accounts/${account}/`, '').split('/'), [name, resourceId, relation] = segments;
        const collection = samples[name];
        const related = relation === 'pool' ? samples.keys : relation === 'owners' || relation === 'owner' ? samples.users : relation === 'second-factors' ? factor : samples[relation] ?? samples[relation + 's'];
        const many = ['tokens', 'entitlements', 'licenses', 'policies', 'products', 'users', 'owners', 'pool', 'second-factors'].includes(relation);
        const data = relation ? related ? many ? [related] : related : null : resourceId ? collection : request.query['page[number]'] && Number(request.query['page[number]']) > 1 ? [] : [collection].filter(Boolean);
        return create(CallResponseSchema, { status: 200, body: new TextEncoder().encode(JSON.stringify({ data, links: {} })) });
      },
    } as unknown as T;
  },
};
