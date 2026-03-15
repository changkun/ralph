package backend

import "testing"

func TestExtractResult(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{"valid", []byte(`{"result":"hello"}`), "hello"},
		{"empty", []byte(`{"result":""}`), ""},
		{"invalid", []byte(`not json`), ""},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractResult(tt.raw); got != tt.want {
				t.Errorf("ExtractResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	if be := New("claude"); be == nil {
		t.Error("expected Claude backend")
	}
	if be := New("codex"); be == nil {
		t.Error("expected Codex backend")
	}
	if be := New("other"); be != nil {
		t.Error("expected nil")
	}
}
