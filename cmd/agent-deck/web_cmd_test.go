package main

import "testing"

func TestBuildWebServer_NotGatedOnNativeWindows(t *testing.T) {
	server, err := buildWebServer("default", nil, nil)
	if err != nil {
		t.Fatalf("buildWebServer() unexpected error: %v", err)
	}
	if server == nil {
		t.Fatal("buildWebServer() returned nil server")
	}
}
