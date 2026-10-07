package protocol

import "github.com/invopop/jsonschema"

// Action describes one command an agent can send. Args and Result are zero values of the structs
// that get reflected into JSON Schema; Result is what the agent receives in "data".
type Action struct {
	Name             string
	Description      string
	Args             any
	Result           any
	DefaultTimeoutMs int
}

const (
	navigationTimeoutMs = 30000
	actionTimeoutMs     = 15000
	MaxTimeoutMs        = 120000
)

type NoArgs struct{}

type EmptyResult struct{}

type NavigateArgs struct {
	URL        string `json:"url" jsonschema:"minLength=1" jsonschema_description:"http(s) URL or about:blank"`
	NewTab     bool   `json:"newTab,omitempty" jsonschema_description:"Open a new background tab instead of reusing the current tab"`
	GroupTitle string `json:"groupTitle,omitempty" jsonschema_description:"Tab group title, used only when the group is created. Defaults to the session name"`
}

type TabResult struct {
	TabID int    `json:"tabId"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type FindTabArgs struct {
	URL    string `json:"url,omitempty" jsonschema_description:"Host to match: example.com also matches www.example.com. Path is ignored"`
	Active bool   `json:"active,omitempty" jsonschema_description:"Borrow the tab the user is looking at instead of searching the session's tabs"`
}

type FindTabResult struct {
	TabID    int    `json:"tabId"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Borrowed bool   `json:"borrowed"`
}

type ListTabsResult struct {
	Tabs []TabEntry `json:"tabs"`
}

