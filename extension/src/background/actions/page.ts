import { browser } from 'wxt/browser';
import type {
  CDPArgs,
  ClickResult,
  Dialog,
  EvaluateArgs,
  EvaluateResult,
  FillArgs,
  FillResult,
  HandleDialogArgs,
  PressKeyArgs,
  PressKeyResult,
  ScreenshotArgs,
  ScreenshotCapture,
  ScrollArgs2,
  ScrollResult,
  SelectArgs2,
  SelectorArgs,
  SelectResult,
  SnapshotArgs,
  SnapshotResult,
  UploadArgs,
  UploadResult,
  WaitForArgs2,
  WaitForResult,
} from '../../generated/protocol';
import type { Clip, FillPlan, Point } from '../../page-agent/actions';
import type { Snapshot } from '../../page-agent/snapshot';
import { BridgeError, blockedHost } from '../errors';
import { isBlocked } from '../hosts';
import { keyEvents, parseKey } from '../keys';
import { isLostContext } from '../page';
import type { Handler } from '../router';
import { currentTab, type TabCtx } from './context';

const MAX_EVAL_RESULT_CHARS = 4 * 1024 * 1024;
const POLL_MS = 100;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * Runs work that may make the page open a JS dialog. Chrome answers no command until the dialog
 * closes, so the dialog's opening ends the wait instead.
 */
async function watchingDialogs<T>(ctx: TabCtx, tabId: number, work: () => Promise<T>): Promise<{ value: T } | { dialog: Dialog }> {
  const opened = ctx.cdp.waitForEvent<Dialog>(tabId, (method) => method === 'Page.javascriptDialogOpening', ctx.deadline);
  const done = work().then((value) => ({ value }));
  done.catch(() => {}); // once a dialog wins the race, a late failure here has nobody to tell
  try {
    return await Promise.race([done, opened.done.then((p) => ({ dialog: { type: p.type, message: p.message } }))]);
  } finally {
    opened.cancel();
  }
}

/** For actions whose result has no room for a dialog: the dialog becomes the answer. */
async function failingOnDialog<T>(ctx: TabCtx, tabId: number, work: () => Promise<T>): Promise<T> {
  const r = await watchingDialogs(ctx, tabId, work);
  if ('value' in r) return r.value;
  throw new BridgeError(
    'DIALOG_OPEN',
    `the page answered with a ${r.dialog.type} dialog, which stopped this action midway`,
    `Call handle_dialog with accept true or false, then check the page. The dialog says: "${r.dialog.message.slice(0, 200)}"`,
  );
}

async function pressKeys(ctx: TabCtx, tabId: number, spec: string): Promise<void> {
  for (const params of keyEvents(parseKey(spec))) await ctx.cdp.send(tabId, 'Input.dispatchKeyEvent', params);
}

const snapshot: Handler<TabCtx> = async (ctx, args: SnapshotArgs): Promise<SnapshotResult> => {
  const tabId = await currentTab(ctx);
  const start = await ctx.refs.next(tabId);
  const { next, ...result } = await ctx.page.call<Snapshot>(tabId, 'snapshot', { maxChars: args.maxChars ?? 20000, start });
  await ctx.refs.advance(tabId, next);
  if (isBlocked(result.url, ctx.blockedHosts())) throw blockedHost();
  return result;
};

const click: Handler<TabCtx> = async (ctx, args: SelectorArgs): Promise<ClickResult> => {
  const tabId = await currentTab(ctx);
  const { x, y, tag, text } = await ctx.page.call<Point>(tabId, 'clickPoint', args.selector);
  const mouse = (type: string, extra: Record<string, unknown> = {}) => ctx.cdp.send(tabId, 'Input.dispatchMouseEvent', { type, x, y, ...extra });
  const r = await watchingDialogs(ctx, tabId, async () => {
    await mouse('mouseMoved');
    await mouse('mousePressed', { button: 'left', buttons: 1, clickCount: 1 });
    await mouse('mouseReleased', { button: 'left', buttons: 0, clickCount: 1 });
  });
  return 'dialog' in r ? { tag, text, dialog: r.dialog } : { tag, text };
};

/** Replaces the selected content with value, the way typing would. */
async function typeOver(ctx: TabCtx, tabId: number, value: string): Promise<void> {
  if (value === '') await pressKeys(ctx, tabId, 'Delete');
  else await ctx.cdp.send(tabId, 'Input.insertText', { text: value });
}

