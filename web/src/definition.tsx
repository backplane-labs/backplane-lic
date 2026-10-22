import { createContext, useContext, useEffect, useState } from 'react';
import { Outlet, useLocation, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useClient } from '@backplane-labs/backplane-react';
import { definePlugin, usePluginContext, usePluginNavigate } from '@backplane-labs/backplane-plugin-sdk';
import { ConnectionNotice, usePlatformQuery } from '@backplane-labs/backplane-platform-ui';
import { Badge, Button, DataTable, EmptyState, EntityHeader, Input, MetaItem, MetaList, PageHeader, Panel, SelectControl, Stack, TabBar, TabItem, Timestamp, toast, type DataColumn } from '@backplane-labs/backplane-ui';
import type { NativeStruct } from '@gopherex/schemapb';
import { call, ConsoleServiceClient, info, request, type Info } from './rpc';
import { byName, resources, tokenFields, type Action, type FieldSpec, type Relationship, type ResourceSpec } from './resources';
import { decodeDocument, labelOf, listData, nextPage, parseJSON, secretsOf, writableAttributes, type Document, type Resource } from './model';
import { relationshipBody, relationFields } from './schemas';
import { actionPayload } from './editor-model';
import { Attributes, LicenseSummary, Status, Value } from './display';
import { AttributeList, CopyButton, ErrorNotice, Loading, ObjectView, OperationForm, Result, mono, row, useText, type Operation } from './components';
import navigation from './navigation';
import ru, { schemaFormsRussian } from './locales/ru';

