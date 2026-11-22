package setup

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestExpandAndFormatHomePath(t *testing.T) {
	home := "/Users/alice"

	if got := ExpandPath("~", home); got != home {
		t.Fatalf("ExpandPath(~) = %q, want %q", got, home)
	}
	if got := ExpandPath("~/Dev/Projects", home); got != filepath.Join(home, "Dev", "Projects") {
		t.Fatalf("ExpandPath(~/Dev/Projects) = %q", got)
	}
	if got := FormatHomePath("/Users/alice/Dev/Projects", home); got != "~/Dev/Projects" {
		t.Fatalf("FormatHomePath = %q, want ~/Dev/Projects", got)
	}
	if got := FormatHomePath("/opt/work", home); got != "/opt/work" {
		t.Fatalf("FormatHomePath(/opt/work) = %q, want /opt/work", got)
	}
}

func TestParseRootInputs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "comma separated",
			raw:  "~/Dev/Personal, ~/Dev/Work",
			want: []string{"~/Dev/Personal", "~/Dev/Work"},
		},
		{
			name: "space separated with tilde and slash",
			raw:  "~/Dev/Personal /opt/repos ./local",
			want: []string{"~/Dev/Personal", "/opt/repos", "./local"},
		},
		{
			name: "quoted path with spaces",
			raw:  `"/tmp/Mobile Documents", '~/My Projects'`,
			want: []string{"/tmp/Mobile Documents", "~/My Projects"},
		},
		{
			name: "unquoted path with spaces",
			raw:  `/tmp/Mobile Documents, ~/Dev/Personal`,
			want: []string{"/tmp/Mobile Documents", "~/Dev/Personal"},
		},
		{
			name: "escaped space",
			raw:  `~/My\ Projects ~/Other`,
			want: []string{"~/My Projects", "~/Other"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRootInputs(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseRootInputs(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSplitEnvRoots(t *testing.T) {
	got := SplitEnvRoots("~/a:/tmp/b, ~/c")
	want := []string{"~/a", "/tmp/b", "~/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitEnvRoots = %#v, want %#v", got, want)
	}
}
