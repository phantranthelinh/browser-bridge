package protocol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestResponseEnvelopeOmitsEmptyFields(t *testing.T) {
	ok, _ := json.Marshal(Response{OK: true, Data: json.RawMessage(`{"a":1}`)})
	if string(ok) != `{"ok":true,"data":{"a":1}}` {
		t.Fatalf("got %s", ok)
	}
	fail, _ := json.Marshal(Fail(ErrStaleRef, "@e12 is no longer in the page", "Take a new snapshot"))
	want := `{"ok":false,"error":{"code":"STALE_REF","message":"@e12 is no longer in the page","hint":"Take a new snapshot"}}`
	if string(fail) != want {
		t.Fatalf("got %s", fail)
	}
}

func TestHTTPStatus(t *testing.T) {
	cases := map[string]int{ErrInvalidRequest: 400, ErrUnknownAction: 400, ErrForbidden: 403, ErrStaleRef: 200, ErrTimeout: 200}
	for code, want := range cases {
		if got := HTTPStatus(&Error{Code: code}); got != want {
			t.Errorf("%s: got %d want %d", code, got, want)
		}
	}
	if HTTPStatus(nil) != 200 {
		t.Error("nil error must be 200")
	}
}

func TestDefaultExtensionIDMatchesManifestKey(t *testing.T) {
	key, err := os.ReadFile("../../../extension/manifest-key.txt")
	if err != nil {
		t.Fatal(err)
	}
	id, err := ExtensionIDFromKey(string(key))
	if err != nil {
		t.Fatal(err)
	}
	if id != DefaultExtensionID {
		t.Fatalf("manifest key gives %s, DefaultExtensionID is %s", id, DefaultExtensionID)
	}
}
