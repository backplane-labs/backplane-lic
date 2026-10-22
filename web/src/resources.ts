export type FieldSpec = { name: string; kind?: 'string' | 'bool' | 'int64' | 'json'; required?: boolean; secret?: boolean; nullable?: boolean; createOnly?: boolean; choices?: string[]; fields?: FieldSpec[] };
export type Relationship = { name: string; type: string; many?: boolean; attach?: boolean; update?: 'PUT' | 'PATCH'; create?: boolean; secret?: boolean };
export type Action = { name: string; method: string; suffix: string; fields?: FieldSpec[]; meta?: boolean; collection?: boolean; dangerous?: boolean; removes?: boolean; secret?: boolean; binary?: boolean; initial?: Record<string, unknown> };
export type ResourceSpec = { name: string; type: string; attributes: FieldSpec[]; createRelationships?: { name: string; type: string; required?: boolean }[]; relationships?: Relationship[]; actions?: Action[]; readOnly?: boolean; noCreate?: boolean; noEdit?: boolean; filters?: string[] };
const strings = (names: string): FieldSpec[] => names.split(' ').map((name) => ({ name }));
const limits = (names: string): FieldSpec[] => names.split(' ').map((name) => ({ name, kind: 'int64', nullable: true }));
const bools = (names: string): FieldSpec[] => names.split(' ').map((name) => ({ name, kind: 'bool' }));
const metadata: FieldSpec = { name: 'metadata', kind: 'json' };
const tokenRel: Relationship = { name: 'tokens', type: 'tokens', many: true, create: true, secret: true };
const tokenAttrs: FieldSpec[] = [{ name: 'name', nullable: true }, { name: 'expiry', nullable: true }];
const usage = (verb: 'increment' | 'decrement'): Action => ({ name: `${verb}Usage`, method: 'POST', suffix: `actions/${verb}-usage`, fields: [{ name: verb, kind: 'int64' }], meta: true, initial: { [verb]: 1 } });
export const resources: ResourceSpec[] = [
  { name: 'products', type: 'products', attributes: [{ name: 'name', required: true }, { name: 'code', nullable: true }, { name: 'distributionStrategy', choices: ['LICENSED', 'OPEN', 'CLOSED'], nullable: true }, { name: 'url', nullable: true }, { name: 'platforms', kind: 'json', nullable: true }, metadata],
    relationships: [{ name: 'policies', type: 'policies', many: true }, { name: 'licenses', type: 'licenses', many: true }, { name: 'users', type: 'users', many: true }, tokenRel] },
  { name: 'policies', type: 'policies', attributes: [{ name: 'name', required: true }, { name: 'scheme', createOnly: true, nullable: true, choices: ['LEGACY_ENCRYPT', 'ED25519_SIGN', 'ECDSA_P256_SIGN', 'RSA_2048_PKCS1_SIGN', 'RSA_2048_PKCS1_SIGN_V2', 'RSA_2048_PKCS1_PSS_SIGN', 'RSA_2048_PKCS1_PSS_SIGN_V2', 'RSA_2048_PKCS1_ENCRYPT', 'RSA_2048_JWT_RS256'] }, { name: 'encrypted', kind: 'bool', createOnly: true }, { name: 'usePool', kind: 'bool', createOnly: true },
    ...bools('strict floating protected requireProductScope requirePolicyScope requireMachineScope requireFingerprintScope requireComponentsScope requireUserScope requireChecksumScope requireVersionScope requireCheckIn requireHeartbeat'),
    ...limits('duration maxMachines maxProcesses maxUsers maxCores maxMemory maxDisk maxUses checkInIntervalCount heartbeatDuration'),
    ...strings('machineUniquenessStrategy machineMatchingStrategy componentUniquenessStrategy componentMatchingStrategy expirationStrategy expirationBasis renewalBasis transferStrategy authenticationStrategy machineLeasingStrategy processLeasingStrategy overageStrategy checkInInterval heartbeatCullStrategy heartbeatResurrectionStrategy heartbeatBasis'), metadata],
    createRelationships: [{ name: 'product', type: 'products', required: true }], filters: ['product'], relationships: [{ name: 'product', type: 'products' }, { name: 'licenses', type: 'licenses', many: true }, { name: 'entitlements', type: 'entitlements', many: true, attach: true }, { name: 'pool', type: 'keys', many: true, create: true }],
    actions: [{ name: 'popPool', method: 'DELETE', suffix: 'pool', dangerous: true }] },
  { name: 'licenses', type: 'licenses', attributes: [{ name: 'name', nullable: true }, { name: 'key', createOnly: true }, { name: 'expiry', nullable: true }, ...bools('protected suspended'), ...limits('maxMachines maxProcesses maxUsers maxCores maxMemory maxDisk maxUses'), metadata],
    createRelationships: [{ name: 'policy', type: 'policies', required: true }, { name: 'owner', type: 'users' }, { name: 'group', type: 'groups' }], filters: ['status', 'product', 'policy', 'user', 'group', 'key', 'name'],
    relationships: [{ name: 'product', type: 'products' }, { name: 'policy', type: 'policies', update: 'PATCH' }, { name: 'owner', type: 'users', update: 'PATCH' }, { name: 'group', type: 'groups', update: 'PATCH' }, { name: 'users', type: 'users', many: true, attach: true }, { name: 'entitlements', type: 'entitlements', many: true, attach: true }, tokenRel],
    actions: [
      { name: 'quickValidate', method: 'GET', suffix: 'actions/validate' },
      { name: 'validate', method: 'POST', suffix: 'actions/validate', fields: [{ name: 'nonce', kind: 'int64' }, { name: 'scope', fields: [...strings('product policy user machine fingerprint checksum version'), { name: 'fingerprints', kind: 'json' }, { name: 'components', kind: 'json' }, { name: 'entitlements', kind: 'json' }] }], meta: true },
      { name: 'validateKey', method: 'POST', suffix: 'actions/validate-key', collection: true, fields: [{ name: 'key', required: true }, { name: 'nonce', kind: 'int64' }, { name: 'scope', kind: 'json' }, ...bools('encrypted legacyEncrypted')], meta: true },
      ...['renew', 'suspend', 'reinstate', 'check-in'].map((name) => ({ name, method: 'POST', suffix: `actions/${name}`, dangerous: name === 'suspend' })),
      usage('increment'), usage('decrement'), { name: 'resetUsage', method: 'POST', suffix: 'actions/reset-usage', dangerous: true },
      { name: 'checkout', method: 'POST', suffix: 'actions/check-out', fields: [...bools('encrypt'), { name: 'algorithm', choices: ['ed25519', 'rsa-pss-sha256', 'rsa-sha256', 'ecdsa-p256'] }, { name: 'ttl', kind: 'int64', nullable: true }, { name: 'include', kind: 'json' }], meta: true },
      { name: 'downloadLicense', method: 'GET', suffix: 'actions/check-out', binary: true, fields: [...bools('encrypt'), { name: 'algorithm', choices: ['ed25519', 'rsa-pss-sha256', 'rsa-sha256', 'ecdsa-p256'] }, { name: 'ttl', kind: 'int64', nullable: true }] },
      { name: 'revoke', method: 'DELETE', suffix: 'actions/revoke', dangerous: true, removes: true },
    ] },
  { name: 'users', type: 'users', attributes: [{ name: 'email', required: true }, ...strings('firstName lastName'), { name: 'password', secret: true }, { name: 'role', choices: ['admin', 'developer', 'sales-agent', 'support-agent', 'read-only', 'user'] }, metadata], createRelationships: [{ name: 'group', type: 'groups' }], filters: ['email', 'status', 'group', 'product', 'license', 'roles[]'],
    relationships: [{ name: 'group', type: 'groups', update: 'PUT' }, { name: 'products', type: 'products', many: true }, { name: 'licenses', type: 'licenses', many: true }, tokenRel, { name: 'second-factors', type: 'second-factors', many: true, create: true, secret: true }],
    actions: [{ name: 'ban', method: 'POST', suffix: 'actions/ban', dangerous: true }, { name: 'unban', method: 'POST', suffix: 'actions/unban' },
      { name: 'requestReset', method: 'POST', suffix: 'passwords', collection: true, meta: true, fields: [{ name: 'email', required: true }, { name: 'deliver', kind: 'bool' }], initial: { deliver: true } },
      { name: 'resetPassword', method: 'POST', suffix: 'actions/reset-password', fields: [{ name: 'passwordResetToken', required: true, secret: true }, { name: 'newPassword', required: true, secret: true }], meta: true },
      { name: 'updatePassword', method: 'POST', suffix: 'actions/update-password', fields: [{ name: 'oldPassword', required: true, secret: true }, { name: 'newPassword', required: true, secret: true }], meta: true }] },
  { name: 'groups', type: 'groups', attributes: [{ name: 'name', required: true }, ...limits('maxUsers maxLicenses maxMachines'), metadata], 
    relationships: [{ name: 'owners', type: 'users', many: true, attach: true }, { name: 'users', type: 'users', many: true }, { name: 'licenses', type: 'licenses', many: true }] },
  { name: 'entitlements', type: 'entitlements', attributes: [{ name: 'name', required: true }, { name: 'code', required: true }, metadata] },
  { name: 'tokens', type: 'tokens', noEdit: true, attributes: tokenAttrs, createRelationships: [{ name: 'bearer', type: 'users', required: true }], filters: ['bearer[type]', 'bearer[id]', 'bearer[role][]'],
    actions: [{ name: 'regenerate', method: 'PUT', suffix: '', dangerous: true, secret: true }] },
  { name: 'webhook-endpoints', type: 'webhook-endpoints', attributes: [{ name: 'url', required: true }, { name: 'subscriptions', kind: 'json' }, ...strings('apiVersion signatureAlgorithm')], createRelationships: [{ name: 'product', type: 'products' }], filters: ['product'] },
  { name: 'webhook-events', type: 'webhook-events', noCreate: true, noEdit: true, attributes: [], filters: ['events[]'], actions: [{ name: 'retry', method: 'POST', suffix: 'actions/retry' }] },
  { name: 'keys', type: 'keys', attributes: [{ name: 'key', required: true }], createRelationships: [{ name: 'policy', type: 'policies', required: true }], filters: ['policy', 'product'], relationships: [{ name: 'policy', type: 'policies' }, { name: 'product', type: 'products' }] },
];

