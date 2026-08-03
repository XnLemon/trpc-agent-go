//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package a2aagent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"trpc.group/trpc-go/trpc-a2a-go/client"
)

// NewAnonymousA2AClient creates an A2A client for anonymous cookie-based
// sessions.
//
// The client installs a cookie jar when the configured HTTP client does not
// have one and serializes requests that race before a valid anonymous cookie
// is available. The serialization guarantee is limited to this client
// instance. Callers using multiple clients or processes must coordinate those
// clients separately.
func NewAnonymousA2AClient(agentURL string, opts ...client.Option) (*client.A2AClient, error) {
	clientOpts := make([]client.Option, 0, len(opts)+1)
	// Register the gate first so it remains the outermost middleware even when
	// callers add their own middleware through opts.
	clientOpts = append(clientOpts, client.WithMiddleware(newAnonymousA2AClientInitMiddleware()))
	clientOpts = append(clientOpts, opts...)
	return client.NewA2AClient(agentURL, clientOpts...)
}

type anonymousA2AClientInitMiddleware struct {
	gate        chan struct{}
	jarMu       sync.Mutex
	jar         http.CookieJar
	initialized bool
	waitHook    func()
}

func newAnonymousA2AClientInitMiddleware() *anonymousA2AClientInitMiddleware {
	return &anonymousA2AClientInitMiddleware{
		gate: make(chan struct{}, 1),
	}
}

func (m *anonymousA2AClientInitMiddleware) Wrap(next client.HTTPReqHandler) client.HTTPReqHandler {
	return &anonymousA2AClientInitHandler{
		middleware: m,
		next:       next,
	}
}

type anonymousA2AClientInitHandler struct {
	middleware *anonymousA2AClientInitMiddleware
	next       client.HTTPReqHandler
}

