package config

import "testing"

func TestNormalizeMinIOEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"minio:9000", "minio:9000"},
		{"localhost:9000", "localhost:9000"},
		{"localhost:9000/", "localhost:9000"},
		{"minio:9000/aihub", "minio:9000"},
		{"http://minio:9000", "minio:9000"},
		{"http://minio:9000/aihub", "minio:9000"},
		{"https://minio.example.com", "minio.example.com"},
		{"https://minio.example.com/", "minio.example.com"},
		{"  http://minio:9000/aihub  ", "minio:9000"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeMinIOEndpoint(tt.in); got != tt.want {
			t.Errorf("normalizeMinIOEndpoint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