type TabEntry struct {
	TabID    int    `json:"tabId"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Current  bool   `json:"current"`
	Borrowed bool   `json:"borrowed"`
}

type CloseTabResult struct {
	Closed   bool `json:"closed"`
	Released bool `json:"released"`
}

type CloseSessionResult struct {
	Closed int `json:"closed"`
}

type PageResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type SnapshotArgs struct {
	MaxChars int `json:"maxChars,omitempty" jsonschema:"minimum=1,default=20000"`
}

type SnapshotResult struct {
	URL       string      `json:"url"`
	Title     string      `json:"title"`
	Tree      string      `json:"tree"`
	Frames    []FrameInfo `json:"frames"`
	Truncated bool        `json:"truncated"`
}

type FrameInfo struct {
	Frame  int    `json:"frame" jsonschema_description:"0-based index of the iframe in document order"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type SelectorArgs struct {
	Selector string `json:"selector" jsonschema:"minLength=1" jsonschema_description:"Element ref from snapshot (@e12) or a CSS selector that matches exactly one element"`
}

type Dialog struct {
	Type    string `json:"type" jsonschema:"enum=alert,enum=confirm,enum=prompt,enum=beforeunload"`
	Message string `json:"message"`
}

type ClickResult struct {
	Tag    string  `json:"tag"`
	Text   string  `json:"text"`
	Dialog *Dialog `json:"dialog,omitempty"`
}

type FillArgs struct {
	Selector string `json:"selector" jsonschema:"minLength=1" jsonschema_description:"Element ref from snapshot (@e12) or a CSS selector that matches exactly one element"`
	Value    string `json:"value" jsonschema_description:"Text to put in the field. Empty string clears it"`
}

type FillResult struct {
	Mode string `json:"mode" jsonschema:"enum=value,enum=contenteditable"`
}

type SelectArgs struct {
	Selector string `json:"selector" jsonschema:"minLength=1" jsonschema_description:"Element ref from snapshot (@e12) or a CSS selector that matches exactly one element"`
	Value    string `json:"value,omitempty" jsonschema:"oneof_required=byValue" jsonschema_description:"Option value. Give exactly one of value or label"`
	Label    string `json:"label,omitempty" jsonschema:"oneof_required=byLabel" jsonschema_description:"Visible option text. Give exactly one of value or label"`
}

type SelectResult struct {
	Selected SelectedOption `json:"selected"`
}

type SelectedOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type PressKeyArgs struct {
	Key      string `json:"key" jsonschema:"minLength=1" jsonschema_description:"Enter, Escape, Tab, ArrowDown, Control+A ..."`
	Selector string `json:"selector,omitempty" jsonschema_description:"Focus this element first"`
}

type PressKeyResult struct {
	Dialog *Dialog `json:"dialog,omitempty"`
}

type ScrollArgs struct {
	Selector  string `json:"selector,omitempty" jsonschema:"oneof_required=toElement" jsonschema_description:"Scroll this element into view. Give either selector or direction"`
	Direction string `json:"direction,omitempty" jsonschema:"oneof_required=byAmount,enum=up,enum=down,enum=left,enum=right"`
	Amount    int    `json:"amount,omitempty" jsonschema:"minimum=1,default=600" jsonschema_description:"Pixels, only together with direction"`
}

// JSONSchemaExtend forbids amount without direction.
func (ScrollArgs) JSONSchemaExtend(s *jsonschema.Schema) {
	s.DependentRequired = map[string][]string{"amount": {"direction"}}
}

type ScrollResult struct {
	ScrollX float64 `json:"scrollX"`
	ScrollY float64 `json:"scrollY"`
}

type UploadArgs struct {
	Selector string   `json:"selector" jsonschema:"minLength=1" jsonschema_description:"An input[type=file], as a ref or a CSS selector"`
	Files    []string `json:"files" jsonschema:"minItems=1" jsonschema_description:"Absolute paths of existing files"`
}

type UploadResult struct {
	FileCount int `json:"fileCount"`
}

type WaitForArgs struct {
	Selector    string `json:"selector,omitempty" jsonschema:"oneof_required=selector" jsonschema_description:"Wait for this element to reach state"`
	State       string `json:"state,omitempty" jsonschema:"enum=visible,enum=hidden,default=visible"`
	Text        string `json:"text,omitempty" jsonschema:"oneof_required=text" jsonschema_description:"Wait until the page text contains this"`
	URLContains string `json:"urlContains,omitempty" jsonschema:"oneof_required=urlContains"`
	Load        bool   `json:"load,omitempty" jsonschema:"oneof_required=load" jsonschema_description:"Wait for the load event. Must be true"`
}

// JSONSchemaExtend pins load to true and allows state only next to selector.
func (WaitForArgs) JSONSchemaExtend(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("load"); ok {
		p.Const = true
	}
	s.DependentRequired = map[string][]string{"state": {"selector"}}
}

type WaitForResult struct {
	Matched   bool `json:"matched"`
	ElapsedMs int  `json:"elapsedMs"`
}

type ScreenshotArgs struct {
	Format   string `json:"format,omitempty" jsonschema:"enum=png,enum=jpeg,default=png"`
	Quality  int    `json:"quality,omitempty" jsonschema:"minimum=0,maximum=100,default=80" jsonschema_description:"JPEG quality, ignored for png"`
	Selector string `json:"selector,omitempty" jsonschema_description:"Capture only this element"`
	FullPage bool   `json:"fullPage,omitempty"`
	Path     string `json:"path,omitempty" jsonschema_description:"Absolute file path to write. Parent folders are created and an existing file is overwritten. Default: ~/.browser-bridge/artifacts/"`
}

// ScreenshotCapture is what the extension sends back. The daemon writes the image to disk, reads
// its size from the image header and answers the agent with ScreenshotResult instead.
type ScreenshotCapture struct {
	Data     string `json:"data" jsonschema_description:"Base64-encoded image bytes"`
	MimeType string `json:"mimeType" jsonschema:"enum=image/png,enum=image/jpeg"`
}

type ScreenshotResult struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	MimeType  string `json:"mimeType"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type NetworkFilterArgs struct {
	Filter string `json:"filter,omitempty" jsonschema_description:"Only requests whose URL contains this"`
}

type NetworkRequestsResult struct {
	Capturing bool                    `json:"capturing"`
	Count     int                     `json:"count"`
	Requests  []NetworkRequestSummary `json:"requests"`
}

type NetworkRequestSummary struct {
	RequestID string `json:"requestId"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Status    int    `json:"status"`
	MimeType  string `json:"mimeType"`
	Completed bool   `json:"completed"`
}

type NetworkRequestDetailArgs struct {
	RequestID string `json:"requestId" jsonschema:"minLength=1"`
}

type NetworkRequestDetailResult struct {
	Request           NetworkRequestInfo   `json:"request"`
	Response          *NetworkResponseInfo `json:"response,omitempty"`
	Body              string               `json:"body"`
	BodyBase64Encoded bool                 `json:"bodyBase64Encoded"`
	BodyError         string               `json:"bodyError,omitempty"`
}

type NetworkRequestInfo struct {
	URL      string            `json:"url"`
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"`
	PostData string            `json:"postData,omitempty"`
}

type NetworkResponseInfo struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	MimeType string            `json:"mimeType"`
}

