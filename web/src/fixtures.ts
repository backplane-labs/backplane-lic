import type { ClientConstructor, ClientRuntime, ClientState } from '@backplane-labs/backplane-client';
import { create, fromBinary, toBinary } from '@bufbuild/protobuf';
import { FakeSocket, type Interceptor, route, WsTransport } from '@gopherex/ws-proto-transport';
import { CallRequestSchema, CallResponseSchema, InfoResponseSchema } from './gen/proto/lic/console/v1/console_pb';
import type { JSONValue, Resource } from './model';
import { resources } from './resources';

const account = '10000000-0000-4000-8000-000000000000';
const id = (i: number) => `10000000-0000-4000-8000-${String(i + 1).padStart(12, '0')}`;
// Samples always carry attributes, so the fixture reads them without optional checks.
type Sample = Resource & { attributes: Record<string, JSONValue> };
const samples: Record<string, Sample> = Object.fromEntries(
  resources.map((resource, i) => [
    resource.name,
    {
      type: resource.type,
      id: id(i),
      attributes: {
        name: resource.name,
        created: '2026-10-22T12:00:00Z',
        updated: '2026-10-22T12:00:00Z',
        metadata: { sample: true },
      },
    },
  ]),
);
Object.assign(samples.products.attributes, { name: 'Desktop SDK', code: 'desktop', distributionStrategy: 'LICENSED' });
Object.assign(samples.policies.attributes, {
  name: 'Professional',
  duration: 31536000,
  strict: true,
  floating: false,
  maxMachines: 3,
});
Object.assign(samples.licenses.attributes, {
  name: 'Acme license',
  status: 'ACTIVE',
  key: 'SAMPLE-LICENSE-KEY',
  suspended: false,
  uses: 12,
  maxUses: 1000,
});
Object.assign(samples.users.attributes, {
  email: 'operator@example.invalid',
  role: 'admin',
  firstName: 'Sample',
  lastName: 'Operator',
});
Object.assign(samples.groups.attributes, { name: 'Acme', maxUsers: 50 });
Object.assign(samples.entitlements.attributes, { name: 'Export reports', code: 'reports.export' });
Object.assign(samples.tokens.attributes, { kind: 'admin-token', name: 'CI', expiry: null });
Object.assign(samples['webhook-endpoints'].attributes, {
  url: 'https://example.invalid/webhook',
  subscriptions: ['license.created', 'license.revoked'],
});
Object.assign(samples['webhook-events'].attributes, {
  status: 'FAILED',
  payload: { event: 'license.created', data: samples.licenses },
});
Object.assign(samples.keys.attributes, { key: 'SAMPLE-POOL-KEY' });
const factor: Sample = { type: 'second-factors', id: id(20), attributes: { enabled: true } };
const state: ClientState = { connection: 'connected', session: null, connectionId: 1 };
// The fixture answers through the real generated client: a fake socket carries
// nothing, and the transport interceptor short-circuits each typed call with a
// canned message. The binary round trip is the same parse the server would do.
const consoleService = 'lic.console.v1.ConsoleService';
const fixture: Interceptor = (next) => (request) => {
  if (request.stream) return Promise.reject(new Error('The standalone preview makes no streaming calls'));
  switch (route(request.method)) {
    case `/${consoleService}/Info`:
      return Promise.resolve({
        stream: false,
        method: request.method,
        header: {},
        trailer: {},
        message: create(InfoResponseSchema, {
          accountId: account,
          accountSlug: 'sample',
          accountName: 'Sample installation',
          version: 'preview',
          publicKeys: { ed25519: 'Sample public key — not for production' },
          counts: Object.fromEntries(resources.map((r) => [r.name, 1n])),
        }),
      });
    case `/${consoleService}/Call`: {
      const call = fromBinary(CallRequestSchema, toBinary(request.method.input, request.message));
      if (call.method !== 'GET') throw new Error('Standalone preview is read-only');
      const segments = call.path.replace(`/v1/accounts/${account}/`, '').split('/');
      const [name, resourceId, relation] = segments;
      const collection = samples[name];
      const related =
        relation === 'pool'
          ? samples.keys
          : relation === 'owners' || relation === 'owner'
            ? samples.users
            : relation === 'second-factors'
              ? factor
              : (samples[relation] ?? samples[`${relation}s`]);
      const many = [
        'tokens',
        'entitlements',
        'licenses',
        'policies',
        'products',
        'users',
        'owners',
        'pool',
        'second-factors',
      ].includes(relation);
      const data = relation
        ? related
          ? many
            ? [related]
            : related
          : null
        : resourceId
          ? collection
          : call.query['page[number]'] && Number(call.query['page[number]']) > 1
            ? []
            : [collection].filter(Boolean);
      return Promise.resolve({
        stream: false,
        method: request.method,
        header: {},
        trailer: {},
        message: create(CallResponseSchema, {
          status: 200,
          body: new TextEncoder().encode(JSON.stringify({ data, links: {} })),
        }),
      });
    }
    default:
      return next(request);
  }
};
const transport = WsTransport.fromSocket(new FakeSocket(), { interceptors: [fixture] });
export const fixtureClient: ClientRuntime = {
  getSnapshot: () => state,
  subscribe: () => () => {},
  client<T extends object>(Client: ClientConstructor<T>): T {
    return new Client(transport);
  },
};
