import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Providers } from './App';
import { type JSONValue, type Resource, saveSession } from './api';
import { Machines } from './machines';

const fetchMock = vi.fn();
const license: Resource = { id: 'license-1', type: 'licenses', attributes: { status: 'ACTIVE', protected: false } };
const machine: Resource = {
  id: 'machine-1',
  type: 'machines',
  attributes: {
    fingerprint: 'fp-1',
    status: 'ALIVE',
    metadata: { vendorUsageAck: { epoch: 1, seq: 41, hash: 'aa' } },
  },
};
const document = (data: JSONValue, meta?: JSONValue) =>
  new Response(JSON.stringify({ data, ...(meta ? { meta } : {}) }), {
    status: 200,
    headers: { 'Content-Type': 'application/vnd.api+json' },
  });
const path = (input: string) => new URL(input, window.location.origin).pathname.replace('/v1/accounts/default', '');
const calls = (at: string) => fetchMock.mock.calls.filter((call) => path(call[0]) === at);

function api(url: string) {
  const at = path(url);
  if (at === '/profile') return document({ id: 'user-1', type: 'users', attributes: { role: 'user' } });
  if (at === '/licenses/license-1/machines') return document([machine]);
  if (at === '/machines') return document({ ...machine, id: 'machine-2' });
  if (at.endsWith('/actions/check-out'))
    return document({ id: 'file', type: 'machine-files', attributes: { certificate: 'MACHINE FILE' } });
  if (at.endsWith('/actions/report-usage')) return document(machine, { ack: { epoch: 1, seq: 52, hash: 'bb' } });
  throw new Error(`Unexpected ${url}`);
}

function show(subject = license) {
  return render(
    <Providers>
      <MemoryRouter>
        <Machines license={subject} />
      </MemoryRouter>
    </Providers>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  sessionStorage.setItem('lic.portal.locale', 'en');
  saveSession({ token: 'session-secret', tokenId: 'session-1' });
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
  fetchMock.mockImplementation((url: string) => api(url));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('installations', () => {
  it('lists the installations with the usage the vendor accepted', async () => {
    show();
    expect(await screen.findByText('fp-1')).toBeTruthy();
    expect(screen.getByText('Usage accepted through event 41')).toBeTruthy();
  });

  it('activates an installation by its fingerprint and installation key', async () => {
    show();
    await screen.findByText('fp-1');
    fireEvent.change(screen.getByLabelText('Fingerprint'), { target: { value: ' fp-2 ' } });
    fireEvent.change(screen.getByLabelText('Installation public key'), { target: { value: 'KEY' } });
    fireEvent.click(screen.getByRole('button', { name: 'Activate' }));
    await waitFor(() => expect(calls('/machines')).toHaveLength(1));
    expect(JSON.parse(calls('/machines')[0][1].body)).toEqual({
      data: {
        type: 'machines',
        attributes: { fingerprint: 'fp-2', metadata: { publicKey: 'KEY' } },
        relationships: { license: { data: { type: 'licenses', id: 'license-1' } } },
      },
    });
  });

  it('offers no activation on a protected license', async () => {
    show({ ...license, attributes: { ...license.attributes, protected: true } });
    await screen.findByText('fp-1');
    expect(screen.queryByRole('button', { name: 'Activate' })).toBeNull();
  });

  it('downloads the lease without a bearer in the URL', async () => {
    URL.createObjectURL = vi.fn().mockReturnValue('blob:lease');
    URL.revokeObjectURL = vi.fn();
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    show();
    fireEvent.click(await screen.findByRole('button', { name: 'Download lease' }));
    await waitFor(() => expect(click).toHaveBeenCalledOnce());
    const [url, init] = calls('/machines/machine-1/actions/check-out')[0];
    expect(init.method).toBe('POST');
    expect(url).not.toContain('token=');
    expect(click.mock.contexts[0]).toHaveProperty('download', 'machine-1.lic');
    click.mockRestore();
  });

  it('hands in a usage report file and shows its acknowledgement', async () => {
    show();
    await screen.findByText('fp-1');
    const report = new File(['{"format":1,"seqFrom":42,"seqTo":52}'], 'report.json', { type: 'application/json' });
    fireEvent.change(screen.getByLabelText('Upload usage report', { selector: 'input' }), {
      target: { files: [report] },
    });
    expect(await screen.findByText('Report accepted through event 52')).toBeTruthy();
    expect(JSON.parse(calls('/machines/machine-1/actions/report-usage')[0][1].body)).toEqual({
      meta: { report: { format: 1, seqFrom: 42, seqTo: 52 } },
    });
  });
});