const policyChoices: Record<string, string[]> = {
  "machineUniquenessStrategy": [
    "UNIQUE_PER_ACCOUNT",
    "UNIQUE_PER_PRODUCT",
    "UNIQUE_PER_POLICY",
    "UNIQUE_PER_LICENSE"
  ],
  "componentUniquenessStrategy": [
    "UNIQUE_PER_ACCOUNT",
    "UNIQUE_PER_PRODUCT",
    "UNIQUE_PER_POLICY",
    "UNIQUE_PER_LICENSE",
    "UNIQUE_PER_MACHINE"
  ],
  "machineMatchingStrategy": [
    "MATCH_ANY",
    "MATCH_TWO",
    "MATCH_MOST",
    "MATCH_ALL"
  ],
  "componentMatchingStrategy": [
    "MATCH_ANY",
    "MATCH_TWO",
    "MATCH_MOST",
    "MATCH_ALL"
  ],
  "expirationStrategy": [
    "RESTRICT_ACCESS",
    "REVOKE_ACCESS",
    "MAINTAIN_ACCESS",
    "ALLOW_ACCESS"
  ],
  "expirationBasis": [
    "FROM_CREATION",
    "FROM_FIRST_VALIDATION",
    "FROM_FIRST_ACTIVATION",
    "FROM_FIRST_DOWNLOAD",
    "FROM_FIRST_USE"
  ],
  "renewalBasis": [
    "FROM_EXPIRY",
    "FROM_NOW",
    "FROM_NOW_IF_EXPIRED"
  ],
  "transferStrategy": [
    "RESET_EXPIRY",
    "KEEP_EXPIRY"
  ],
  "authenticationStrategy": [
    "TOKEN",
    "LICENSE",
    "SESSION",
    "MIXED",
    "NONE"
  ],
  "machineLeasingStrategy": [
    "PER_LICENSE",
    "PER_USER"
  ],
  "processLeasingStrategy": [
    "PER_LICENSE",
    "PER_MACHINE",
    "PER_USER"
  ],
  "overageStrategy": [
    "ALWAYS_ALLOW_OVERAGE",
    "ALLOW_1_25X_OVERAGE",
    "ALLOW_1_5X_OVERAGE",
    "ALLOW_2X_OVERAGE",
    "NO_OVERAGE"
  ],
  "heartbeatCullStrategy": [
    "DEACTIVATE_DEAD",
    "KEEP_DEAD"
  ],
  "heartbeatResurrectionStrategy": [
    "ALWAYS_REVIVE",
    "15_MINUTE_REVIVE",
    "10_MINUTE_REVIVE",
    "5_MINUTE_REVIVE",
    "2_MINUTE_REVIVE",
    "1_MINUTE_REVIVE",
    "NO_REVIVE"
  ],
  "heartbeatBasis": [
    "FROM_CREATION",
    "FROM_FIRST_PING"
  ],
  "checkInInterval": [
    "day",
    "week",
    "month",
    "year"
  ]
};
for (const field of resources.find((resource) => resource.name === 'policies')!.attributes) {
  if (policyChoices[field.name]) { field.choices = policyChoices[field.name]; field.nullable = true; }
}

export const secondFactors: ResourceSpec = { name: 'second-factors', type: 'second-factors', attributes: [{ name: 'enabled', kind: 'bool' }], actions: [] };
export const byName = (name: string) => name === 'second-factors' ? secondFactors : resources.find((r) => r.name === name);
export const tokenFields = tokenAttrs;
