package main

import (
	"testing"
)

func TestParseCLIFlags(t *testing.T) {
	f, err := parseCLIFlags([]string{"--headless", "--prompt", "hello", "--ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Headless || f.Prompt != "hello" {
		t.Fatalf("got %+v", f)
	}

	f, err = parseCLIFlags([]string{"--prompt=world", "-p=later"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Prompt != "later" {
		t.Fatalf("later flag should overwrite prompt, got %q", f.Prompt)
	}

	f, err = parseCLIFlags([]string{"--prompt-file", "/tmp/t.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if f.PromptFile != "/tmp/t.txt" {
		t.Fatalf("got %+v", f)
	}

	_, err = parseCLIFlags([]string{"--prompt"})
	if err == nil {
		t.Fatal("expected error")
	}

	f, err = parseCLIFlags([]string{"--result-json", "/tmp/out.json"})
	if err != nil {
		t.Fatal(err)
	}
	if f.ResultJSON != "/tmp/out.json" {
		t.Fatalf("got %+v", f)
	}
	f, err = parseCLIFlags([]string{"--result-json=/x.json"})
	if err != nil || f.ResultJSON != "/x.json" {
		t.Fatalf("got %+v err=%v", f, err)
	}
}
