import type { ErrorBody } from '../generated/protocol';

export type ErrorCode = ErrorBody['code'];

/** An error that reaches the agent as {code, message, hint}. Anything else becomes INTERNAL. */
export class BridgeError extends Error {
  constructor(
    readonly code: ErrorCode,
    message: string,
    readonly hint?: string,
  ) {
    super(message);
  }
}

export function toErrorBody(e: unknown): ErrorBody {
  if (e instanceof BridgeError) {
    return e.hint ? { code: e.code, message: e.message, hint: e.hint } : { code: e.code, message: e.message };
  }
  return { code: 'INTERNAL', message: e instanceof Error ? e.message : String(e) };
}

export function blockedHost(): BridgeError {
  // No URL or title in the message: the point of blocking is that the agent learns nothing of the page.
  return new BridgeError('BLOCKED_HOST', 'the tab is on a host in blockedHosts', 'The user has blocked this site. Do not try to reach it another way');
}

export function noCurrentTab(session: string): BridgeError {
  return new BridgeError('NO_CURRENT_TAB', `session ${session} has no current tab`, 'Use navigate to open a page, or find_tab to pick an existing tab');
}
