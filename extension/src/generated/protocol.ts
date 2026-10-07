/* Generated from schema/protocol.schema.json by scripts/gen-types.mjs. Do not edit. */

/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ActionName".
 */
export type ActionName =
  | 'navigate'
  | 'find_tab'
  | 'activate_tab'
  | 'list_tabs'
  | 'close_tab'
  | 'close_session'
  | 'go_back'
  | 'go_forward'
  | 'reload'
  | 'snapshot'
  | 'click'
  | 'fill'
  | 'select'
  | 'press_key'
  | 'scroll'
  | 'upload'
  | 'wait_for'
  | 'screenshot'
  | 'network_start'
  | 'network_requests'
  | 'network_request_detail'
  | 'network_stop'
  | 'handle_dialog'
  | 'evaluate'
  | 'cdp';
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ScrollArgs".
 */
export type ScrollArgs = ScrollArgs1 & ScrollArgs2;
export type ScrollArgs1 = {
  [k: string]: unknown;
};
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "SelectArgs".
 */
export type SelectArgs = SelectArgs1 & SelectArgs2;
export type SelectArgs1 = {
  [k: string]: unknown;
};
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "WaitForArgs".
 */
export type WaitForArgs = WaitForArgs1 & WaitForArgs2;
export type WaitForArgs1 = {
  [k: string]: unknown;
};

