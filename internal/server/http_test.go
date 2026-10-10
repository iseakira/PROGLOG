package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// テストごとに新しいサーバーを作る。
// NewHTTPServer の中で NewLog() が呼ばれるので、ログは毎回空から始まる。
// Handler だけ取り出せばポートを開かずに呼べる。
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return NewHTTPServer(":0").Handler
}

// POST / を1回叩き、採番されたオフセットを返す。
func produce(t *testing.T, h http.Handler, value []byte) uint64 {
	t.Helper()

	body, err := json.Marshal(ProduceRequest{Record: Record{Value: value}})
	if err != nil {
		t.Fatalf("リクエストのマーシャルに失敗: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST: ステータス = %d, 欲しいのは 200 (body=%q)", rec.Code, rec.Body.String())
	}

	var res ProduceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("POST: レスポンスのアンマーシャルに失敗: %v (body=%q)", err, rec.Body.String())
	}
	return res.Offset
}

// GET / を1回叩き、返ってきたレコードを返す。
func consume(t *testing.T, h http.Handler, offset uint64) Record {
	t.Helper()

	body, err := json.Marshal(ConsumeRequest{Offset: offset})
	if err != nil {
		t.Fatalf("リクエストのマーシャルに失敗: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", bytes.NewReader(body))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET: ステータス = %d, 欲しいのは 200 (body=%q)", rec.Code, rec.Body.String())
	}

	var res ConsumeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("GET: レスポンスのアンマーシャルに失敗: %v (body=%q)", err, rec.Body.String())
	}
	return res.Record
}

// 書いたものがそのまま読めること（往復できること）。
func TestProduceConsume(t *testing.T) {
	h := newTestHandler(t)
	want := []byte("Let's Go #1\n")

	off := produce(t, h, want)
	if off != 0 {
		t.Errorf("最初のオフセット = %d, 欲しいのは 0", off)
	}

	got := consume(t, h, off)
	if !bytes.Equal(got.Value, want) {
		t.Errorf("value = %q, 欲しいのは %q", got.Value, want)
	}
	if got.Offset != off {
		t.Errorf("レコードの offset = %d, 欲しいのは %d", got.Offset, off)
	}
}

// 複数書くとオフセットが 0,1,2... と増え、それぞれ正しく読み分けられること。
func TestProduceConsumeMultiple(t *testing.T) {
	h := newTestHandler(t)
	values := [][]byte{
		[]byte("Let's Go #1\n"),
		[]byte("Let's Go #2\n"),
		[]byte("Let's Go #3\n"),
	}

	for i, want := range values {
		off := produce(t, h, want)
		if off != uint64(i) {
			t.Fatalf("%d 番目の POST: offset = %d, 欲しいのは %d", i, off, i)
		}
	}

	for i, want := range values {
		got := consume(t, h, uint64(i))
		if !bytes.Equal(got.Value, want) {
			t.Errorf("offset %d: value = %q, 欲しいのは %q", i, got.Value, want)
		}
	}
}

// クライアントが offset を指定しても無視され、サーバーが採番すること。
func TestProduceIgnoresClientOffset(t *testing.T) {
	h := newTestHandler(t)

	body, err := json.Marshal(ProduceRequest{
		Record: Record{Value: []byte("hi"), Offset: 999},
	})
	if err != nil {
		t.Fatalf("マーシャルに失敗: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)))

	var res ProduceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("アンマーシャルに失敗: %v", err)
	}
	if res.Offset != 0 {
		t.Errorf("offset = %d, 欲しいのは 0（クライアント指定の 999 は無視されるべき）", res.Offset)
	}
}

// エラーのときに正しいステータスコードが返ること。
func TestErrorStatusCodes(t *testing.T) {
	cases := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{"壊れたJSONをPOST", http.MethodPost, `{壊れてる`, http.StatusBadRequest},
		{"壊れたJSONをGET", http.MethodGet, `{壊れてる`, http.StatusBadRequest},
		{"存在しないオフセットをGET", http.MethodGet, `{"offset": 99}`, http.StatusNotFound},
		{"未対応のメソッド", http.MethodDelete, ``, http.StatusMethodNotAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/", bytes.NewReader([]byte(tc.body)))
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("ステータス = %d, 欲しいのは %d (body=%q)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
