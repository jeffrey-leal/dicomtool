package cmd

import "testing"

func TestParseArgs(t *testing.T) {
    if err := parseArgs([]string{"input:foo.dcm", "verbose:true", "output:/tmp/out", "barefile.dcm"}); err != nil {
        t.Fatalf("parseArgs error: %v", err)
    }
    if !Opts.Verbose {
        t.Fatalf("expected verbose true")
    }
    if Opts.Output != "/tmp/out" {
        t.Fatalf("expected output /tmp/out, got %q", Opts.Output)
    }
    if len(Opts.Inputs) != 2 {
        t.Fatalf("expected 2 inputs, got %d", len(Opts.Inputs))
    }
    if v := paramOne("input"); v != "foo.dcm" {
        t.Fatalf("expected paramOne input foo.dcm, got %q", v)
    }
}
