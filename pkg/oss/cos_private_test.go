package oss

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/crc64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/tencentyun/cos-go-sdk-v5"
)

func TestPrivateUploadAndHeadMetadataArePreservedThroughTracing(t *testing.T) {
	var uploaded bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			uploaded = true
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Cos-Hash-Crc64ecma", strconv.FormatUint(crc64.Checksum(body, crc64.MakeTable(crc64.ECMA)), 10))
			if r.Header.Get("X-Cos-Acl") != "private" || r.Header.Get("Cache-Control") != "private, no-store, max-age=0" || r.Header.Get("Content-Type") != "image/jpeg" {
				t.Errorf("私有上传头错误：%v", r.Header)
			}
		case "HEAD":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", "1234")
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
			w.Header().Set("X-Cos-Meta-Capture-Session", "session")
			w.Header().Set("X-Cos-Meta-Sha256", "image-hash")
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	store := NewTracedOSS(&Cos{client: cos.NewClient(&cos.BaseURL{BucketURL: base}, srv.Client())})
	private, ok := store.(PrivateObjectStore)
	if !ok {
		t.Fatal("追踪包装丢失私有上传能力")
	}
	if err := private.PutPrivateObject(context.Background(), "private/test.jpg", bytes.NewReader([]byte("jpeg")), 4, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	info, err := store.StatObject(context.Background(), "private/test.jpg")
	if err != nil || !uploaded || info.Size != 1234 || info.Metadata["capture-session"] != "session" || info.Metadata["sha256"] != "image-hash" {
		t.Fatal(info, err)
	}
}

func TestPersistentThumbnailUsesBucketAbsoluteFileIDAndPHPProcessingRule(t *testing.T) {
	var processed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			processed = true
			if r.URL.Path != "/forum/202609/29/42/source.jpg" || !r.URL.Query().Has("image_process") {
				t.Error("缩略图源请求错误", r.URL.Path)
			}
			var options cos.ImageProcessOptions
			if err := json.Unmarshal([]byte(r.Header.Get("Pic-Operations")), &options); err != nil {
				t.Error(err)
			}
			if options.IsPicInfo != 1 || len(options.Rules) != 1 || options.Rules[0].FileId != "/cyber-checkin-thumbnails/42/1/thumb_256.jpg" || options.Rules[0].Rule != "imageMogr2/auto-orient/thumbnail/256x256>/strip/format/jpg/quality/82!/interlace/1" {
				t.Error(options)
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, "<UploadResult></UploadResult>")
			return
		}
		if r.Method != "HEAD" || r.URL.Path != "/cyber-checkin-thumbnails/42/1/thumb_256.jpg" {
			t.Error("没有按目标键回读缩略图", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Length", "1000")
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	store := NewTracedOSS(&Cos{client: cos.NewClient(&cos.BaseURL{BucketURL: base}, srv.Client())})
	if err := store.(PersistentThumbnailStore).PersistImageThumbnail(context.Background(), "forum/202609/29/42/source.jpg", "cyber-checkin-thumbnails/42/1/thumb_256.jpg", 256, 82); err != nil || !processed {
		t.Fatal(err, processed)
	}
}
