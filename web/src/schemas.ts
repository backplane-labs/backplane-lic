import { fromJson, type JsonValue } from '@bufbuild/protobuf';
import { Engine, type NativeStruct, type Schema, SchemaSchema } from '@gopherex/schemapb';
import type { WireValue } from './model';
import type { FieldSpec } from './resources';

export function formSchema(fields: FieldSpec[], title: (key: string) => string = (key) => key): Schema {
  const convert = (field: FieldSpec): JsonValue => ({
    name: field.name,
    title: title(field.label ?? field.name),
    required: !!field.required,
    nullable: !!field.nullable,
    secret: !!field.secret,
    ...(field.fields
      ? { object: { schema: { fields: field.fields.map(convert) } } }
      : field.options
        ? {
            choice: {
              options: field.options.map((option) => ({ value: { stringValue: option.value }, label: option.label })),
            },
          }
        : field.choices
          ? { choice: { options: field.choices.map((value) => ({ value: { stringValue: value }, label: value })) } }
          : { [field.kind ?? 'string']: {} }),
  });
  return fromJson(SchemaSchema, { id: { name: 'lic-console-form' }, fields: fields.map(convert) });
}
export function validateDraft(schema: Schema, values: NativeStruct) {
  const engine = Engine.compile(schema);
  return engine.bake({ ...values });
}
export function relationFields(relations: { name: string; type: string; required?: boolean }[]): FieldSpec[] {
  return relations.map((relation) => ({
    name: `${relation.name}Id`,
    required: relation.required,
    nullable: !relation.required,
    relatesTo: relation.type,
  }));
}
export function relationshipBody(values: Record<string, WireValue>, relations: { name: string; type: string }[]) {
  const relationships: Record<string, WireValue> = {};
  for (const rel of relations)
    if (Object.hasOwn(values, `${rel.name}Id`)) {
      const id = values[`${rel.name}Id`];
      relationships[rel.name] = { data: id ? { type: rel.type, id } : null };
    }
  return relationships;
}
