package oss

import (
	"bytes"
	"context"
	"hash/crc64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/tencentyun/cos-go-sdk-v5"
)

func TestPublicMaterialUploadCarriesImmutableMetadataThroughTracing(t *testing.T) {
	metadata := map[string]string{"sha256": "hash", "font-family": "Zm9udA==", "font-weight": "400", "sanitized": "ots-v1"}
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Cos-Hash-Crc64ecma", strconv.FormatUint(crc64.Checksum(body, crc64.MakeTable(crc64.ECMA)), 10))
		if r.Method != "PUT" || r.URL.Path != "/cyber-checkin-materials/test/font/hash.ttf" || r.Header.Get("X-Cos-Acl") != "public-read" || r.Header.Get("Content-Type") != "font/ttf" || r.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" || string(body) != "font" {
			t.Error("公共素材交付参数错误", r.Method, r.URL.Path, r.Header)
		}
		for key, value := range metadata {
			if r.Header.Get("X-Cos-Meta-"+key) != value {
				t.Errorf("云元数据 %s 未保留", key)
			}
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	store := NewTracedOSS(&Cos{client: cos.NewClient(&cos.BaseURL{BucketURL: base}, srv.Client())})
	err := store.(ImmutablePublicStore).PutImmutablePublicObject(context.Background(), "cyber-checkin-materials/test/font/hash.ttf", bytes.NewReader([]byte("font")), 4, "font/ttf", metadata)
	if err != nil || !called {
		t.Fatal(err, called)
	}
}
