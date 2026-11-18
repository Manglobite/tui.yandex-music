package music

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAPI(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewWithEndpoints(Credentials{ClientID: "test-client", ClientSecret: "test-secret", StreamKey: "test-stream"}, server.URL, server.URL)
}

func TestAPIContracts(t *testing.T) {
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "OAuth test-token" {
			t.Error("missing auth")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/search":
			if r.URL.Query().Get("text") != "Тихий город" {
				t.Error("query encoding")
			}
			_, _ = w.Write([]byte(`{"result":{"tracks":{"results":[{"id":12,"title":"title","durationMs":123000,"albums":[{"id":"3"}]}]}}}`))
		case "/users/1/playlists/2/change":
			if r.Form.Get("revision") != "7" {
				t.Error("revision")
			}
			var diff []map[string]any
			if err := json.Unmarshal([]byte(r.Form.Get("diff")), &diff); err != nil {
				t.Error(err)
			}
			if diff[0]["op"] != "insert" {
				t.Error("diff")
			}
			_, _ = w.Write([]byte(`{"result":{}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	a.token = "test-token"
	tracks, err := a.Search(context.Background(), "Тихий город", 0)
	if err != nil || len(tracks) != 1 || tracks[0].ID != "12" {
		t.Fatalf("search %v %v", tracks, err)
	}
	p := Playlist{Kind: "2", Revision: 7}
	p.Owner.UID = "1"
	if err = a.Mutate(context.Background(), "1", "insert", "", p, tracks[0], 0); err != nil {
		t.Fatal(err)
	}
	if err = a.Mutate(context.Background(), "999", "delete", "", p, Track{}, 0); err == nil {
		t.Fatal("foreign playlist allowed")
	}
}

func TestOAuthDeviceScopes(t *testing.T) {
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/device/code" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if got := r.PostForm.Get("scope"); got != "music:content music:read music:write" {
			t.Errorf("unexpected scopes: %q", got)
		}
		if r.PostForm.Get("optional_scope") != "" {
			t.Error("unexpected optional scopes")
		}
		if r.PostForm.Get("client_id") != "test-client" || r.PostForm.Get("device_id") != "test-device" {
			t.Error("missing client or device ID")
		}
		_, _ = w.Write([]byte(`{"device_code":"test-code","user_code":"test-user-code","expires_in":300,"interval":5}`))
	})
	code, err := a.Device(context.Background(), "test-device")
	if err != nil || code.Device != "test-code" {
		t.Fatalf("device authorization: %v %v", code, err)
	}
}

func TestOAuthAndErrorRedaction(t *testing.T) {
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("token leaked to OAuth")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.PostForm.Get("client_id") != "test-client" || r.PostForm.Get("client_secret") != "test-secret" {
			t.Error("configured credentials were not sent")
		}
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"authorization_pending","error_description":"secret-value"}`))
	})
	a.token = "private"
	_, err := a.RequestToken(context.Background(), "device_code", "device-secret")
	var ae Error
	if !errors.As(err, &ae) || ae.Code != "authorization_pending" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("%v", err)
	}
}

func TestMissingCredentialsDoNotSendRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	a := NewWithEndpoints(Credentials{}, server.URL, server.URL)
	ctx := context.Background()
	if _, err := a.Device(ctx, "test-device"); err == nil || !strings.Contains(err.Error(), "YAMUSIC_CLIENT_ID") {
		t.Fatal("expected OAuth configuration error", err)
	}
	if _, err := a.RequestToken(ctx, "refresh_token", "test-refresh"); err == nil || !strings.Contains(err.Error(), "YAMUSIC_CLIENT_SECRET") {
		t.Fatal("expected token configuration error", err)
	}
	if _, err := a.Resolve(ctx, "12"); err == nil || !strings.Contains(err.Error(), "YAMUSIC_STREAM_KEY") {
		t.Fatal("expected playback configuration error", err)
	}
}

func TestSectionAPIContracts(t *testing.T) {
	calls := map[string]int{}
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/users/7/playlists/list":
			_, _ = w.Write([]byte(`{"result":[{"kind":1,"title":"Mine","owner":{"uid":7}}]}`))
		case "/feed":
			_, _ = w.Write([]byte(`{"result":{"generatedPlaylists":[{"ready":false,"data":{"kind":9,"owner":{"uid":8}}},{"ready":true,"data":null},{"ready":true,"data":{"kind":"2","title":"Day","owner":{"uid":"8"}}},{"ready":true,"data":{"kind":2,"owner":{"uid":8}}},{"ready":true,"data":{"kind":4}}]}}`))
		case "/landing3":
			if r.URL.Query().Get("blocks") != "new-playlists,mixes" {
				t.Error("blocks query")
			}
			_, _ = w.Write([]byte(`{"result":{"blocks":[{"entities":[{"type":"album","data":{"kind":90,"owner":{"uid":8}}},{"type":"mix-link","data":{"url":"ignored"}},{"type":"unknown","data":[]},{"type":"playlist","data":null},{"type":"playlist","data":[]},{"type":"playlist","data":{"kind":3,"title":"Editorial","owner":{"uid":8}}}]},{"entities":[{"type":"playlist","data":{"kind":"3","owner":{"uid":"8"}}},{"type":"playlist","data":{"kind":3,"owner":{"uid":9}}}]}]}}`))
		case "/users/8/playlists/2":
			_, _ = w.Write([]byte(`{"result":{"kind":2,"owner":{"uid":8},"tracks":[]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	for section, count := range []int{1, 1, 2} {
		lists, err := a.SectionCatalog(ctx, "7", section)
		if err != nil || len(lists) != count {
			t.Fatalf("section %d: %v %v", section, lists, err)
		}
		if section == SectionForYou {
			p, err := a.Playlist(ctx, lists[0])
			if err != nil || p.Owner.UID != "8" || p.Kind != "2" {
				t.Fatalf("foreign owner playlist: %v %v", p, err)
			}
		}
	}
	for _, section := range []int{SectionPersonal, SectionForYou} {
		lists, err := a.SectionCatalog(ctx, "", section)
		if err != nil || len(lists) != 0 {
			t.Fatal("guest private section")
		}
	}
	if calls["/feed"] != 1 || calls["/users/7/playlists/list"] != 1 {
		t.Fatal("guest requested private API")
	}
	if lists, err := a.SectionCatalog(ctx, "", SectionCollections); err != nil || len(lists) != 2 {
		t.Fatalf("guest collections %v %v", lists, err)
	}
}

func TestSectionEmptyAndErrors(t *testing.T) {
	for _, body := range []string{`{"result":{}}`, `{"result":null}`, `{"result":{"generatedPlaylists":[],"blocks":[]}}`, `{"error":"failed"}`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			a := testAPI(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			for _, section := range []int{SectionForYou, SectionCollections} {
				lists, err := a.SectionCatalog(context.Background(), "7", section)
				wantErr := body == `{"error":"failed"}` || body == `not-json`
				if (err != nil) != wantErr || len(lists) != 0 {
					t.Fatalf("section %d: %v %v", section, lists, err)
				}
			}
		})
	}
}

func TestPlaylistAPIRejectsInvalidIdentity(t *testing.T) {
	a := testAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":null}`))
	})
	p := Playlist{Kind: "2"}
	if _, err := a.Playlist(context.Background(), p); err == nil {
		t.Fatal("missing owner accepted")
	}
	p.Owner.UID = "8"
	if _, err := a.Playlist(context.Background(), p); err == nil {
		t.Fatal("missing response identity accepted")
	}
}
