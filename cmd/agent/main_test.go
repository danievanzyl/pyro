package main

import (
	"strings"
	"testing"
)

const passwdFixture = `root:x:0:0:root:/root:/bin/bash
# comment line
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
appuser:x:5000:5000:App User:/home/appuser:/bin/sh
broken:x:notanumber:1::/x:/bin/sh
`

func TestParsePasswdUID_Found(t *testing.T) {
	uid, ok := parsePasswdUID(strings.NewReader(passwdFixture), "appuser")
	if !ok || uid != 5000 {
		t.Errorf("uid=%d ok=%v want 5000,true", uid, ok)
	}
}

func TestParsePasswdUID_Root(t *testing.T) {
	uid, ok := parsePasswdUID(strings.NewReader(passwdFixture), "root")
	if !ok || uid != 0 {
		t.Errorf("uid=%d ok=%v want 0,true", uid, ok)
	}
}

func TestParsePasswdUID_Missing(t *testing.T) {
	uid, ok := parsePasswdUID(strings.NewReader(passwdFixture), "ghost")
	if ok || uid != 0 {
		t.Errorf("uid=%d ok=%v want 0,false", uid, ok)
	}
}

func TestParsePasswdUID_MalformedUID(t *testing.T) {
	uid, ok := parsePasswdUID(strings.NewReader(passwdFixture), "broken")
	if ok || uid != 0 {
		t.Errorf("uid=%d ok=%v want 0,false", uid, ok)
	}
}

func TestParsePasswdUID_Empty(t *testing.T) {
	uid, ok := parsePasswdUID(strings.NewReader(""), "anything")
	if ok || uid != 0 {
		t.Errorf("uid=%d ok=%v want 0,false", uid, ok)
	}
}
