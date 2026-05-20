package cmd

import (
    "testing"
    "github.com/spf13/cobra"
)

func TestRequiresOutput(t *testing.T) {
    if !requiresOutput("convert") {
        t.Fatalf("convert should require output")
    }
    if requiresOutput("inspect") {
        t.Fatalf("inspect should not require output")
    }
}

func TestValidateParams(t *testing.T) {
    Opts = Options{Output: ""}
    cmd := &cobra.Command{Use: "convert"}
    if err := validateParams(cmd); err == nil {
        t.Fatalf("expected error when output missing for convert")
    }
    Opts = Options{Output: "/tmp/out"}
    if err := validateParams(cmd); err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