// Focus, input and change handlers can open a dialog too.
const fill: Handler<TabCtx> = async (ctx, args: FillArgs): Promise<FillResult> => {
  const tabId = await currentTab(ctx);
  return failingOnDialog(ctx, tabId, () => fillIn(ctx, tabId, args));
};

async function fillIn(ctx: TabCtx, tabId: number, args: FillArgs): Promise<FillResult> {
  const plan = await ctx.page.call<FillPlan>(tabId, 'prepareFill', args.selector, args.value);
  if (plan.done) return { mode: plan.mode };
  await typeOver(ctx, tabId, args.value);
  if (plan.mode === 'value') return { mode: plan.mode };
  // Rich text editors sometimes keep part of the old content when everything is replaced at once
  // (seen with ProseMirror in a background tab). One more round fixes it.
  for (let round = 0; !(await ctx.page.call<boolean>(tabId, 'hasText', args.selector, args.value)); round++) {
    if (round === 1) {
      throw new BridgeError('INTERNAL', 'the editor did not take the text', 'Click the editor, press_key Control+A, then fill again');
    }
    await ctx.page.call(tabId, 'prepareFill', args.selector, args.value);
    await typeOver(ctx, tabId, args.value);
  }
  return { mode: plan.mode };
}

const select: Handler<TabCtx> = async (ctx, args: SelectArgs2): Promise<SelectResult> => {
  const tabId = await currentTab(ctx);
  return failingOnDialog(ctx, tabId, () => ctx.page.call<SelectResult>(tabId, 'select', args.selector, { value: args.value, label: args.label }));
};

const pressKey: Handler<TabCtx> = async (ctx, args: PressKeyArgs): Promise<PressKeyResult> => {
  const tabId = await currentTab(ctx);
  const events = keyEvents(parseKey(args.key)); // a bad key fails before anything is focused
  const r = await watchingDialogs(ctx, tabId, async () => {
    if (args.selector) await ctx.page.call(tabId, 'focus', args.selector);
    for (const params of events) await ctx.cdp.send(tabId, 'Input.dispatchKeyEvent', params);
  });
  return 'dialog' in r ? { dialog: r.dialog } : {};
};

const scroll: Handler<TabCtx> = async (ctx, args: ScrollArgs2): Promise<ScrollResult> => {
  const tabId = await currentTab(ctx);
  return ctx.page.call<ScrollResult>(tabId, 'scroll', args);
};

const upload: Handler<TabCtx> = async (ctx, args: UploadArgs): Promise<UploadResult> => {
  const tabId = await currentTab(ctx);
  const objectId = await ctx.page.element(tabId, 'fileInput', args.selector, args.files.length);
  try {
    await ctx.cdp.send(tabId, 'DOM.setFileInputFiles', { objectId, files: args.files });
  } finally {
    await ctx.cdp.send(tabId, 'Runtime.releaseObject', { objectId }).catch(() => {});
  }
  return { fileCount: args.files.length };
};

/** One check of a wait_for condition on whatever document the tab shows right now. */
async function matches(ctx: TabCtx, tabId: number, args: WaitForArgs2): Promise<boolean> {
  const tab = await browser.tabs.get(tabId).catch(() => null);
  if (!tab) throw new BridgeError('TAB_NOT_FOUND', 'the tab was closed');
  if (isBlocked(tab.url, ctx.blockedHosts())) throw blockedHost();
  const dialog = ctx.dialogs.of(tabId);
  if (dialog) throw new BridgeError('DIALOG_OPEN', `a ${dialog.type} dialog opened while waiting`, 'Call handle_dialog, then wait again');
  if (args.urlContains !== undefined) return (tab.url ?? '').includes(args.urlContains);
  // The tab, not the page: right after a click on a link the old document still reads
  // "complete", while the tab already reports the navigation as loading.
  if (args.load) return tab.status === 'complete';
  try {
    return await ctx.page.call<boolean>(tabId, 'check', { selector: args.selector, state: args.state, text: args.text });
  } catch (e) {
    if (isLostContext(e)) return false; // mid-navigation: look again in the next document
    throw e;
  }
}

