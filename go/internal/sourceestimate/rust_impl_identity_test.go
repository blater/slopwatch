package sourceestimate

import (
	"strings"
	"testing"
)

func TestRustImplIdentityIgnoresBindersAndWhereClauses(t *testing.T) {
	for _, tc := range []struct{ header, trait, owner string }{
		{`Buffer`, "", "Buffer"},
		{`<T> Buffer<T>`, "", "Buffer"},
		{`<'a,T> Buffer<'a,T> where T: Clone`, "", "Buffer"},
		{`<T: for<'a> Fn(&'a str)> crate::Buffer<T>`, "", "Buffer"},
		{`<T> crate::api::Read<T> for crate::Buffer<T> where T: for<'a> Fn(&'a str)`, "Read", "Buffer"},
		{`<'a> core::ops::Drop for Buffer<'a>`, "Drop", "Buffer"},
		{`<'a,T> api::Read<Vec<T>> for &'a mut crate::Buffer<T> where T: Clone`, "Read", "Buffer"},
		{`<T: Trait<Vec<Vec<u8>>>> Buffer<T>`, "", "Buffer"},
		{`<const N: usize> Buffer<{N + 1}>`, "", "Buffer"},
	} {
		tokens, _, _ := lex([]byte("impl " + tc.header + "{pub fn read(&self,x:i32)->i32{x}}"))
		ranges := rustImplRanges(tokens)
		if len(ranges) != 1 || ranges[0].trait != tc.trait || ranges[0].selfType != tc.owner {
			t.Fatalf("impl %s: %+v", tc.header, ranges)
		}
	}
}

func TestRustImplGenericRepresentationDoesNotChangeAttribution(t *testing.T) {
	direct := `pub struct Buffer{value:i32}impl Buffer{pub fn read(&self,x:i32,a:i32,b:i32,c:i32)->i32{x+self.value}}`
	generic := `pub struct Buffer<T>{value:i32,marker:PhantomData<T>}impl<T> Buffer<T> where T: Clone{pub fn read(&self,x:i32,a:i32,b:i32,c:i32)->i32{x+self.value}}`
	lifetime := `pub struct Buffer<'a>{value:i32,marker:PhantomData<&'a ()>}impl<'a> Buffer<'a>{pub fn read(&self,x:i32,a:i32,b:i32,c:i32)->i32{x+self.value}}`
	first := rustResult(direct)
	for _, source := range []string{generic, lifetime} {
		got := rustResult(source)
		if got.Grade == nil || first.Grade == nil || got.Grade.Value() != first.Grade.Value() || got.Grade.Hidden != first.Grade.Hidden || got.Grade.Surface.InputUnits != first.Grade.Surface.InputUnits {
			t.Fatalf("generic binder changes grade: %+v / %+v", first.Grade, got.Grade)
		}
		if len(got.Abstractions) != 1 || got.Abstractions[0].Audience != "external" || !strings.HasSuffix(got.Abstractions[0].Name, "external:Buffer") {
			t.Fatalf("binder creates fake owner: %+v", got.Abstractions)
		}
	}
}

func TestRustImplQualifiedTraitKeepsDeclaredContract(t *testing.T) {
	direct := `pub trait Read{fn read(&self,x:i32)->i32;}struct Buffer;impl Read for Buffer{fn read(&self,x:i32)->i32{x*2}}`
	generic := `pub trait Read{fn read(&self,x:i32)->i32;}struct Buffer<T>(T);impl<T> crate::Read for Buffer<T> where T: Clone{fn read(&self,x:i32)->i32{x*2}}`
	a, b := rustResult(direct), rustResult(generic)
	if a.Grade == nil || b.Grade == nil || a.Grade.Value() != b.Grade.Value() || len(b.Abstractions) != 1 || b.Abstractions[0].Audience != "external" || !strings.HasSuffix(b.Abstractions[0].Name, "external:Buffer") {
		t.Fatalf("qualified generic trait contract changed: %+v / %+v", a, b)
	}
}
