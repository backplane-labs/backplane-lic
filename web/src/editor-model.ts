import type { Native, NativeStruct } from '@gopherex/schemapb';
import type { EditorCompletionResult, EditorDiagnostic } from '@backplane-labs/backplane-editors';
import { json, parseJSON, type APIError } from './model.ts';
import { formSchema, validateDraft } from './schemas.ts';
import type { FieldSpec, Action } from './resources.ts';

/** Only fill required inputs. Optional defaults must remain server-owned. */
export function scaffoldDraft(fields: FieldSpec[], values: NativeStruct = {}): NativeStruct {
  const result = { ...values };
  for (const field of fields) {
    if (Object.hasOwn(result, field.name)) {
      const value = result[field.name];
      if (field.fields && value && typeof value === 'object' && !Array.isArray(value)) result[field.name] = scaffoldDraft(field.fields, value as NativeStruct);
    } else if (field.required) result[field.name] = scaffoldField(field);
  }
  return result;
}
function scaffoldField(field: FieldSpec): Native {
  if (field.fields) return scaffoldDraft(field.fields);
  if (field.options?.length) return field.options[0].value;
  if (field.choices?.length) return field.choices[0];
  if (field.kind === 'bool') return false;
  if (field.kind === 'int64') return 0n;
  if (field.kind === 'json') return {};
  return '';
}
export function parseDraft(text: string): NativeStruct {
  const value = parseJSON(text);
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Expected a JSON object');
  return value as NativeStruct;
}
function fieldsAt(fields: FieldSpec[], path: string[]): FieldSpec[] {
  let current = fields;
  for (const key of path) current = current.find((field) => field.name === key)?.fields ?? [];
  return current;
}
/** Find a token range without parsing numbers through Number. */
export function fieldRange(text: string, path: string[]) {
  const key = path.at(-1);
  if (!key) return { from: 0, to: Math.min(1, text.length) };
  const encoded = JSON.stringify(key), from = text.indexOf(encoded);
  return from < 0 ? { from: 0, to: Math.min(1, text.length) } : { from, to: from + encoded.length };
}
export function validateEditor(fields: FieldSpec[], text: string): EditorDiagnostic[] {
  let values: NativeStruct;
  try { values = parseDraft(text); }
  catch (error) { return [{ from: 0, to: Math.min(1, text.length), severity: 'error', message: error instanceof Error ? error.message : String(error) }]; }
  const errors: EditorDiagnostic[] = [];
  const check = (known: FieldSpec[], object: NativeStruct, path: string[]) => {
    for (const [key, value] of Object.entries(object)) {
      const field = known.find((item) => item.name === key);
      if (!field) errors.push({ ...fieldRange(text, [...path, key]), severity: 'error', message: `/${[...path, key].join('/')}: unknown field` });
      else if (field.fields && value && typeof value === 'object' && !Array.isArray(value)) check(field.fields, value as NativeStruct, [...path, key]);
    }
  };
  check(fields, values, []);
  try {
    const outcome = validateDraft(formSchema(fields), values);
    errors.push(...outcome.result.errors.map((error): EditorDiagnostic => {
      const path = error.pathSegments.map((segment) => String(segment.segment.value ?? ''));
      return { ...fieldRange(text, path), severity: 'error', message: `${error.path}: ${error.message}` };
    }));
  } catch (error) { errors.push({ from: 0, to: 0, severity: 'error', message: error instanceof Error ? error.message : String(error) }); }
  return errors;
}
type Frame = { path: string[]; array: boolean; phase: 'key' | 'colon' | 'value' | 'after'; key?: string; seen: Set<string> };
/** A tolerant cursor scanner: works while JSON is incomplete and ignores braces in strings. */
function cursorContext(text: string, position: number) {
  const stack: Frame[] = [];
  let stringStart: number | undefined;
  const finish = () => { const frame = stack.at(-1); if (frame) frame.phase = 'after'; };
  for (let i = 0; i < position;) {
    const char = text[i], frame = stack.at(-1);
    if (/\s/.test(char)) { i++; continue; }
    if (char === '"') {
      const start = i++;
      while (i < position && text[i] !== '"') { if (text[i] === '\\') i++; i++; }
      if (i >= position) { stringStart = start; break; }
      const raw = text.slice(start, ++i);
      if (frame?.phase === 'key') { try { frame.key = JSON.parse(raw); } catch { return undefined; } frame.seen.add(frame.key!); frame.phase = 'colon'; }
      else finish();
      continue;
    }
    if (char === '{' || char === '[') {
      const path = frame ? frame.array ? [...frame.path, '*'] : [...frame.path, frame.key ?? ''] : [];
      stack.push({ path, array: char === '[', phase: char === '[' ? 'value' : 'key', seen: new Set() }); i++; continue;
    }
    if (char === '}' || char === ']') { stack.pop(); finish(); i++; continue; }
    if (char === ':') { if (frame) frame.phase = 'value'; i++; continue; }
    if (char === ',') { if (frame) { frame.phase = frame.array ? 'value' : 'key'; frame.key = undefined; } i++; continue; }
    while (i < position && !/[\s,}\]]/.test(text[i])) i++;
    if (i < position) finish(); else break;
  }
  return { frame: stack.at(-1), stringStart };
}
export function completeEditor(fields: FieldSpec[], text: string, position: number): EditorCompletionResult | null {
  const context = cursorContext(text, position), frame = context?.frame;
  if (!frame) return text.slice(0, position).trim() ? null : { from: 0, options: [{ label: '{}', type: 'keyword', apply: json(scaffoldDraft(fields)) }] };
  if (frame.array) return null;
  const quoted = context.stringStart !== undefined;
  const from = quoted ? context.stringStart! + 1 : position;
  const prefix = quoted ? text.slice(from, position) : '';
  const closingQuote = quoted && text.slice(position).trimStart().startsWith('"');
  if (frame.phase === 'key') {
    const options = fieldsAt(fields, frame.path).filter((field) => !frame.seen.has(field.name) && field.name.startsWith(prefix)).map((field) => ({
      label: field.name, type: 'property' as const,
      detail: field.required ? 'required' : 'optional',
      apply: quoted ? field.name + (closingQuote ? '' : '": ' + json(scaffoldField(field))) : JSON.stringify(field.name) + ': ' + json(scaffoldField(field)),
    }));
    return { from, options };
  }
  if (frame.phase === 'value') {
    const field = fieldsAt(fields, frame.path).find((item) => item.name === frame.key);
    if (!field) return null;
    const values: Native[] = field.choices ? [...field.choices] : (field.kind === 'bool' ? [true, false] : [scaffoldField(field)]);
    if (field.nullable) values.push(null);
    const options = values.filter((value) => !quoted || typeof value === 'string' && value.startsWith(prefix)).map((value) => ({ label: json(value), type: 'keyword' as const, apply: quoted ? String(value) + (closingQuote ? '' : '"') : json(value) }));
    return { from, options };
  }
  return null;
}
export function errorFieldPath(error: APIError, fields: FieldSpec[]): string[] | undefined {
  const pointer = error.source?.pointer ?? error.source?.parameter;
  if (!pointer) return undefined;
  let path = pointer.startsWith('/') ? pointer.slice(1).split('/').map((key) => key.replace(/~1/g, '/').replace(/~0/g, '~')) : pointer.split('.');
  if (path[0] === 'data') path.shift();
  if (path[0] === 'meta') path.shift();
  if (path[0] === 'relationships' && path[1]) path = [path[1] + 'Id'];
  let current = fields;
  const matched: string[] = [];
  for (const segment of path) {
    const normalized = segment.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase());
    const field = current.find((candidate) => candidate.name === segment || candidate.name === normalized);
    if (!field) break;
    matched.push(field.name); current = field.fields ?? [];
  }
  return matched.length ? matched : undefined;
}
export function actionPayload(action: Action, values: NativeStruct): unknown {
  const payload = { ...values };
  if (action.name === 'validateKey' && Object.hasOwn(payload, 'legacyEncrypted')) { payload.legacy_encrypted = payload.legacyEncrypted; delete payload.legacyEncrypted; }
  return action.meta ? { meta: payload } : Object.keys(payload).length ? payload : undefined;
}
