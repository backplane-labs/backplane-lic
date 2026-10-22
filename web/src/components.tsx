import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { structToNative, type Baked, type NativeStruct } from '@gopherex/schemapb';
import { SchemaForm } from '@backplane-labs/backplane-schema-forms';
import { CodeEditor, JSONViewer } from '@backplane-labs/backplane-editors';
import { usePluginContext } from '@backplane-labs/backplane-plugin-sdk';
import { Button, Checkbox, EmptyState, Panel, Stack } from '@backplane-labs/backplane-ui';
import { formSchema, validateDraft } from './schemas';
import { json, parseJSON, redact, RequestError, secretsOf, type Document } from './model';
import type { FieldSpec } from './resources';
import { completeEditor, errorFieldPath, fieldRange, parseDraft, scaffoldDraft, validateEditor } from './editor-model';

export function useText() { const { t } = useTranslation('module.lic'); return (key: string, args?: Record<string, unknown>) => t(key, { defaultValue: key, ...args }); }
export const row = { display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' } as const;
export const mono = { fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' } as const;
export function ErrorNotice({ error, retry }: { error: unknown; retry?: () => void }) {
  const t = useText();
  return <div role="alert"><strong>{t('error')}</strong><pre style={{ ...mono, whiteSpace: 'pre-wrap', marginBlock: 8 }}>{error instanceof Error ? error.message : String(error)}</pre>{retry && <Button variant="outline" onClick={retry}>{t('retryRead')}</Button>}</div>;
}
export function ObjectView({ value, label }: { value: unknown; label: string }) {
  const { mode } = usePluginContext();
  return <div style={{ minWidth: 0, maxWidth: '100%' }}><JSONViewer label={label} value={json(redact(value))} mode={mode} height={280} /></div>;
}
export function Result({ document, dismiss }: { document: Document; dismiss: () => void }) {
  const t = useText(), secrets = secretsOf(document), [copied, setCopied] = useState('');
  return <Panel title={Object.keys(secrets).length ? t('oneTimeSecret') : t('result')} actions={<Button size="sm" variant="ghost" onClick={dismiss}>{t('dismiss')}</Button>}>
    {Object.keys(secrets).length > 0 && <Stack gap={8}><p>{t('secretHelp')}</p><pre tabIndex={0} style={{ ...mono, whiteSpace: 'pre-wrap' }}>{json(secrets)}</pre>
      <Button variant="outline" onClick={() => { void navigator.clipboard.writeText(json(secrets)).then(() => setCopied(t('copied')), () => setCopied(t('copyFailed'))); }}>{t('copy')}</Button>{copied && <p role="status">{copied}</p>}
    </Stack>}
    <ObjectView value={document} label={t('result')} />
  </Panel>;
}
export type Operation = { title: string; fields: FieldSpec[]; initial?: NativeStruct; dangerous?: boolean; submitLabel?: string; execute: (values: NativeStruct, signal: AbortSignal) => Promise<Document>; success?: (document: Document) => void };

/** The same descriptor drives ordinary controls and the advanced typed JSON editor. */
export function OperationForm({ operation, close, done }: { operation: Operation; close: () => void; done: (document: Document) => void }) {
  const t = useText(), { mode } = usePluginContext();
  const [advanced, setAdvanced] = useState(false), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [confirmed, setConfirmed] = useState(false);
  const baseline = useMemo(() => json(scaffoldDraft(operation.fields, operation.initial)), [operation]);
  const [text, setText] = useState(baseline), [error, setError] = useState<unknown>();
  const [reveal, setReveal] = useState<{ from: number; to: number; key: number }>();
  const active = useRef<AbortController | null>(null), mounted = useRef(true), formRoot = useRef<HTMLDivElement>(null);
  const errorsId = useId();
  const fieldErrors = error instanceof RequestError ? error.errors : [];
  const targetFor = (path: string[]) => {
    const root = formRoot.current;
    if (advanced) return root?.querySelector<HTMLElement>('[role="textbox"]');
    const title = t(path.at(-1) ?? '');
    const label = Array.from(root?.querySelectorAll<HTMLLabelElement>('label[for]') ?? []).find((label) => label.textContent?.replace(/\s*\*\s*$/, '').trim() === title);
    const control = label?.htmlFor ? document.getElementById(label.htmlFor) : undefined;
    return control && root?.contains(control) ? control : label?.closest('[data-slot="field"]')?.querySelector<HTMLElement>('input,textarea,button,[tabindex]');
  };
  useEffect(() => {
    const restore: (() => void)[] = [];
    fieldErrors.forEach((failure, index) => {
      const path = errorFieldPath(failure, operation.fields), target = path && targetFor(path);
      if (!target) return;
      const invalid = target.getAttribute('aria-invalid'), described = target.getAttribute('aria-describedby');
      target.setAttribute('aria-invalid', 'true'); target.setAttribute('aria-describedby', [described, `${errorsId}-${index}`].filter(Boolean).join(' '));
      restore.push(() => { if (invalid === null) target.removeAttribute('aria-invalid'); else target.setAttribute('aria-invalid', invalid); if (described === null) target.removeAttribute('aria-describedby'); else target.setAttribute('aria-describedby', described); });
    });
    return () => { for (const cleanup of restore.reverse()) cleanup(); };
  }, [error, advanced, operation, errorsId]);
  const schema = useMemo(() => formSchema(operation.fields, t), [operation.fields, t]);
  // This component is keyed by the caller's selected operation, never by query refresh.
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; active.current?.abort(); }; }, []);
  const cancel = () => { if (!busy && dirty && !window.confirm(t('unsaved'))) return; active.current?.abort(); if (busy) done({ meta: { warning: t('requestCancelled') } }); else close(); };
  const submit = async (values: NativeStruct, parentSignal?: AbortSignal) => {
    if (active.current || operation.dangerous && !confirmed) return;
    const controller = new AbortController(); active.current = controller;
    const abort = () => controller.abort(); parentSignal?.addEventListener('abort', abort, { once: true });
    setBusy(true); setError(undefined);
    try {
      const result = await operation.execute(values, controller.signal);
      if (!controller.signal.aborted && mounted.current) { operation.success?.(result); done(result); }
    } catch (failure) { if (!controller.signal.aborted && mounted.current) setError(failure); throw failure; }
    finally { parentSignal?.removeEventListener('abort', abort); active.current = null; if (mounted.current) setBusy(false); }
  };
  return <Panel title={operation.title} actions={<Button size="sm" variant="outline" onClick={cancel}>{t('cancel')}</Button>}>
    <div ref={formRoot}><Stack gap={16}>
      {operation.dangerous && <div role="note"><p>{t('confirmHelp')}</p><label style={row}><Checkbox checked={confirmed} onCheckedChange={(value) => setConfirmed(value === true)} />{t('confirmTitle', { action: operation.title })}</label></div>}
      <div style={row}><Button size="sm" variant="outline" disabled={busy || dirty} onClick={() => setAdvanced(!advanced)}>{t(advanced ? 'form' : 'advanced')}</Button></div>
      {advanced ? <Stack gap={12}><p>{t('jsonHelp')}</p>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => { try { const next = json(scaffoldDraft(operation.fields, parseDraft(text))); setText(next); setDirty(next !== baseline); } catch (failure) { setError(failure); } }}>{t('scaffold')}</Button>
        <CodeEditor label={t('advanced')} language="json" mode={mode} value={text} disabled={busy} reveal={reveal}
          complete={async ({ value, position, signal }) => signal.aborted ? null : completeEditor(operation.fields, value, position)}
          validate={async (value, signal) => signal.aborted ? [] : validateEditor(operation.fields, value)}
          onChange={(value) => { setText(value); setDirty(value !== baseline); }} height={360} />
        <Button disabled={busy || operation.dangerous && !confirmed} onClick={() => {
          let values: NativeStruct;
          try { const parsed = parseJSON(text); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(t('invalidJSON')); values = parsed as NativeStruct; }
          catch (failure) { setError(failure); return; }
          try {
            const diagnostics = validateEditor(operation.fields, text);
            if (diagnostics.some((item) => item.severity === 'error')) { setError(new RequestError(422, diagnostics.map((item) => ({ detail: item.message })))); return; }
            const outcome = validateDraft(schema, values);
            if (!outcome.baked) { setError(new RequestError(422, outcome.result.errors.map((e) => ({ detail: e.message, source: { pointer: e.path } })))); return; }
            void submit(structToNative(outcome.baked.values!)).catch(() => {});
          } catch (failure) { setError(failure); }
        }}>{busy ? t('loading') : operation.submitLabel ?? t('confirm')}</Button>
      </Stack> : <SchemaForm schema={schema} initialValues={operation.initial} label={operation.title} disabled={busy || operation.dangerous && !confirmed} submitLabel={operation.submitLabel ?? t('confirm')} onDirtyChange={setDirty} onSubmit={(baked: Baked, signal) => submit(structToNative(baked.values!), signal)} />}
      {fieldErrors.length > 0 ? <section role="alert" aria-label={t('validation')}><strong>{t('validation')}</strong><ul>{fieldErrors.map((failure, index) => {
        const path = errorFieldPath(failure, operation.fields);
        return <li key={index} id={`${errorsId}-${index}`}>
          {path && <Button variant="ghost" size="sm" onClick={() => { if (advanced) setReveal({ ...fieldRange(text, path), key: Date.now() }); const target = targetFor(path); target?.scrollIntoView({ block: 'center' }); target?.focus(); }}>{path.map((key) => t(key)).join(' / ')}</Button>}
          <code>{failure.source?.pointer ?? failure.source?.parameter}</code>{' '}{failure.detail ?? failure.title}{failure.code && ` (${failure.code})`}
        </li>;
      })}</ul></section> : error !== undefined && <ErrorNotice error={error} />}
    </Stack></div>
  </Panel>;
}
export function Loading({ children }: { children?: ReactNode }) { const t = useText(); return <EmptyState title={children ?? t('loading')} role="status" />; }
