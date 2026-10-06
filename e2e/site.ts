import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { extname, join, normalize, sep } from 'node:path';
import { TESTPAGE_DIR } from './paths';

export interface Site {
  port: number;
  /** The same server answers on any loopback name: 127.0.0.1, localhost, blocked.localhost. */
  url(path: string, host?: string): string;
  close(): Promise<void>;
}

const types: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json',
};

/** Serves testpage/ plus /redirect?to=<url>, which answers 302 to that URL. */
export async function startSite(): Promise<Site> {
  const server = createServer(async (req, res) => {
    const u = new URL(req.url ?? '/', 'http://site');
    if (u.pathname === '/redirect') {
      res.writeHead(302, { Location: u.searchParams.get('to') ?? '/' }).end();
      return;
    }
    const file = normalize(join(TESTPAGE_DIR, u.pathname === '/' ? 'index.html' : u.pathname));
    if (!file.startsWith(TESTPAGE_DIR + sep)) {
      res.writeHead(403).end();
      return;
    }
    try {
      const body = await readFile(file);
      res.writeHead(200, { 'Content-Type': types[extname(file)] ?? 'application/octet-stream' }).end(body);
    } catch {
      res.writeHead(404).end('not found');
    }
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = (server.address() as AddressInfo).port;
  return {
    port,
    url: (path, host = '127.0.0.1') => `http://${host}:${port}${path}`,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}
