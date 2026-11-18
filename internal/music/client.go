package music

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const musicURL = "https://api.music.yandex.net"
const oauthURL = "https://oauth.yandex.ru"

type Credentials struct {
	ClientID, ClientSecret, StreamKey string
}

const (
	SectionPersonal = iota
	SectionForYou
	SectionCollections
)

type Client struct {
	credentials        Credentials
	base, oauth, token string
	http               *http.Client
}

func New(credentials Credentials) Client {
	return NewWithEndpoints(credentials, musicURL, oauthURL)
}

func NewWithEndpoints(credentials Credentials, music, oauth string) Client {
	return Client{credentials: credentials, base: music, oauth: oauth, http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (a Client) WithToken(token string) Client {
	a.token = token
	return a
}

type Error struct {
	Status int
	Code   string
}

func (e Error) Error() string {
	switch e.Status {
	case 401:
		return "Сессия истекла: войдите заново (A)"
	case 403:
		return "Нет доступа: проверьте подписку и регион"
	case 409:
		return "Плейлист изменился на сервере: обновите треки (L)"
	case 429:
		return "Лимит API: повторите позже"
	}
	return fmt.Sprintf("API: HTTP %d (%s)", e.Status, e.Code)
}
func (a Client) request(ctx context.Context, method, path string, form url.Values, out any, oauth bool) error {
	base := a.base
	if oauth {
		base = a.oauth
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return errors.New("некорректный запрос")
	}
	req.Header.Set("Accept-Language", "ru")
	req.Header.Set("User-Agent", "YamusicTUI/0.2")
	req.Header.Set("X-Yandex-Music-Client", "YandexMusicAndroid/24023621")
	if strings.HasPrefix(path, "/get-file-info") {
		req.Header.Set("X-Yandex-Music-Client", "YandexMusicDesktopAppWindows/5.95.0")
	}
	if !oauth && a.token != "" {
		req.Header.Set("Authorization", "OAuth "+a.token)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := a.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("сеть недоступна или истёк таймаут")
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return errors.New("не удалось прочитать ответ API")
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err = json.Unmarshal(data, &env); err != nil {
		return errors.New("API вернул не JSON")
	}
	if res.StatusCode >= 300 || len(env.Error) > 0 && string(env.Error) != "null" {
		code := "request_failed"
		var s string
		if json.Unmarshal(env.Error, &s) == nil {
			switch s {
			case "authorization_pending", "slow_down", "expired_token", "access_denied", "invalid_client", "invalid_grant":
				code = s
			}
		}
		return Error{res.StatusCode, code}
	}
	if out == nil {
		return nil
	}
	if !oauth {
		data = env.Result
	}
	if len(data) == 0 {
		return errors.New("API: отсутствует result")
	}
	if err = json.Unmarshal(data, out); err != nil {
		return errors.New("неожиданный формат API")
	}
	return nil
}

type ID string

func (s *ID) UnmarshalJSON(b []byte) error {
	var text string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &text); err != nil {
			return err
		}
	} else {
		text = string(b)
	}
	if text == "null" {
		text = ""
	}
	*s = ID(text)
	return nil
}

type Track struct {
	ID        ID     `json:"id"`
	Title     string `json:"title"`
	Duration  int    `json:"durationMs"`
	Available *bool  `json:"available"`
	Artists   []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Albums []struct {
		ID ID `json:"id"`
	} `json:"albums"`
}

func (t Track) AlbumID() string {
	if len(t.Albums) > 0 {
		return string(t.Albums[0].ID)
	}
	return ""
}

type Playlist struct {
	Kind     ID     `json:"kind"`
	Title    string `json:"title"`
	Revision int    `json:"revision"`
	Owner    struct {
		UID ID `json:"uid"`
	} `json:"owner"`
	Tracks []struct {
		ID      ID     `json:"id"`
		AlbumID ID     `json:"albumId"`
		Track   *Track `json:"track"`
	} `json:"tracks"`
}

func (p Playlist) path() string {
	return "/users/" + url.PathEscape(string(p.Owner.UID)) + "/playlists/" + url.PathEscape(string(p.Kind))
}
func (a Client) Playlist(ctx context.Context, p Playlist) (Playlist, error) {
	var result Playlist
	if p.Owner.UID == "" || p.Kind == "" {
		return result, errors.New("API: плейлист не определён")
	}
	err := a.request(ctx, "GET", p.path(), nil, &result, false)
	if err != nil {
		return result, err
	}
	if result.Owner.UID != p.Owner.UID || result.Kind != p.Kind {
		return result, errors.New("API: ответ не соответствует выбранному плейлисту")
	}
	ids := []string{}
	for _, entry := range result.Tracks {
		if entry.Track == nil {
			id := string(entry.ID)
			if entry.AlbumID != "" {
				id += ":" + string(entry.AlbumID)
			}
			ids = append(ids, id)
		}
	}
	for start := 0; start < len(ids); start += 100 {
		var tracks []Track
		err = a.request(ctx, "POST", "/tracks", url.Values{"track-ids": {strings.Join(ids[start:min(start+100, len(ids))], ",")}}, &tracks, false)
		if err != nil {
			return result, err
		}
		for _, tr := range tracks {
			for i := range result.Tracks {
				if result.Tracks[i].ID == tr.ID {
					copy := tr
					result.Tracks[i].Track = &copy
				}
			}
		}
	}
	return result, nil
}
func (a Client) catalog(ctx context.Context, uid string) ([]Playlist, error) {
	var lists []Playlist
	err := a.request(ctx, "GET", "/users/"+url.PathEscape(uid)+"/playlists/list", nil, &lists, false)
	return lists, err
}

func (a Client) Chart(ctx context.Context) ([]Playlist, error) {
	var chart struct {
		Chart Playlist `json:"chart"`
	}
	err := a.request(ctx, "GET", "/landing3/chart", nil, &chart, false)
	if err != nil {
		return nil, err
	}
	if chart.Chart.Kind == "" {
		return nil, errors.New("API: чарт недоступен")
	}
	return []Playlist{chart.Chart}, nil
}
func validPlaylists(lists []Playlist) []Playlist {
	result := []Playlist{}
	seen := map[[2]ID]bool{}
	for _, p := range lists {
		key := [2]ID{p.Owner.UID, p.Kind}
		if p.Owner.UID == "" || p.Kind == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, p)
	}
	return result
}

