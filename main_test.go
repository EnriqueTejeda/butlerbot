package main

import "testing"

func TestReplaceDotAndUppercase(t *testing.T) {
	got := replaceDotAndUppercase("HOTELXCARET.COM")
	want := "HOTELXCARET_COM"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