const Account = createContext<Info | undefined>(undefined);
function useAPI() {
  const installation = useContext(Account), client = useClient(ConsoleServiceClient);
  if (!installation) throw new Error('Account context is required');
  return { installation, client, call: (method: string, path: string, query: Record<string, string> = {}, body?: unknown, signal?: AbortSignal) => call(client, installation.accountId, method, path, query, body, signal) };
}
function Shell() {
  const { i18n } = useTranslation();
  useEffect(() => { i18n.addResourceBundle('ru', 'module.lic', ru, false, true); i18n.addResourceBundle('ru', 'backplane.schemaForms', schemaFormsRussian, false, true); }, [i18n]);
  const t = useText(), client = useClient(ConsoleServiceClient), { environment } = usePluginContext();
  const location = useLocation();
  const state = usePlatformQuery(`lic:info:${location.pathname}`, (signal) => info(client, signal));
  return <Stack gap={16} style={{ minWidth: 0 }}><ConnectionNotice />
    {environment === 'standalone' && <Panel><p>{t('standaloneHelp')}</p></Panel>}
    {state.error !== undefined ? <ErrorNotice error={state.error} retry={state.refresh} /> : !state.value ? <Loading /> : <Account.Provider value={state.value}><Outlet /></Account.Provider>}
  </Stack>;
}
function AccountPage() {
  const t = useText(), { installation } = useAPI();
  return <Stack gap={16}><PageHeader title={installation.accountName || t('title')} description={installation.accountSlug} meta={<Badge variant="outline">{installation.version}</Badge>} />
    <Panel title={t('account')}><AttributeList value={{ accountId: installation.accountId, accountSlug: installation.accountSlug }} /></Panel>
    <Panel title={t('publicKeys')}><Stack gap={16}>{Object.entries(installation.publicKeys).map(([algorithm, key]) => <div key={algorithm}><div style={{ ...row, justifyContent: 'space-between' }}><h3>{algorithm}</h3><CopyButton value={key} /></div><pre tabIndex={0} style={{ ...mono, whiteSpace: 'pre-wrap' }}>{key}</pre></div>)}</Stack></Panel>
    <Panel title={t('counts')}><dl>{Object.entries(installation.counts).map(([key, count]) => <div key={key} style={row}><dt>{t(key)}</dt><dd>{String(count)}</dd></div>)}</dl></Panel>
  </Stack>;
}
function resourceOperation(spec: ResourceSpec, path: string, api: ReturnType<typeof useAPI>, resource?: Resource, nested = false): Operation {
  const updating = !!resource;
  const attrs = spec.attributes.filter((field) => !(updating && field.createOnly)).map((field) => ({ ...field, required: updating ? false : field.required }));
  const relations = !updating || spec.name === 'webhook-endpoints' ? (nested ? [] : spec.createRelationships ?? []) : [];
  const initial: NativeStruct = { attributes: (resource ? writableAttributes(resource, spec.attributes, true) : {}) as NativeStruct };
  for (const rel of relations) {
    const linked = resource?.relationships?.[rel.name]?.data;
    if (linked && !Array.isArray(linked)) initial[rel.name + 'Id'] = linked.id;
  }
  return {
    title: updating ? 'edit' : 'create', fields: [{ name: 'attributes', required: true, fields: attrs }, ...relationFields(relations)], initial,
    execute: (values, signal) => {
      const relationships = relationshipBody(values, relations);
      return api.call(updating ? 'PATCH' : 'POST', path, {}, { data: { type: spec.type, ...(updating ? { id: resource.id } : {}), attributes: values.attributes, ...(Object.keys(relationships).length ? { relationships } : {}) } }, signal);
    },
  };
}
// withPickers turns relationship id inputs into a choice over the related
// records when one page holds them all; a larger set keeps the id input.
async function withPickers(operation: Operation, api: ReturnType<typeof useAPI>, signal?: AbortSignal): Promise<Operation> {
  const fields = await Promise.all(operation.fields.map(async (field) => {
    const type = field.relatesTo;
    if (!type) return field;
    try {
      const document = await api.call('GET', type, { 'page[number]': '1', 'page[size]': '100' }, undefined, signal);
      if (nextPage(document.links)) return field;
      const options = listData(document).map((resource) => ({ value: resource.id, label: `${labelOf(resource)} · ${resource.id.slice(0, 8)}` }));
      return options.length ? { ...field, options, label: field.name.replace(/Id$/, '') } : field;
    } catch { return field; }
  }));
  return { ...operation, fields };
}
function ResourcePage() {
  const { resource: name, id } = useParams(), spec = byName(name ?? '');
  const t = useText();
  if (!spec || spec.name === 'second-factors') return <EmptyState title={t('empty')} />;
  return id ? <Detail key={`${name}:${id}`} spec={spec} path={`${name}/${encodeURIComponent(id)}`} /> : <ResourceList key={name} spec={spec} path={spec.name} />;
}
function ResourceList({ spec, path, relationship, parentPath, onInspect, onChanged }: { spec: ResourceSpec; path: string; relationship?: Relationship; parentPath?: string; onInspect?: (resource: Resource) => void; onChanged?: () => void }) {
  const t = useText(), api = useAPI(), navigate = usePluginNavigate(), context = usePluginContext();
  const [page, setPage] = useState(1), [size, setSize] = useState('25'), [cursorQuery, setCursorQuery] = useState<Record<string, string>>();
  const [filter, setFilter] = useState(spec.filters?.[0] ?? ''), [filterText, setFilterText] = useState(''), [applied, setApplied] = useState<Record<string, string>>({});
  const [operation, setOperation] = useState<Operation>(), [result, setResult] = useState<Document>();
  const query = { ...applied, ...(cursorQuery ?? { 'page[number]': String(page), 'page[size]': size }) };
  const state = usePlatformQuery(`lic:list:${path}:${JSON.stringify(query)}`, (signal) => api.call('GET', path, query, undefined, signal));
  const rows = listData(state.value), next = nextPage(state.value?.links), standalone = context.environment === 'standalone';
  // A row is named by its name; an identifier shown in its own column
  // (a license key, a user email) is not repeated as the name.
  const nameOf = (resource: Resource) => {
    const label = labelOf(resource);
    return columns.some((column) => resource.attributes?.[column] === label) ? String(resource.attributes?.name ?? t('unnamed')) : label;
  };
  const columns = (spec.columns ?? ['status']).filter((column) => column !== 'name' && rows.some((resource) => resource.attributes?.[column] !== undefined));
  const tableColumns: DataColumn<Resource>[] = [
    { id: 'name', label: t('name'), value: (resource) => nameOf(resource), render: (resource) => <Button variant="link" size="sm" style={{ padding: 0, height: 'auto' }} onClick={() => open(resource)}>{nameOf(resource)}</Button> },
    ...columns.map((column): DataColumn<Resource> => ({ id: column, ...(column === 'key' || column === 'email' ? { width: 320 } : {}), label: t(`${column}Column`, { defaultValue: t(column) }), value: (resource) => cellText(resource.attributes?.[column]),
      render: (resource) => <Value name={column} value={resource.attributes?.[column]} /> })),
    { id: 'id', label: 'ID', value: (resource) => resource.id, render: (resource) => <span style={{ ...mono, color: 'var(--muted-foreground)' }} title={resource.id}>{resource.id.slice(0, 8)}</span> },
  ];
  const open = (resource: Resource) => onInspect ? onInspect(resource) : navigate(`${spec.name}/${encodeURIComponent(resource.id)}`);
  const create = async () => {
    if (spec.name === 'second-factors') {
      setOperation({ title: t('create'), fields: [{ name: 'password', secret: true }, { name: 'otp', secret: true }], execute: (values, signal) => api.call('POST', path, {}, { data: { type: spec.type }, meta: values }, signal) });
    } else {
      const pool = relationship?.name === 'pool';
      const op = resourceOperation(spec, pool ? 'keys' : path, api, undefined, !!relationship && !pool);
      if (pool) op.initial = { attributes: {}, policyId: parentPath?.split('/')[1] ?? '' };
      setOperation(await withPickers({ ...op, title: `${t('create')} · ${t(spec.name)}`, submitLabel: t('create') }, api));
    }
  };
  const relationAction = (attach: boolean) => setOperation({ title: t(attach ? 'attach' : 'detach'), dangerous: !attach, fields: [{ name: 'ids', kind: 'json', required: true }], initial: { ids: [] }, execute: async (values, signal) => {
    if (!Array.isArray(values.ids) || !values.ids.length || values.ids.some((id) => typeof id !== 'string' || !id.trim())) throw new Error(t('relationshipIds'));
    return api.call(attach ? 'POST' : 'DELETE', path, {}, { data: values.ids.map((id) => ({ type: relationship!.type, id })) }, signal);
  } });
  return <Stack gap={16}>
    {!relationship && <PageHeader title={t(spec.name)} description={t(`${spec.name}Description`)} />}
    {operation && <OperationForm key={operation.title} operation={operation} close={() => setOperation(undefined)} done={(document) => {
      setOperation(undefined); state.refresh(); onChanged?.();
      const created = !Array.isArray(document.data) ? document.data : undefined;
      if (!relationship && created?.id && created.type === spec.type && !Object.keys(secretsOf(document)).length) navigate(`${spec.name}/${encodeURIComponent(created.id)}`);
      else setResult(document);
    }} />}
    {result && <Result document={result} dismiss={() => setResult(undefined)} />}
    <Panel title={t(relationship?.name ?? spec.name)} count={rows.length} actions={<div style={row}>
      <Button variant="outline" size="sm" onClick={state.refresh}>{t('refresh')}</Button>
      {(!relationship && !spec.noCreate || relationship?.create) && <Button size="sm" disabled={standalone} onClick={() => { void create(); }}>{t('create')}</Button>}
      {relationship?.attach && <><Button size="sm" disabled={standalone} onClick={() => relationAction(true)}>{t('attach')}</Button><Button size="sm" variant="outline" disabled={standalone} onClick={() => relationAction(false)}>{t('detach')}</Button></>}
      {!relationship && spec.actions?.filter((a) => a.collection).map((action) => <Button key={action.name} size="sm" variant="outline" disabled={standalone} onClick={() => setOperation(actionOperation(action, spec, path, api, t))}>{t(action.name)}</Button>)}
    </div>}>
      {!relationship && !!spec.filters?.length && <form aria-label={t('filters')} style={{ ...row, marginBottom: 16 }} onSubmit={(event) => { event.preventDefault(); setApplied(filterText ? { [filter]: filterText } : {}); setPage(1); setCursorQuery(undefined); }}>
        <SelectControl aria-label={t('field')} value={filter} onValueChange={setFilter} options={spec.filters.map((name) => ({ value: name, label: t(name) }))} />
        <Input aria-label={t('filterValue')} value={filterText} onChange={(event) => setFilterText(event.target.value)} style={{ maxWidth: 300 }} />
        <Button type="submit" variant="outline" size="sm">{t('apply')}</Button><Button type="button" variant="ghost" size="sm" onClick={() => { setFilterText(''); setApplied({}); setPage(1); setCursorQuery(undefined); }}>{t('clear')}</Button>
      </form>}
      {spec.name === 'tokens' && !relationship && <p>{t('tokenHelp')}</p>}
      {relationship?.name === 'users' && parentPath?.startsWith('groups/') && <p>{t('membersHelp')}</p>}
      {relationship?.name === 'owners' && <p>{t('ownerHelp')}</p>}
      {state.error !== undefined ? <ErrorNotice error={state.error} retry={state.refresh} /> : state.loading ? <Loading /> : !rows.length ? <EmptyState title={t('empty')} /> : <DataTable label={t(relationship?.name ?? spec.name)} data={rows} columns={tableColumns} getRowId={(resource) => resource.id} onActivate={open} height={Math.min(640, 56 + rows.length * 40)} />}
      <div style={{ ...row, marginTop: 16 }}><Button variant="outline" size="sm" disabled={page === 1 || state.loading} onClick={() => { setPage(page - 1); setCursorQuery(undefined); }}>{t('previous')}</Button><span>{t('page', { page })}</span>
        <Button variant="outline" size="sm" disabled={state.loading || !next} onClick={() => { setPage(page + 1); setCursorQuery(next); }}>{t('next')}</Button>
        <SelectControl aria-label={t('pageSize')} value={size} onValueChange={(value) => { setSize(value); setPage(1); setCursorQuery(undefined); }} options={['10', '25', '50', '100'].map((value) => ({ value, label: value }))} />
      </div>
    </Panel>
  </Stack>;
}
function actionOperation(action: Action, spec: ResourceSpec, path: string, api: ReturnType<typeof useAPI>, t: ReturnType<typeof useText>): Operation {
  return { title: t(action.name), dangerous: action.dangerous, fields: action.fields ?? [], initial: action.initial as NativeStruct | undefined,
    execute: async (values, signal) => {
      const base = action.collection ? (action.suffix === 'passwords' ? '' : spec.name) : path;
      const target = [base, action.suffix].filter(Boolean).join('/');
      if (action.binary) {
        const response = await request(api.client, api.installation.accountId, 'GET', target, Object.fromEntries(Object.entries(values).map(([key, value]) => [key, String(value)])), undefined, signal);
        if (response.status >= 400) decodeDocument(response.status, response.body);
        const url = URL.createObjectURL(new Blob([new Uint8Array(response.body)], { type: 'application/octet-stream' }));
        const link = document.createElement('a'); link.href = url; link.download = `${path.split('/').pop()}.lic`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
        return { meta: { status: t('completed') } };
      }
      return api.call(action.method, target, {}, actionPayload(action, values), signal);
    },
  };
}
function Detail({ spec, path, onBack }: { spec: ResourceSpec; path: string; onBack?: () => void }) {
  const t = useText(), api = useAPI(), navigate = usePluginNavigate(), { environment } = usePluginContext();
  const [operation, setOperation] = useState<Operation>(), [result, setResult] = useState<Document>(), [relation, setRelation] = useState<Relationship>(), [factor, setFactor] = useState<Resource>(), [running, setRunning] = useState(false), [failure, setFailure] = useState<unknown>(), [raw, setRaw] = useState(false);
  const state = usePlatformQuery(`lic:detail:${path}`, (signal) => api.call('GET', path, {}, undefined, signal));
  const resource = listData(state.value)[0], back = onBack ?? (() => navigate(spec.name));
  if (factor && relation?.type === 'second-factors') return <Detail key={factor.id} spec={byName('second-factors')!} path={`${path}/second-factors/${encodeURIComponent(factor.id)}`} onBack={() => setFactor(undefined)} />;
  if (state.error !== undefined) return <Stack gap={12}><Button variant="outline" onClick={back}>{t('back')}</Button><ErrorNotice error={state.error} retry={state.refresh} /></Stack>;
  if (!resource) return <Loading />;
  const blocked = environment === 'standalone';
  const deletion = () => setOperation({ title: t('delete'), dangerous: true, fields: spec.name === 'second-factors' ? [{ name: 'otp', secret: true }, { name: 'password', secret: true }] : [],
    execute: (values, signal) => api.call('DELETE', path, {}, spec.name === 'second-factors' ? { meta: values } : undefined, signal), success: back });
  const edit = () => {
    if (spec.name === 'second-factors') setOperation({ title: t('edit'), fields: [{ name: 'enabled', kind: 'bool', required: true }, { name: 'otp', required: true, secret: true }], initial: { enabled: !!resource.attributes?.enabled }, execute: (values, signal) => api.call('PATCH', path, {}, { data: { type: spec.type, id: resource.id, attributes: { enabled: values.enabled } }, meta: { otp: values.otp } }, signal) });
    else { const op = resourceOperation(spec, path, api, resource); setOperation({ ...op, title: `${t('edit')} · ${t(spec.name)}`, submitLabel: t('save') }); }
  };
  // settle shows what an action changed: a secret or a verdict stays on
  // screen; anything else is a notice and the refreshed record itself.
  const settle = (document: Document) => {
    state.refresh();
    const verdict = typeof document.meta?.valid === 'boolean';
    if (verdict || Object.keys(secretsOf(document)).length || document.meta?.warning) setResult(document);
    else toast.success(t('done'));
  };
  const tabs = ['overview', ...(spec.relationships ?? []).map((rel) => rel.name), 'raw'];
  const created = typeof resource.attributes?.created === 'string' ? new Date(resource.attributes.created) : undefined;
  return <Stack gap={16}>
    <EntityHeader title={labelOf(resource)} status={resource.attributes?.status !== undefined ? <Status value={resource.attributes.status} /> : undefined}
      meta={<MetaList><MetaItem label="ID" mono>{resource.id}</MetaItem>{created && <MetaItem label={t('created')}><Timestamp value={created} /></MetaItem>}</MetaList>}
      actions={<div style={row}><Button variant="outline" onClick={back}>{t('back')}</Button><Button variant="outline" onClick={state.refresh}>{t('refresh')}</Button>{!spec.noEdit && <Button disabled={blocked} onClick={edit}>{t('edit')}</Button>}<Button variant="outline" disabled={blocked} onClick={deletion}>{t('delete')}</Button></div>}
      tabs={<TabBar aria-label={t('sections')}>{tabs.map((tab) => <TabItem asChild key={tab} aria-current={(relation?.name ?? 'overview') === tab || (tab === 'raw' && raw) ? 'page' : undefined}><button type="button" onClick={() => { setRaw(tab === 'raw'); setRelation(tab === 'overview' || tab === 'raw' ? undefined : spec.relationships?.find((rel) => rel.name === tab)); setFactor(undefined); }}>{t(tab)}</button></TabItem>)}</TabBar>} />
    {operation && <OperationForm key={operation.title} operation={operation} close={() => setOperation(undefined)} done={(document) => { setOperation(undefined); settle(document); }} />}
    {result && <Result document={result} dismiss={() => setResult(undefined)} />}
    {failure !== undefined && <ErrorNotice error={failure} />}
    {raw ? <ObjectView value={state.value} label={t('raw')} />
      : relation ? (relation.many ? <ResourceList key={relation.name} spec={byName(relation.type)!} path={`${path}/${relation.name}`} relationship={relation} parentPath={path} onChanged={state.refresh} onInspect={relation.type === 'second-factors' ? setFactor : undefined} />
        : <ToOne key={relation.name} relationship={relation} path={`${path}/${relation.name}`} onChanged={state.refresh} />)
      : <>
        {spec.name === 'licenses' && <LicenseSummary resource={resource} />}
        {!!spec.actions?.length && <Panel title={t('actions')}><div style={row}>{spec.actions.map((action) => <Button variant="outline" key={action.name} disabled={blocked || running} onClick={() => {
          const op = actionOperation(action, spec, path, api, t);
          // An action with nothing to fill and nothing to confirm runs at once.
          if (!op.fields.length && !op.dangerous && !action.removes) {
            setRunning(true); setOperation(undefined); setFailure(undefined); setResult(undefined);
            op.execute({}, new AbortController().signal).then(settle, setFailure).finally(() => setRunning(false));
            return;
          }
          setOperation({ ...op, initial: action.name === 'requestReset' ? { email: String(resource.attributes?.email ?? ''), deliver: true } : op.initial, success: action.removes ? back : undefined });
        }}>{t(action.name)}</Button>)}</div></Panel>}
        <Panel title={t('attributes')}><Attributes resource={resource} omit={['metadata', 'payload', 'created', ...(spec.name === 'licenses' ? ['status', 'uses', 'expiry', 'maxMachines'] : [])]} /></Panel>
        {resource.attributes?.metadata && Object.keys(resource.attributes.metadata as object).length > 0 && <Panel title={t('metadata')}><Value name="metadata" value={resource.attributes.metadata} /></Panel>}
        {spec.name === 'webhook-events' && <Panel title={t('payload')}><ObjectView value={payloadOf(resource.attributes?.payload) ?? state.value} label={t('payload')} /></Panel>}
      </>}
  </Stack>;
}
function cellText(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—';
  if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T/.test(value)) return new Date(value).toLocaleString();
  if (Array.isArray(value)) return value.join(', ');
  return typeof value === 'object' ? JSON.stringify(value) : String(value);
}
function payloadOf(value: unknown) { if (typeof value !== 'string') return value; try { return parseJSON(value); } catch { return value; } }
function ToOne({ relationship: rel, path, onChanged }: { relationship: Relationship; path: string; onChanged: () => void }) {
  const t = useText(), api = useAPI(), navigate = usePluginNavigate(), { environment } = usePluginContext();
  const state = usePlatformQuery(`lic:relation:${path}`, (signal) => api.call('GET', path, {}, undefined, signal));
  const [operation, setOperation] = useState<Operation>(), [result, setResult] = useState<Document>();
  const resource = listData(state.value)[0];
  return <Stack gap={12}>{operation && <OperationForm key={operation.title} operation={operation} close={() => setOperation(undefined)} done={() => { setOperation(undefined); toast.success(t('done')); state.refresh(); onChanged(); }} />}
    <Panel title={t(rel.name)} actions={rel.update && <Button disabled={environment === 'standalone'} size="sm" onClick={() => setOperation({ title: t('setRelationship'), fields: [{ name: 'id', nullable: rel.name !== 'policy', required: true }], initial: { id: resource?.id ?? null }, execute: (values, signal) => api.call(rel.update!, path, {}, { data: values.id ? { type: rel.type, id: values.id } : null }, signal) })}>{t('setRelationship')}</Button>}>
      {state.error !== undefined ? <ErrorNotice error={state.error} retry={state.refresh} /> : state.loading ? <Loading /> : resource ? <Stack gap={12}><div><Button variant="link" style={{ padding: 0 }} onClick={() => navigate(`${rel.type}/${encodeURIComponent(resource.id)}`)}>{labelOf(resource)}</Button></div><Attributes resource={resource} omit={['metadata']} /></Stack> : <EmptyState title={t('empty')} />}
    </Panel>
  </Stack>;
}
export const definition = definePlugin({ ...navigation, routes: [{ element: <Shell />, children: [
  { index: true, element: <AccountPage /> }, { path: 'account', element: <AccountPage /> }, { path: ':resource', element: <ResourcePage /> }, { path: ':resource/:id', element: <ResourcePage /> }, { path: '*', element: <EmptyState title="404" /> },
] }] });