func (a Client) SectionCatalog(ctx context.Context, uid string, section int) ([]Playlist, error) {
	switch section {
	case SectionPersonal:
		if uid == "" {
			return nil, nil
		}
		lists, err := a.catalog(ctx, uid)
		return validPlaylists(lists), err
	case SectionForYou:
		if uid == "" {
			return nil, nil
		}
		var feed struct {
			Generated []struct {
				Ready bool     `json:"ready"`
				Data  Playlist `json:"data"`
			} `json:"generatedPlaylists"`
		}
		err := a.request(ctx, "GET", "/feed", nil, &feed, false)
		var lists []Playlist
		for _, item := range feed.Generated {
			if item.Ready {
				lists = append(lists, item.Data)
			}
		}
		return validPlaylists(lists), err
	default:
		var landing struct {
			Blocks []struct {
				Entities []struct {
					Type string          `json:"type"`
					Data json.RawMessage `json:"data"`
				} `json:"entities"`
			} `json:"blocks"`
		}
		err := a.request(ctx, "GET", "/landing3?blocks=new-playlists,mixes", nil, &landing, false)
		var lists []Playlist
		for _, block := range landing.Blocks {
			for _, entity := range block.Entities {
				if entity.Type != "playlist" {
					continue
				}
				var p Playlist
				if json.Unmarshal(entity.Data, &p) == nil {
					lists = append(lists, p)
				}
			}
		}
		return validPlaylists(lists), err
	}
}

func (a Client) Search(ctx context.Context, text string, page int) ([]Track, error) {
	var result struct {
		Tracks struct {
			Results []Track `json:"results"`
		} `json:"tracks"`
	}
	err := a.request(ctx, "GET", "/search?"+url.Values{"text": {text}, "type": {"track"}, "page": {strconv.Itoa(page)}}.Encode(), nil, &result, false)
	return result.Tracks.Results, err
}
func (a Client) Account(ctx context.Context) (string, error) {
	var result struct {
		Account struct {
			UID ID `json:"uid"`
		} `json:"account"`
	}
	err := a.request(ctx, "GET", "/account/status", nil, &result, false)
	if err == nil && result.Account.UID == "" {
		err = errors.New("API: аккаунт не определён")
	}
	return string(result.Account.UID), err
}

type Stream struct {
	URL       string   `json:"url"`
	URLs      []string `json:"urls"`
	Quality   string   `json:"quality"`
	Transport string   `json:"transport"`
}

