import type { Dialog } from '../generated/protocol';
import type { Cdp } from './cdp';

/**
 * The JS dialog (alert, confirm, prompt, beforeunload) open in each tab. While one is open the
 * page runs no script, so every command but handle_dialog and cleanup is refused up front rather
 * than left to hang until its timeout.
 */
export class Dialogs {
  private open = new Map<number, Dialog>();

  constructor(cdp: Cdp) {
    cdp.subscribe({
      event: (tabId, method, params) => {
        if (method === 'Page.javascriptDialogOpening') this.open.set(tabId, { type: params.type, message: params.message });
        else if (method === 'Page.javascriptDialogClosed') this.open.delete(tabId);
      },
      detached: (tabId) => this.open.delete(tabId),
    });
  }

  of(tabId: number): Dialog | undefined {
    return this.open.get(tabId);
  }

  closed(tabId: number): void {
    this.open.delete(tabId);
  }
}
