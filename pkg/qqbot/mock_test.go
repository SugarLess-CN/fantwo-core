package qqbot

// 单测公共脚手架：httptest 造假上游，绝不发真网络。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mockUpstream 假上游：一个 server 同时扮演 token 接口与两个发送接口。
type mockUpstream struct {
	srv *httptest.Server

	mu sync.Mutex
	// tokenBody / sendBody 每次请求时按当前值返回（用例可随时改）。
	tokenBody string
	tokenCode int // 覆盖 HTTP 状态码（0 = 200）
	sendBody  string
	sendCode  int
	// 计数与抓包
	tokenCalls   int
	sendCalls    int
	lastSend     map[string]any
	lastSendPath string
	// lastSendURI 原始请求行（未解码）：路径转义要靠它验证，
	// r.URL.Path 是解码后的值（%2F 会变回 /，看不出转义）。
	lastSendURI     string
	lastSendHeaders http.Header
	lastTokenReq    map[string]any
}

func newMockUpstream(t *testing.T) *mockUpstream {
	t.Helper()
	m := &mockUpstream{
		tokenBody: `{"access_token":"TOK-1","expires_in":"7200"}`,
		sendBody:  `{"id":"ROBOT1.0_abc","timestamp":"2026-07-21T10:30:00+08:00","ext_info":{"ref_idx":"REFIDX_x=="}}`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/app/getAppAccessToken", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		m.mu.Lock()
		m.tokenCalls++
		m.lastTokenReq = body
		code, resp := m.tokenCode, m.tokenBody
		m.mu.Unlock()

		if code != 0 {
			w.WriteHeader(code)
		}
		_, _ = io.WriteString(w, resp)
	})
	sendHandler := func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		m.mu.Lock()
		m.sendCalls++
		m.lastSend = body
		m.lastSendPath = r.URL.Path
		m.lastSendURI = r.RequestURI
		m.lastSendHeaders = r.Header.Clone()
		code, resp := m.sendCode, m.sendBody
		m.mu.Unlock()

		if code != 0 {
			w.WriteHeader(code)
		}
		_, _ = io.WriteString(w, resp)
	}
	mux.HandleFunc("/v2/users/", sendHandler)
	mux.HandleFunc("/v2/groups/", sendHandler)

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

// newTestClient 构造指向假上游的 client（返回具体类型，便于拿 tokens）。
func (m *mockUpstream) newTestClient(t *testing.T) *client {
	t.Helper()
	c, err := newClient(Config{
		APIBase:   m.srv.URL,
		TokenURL:  m.srv.URL + "/app/getAppAccessToken",
		AppID:     "11111111",
		AppSecret: "DG5g3B4j9X2KOErG",
		BotSecret: "DG5g3B4j9X2KOErG",
	}, m.srv.Client())
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func (m *mockUpstream) setSend(t *testing.T, body string, httpCode int) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendBody, m.sendCode = body, httpCode
}

func (m *mockUpstream) setToken(t *testing.T, body string, httpCode int) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenBody, m.tokenCode = body, httpCode
}

func (m *mockUpstream) counts() (token, send int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokenCalls, m.sendCalls
}

func (m *mockUpstream) sendReq() (map[string]any, string, http.Header) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSend, m.lastSendPath, m.lastSendHeaders
}

// sendURI 原始请求行（含未解码的转义序列）。
func (m *mockUpstream) sendURI() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSendURI
}
