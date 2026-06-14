package users

import (
	"reflect"
	"testing"
)

func TestParsePasswd(t *testing.T) {
	in := "root:x:0:0:root:/root:/bin/bash\n" +
		"daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n" +
		"alice:x:1001:1001::/home/alice:/bin/bash\n" +
		"bob:x:1002:1002::/home/bob:/bin/bash\n"
	got := parsePasswd(in)
	want := []string{"alice", "bob"} // UID < 1000 dropped, sorted
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePasswd=%v want %v", got, want)
	}
}

func TestParseLDAP(t *testing.T) {
	in := "SASL/GSSAPI authentication started\n" +
		"dn: CN=Administrator\n" +
		"sAMAccountName: Administrator\n" +
		"\n" +
		"dn: CN=Alice\n" +
		"sAMAccountName: alice-w\n" +
		"sAMAccountName:: Ym9iLXc=\n" // base64 "bob-w"
	got := parseLDAP(in)
	want := []string{"alice-w", "bob-w"} // Administrator excluded, sorted
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLDAP=%v want %v", got, want)
	}
}
