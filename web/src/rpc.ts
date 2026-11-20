import { create } from '@bufbuild/protobuf';
import { CallRequestSchema, InfoRequestSchema } from './gen/proto/lic/console/v1/console_pb';
import { ConsoleServiceClient } from './gen/proto/lic/console/v1/console_ws_pb';
import { accountPath, decodeDocument, type WireValue, wireJSON } from './model';

export type { CallResponse, Declared, InfoResponse as Info } from './gen/proto/lic/console/v1/console_pb';
export { CallRequestSchema, ConsoleServiceClient, InfoRequestSchema };
export const info = (client: ConsoleServiceClient, signal?: AbortSignal) =>
  client.info(create(InfoRequestSchema), { signal });
export const request = (
  client: ConsoleServiceClient,
  account: string,
  method: string,
  path: string,
  query: Record<string, string> = {},
  body?: WireValue,
  signal?: AbortSignal,
) =>
  client.call(
    create(CallRequestSchema, {
      method,
      path: accountPath(account, path),
      query,
      // The generated protobuf JSON codec encodes bytes as base64 for the bridge.
      body: body === undefined ? new Uint8Array() : new TextEncoder().encode(wireJSON(body)),
    }),
    { signal },
  );
export async function call(
  client: ConsoleServiceClient,
  account: string,
  method: string,
  path: string,
  query: Record<string, string> = {},
  body?: WireValue,
  signal?: AbortSignal,
) {
  const response = await request(client, account, method, path, query, body, signal);
  return decodeDocument(response.status, response.body);
}