type HandleDialogArgs struct {
	Accept     bool   `json:"accept"`
	PromptText string `json:"promptText,omitempty" jsonschema_description:"Text to type into a prompt() dialog"`
}

type EvaluateArgs struct {
	Code string `json:"code" jsonschema:"minLength=1" jsonschema_description:"JavaScript run in the page's main world. Top-level await is allowed"`
}

type EvaluateResult struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type CDPArgs struct {
	Method string         `json:"method" jsonschema:"minLength=1" jsonschema_description:"CDP method such as DOM.getDocument. Browser.* and Target.* are blocked"`
	Params map[string]any `json:"params,omitempty"`
}

var Actions = []Action{
	{"navigate", "Open a URL in the session's current tab, or in a new background tab. Waits for the load event.", NavigateArgs{}, TabResult{}, navigationTimeoutMs},
	{"find_tab", "Make an existing tab the session's current tab: search the session's tabs by host, or borrow the tab the user is looking at.", FindTabArgs{}, FindTabResult{}, actionTimeoutMs},
	{"activate_tab", "Bring the current tab to the front and focus its window. Chrome holds back some things, such as starting video playback, until a tab has been visible.", NoArgs{}, TabResult{}, actionTimeoutMs},
	{"list_tabs", "List the session's tabs.", NoArgs{}, ListTabsResult{}, actionTimeoutMs},
	{"close_tab", "Close the current tab. A borrowed tab is only released, never closed.", NoArgs{}, CloseTabResult{}, actionTimeoutMs},
	{"close_session", "Close every tab of the session, release borrowed tabs and remove the tab group.", NoArgs{}, CloseSessionResult{}, actionTimeoutMs},
	{"go_back", "Go back in the current tab's history.", NoArgs{}, PageResult{}, navigationTimeoutMs},
	{"go_forward", "Go forward in the current tab's history.", NoArgs{}, PageResult{}, navigationTimeoutMs},
	{"reload", "Reload the current tab.", NoArgs{}, PageResult{}, navigationTimeoutMs},
	{"snapshot", "Read the page as an accessibility tree. Interactive elements get refs like @e12 to use as selector.", SnapshotArgs{}, SnapshotResult{}, actionTimeoutMs},
	{"click", "Click an element with a real mouse event.", SelectorArgs{}, ClickResult{}, actionTimeoutMs},
	{"fill", "Replace the text of an input, textarea or rich text editor.", FillArgs{}, FillResult{}, actionTimeoutMs},
	{"select", "Choose an option in a <select> by value or by visible label.", SelectArgs{}, SelectResult{}, actionTimeoutMs},
	{"press_key", "Press a key or key combination, optionally after focusing an element.", PressKeyArgs{}, PressKeyResult{}, actionTimeoutMs},
	{"scroll", "Scroll an element into view, or scroll the page in a direction.", ScrollArgs{}, ScrollResult{}, actionTimeoutMs},
	{"upload", "Set the files of an input[type=file].", UploadArgs{}, UploadResult{}, actionTimeoutMs},
	{"wait_for", "Wait until an element is visible or hidden, the page contains a text, the URL contains a string, or the page has loaded.", WaitForArgs{}, WaitForResult{}, actionTimeoutMs},
	{"screenshot", "Capture the visible tab, the full page or one element to an image file.", ScreenshotArgs{}, ScreenshotResult{}, actionTimeoutMs},
	{"network_start", "Start recording the current tab's network requests, discarding earlier ones.", NetworkFilterArgs{}, EmptyResult{}, actionTimeoutMs},
	{"network_requests", "List recorded network requests.", NetworkFilterArgs{}, NetworkRequestsResult{}, actionTimeoutMs},
	{"network_request_detail", "Headers and body of one recorded request.", NetworkRequestDetailArgs{}, NetworkRequestDetailResult{}, actionTimeoutMs},
	{"network_stop", "Stop recording and discard recorded requests.", NoArgs{}, EmptyResult{}, actionTimeoutMs},
	{"handle_dialog", "Accept or dismiss the open alert, confirm or prompt dialog.", HandleDialogArgs{}, Dialog{}, actionTimeoutMs},
	{"evaluate", "Run JavaScript in the page and return the result.", EvaluateArgs{}, EvaluateResult{}, actionTimeoutMs},
	{"cdp", "Send a raw Chrome DevTools Protocol command to the current tab.", CDPArgs{}, map[string]any{}, actionTimeoutMs},
}

func Lookup(name string) (Action, bool) {
	for _, a := range Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}
