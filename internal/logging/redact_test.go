/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package logging

import "testing"

func TestHasValue(t *testing.T) {
	if HasValue("   ") {
		t.Fatal("expected whitespace-only value to be treated as empty")
	}
	if !HasValue("configured") {
		t.Fatal("expected non-empty value to be treated as present")
	}
}

func TestRedactString(t *testing.T) {
	if got := RedactString(""); got != "" {
		t.Fatalf("expected empty value to stay empty, got %q", got)
	}
	if got := RedactString("secret"); got != RedactedValue {
		t.Fatalf("expected redacted value %q, got %q", RedactedValue, got)
	}
}

func TestSafeResourceIdentifier(t *testing.T) {
	if got := SafeResourceIdentifier("  ocid1.vcn.oc1..example  "); got != "ocid1.vcn.oc1..example" {
		t.Fatalf("unexpected resource identifier: %q", got)
	}
}

func TestIdentifierHint(t *testing.T) {
	if got := IdentifierHint(""); got != "" {
		t.Fatalf("expected empty hint for empty value, got %q", got)
	}
	if got := IdentifierHint("abc12345"); got != "abc12345" {
		t.Fatalf("expected short value to pass through, got %q", got)
	}
	if got := IdentifierHint("ocid1.subnet.oc1..aaaaaaaexample12345678"); got != "...12345678" {
		t.Fatalf("unexpected identifier hint: %q", got)
	}
}

func TestSortedMapKeys(t *testing.T) {
	keys := SortedMapKeys(map[string]int{
		"b": 2,
		"a": 1,
	})
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestSortedNestedMapKeys(t *testing.T) {
	keys := SortedNestedMapKeys(map[string]map[string]string{
		"ns-b": {"b": "2"},
		"ns-a": {"c": "3", "a": "1"},
	})
	if len(keys) != 3 || keys[0] != "ns-a/a" || keys[1] != "ns-a/c" || keys[2] != "ns-b/b" {
		t.Fatalf("unexpected nested keys: %#v", keys)
	}
}