export interface BrowserBridgeProtocol {
  [k: string]: unknown;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "CDPArgs".
 */
export interface CDPArgs {
  /**
   * CDP method such as DOM.getDocument. Browser.* and Target.* are blocked
   */
  method: string;
  params?: {};
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ClickResult".
 */
export interface ClickResult {
  tag: string;
  text: string;
  dialog?: {
    type: 'alert' | 'confirm' | 'prompt' | 'beforeunload';
    message: string;
  };
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "CloseSessionResult".
 */
export interface CloseSessionResult {
  closed: number;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "CloseTabResult".
 */
export interface CloseTabResult {
  closed: boolean;
  released: boolean;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "Dialog".
 */
export interface Dialog {
  type: 'alert' | 'confirm' | 'prompt' | 'beforeunload';
  message: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "EmptyResult".
 */
export interface EmptyResult {}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "Error".
 */
export interface ErrorBody {
  code:
    | 'INVALID_REQUEST'
    | 'UNKNOWN_ACTION'
    | 'FORBIDDEN'
    | 'EXTENSION_NOT_CONNECTED'
    | 'VERSION_MISMATCH'
    | 'NO_CURRENT_TAB'
    | 'TAB_NOT_FOUND'
    | 'STALE_REF'
    | 'ELEMENT_NOT_FOUND'
    | 'AMBIGUOUS_SELECTOR'
    | 'ELEMENT_NOT_INTERACTABLE'
    | 'NAVIGATION_FAILED'
    | 'RESTRICTED_URL'
    | 'BLOCKED_HOST'
    | 'DIALOG_OPEN'
    | 'NO_DIALOG'
    | 'DETACHED_BY_USER'
    | 'TIMEOUT'
    | 'EVAL_ERROR'
    | 'CDP_ERROR'
    | 'INTERNAL';
  message: string;
  hint?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "EvaluateArgs".
 */
export interface EvaluateArgs {
  /**
   * JavaScript run in the page's main world. Top-level await is allowed
   */
  code: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "EvaluateResult".
 */
export interface EvaluateResult {
  type: string;
  value: unknown;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "EventFrame".
 */
export interface EventFrame {
  type: 'event';
  name: 'tab.closed' | 'dialog.opened' | 'debugger.detached';
  data?: unknown;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "FillArgs".
 */
export interface FillArgs {
  /**
   * Element ref from snapshot (@e12) or a CSS selector that matches exactly one element
   */
  selector: string;
  /**
   * Text to put in the field. Empty string clears it
   */
  value: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "FillResult".
 */
export interface FillResult {
  mode: 'value' | 'contenteditable';
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "FindTabArgs".
 */
export interface FindTabArgs {
  /**
   * Host to match: example.com also matches www.example.com. Path is ignored
   */
  url?: string;
  /**
   * Borrow the tab the user is looking at instead of searching the session's tabs
   */
  active?: boolean;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "FindTabResult".
 */
export interface FindTabResult {
  tabId: number;
  url: string;
  title: string;
  borrowed: boolean;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "HandleDialogArgs".
 */
export interface HandleDialogArgs {
  accept: boolean;
  /**
   * Text to type into a prompt() dialog
   */
  promptText?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "Hello".
 */
export interface Hello {
  type: 'hello';
  protocolVersion: 2;
  extensionVersion: string;
  extensionId: string;
  browser: 'chrome' | 'edge';
  actions: string[];
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ListTabsResult".
 */
export interface ListTabsResult {
  tabs: {
    tabId: number;
    url: string;
    title: string;
    current: boolean;
    borrowed: boolean;
  }[];
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "NavigateArgs".
 */
export interface NavigateArgs {
  /**
   * http(s) URL or about:blank
   */
  url: string;
  /**
   * Open a new background tab instead of reusing the current tab
   */
  newTab?: boolean;
  /**
   * Tab group title, used only when the group is created. Defaults to the session name
   */
  groupTitle?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "NetworkFilterArgs".
 */
export interface NetworkFilterArgs {
  /**
   * Only requests whose URL contains this
   */
  filter?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "NetworkRequestDetailArgs".
 */
export interface NetworkRequestDetailArgs {
  requestId: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "NetworkRequestDetailResult".
 */
export interface NetworkRequestDetailResult {
  request: {
    url: string;
    method: string;
    headers: {
      [k: string]: string;
    };
    postData?: string;
  };
  response?: {
    status: number;
    headers: {
      [k: string]: string;
    };
    mimeType: string;
  };
  body: string;
  bodyBase64Encoded: boolean;
  bodyError?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "NetworkRequestsResult".
 */
export interface NetworkRequestsResult {
  capturing: boolean;
  count: number;
  requests: {
    requestId: string;
    url: string;
    method: string;
    status: number;
    mimeType: string;
    completed: boolean;
  }[];
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "NoArgs".
 */
export interface NoArgs {}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "PageResult".
 */
export interface PageResult {
  url: string;
  title: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "PingFrame".
 */
export interface PingFrame {
  type: 'ping';
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "PongFrame".
 */
export interface PongFrame {
  type: 'pong';
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "PressKeyArgs".
 */
export interface PressKeyArgs {
  /**
   * Enter, Escape, Tab, ArrowDown, Control+A ...
   */
  key: string;
  /**
   * Focus this element first
   */
  selector?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "PressKeyResult".
 */
export interface PressKeyResult {
  dialog?: {
    type: 'alert' | 'confirm' | 'prompt' | 'beforeunload';
    message: string;
  };
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "RequestFrame".
 */
export interface RequestFrame {
  type: 'request';
  id: string;
  session: string;
  action: string;
  args: unknown;
  deadline: number;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ResponseFrame".
 */
export interface ResponseFrame {
  type: 'response';
  id: string;
  ok: boolean;
  data?: unknown;
  error?: {
    code:
      | 'INVALID_REQUEST'
      | 'UNKNOWN_ACTION'
      | 'FORBIDDEN'
      | 'EXTENSION_NOT_CONNECTED'
      | 'VERSION_MISMATCH'
      | 'NO_CURRENT_TAB'
      | 'TAB_NOT_FOUND'
      | 'STALE_REF'
      | 'ELEMENT_NOT_FOUND'
      | 'AMBIGUOUS_SELECTOR'
      | 'ELEMENT_NOT_INTERACTABLE'
      | 'NAVIGATION_FAILED'
      | 'RESTRICTED_URL'
      | 'BLOCKED_HOST'
      | 'DIALOG_OPEN'
      | 'NO_DIALOG'
      | 'DETACHED_BY_USER'
      | 'TIMEOUT'
      | 'EVAL_ERROR'
      | 'CDP_ERROR'
      | 'INTERNAL';
    message: string;
    hint?: string;
  };
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ScreenshotArgs".
 */
export interface ScreenshotArgs {
  format?: 'png' | 'jpeg';
  /**
   * JPEG quality, ignored for png
   */
  quality?: number;
  /**
   * Capture only this element
   */
  selector?: string;
  fullPage?: boolean;
  /**
   * Absolute file path to write. Parent folders are created and an existing file is overwritten. Default: ~/.browser-bridge/artifacts/
   */
  path?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ScreenshotCapture".
 */
export interface ScreenshotCapture {
  /**
   * Base64-encoded image bytes
   */
  data: string;
  mimeType: 'image/png' | 'image/jpeg';
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ScreenshotResult".
 */
export interface ScreenshotResult {
  path: string;
  sizeBytes: number;
  mimeType: string;
  width: number;
  height: number;
}
export interface ScrollArgs2 {
  /**
   * Scroll this element into view. Give either selector or direction
   */
  selector?: string;
  direction?: 'up' | 'down' | 'left' | 'right';
  /**
   * Pixels, only together with direction
   */
  amount?: number;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "ScrollResult".
 */
export interface ScrollResult {
  scrollX: number;
  scrollY: number;
}
export interface SelectArgs2 {
  /**
   * Element ref from snapshot (@e12) or a CSS selector that matches exactly one element
   */
  selector: string;
  /**
   * Option value. Give exactly one of value or label
   */
  value?: string;
  /**
   * Visible option text. Give exactly one of value or label
   */
  label?: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "SelectResult".
 */
export interface SelectResult {
  selected: {
    value: string;
    label: string;
  };
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "SelectorArgs".
 */
export interface SelectorArgs {
  /**
   * Element ref from snapshot (@e12) or a CSS selector that matches exactly one element
   */
  selector: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "SnapshotArgs".
 */
export interface SnapshotArgs {
  maxChars?: number;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "SnapshotResult".
 */
export interface SnapshotResult {
  url: string;
  title: string;
  tree: string;
  frames: {
    /**
     * 0-based index of the iframe in document order
     */
    frame: number;
    url: string;
    width: number;
    height: number;
  }[];
  truncated: boolean;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "TabResult".
 */
export interface TabResult {
  tabId: number;
  url: string;
  title: string;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "UploadArgs".
 */
export interface UploadArgs {
  /**
   * An input[type=file], as a ref or a CSS selector
   */
  selector: string;
  /**
   * Absolute paths of existing files
   *
   * @minItems 1
   */
  files: [string, ...string[]];
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "UploadResult".
 */
export interface UploadResult {
  fileCount: number;
}
export interface WaitForArgs2 {
  /**
   * Wait for this element to reach state
   */
  selector?: string;
  state?: 'visible' | 'hidden';
  /**
   * Wait until the page text contains this
   */
  text?: string;
  urlContains?: string;
  /**
   * Wait for the load event. Must be true
   */
  load?: true;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "WaitForResult".
 */
export interface WaitForResult {
  matched: boolean;
  elapsedMs: number;
}
/**
 * This interface was referenced by `BrowserBridgeProtocol`'s JSON-Schema
 * via the `definition` "Welcome".
 */
export interface Welcome {
  type: 'welcome';
  protocolVersion: 2;
  daemonVersion: string;
  blockedHosts: string[];
}
