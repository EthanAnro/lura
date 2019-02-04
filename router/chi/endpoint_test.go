package chi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi"

	"github.com/devopsfaith/krakend/config"
	"github.com/devopsfaith/krakend/proxy"
	"github.com/devopsfaith/krakend/router"
)

func TestEndpointHandler_ok(t *testing.T) {
	p := func(_ context.Context, req *proxy.Request) (*proxy.Response, error) {
		data, _ := json.Marshal(req.Query)
		if string(data) != `{"b":["1"],"c[]":["x","y"],"d":["1","2"]}` {
			t.Errorf("unexpected querystring: %s", data)
		}
		return &proxy.Response{
			IsComplete: true,
			Data:       map[string]interface{}{"supu": "tupu"},
		}, nil
	}
	expectedBody := "{\"supu\":\"tupu\"}"
	testEndpointHandler(t, 10, p, "GET", expectedBody, "public, max-age=21600", "application/json", http.StatusOK, true)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_incomplete(t *testing.T) {
	p := func(_ context.Context, _ *proxy.Request) (*proxy.Response, error) {
		return &proxy.Response{
			IsComplete: false,
			Data:       map[string]interface{}{"foo": "bar"},
		}, nil
	}
	expectedBody := "{\"foo\":\"bar\"}"
	testEndpointHandler(t, 10, p, "GET", expectedBody, "", "application/json", http.StatusOK, false)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_ko(t *testing.T) {
	p := func(_ context.Context, _ *proxy.Request) (*proxy.Response, error) {
		return nil, fmt.Errorf("This is %s", "a dummy error")
	}
	testEndpointHandler(t, 10, p, "GET", "This is a dummy error\n", "", "text/plain; charset=utf-8", http.StatusInternalServerError, false)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_incompleteAndErrored(t *testing.T) {
	p := func(_ context.Context, _ *proxy.Request) (*proxy.Response, error) {
		return &proxy.Response{
			IsComplete: false,
			Data:       map[string]interface{}{"foo": "bar"},
		}, errors.New("This is a dummy error")
	}
	expectedBody := "{\"foo\":\"bar\"}"
	testEndpointHandler(t, 10, p, "GET", expectedBody, "", "application/json", http.StatusOK, false)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_cancel(t *testing.T) {
	p := func(_ context.Context, _ *proxy.Request) (*proxy.Response, error) {
		time.Sleep(100 * time.Millisecond)
		return &proxy.Response{
			IsComplete: false,
			Data:       map[string]interface{}{"foo": "bar"},
		}, nil
	}
	testEndpointHandler(t, 0, p, "GET", "{\"foo\":\"bar\"}", "", "application/json", http.StatusOK, false)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_cancelEmpty(t *testing.T) {
	p := func(_ context.Context, _ *proxy.Request) (*proxy.Response, error) {
		time.Sleep(100 * time.Millisecond)
		return nil, nil
	}
	testEndpointHandler(t, 0, p, "GET", router.ErrInternalError.Error()+"\n", "", "text/plain; charset=utf-8", http.StatusInternalServerError, false)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_noop(t *testing.T) {
	testEndpointHandler(t, time.Minute, proxy.NoopProxy, "GET", "{}", "", "application/json", http.StatusOK, false)
	time.Sleep(5 * time.Millisecond)
}

func TestEndpointHandler_badMethod(t *testing.T) {
	testEndpointHandler(t, 10, proxy.NoopProxy, "PUT", "\n", "", "text/plain; charset=utf-8", http.StatusMethodNotAllowed, false)
	time.Sleep(5 * time.Millisecond)
}

func testEndpointHandler(t *testing.T, timeout time.Duration, p proxy.Proxy, method, expectedBody, expectedCache, expectedContent string,
	expectedStatusCode int, completed bool) {
	endpoint := &config.EndpointConfig{
		Method:      "GET",
		Timeout:     timeout,
		CacheTTL:    6 * time.Hour,
		QueryString: []string{"b", "c[]", "d"},
	}

	server := startChiServer(NewEndpointHandler(endpoint, p))

	req, _ := http.NewRequest(method, "http://127.0.0.1:8081/_chi_endpoint?b=1&c[]=x&c[]=y&d=1&d=2", ioutil.NopCloser(&bytes.Buffer{}))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	body, ioerr := ioutil.ReadAll(w.Result().Body)
	if ioerr != nil {
		t.Error("Reading the response:", ioerr.Error())
		return
	}
	w.Result().Body.Close()
	content := string(body)
	resp := w.Result()

	if resp.Header.Get("Cache-Control") != expectedCache {
		t.Error("Cache-Control error:", resp.Header.Get("Cache-Control"))
	}
	if completed && resp.Header.Get(router.CompleteResponseHeaderName) != router.HeaderCompleteResponseValue {
		t.Error(router.CompleteResponseHeaderName, "error:", resp.Header.Get(router.CompleteResponseHeaderName))
	}
	if !completed && resp.Header.Get(router.CompleteResponseHeaderName) != router.HeaderIncompleteResponseValue {
		t.Error(router.CompleteResponseHeaderName, "error:", resp.Header.Get(router.CompleteResponseHeaderName))
	}
	if resp.Header.Get("Content-Type") != expectedContent {
		t.Error("Content-Type error:", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("X-Krakend") != "Version undefined" {
		t.Error("X-Krakend error:", resp.Header.Get("X-Krakend"))
	}
	if resp.StatusCode != expectedStatusCode {
		t.Error("Unexpected status code:", resp.StatusCode)
	}
	if content != expectedBody {
		t.Error("Unexpected body:", content, "expected:", expectedBody)
	}
}

func startChiServer(handlerFunc http.HandlerFunc) *chi.Mux {
	router := chi.NewRouter()
	router.Handle("/_chi_endpoint", handlerFunc)
	return router
}