const waitFor: Handler<TabCtx> = async (ctx, args: WaitForArgs2): Promise<WaitForResult> => {
  const tabId = await currentTab(ctx);
  const started = Date.now();
  for (;;) {
    if (await matches(ctx, tabId, args)) return { matched: true, elapsedMs: Date.now() - started };
    if (Date.now() + POLL_MS >= ctx.deadline) {
      throw new BridgeError('TIMEOUT', 'the condition was not met before timeoutMs', 'Retry with a larger timeoutMs, or take a snapshot to see the page');
    }
    await sleep(POLL_MS);
  }
};

const screenshot: Handler<TabCtx> = async (ctx, args: ScreenshotArgs): Promise<ScreenshotCapture> => {
  const tabId = await currentTab(ctx);
  const format = args.format ?? 'png';
  const params: Record<string, unknown> = { format };
  if (format === 'jpeg') params.quality = args.quality ?? 80;
  if (args.selector) {
    const { fits, ...box } = await ctx.page.call<Clip>(tabId, 'clip', args.selector);
    params.clip = { ...box, scale: 1 };
    params.captureBeyondViewport = !fits;
  } else if (args.fullPage) {
    const m = await ctx.cdp.send(tabId, 'Page.getLayoutMetrics');
    const size = m.cssContentSize ?? m.contentSize;
    params.clip = { x: 0, y: 0, width: size.width, height: size.height, scale: 1 };
    params.captureBeyondViewport = true;
  }
  const { data } = await ctx.cdp.send<{ data: string }>(tabId, 'Page.captureScreenshot', params);
  return { data, mimeType: format === 'jpeg' ? 'image/jpeg' : 'image/png' };
};

const evaluate: Handler<TabCtx> = async (ctx, args: EvaluateArgs): Promise<EvaluateResult> => {
  const tabId = await currentTab(ctx);
  let r: any;
  try {
    // userGesture counts as a user click, so calls such as video.play() are allowed.
    r = await ctx.cdp.send(tabId, 'Runtime.evaluate', { expression: args.code, replMode: true, awaitPromise: true, returnByValue: true, userGesture: true });
  } catch (e) {
    if (e instanceof BridgeError && e.code === 'CDP_ERROR') {
      throw new BridgeError('EVAL_ERROR', e.message, 'Return plain data that converts to JSON, e.g. a string, number, array or object of those');
    }
    throw e;
  }
  if (r.exceptionDetails) {
    const d = r.exceptionDetails;
    throw new BridgeError('EVAL_ERROR', d.exception?.description ?? d.text ?? 'the script threw', 'Fix the script. It runs in the page like code typed into the DevTools console');
  }
  const { type, subtype, value, unserializableValue } = r.result;
  const result: EvaluateResult = { type: subtype ?? type, value: value ?? unserializableValue ?? null };
  if ((JSON.stringify(result.value)?.length ?? 0) > MAX_EVAL_RESULT_CHARS) {
    throw new BridgeError('EVAL_ERROR', 'the result is larger than 4 MB', 'Return only the part you need');
  }
  return result;
};

const cdp: Handler<TabCtx> = async (ctx, args: CDPArgs): Promise<unknown> => {
  if (/^(Browser|Target)\./.test(args.method)) {
    throw new BridgeError('CDP_ERROR', `${args.method} is blocked: Browser.* and Target.* reach beyond the current tab`);
  }
  return ctx.cdp.send(await currentTab(ctx), args.method, args.params as Record<string, unknown> | undefined);
};

const handleDialog: Handler<TabCtx> = async (ctx, args: HandleDialogArgs): Promise<Dialog> => {
  const tabId = await currentTab(ctx);
  const dialog = ctx.dialogs.of(tabId);
  if (!dialog) throw new BridgeError('NO_DIALOG', 'the current tab has no open dialog');
  try {
    await ctx.cdp.send(tabId, 'Page.handleJavaScriptDialog', { accept: args.accept, ...(args.promptText !== undefined && { promptText: args.promptText }) });
  } catch (e) {
    if (e instanceof BridgeError && /no dialog/i.test(e.message)) {
      ctx.dialogs.closed(tabId);
      throw new BridgeError('NO_DIALOG', 'the dialog was already closed');
    }
    throw e;
  }
  ctx.dialogs.closed(tabId);
  return dialog;
};

export const pageHandlers = {
  snapshot,
  click,
  fill,
  select,
  press_key: pressKey,
  scroll,
  upload,
  wait_for: waitFor,
  screenshot,
  evaluate,
  cdp,
  handle_dialog: handleDialog,
};
