package otelx

import "testing"

func TestEndpointIsZero(t *testing.T) {
	if !(Endpoint{}).IsZero() {
		t.Error("零值 Endpoint 应当 IsZero")
	}
	if !(Endpoint{Host: "  "}).IsZero() {
		t.Error("纯空白 Host 应当 IsZero")
	}
	if (Endpoint{Host: "localhost:4318"}).IsZero() {
		t.Error("非空 Host 不应 IsZero")
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		insecure   bool
		signalPath string
		want       Endpoint
	}{
		{"空值", "", true, "/v1/traces", Endpoint{}},
		{"纯空白", "   ", true, "/v1/traces", Endpoint{}},
		{"裸 host:port", "localhost:4318", false, "/v1/traces", Endpoint{Host: "localhost:4318"}},
		{"http scheme 强制明文", "http://localhost:4318", false, "/v1/traces", Endpoint{Host: "localhost:4318", Insecure: true}},
		{"https scheme 强制 TLS", "https://collector.example.com:443", true, "/v1/traces", Endpoint{Host: "collector.example.com:443"}},
		{"自定义路径", "http://localhost:4318/custom/traces", false, "/v1/traces", Endpoint{Host: "localhost:4318", Path: "/custom/traces", Insecure: true}},
		{"路径与默认相同则置空", "http://localhost:4318/v1/traces", false, "/v1/traces", Endpoint{Host: "localhost:4318", Insecure: true}},
		{"尾斜杠归零", "http://localhost:4318/v1/traces/", false, "/v1/traces", Endpoint{Host: "localhost:4318", Insecure: true}},
		{"无 scheme 带路径", "localhost:4318/custom", true, "/v1/traces", Endpoint{Host: "localhost:4318", Path: "/custom", Insecure: true}},
		{"非法 URL 降级不 panic", "http://", true, "/v1/traces", Endpoint{Host: "", Insecure: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseEndpoint(tc.raw, tc.insecure, tc.signalPath)
			if got.Host != tc.want.Host || got.Path != tc.want.Path || got.Insecure != tc.want.Insecure {
				t.Errorf("ParseEndpoint(%q) = %+v, 期望 %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestServiceInfoResource(t *testing.T) {
	res := ServiceInfo{
		Name:        "gin-template",
		ID:          "host-1",
		Version:     "v1.2.3",
		Environment: "production",
	}.Resource()

	got := make(map[string]string)
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	for key, want := range map[string]string{
		"service.name":           "gin-template",
		"service.instance.id":    "host-1",
		"service.version":        "v1.2.3",
		"deployment.environment": "production",
	} {
		if got[key] != want {
			t.Errorf("Resource 属性 %s = %q, 期望 %q", key, got[key], want)
		}
	}
}

func TestServiceInfoResourceOmitsEmpty(t *testing.T) {
	res := ServiceInfo{Name: "only-name"}.Resource()

	for _, kv := range res.Attributes() {
		switch string(kv.Key) {
		case "service.version", "service.instance.id", "deployment.environment":
			t.Errorf("空字段不应写入 Resource 属性：%s=%q", kv.Key, kv.Value.AsString())
		}
	}
}
