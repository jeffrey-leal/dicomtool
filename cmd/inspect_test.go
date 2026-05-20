package cmd

import (
    "bytes"
    "io"
    "os"
    "testing"
)

func captureOutput(f func()) string {
    old := os.Stdout
    r, w, _ := os.Pipe()
    os.Stdout = w
    outC := make(chan string)
    go func() {
        var buf bytes.Buffer
        io.Copy(&buf, r)
        outC <- buf.String()
    }()
    f()
    w.Close()
    os.Stdout = old
    return <-outC
}

func TestRunInspect_Errors(t *testing.T) {
    Opts = Options{}
    if err := runInspect(); err == nil {
        t.Fatalf("expected error when no inputs")
    }
    // input present but no tags and not all
    if err := parseArgs([]string{"input:foo.dcm"}); err != nil {
        t.Fatalf("parseArgs failed: %v", err)
    }
    if err := runInspect(); err == nil {
        t.Fatalf("expected error when no tags and not all")
    }
}

func TestRunInspect_Success(t *testing.T) {
    if err := parseArgs([]string{"input:foo.dcm", "all:true", "verbose:true", "output:/tmp/out"}); err != nil {
        t.Fatalf("parseArgs failed: %v", err)
    }
    out := captureOutput(func() { _ = runInspect() })
    if out == "" {
        t.Fatalf("expected output, got empty")
    }
    if !bytes.Contains([]byte(out), []byte("File: foo.dcm")) {
        t.Fatalf("unexpected output: %q", out)
    }
}