func (a Client) Resolve(ctx context.Context, id string) (Stream, error) {
	if a.credentials.StreamKey == "" {
		return Stream{}, errors.New("для воспроизведения задайте YAMUSIC_STREAM_KEY в .env или окружении")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(a.credentials.StreamKey))
	_, _ = mac.Write([]byte(ts + id + "hq" + "mp3" + "raw"))
	sign := strings.TrimRight(base64.StdEncoding.EncodeToString(mac.Sum(nil)), "=")
	params := url.Values{"ts": {ts}, "trackId": {id}, "quality": {"hq"}, "codecs": {"mp3"}, "transports": {"raw"}, "sign": {sign}}
	var result struct {
		Info Stream `json:"downloadInfo"`
	}
	err := a.request(ctx, "GET", "/get-file-info?"+params.Encode(), nil, &result, false)
	if err != nil {
		return result.Info, err
	}
	s := result.Info
	if s.URL == "" && len(s.URLs) > 0 {
		s.URL = s.URLs[0]
	}
	u, e := url.Parse(s.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || s.Transport != "raw" {
		return s, errors.New("API: неподдерживаемый поток")
	}
	return s, nil
}

type DeviceCode struct {
	Device   string `json:"device_code"`
	User     string `json:"user_code"`
	URL      string `json:"verification_url"`
	Expires  int    `json:"expires_in"`
	Interval int    `json:"interval"`
}
type Token struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Expires int    `json:"expires_in"`
}

func (a Client) Device(ctx context.Context, id string) (DeviceCode, error) {
	var code DeviceCode
	if err := a.checkOAuth(); err != nil {
		return code, err
	}
	err := a.request(ctx, "POST", "/device/code", url.Values{"client_id": {a.credentials.ClientID}, "device_id": {id}, "device_name": {"Yamusic Cyberpunk TUI"}, "scope": {"music:content music:read music:write"}}, &code, true)
	if err == nil && (code.Device == "" || code.User == "" || code.Expires <= 0) {
		err = errors.New("OAuth: неполный ответ")
	}
	code.Interval = max(5, code.Interval)
	return code, err
}
func (a Client) checkOAuth() error {
	if a.credentials.ClientID == "" || a.credentials.ClientSecret == "" {
		return errors.New("OAuth: задайте YAMUSIC_CLIENT_ID и YAMUSIC_CLIENT_SECRET в .env или окружении")
	}
	return nil
}

func (a Client) RequestToken(ctx context.Context, grant, code string) (Token, error) {
	if err := a.checkOAuth(); err != nil {
		return Token{}, err
	}
	form := url.Values{"client_id": {a.credentials.ClientID}, "client_secret": {a.credentials.ClientSecret}, "grant_type": {grant}}
	if grant == "refresh_token" {
		form.Set("refresh_token", code)
	} else {
		form.Set("code", code)
	}
	var token Token
	err := a.request(ctx, "POST", "/token", form, &token, true)
	if err == nil && token.Access == "" {
		err = errors.New("OAuth: пустой токен")
	}
	return token, err
}
func (a Client) Mutate(ctx context.Context, uid, action, name string, p Playlist, t Track, index int) error {
	if uid == "" || a.token == "" {
		return errors.New("необходим вход")
	}
	if action != "create" && string(p.Owner.UID) != uid {
		return errors.New("можно изменять только свои плейлисты")
	}
	path := p.path()
	form := url.Values{}
	switch action {
	case "create":
		path = "/users/" + url.PathEscape(uid) + "/playlists/create"
		form.Set("title", name)
		form.Set("visibility", "private")
	case "rename":
		path += "/name"
		form.Set("value", name)
	case "delete":
		path += "/delete"
	case "insert", "remove":
		if p.Revision <= 0 {
			return errors.New("сначала откройте плейлист (Enter)")
		}
		var diff any
		if action == "insert" {
			if t.ID == "" || t.AlbumID() == "" {
				return errors.New("у трека отсутствует альбом")
			}
			diff = []any{map[string]any{"op": "insert", "at": len(p.Tracks), "tracks": []any{map[string]string{"id": string(t.ID), "albumId": t.AlbumID()}}}}
		} else {
			if index < 0 || index >= len(p.Tracks) {
				return errors.New("трек не выбран")
			}
			diff = []any{map[string]any{"op": "delete", "from": index, "to": index + 1}}
		}
		data, err := json.Marshal(diff)
		if err != nil {
			return err
		}
		path += "/change"
		form.Set("kind", string(p.Kind))
		form.Set("revision", strconv.Itoa(p.Revision))
		form.Set("diff", string(data))
	default:
		return errors.New("неизвестная операция")
	}
	return a.request(ctx, "POST", path, form, nil, false)
}

func (a Client) ToggleLike(ctx context.Context, uid string, id ID) (bool, error) {
	var collection struct {
		Library struct {
			Tracks []struct {
				ID ID `json:"id"`
			} `json:"tracks"`
		} `json:"library"`
	}
	path := "/users/" + url.PathEscape(uid) + "/likes/tracks"
	if err := a.request(ctx, "GET", path, nil, &collection, false); err != nil {
		return false, err
	}
	liked := false
	for _, tr := range collection.Library.Tracks {
		if tr.ID == id {
			liked = true
			break
		}
	}
	action := "add-multiple"
	if liked {
		action = "remove"
	}
	err := a.request(ctx, "POST", path+"/"+action, url.Values{"track-ids": {string(id)}}, nil, false)
	return !liked, err
}
