import { fromJson, type JsonValue } from '@bufbuild/protobuf';
import { SchemaSchema, Engine, type NativeStruct, type Schema } from '@gopherex/schemapb';
import type { FieldSpec } from './resources';

export function formSchema(fields: FieldSpec[], title: (key: string) => string = (key) => key): Schema {
  const convert = (field: FieldSpec): Record<string, unknown> => ({
    name: field.name, title: title(field.name), required: !!field.required, nullable: !!field.nullable, secret: !!field.secret,
    ...(field.fields ? { object: { schema: { fields: field.fields.map(convert) } } }
      : field.choices ? { choice: { options: field.choices.map((value) => ({ value: { stringValue: value }, label: value })) } }
      : { [field.kind ?? 'string']: {} }),
  });
  return fromJson(SchemaSchema, { id: { name: 'lic-console-form' }, fields: fields.map(convert) } as JsonValue);
}
export function validateDraft(schema: Schema, values: NativeStruct) {
  const engine = Engine.compile(schema);
  return engine.bake({ ...values });
}
export function relationFields(relations: { name: string; required?: boolean }[]): FieldSpec[] {
  return relations.map((relation) => ({ name: relation.name + 'Id', required: relation.required, nullable: !relation.required }));
}
export function relationshipBody(values: Record<string, unknown>, relations: { name: string; type: string }[]) {
  const relationships: Record<string, unknown> = {};
  for (const rel of relations) if (Object.hasOwn(values, rel.name + 'Id')) {
    const id = values[rel.name + 'Id'];
    relationships[rel.name] = { data: id ? { type: rel.type, id } : null };
  }
  return relationships;
}
