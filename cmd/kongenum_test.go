package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testStringer struct{ val string }

func (t testStringer) String() string { return t.val }

func TestToStrings(t *testing.T) {
	assert.Equal(t,
		toStrings(testStringer{"a"}, testStringer{"b"}, testStringer{"c"}),
		[]string{"a", "b", "c"})
	assert.Equal(t, toStrings(), []string{})
}

func TestJoinStringers(t *testing.T) {
	assert.Equal(t,
		joinStringers("|", testStringer{"a"}, testStringer{"b"}, testStringer{"c"}),
		"a|b|c")
	assert.Equal(t, joinStringers("|", testStringer{"a"}), "a")
	assert.Equal(t, joinStringers("|"), "")
}

func TestEnumTag(t *testing.T) {
	assert.Equal(t, enumTag(testStringer{"a"}, testStringer{"b"}, testStringer{"c"}), "a,b,c")
}
