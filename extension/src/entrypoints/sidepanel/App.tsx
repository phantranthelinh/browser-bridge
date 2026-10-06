import { useEffect, useState, type FormEvent } from 'react';
import { browser } from 'wxt/browser';
import { DEFAULT_DAEMON_URL, KEYS, type ConnectionStatus, type LogEntry, type Sessions } from '../../shared/state';

/** A storage value that re-renders whenever the service worker changes it. */
function useStored<T>(area: 'session' | 'local', key: string): T | undefined {
  const [value, setValue] = useState<T>();
  useEffect(() => {
    void browser.storage[area].get(key).then((r) => setValue(r[key] as T));
    const onChanged = (changes: Record<string, { newValue?: unknown }>, changedArea: string) => {
      if (changedArea === area && key in changes) setValue(changes[key]?.newValue as T);
    };
    browser.storage.onChanged.addListener(onChanged);
    return () => browser.storage.onChanged.removeListener(onChanged);
  }, [area, key]);
  return value;
}

export function App() {
  const conn = useStored<ConnectionStatus>('session', KEYS.connection);
  const sessions = useStored<Sessions>('session', KEYS.sessions) ?? {};
  const log = useStored<LogEntry[]>('session', KEYS.log) ?? [];
  const savedUrl = useStored<string>('local', KEYS.daemonUrl);
  const [draft, setDraft] = useState<string | null>(null);
  const url = draft ?? savedUrl ?? DEFAULT_DAEMON_URL;

  const save = (e: FormEvent) => {
    e.preventDefault();
    void browser.storage.local.set({ [KEYS.daemonUrl]: url.trim() });
    setDraft(null);
  };

  const state = conn?.state ?? 'disconnected';
  return (
    <main>
      <h2>Connection</h2>
      <div>
        <span className={`state ${state}`} data-testid="connection-state">{state}</span> <span className="muted">{conn?.url}</span>
      </div>
      <div className="muted">
        daemon {conn?.daemonVersion ?? '?'} · extension {browser.runtime.getManifest().version}
        {conn?.blockedHosts?.length ? ` · ${conn.blockedHosts.length} blocked hosts` : ''}
      </div>
      {conn?.error && <div className="error">{conn.error}</div>}

      <h2>Sessions</h2>
      {Object.keys(sessions).length === 0 && <div className="muted">none</div>}
      {Object.entries(sessions).map(([name, s]) => (
        <div key={name}>
          <strong>{name}</strong>
          {s.stopped && <span className="error"> stopped by Cancel</span>}
          <ul>
            {[...s.tabIds, ...s.borrowedTabIds].map((id) => (
              <li key={id}>
                tab {id}
                {id === s.currentTabId && ' · current'}
                {s.borrowedTabIds.includes(id) && ' · borrowed'}
              </li>
            ))}
          </ul>
        </div>
      ))}

      <h2>Last commands</h2>
      <table>
        <tbody>
          {log.map((e, i) => (
            <tr key={i}>
              <td className="muted">{new Date(e.at).toLocaleTimeString()}</td>
              <td>{e.session}</td>
              <td>{e.action}</td>
              <td>{e.selector}</td>
              <td className="muted">{e.ms} ms</td>
              <td className={e.code ? 'error' : ''}>{e.code ?? 'ok'}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h2>Daemon address</h2>
      <form onSubmit={save}>
        <input value={url} onChange={(e) => setDraft(e.target.value)} spellCheck={false} />
        <button type="submit">Save</button>
      </form>
    </main>
  );
}
