package cli

import (
	"bytes"
	"testing"
)

func TestShowDiff(t *testing.T) {
	const hunk = "@@ -1,2 +1,2 @@"
	tests := []struct {
		name     string
		color    bool
		old, new string
		want     string
	}{
		{
			name: "plain",
			old:  "a\nb\n", new: "a\nc\n",
			want: "--- old.vcf\n+++ (fetched)\n" + hunk + "\n a\n-b\n+c\n",
		},
		{
			name: "colored",
			old:  "a\nb\n", new: "a\nc\n",
			color: true,
			want: "\x1b[1m--- old.vcf\x1b[0m\n\x1b[1m+++ (fetched)\x1b[0m\n" +
				"\x1b[36m" + hunk + "\x1b[0m\n a\n\x1b[31m-b\x1b[0m\n\x1b[32m+c\x1b[0m\n",
		},
		{
			name: "identical plain",
			old:  "a\nb\n", new: "a\nb\n",
			want: "  No differences.\n",
		},
		{
			name: "identical colored",
			old:  "a\n", new: "a\n",
			color: true,
			want:  "\x1b[32m  No differences.\x1b[0m\n",
		},
		{
			name: "no trailing newline matches trailing newline",
			old:  "a\nb", new: "a\nb\n",
			want: "  No differences.\n",
		},
		{
			name: "no trailing newline with change",
			old:  "a\nb", new: "a\nc",
			want: "--- old.vcf\n+++ (fetched)\n" + hunk + "\n a\n-b\n+c\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			env := &Env{Out: out, Err: out, Color: tc.color}
			env.showDiff(tc.old, tc.new, "old.vcf", "(fetched)")
			if got := out.String(); got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}