func (h *anonymousA2AClientInitHandler) Handle(
	ctx context.Context,
	httpClient *http.Client,
	req *http.Request,
) (*http.Response, error) {
	if h == nil || h.middleware == nil {
		return nil, errors.New("anonymous A2A client: initialization middleware is nil")
	}
	if h.next == nil {
		return nil, errors.New("anonymous A2A client: next HTTP request handler is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if httpClient == nil || req == nil {
		return h.next.Handle(ctx, httpClient, req)
	}
	if err := h.middleware.ensureCookieJar(httpClient.Jar); err != nil {
		return nil, err
	}
	var (
		release func()
		err     error
	)
	if h.middleware.needsInitialization(req) {
		release, err = h.middleware.acquire(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}

	// The first request owns the gate until the downstream handler has
	// processed its response and this middleware has stored Set-Cookie in the
	// jar. A waiter can then send through the same jar and continue under the
	// principal established by the winner.
	requestClient, err := h.middleware.clientWithCookieJar(httpClient)
	if err != nil {
		return nil, err
	}
	request := req.Clone(ctx)
	h.middleware.addJarCookies(request)
	requestJar := newAnonymousA2AClientRequestCookieJar(requestClient.Jar, request.URL)
	requestClient.Jar = requestJar
	resp, handleErr := h.next.Handle(ctx, requestClient, request)
	h.middleware.captureResponseCookies(request, resp, requestJar)
	return resp, handleErr
}

func (m *anonymousA2AClientInitMiddleware) needsInitialization(
	req *http.Request,
) bool {
	if req == nil || req.URL == nil {
		return false
	}
	m.jarMu.Lock()
	defer m.jarMu.Unlock()
	return !m.initialized
}

func (m *anonymousA2AClientInitMiddleware) ensureCookieJar(configured http.CookieJar) error {
	m.jarMu.Lock()
	defer m.jarMu.Unlock()
	if m.jar != nil {
		return nil
	}
	if configured != nil {
		m.jar = configured
		return nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("anonymous A2A client: create cookie jar: %w", err)
	}
	m.jar = jar
	return nil
}

func (m *anonymousA2AClientInitMiddleware) clientWithCookieJar(
	httpClient *http.Client,
) (*http.Client, error) {
	if httpClient == nil {
		return nil, nil
	}
	if err := m.ensureCookieJar(httpClient.Jar); err != nil {
		return nil, err
	}
	m.jarMu.Lock()
	jar := m.jar
	m.jarMu.Unlock()
	requestClient := *httpClient
	requestClient.Jar = jar
	return &requestClient, nil
}

func (m *anonymousA2AClientInitMiddleware) cookieJar() http.CookieJar {
	m.jarMu.Lock()
	defer m.jarMu.Unlock()
	return m.jar
}

// addJarCookies makes cookie state visible to handlers that do not call
// http.Client.Do themselves. The standard client path still receives the jar
// on the shallow-copied client so it can apply its normal cookie behavior.
func (m *anonymousA2AClientInitMiddleware) addJarCookies(req *http.Request) {
	if req == nil || req.URL == nil {
		return
	}
	jar := m.cookieJar()
	if jar == nil {
		return
	}

	jarCookies := jar.Cookies(req.URL)
	if len(jarCookies) == 0 {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	jarCookieNames := make(map[string]struct{}, len(jarCookies))
	for _, cookie := range jarCookies {
		if cookie != nil {
			jarCookieNames[cookie.Name] = struct{}{}
		}
	}

	existingCookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, cookie := range existingCookies {
		if cookie == nil {
			continue
		}
		if _, existsInJar := jarCookieNames[cookie.Name]; existsInJar {
			continue
		}
		req.AddCookie(cookie)
	}
	for _, cookie := range jarCookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
}

func (m *anonymousA2AClientInitMiddleware) captureResponseCookies(
	req *http.Request,
	resp *http.Response,
	requestJar *anonymousA2AClientRequestCookieJar,
) {
	if req == nil || resp == nil {
		return
	}
	jar := m.cookieJar()
	if jar == nil {
		return
	}
	responseURL := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		responseURL = resp.Request.URL
	}
	if responseURL == nil {
		return
	}
	responseCookies := resp.Cookies()
	m.markInitialized(responseCookies)
	if len(responseCookies) == 0 || requestJar.storedCookiesForURL(responseURL) {
		return
	}
	jar.SetCookies(responseURL, responseCookies)
}

func (m *anonymousA2AClientInitMiddleware) markInitialized(cookies []*http.Cookie) {
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name != anonymousUserIDCookieName ||
			!isAnonymousUserIDCookieValue(cookie.Value) {
			continue
		}
		m.jarMu.Lock()
		m.initialized = true
		m.jarMu.Unlock()
		return
	}
}

func (m *anonymousA2AClientInitMiddleware) acquire(ctx context.Context) (func(), error) {
	select {
	case m.gate <- struct{}{}:
		return func() { <-m.gate }, nil
	default:
		if m.waitHook != nil {
			m.waitHook()
		}
	}
	select {
	case m.gate <- struct{}{}:
		return func() { <-m.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// anonymousA2AClientRequestCookieJar lets the middleware inject cookies for
// custom handlers without making the standard http.Client process the initial
// request or final response cookies a second time. Redirects continue to use
// the configured jar normally.
type anonymousA2AClientRequestCookieJar struct {
	base               http.CookieJar
	initialURL         string
	mu                 sync.Mutex
	initialReadSkipped bool
	storedURLs         map[string]struct{}
}

func newAnonymousA2AClientRequestCookieJar(
	base http.CookieJar,
	initialURL *url.URL,
) *anonymousA2AClientRequestCookieJar {
	return &anonymousA2AClientRequestCookieJar{
		base:       base,
		initialURL: cookieJarURLKey(initialURL),
		storedURLs: make(map[string]struct{}),
	}
}

func (j *anonymousA2AClientRequestCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if j == nil || j.base == nil || u == nil {
		return nil
	}
	key := cookieJarURLKey(u)
	j.mu.Lock()
	if !j.initialReadSkipped && key == j.initialURL {
		j.initialReadSkipped = true
		j.mu.Unlock()
		return nil
	}
	j.mu.Unlock()
	return j.base.Cookies(u)
}

func (j *anonymousA2AClientRequestCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if j == nil || j.base == nil || u == nil {
		return
	}
	j.base.SetCookies(u, cookies)
	if len(cookies) == 0 {
		return
	}
	j.mu.Lock()
	j.storedURLs[cookieJarURLKey(u)] = struct{}{}
	j.mu.Unlock()
}

func (j *anonymousA2AClientRequestCookieJar) storedCookiesForURL(u *url.URL) bool {
	if j == nil || u == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, ok := j.storedURLs[cookieJarURLKey(u)]
	return ok
}

func cookieJarURLKey(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

var _ client.Middleware = (*anonymousA2AClientInitMiddleware)(nil)
var _ http.CookieJar = (*anonymousA2AClientRequestCookieJar)(nil)
