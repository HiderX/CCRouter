package main

import (
	"reflect"
	"testing"
)

func TestSplitAccount(t *testing.T) {
	a, rest := splitAccount([]string{"--model", "opus", "--account", "工作", "fix", "--", "--account", "x"})
	if a != "工作" || !reflect.DeepEqual(rest, []string{"--model", "opus", "fix", "--", "--account", "x"}) {
		t.Fatal(a, rest)
	}
	if a, _ = splitAccount([]string{"--account=个人"}); a != "个人" {
		t.Fatal(a)
	}
}
func TestResumeWithoutID(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	for _, c := range []struct {
		args     []string
		from, to int
		query    string
		ok       bool
	}{
		{[]string{"--resume"}, 0, 1, "", true},
		{[]string{"--model", "x", "-r", "--fork-session"}, 2, 3, "", true},
		{[]string{"--resume", "ip 显示"}, 0, 2, "ip 显示", true},
		{[]string{"--resume=账户"}, 0, 1, "账户", true},
		{[]string{"--resume", id}, 0, 0, "", false},
		{[]string{"-r=" + id}, 0, 0, "", false},
		{[]string{"-c"}, 0, 0, "", false},
		{[]string{"--", "--resume"}, 0, 0, "", false},
	} {
		from, to, q, ok := resumeWithoutID(c.args)
		if ok != c.ok || (ok && (from != c.from || to != c.to || q != c.query)) {
			t.Errorf("%v: got %d %d %q %v", c.args, from, to, q, ok)
		}
	}
}
func TestHas(t *testing.T) {
	if !has([]string{"--print=x"}, "-p", "--print") || has([]string{"--", "-p"}, "-p") || has([]string{"--model"}, "-c") {
		t.Fatal("has")
	}
}
