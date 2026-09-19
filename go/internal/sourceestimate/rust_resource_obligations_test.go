package sourceestimate

import (
	"os"
	"strings"
	"testing"
)

func TestRustObservedResourceDiagnostic(t *testing.T) {
	for _, path := range []string{"rust-manually-drop/manually_drop.rs", "rust-vec-drain/drain.rs"} {
		src, e := os.ReadFile("../../../docs/evidence/shallow-v4/connected-high-holdout/snapshots/" + path)
		if e != nil {
			t.Fatal(e)
		}
		r := rustResult(string(src))
		t.Logf("%s overall=%+v", path, r.Grade)
		for _, a := range r.Abstractions {
			t.Logf("%s %s %+v", a.Name, a.Audience, a.Grade)
		}
	}
}

func TestRustAutomaticPointerCleanupContract(t *testing.T) {
	prefix := `use core::ptr;pub struct Buffer{pointer:*mut u8}impl Buffer{pub fn value(&self,x:i32,a:i32,b:i32,c:i32)->i32{x}}`
	release := `impl Drop for Buffer{fn drop(&mut self){unsafe{ptr::drop_in_place(self.pointer);}}}`
	positive := rustResult(prefix + release).Grade
	if positive.Responsibilities["resource"] != 2 {
		t.Fatalf("automatic connected pointer cleanup missing: %+v", positive)
	}
	for _, source := range []string{
		prefix + `impl Buffer{pub unsafe fn release(&mut self){ptr::drop_in_place(self.pointer);}}`,
		prefix + `trait Drop{fn drop(&mut self);}` + release,
		prefix + `mod ptr{pub unsafe fn drop_in_place<T>(p:*mut T){}}` + release,
		prefix + `mod core{pub mod ptr{}}` + release,
		prefix + `impl Drop for Buffer{fn drop(&mut self){let p=elsewhere();unsafe{ptr::drop_in_place(p);}}}`,
		prefix + `impl Drop for Buffer{fn drop(&mut self){return;unsafe{ptr::drop_in_place(self.pointer);}}}`,
		prefix + `impl Drop for Buffer{fn drop(&mut self){let ignored=||{unsafe{ptr::drop_in_place(self.pointer);}};}}`,
		prefix + `impl Drop for Buffer{fn drop(&mut self){let mut p=self.pointer;p=elsewhere();unsafe{ptr::drop_in_place(p);}}}`,
	} {
		g := rustResult(source).Grade
		if g.Responsibilities["resource"] != 0 {
			t.Fatalf("unproven cleanup: %+v source=%s", g, source)
		}
	}
	renamed := strings.NewReplacer("Buffer", "Renamed", "pointer", "storage").Replace(prefix + release)
	if g := rustResult(renamed).Grade; g.Value() != positive.Value() || g.Hidden != positive.Hidden {
		t.Fatalf("renaming changed ownership: %+v / %+v", positive, g)
	}
}

func TestRustConnectedPointerRelocation(t *testing.T) {
	prefix := `use core::ptr;pub struct Buffer{pointer:*mut u8}impl Buffer{pub unsafe fn restore(&mut self,start:usize,end:usize,n:usize){`
	body := `let source=self.pointer.add(start);let target=self.pointer.add(end);ptr::copy(source,target,n);`
	g := rustResult(prefix + body + `}}`).Grade
	if g.Responsibilities["representation-transformation"] != 2 {
		t.Fatalf("connected relocation missing: %+v", g)
	}
	for _, body := range []string{`ptr::copy(self.pointer,self.pointer,n);`, `let target=self.pointer;ptr::copy(self.pointer,target,n);`, `let source=other();let target=other();ptr::copy(source,target,n);`, `let source=self.pointer.add(start);let target=self.pointer.add(end);ptr::copy(source,target,0);`} {
		g := rustResult(prefix + body + `}}`).Grade
		if g.Responsibilities["representation-transformation"] != 0 {
			t.Fatalf("disconnected/no-op copy credited: %+v", g)
		}
	}
}

func TestRustTailQuerySurfaceEquivalence(t *testing.T) {
	tail := `pub struct Example{delegate:Other}impl Example{pub fn read(&self,x:i32,a:i32,b:i32,c:i32)->i32{self.delegate.query(x)}}`
	explicit := strings.Replace(tail, "{self.delegate.query(x)}", "{return self.delegate.query(x);}", 1)
	a, b := rustResult(tail).Grade, rustResult(explicit).Grade
	if a.Surface.OperationUnits != b.Surface.OperationUnits || a.Value() != b.Value() {
		t.Fatalf("tail query differs from return: %+v / %+v", a, b)
	}
}

func TestRustPointerOwnershipBarriers(t *testing.T) {
	prefix := `use core::ptr;pub struct Example{p:*mut u8}impl Example{pub fn run(&self){}}`
	for _, body := range []string{`unsafe{ptr::drop_in_place(unrelated(self.p));}`, `let p=core::ptr::null_mut::<u8>();{let p=self.p;core::hint::black_box(p);}unsafe{ptr::drop_in_place(p);}`, `let p=self.p;{let p:*mut u8=unrelated();unsafe{ptr::drop_in_place(p);}}`, `let p:*mut u8=unrelated();unsafe{ptr::drop_in_place(p);}`} {
		g := rustResult(prefix + `impl Drop for Example{fn drop(&mut self){` + body + `}}`).Grade
		if g.Responsibilities["resource"] != 0 {
			t.Fatalf("unproven pointer ownership: %+v body=%s", g, body)
		}
	}
	for _, body := range []string{`ptr::copy(self.p,self.p.add(0),1);`, `ptr::copy(foo(self.p),bar(self.p),1);`, `ptr::copy(self.p,self.p.cast::<u8>(),1);`} {
		g := rustResult(`use core::ptr;pub struct Example{p:*mut u8}impl Example{pub unsafe fn run(&mut self){` + body + `}}`).Grade
		if g.Responsibilities["representation-transformation"] != 0 {
			t.Fatalf("unproven relocation: %+v", g)
		}
	}
}
func TestRustPointerTypeBindingIdentity(t *testing.T) {
	for _, source := range []string{
		`use core::ptr::{self,NonNull};pub trait PointerSource{fn as_ptr(&self)->*mut u8;}pub struct Example<NonNull:PointerSource>{p:NonNull}impl<NonNull:PointerSource> Example<NonNull>{pub fn run(&self){}}impl<NonNull:PointerSource> Drop for Example<NonNull>{fn drop(&mut self){unsafe{ptr::drop_in_place(self.p.as_ptr());}}}`,
		`use core::ptr::{self,NonNull};pub struct Example{p:custom::NonNull}impl Example{pub fn run(&self){}}impl Drop for Example{fn drop(&mut self){unsafe{ptr::drop_in_place(self.p.as_ptr());}}}`,
		`use core::ptr;use custom::ptr;pub struct Example{p:*mut u8}impl Example{pub fn run(&self){}}impl Drop for Example{fn drop(&mut self){unsafe{ptr::drop_in_place(self.p);}}}`,
	} {
		g := rustResult(source).Grade
		if g.Responsibilities["resource"] != 0 {
			t.Fatalf("type spelling supplies ownership: %+v", g)
		}
	}
}

func TestRustAutomaticCleanupPathsAndPrivateHelper(t *testing.T) {
	base := `use core::ptr;pub struct Example{p:*mut u8}impl Example{pub fn run(&self,x:i32,a:i32,b:i32,c:i32)->i32{x} BODY}impl Drop for Example{fn drop(&mut self){DROP}}`
	direct := strings.NewReplacer("BODY", "", "DROP", `unsafe{ptr::drop_in_place(self.p);}`).Replace(base)
	helper := strings.NewReplacer("BODY", `fn clean(&mut self){unsafe{ptr::drop_in_place(self.p);}}`, "DROP", `self.clean();`).Replace(base)
	a, b := rustResult(direct).Grade, rustResult(helper).Grade
	if a.Responsibilities["resource"] != 2 || b.Responsibilities["resource"] != 2 || a.Value() != b.Value() {
		t.Fatalf("resolved cleanup helper changed ownership: %+v / %+v", a, b)
	}
	conditional := strings.NewReplacer("BODY", "", "DROP", `if opaque_flag(){unsafe{ptr::drop_in_place(self.p);}}`).Replace(base)
	if g := rustResult(conditional).Grade; g.Responsibilities["resource"] != 0 {
		t.Fatalf("conditional cleanup claimed total closure: %+v", g)
	}
}

func TestRustDeferredCleanupDoesNotCloseOwnership(t *testing.T) {
	base := `use core::ptr;pub struct Example{p:*mut u8}impl Example{pub fn run(&self){} BODY}impl Drop for Example{fn drop(&mut self){DROP}}`
	for _, parts := range [][2]string{
		{`async fn clean(&mut self){unsafe{ptr::drop_in_place(self.p);}}`, `self.clean();`},
		{``, `let future = async {unsafe{ptr::drop_in_place(self.p);}};`},
		{``, `let future = async move {unsafe{ptr::drop_in_place(self.p);}};`},
	} {
		source := strings.NewReplacer("BODY", parts[0], "DROP", parts[1]).Replace(base)
		if g := rustResult(source).Grade; g.Responsibilities["resource"] != 0 {
			t.Fatalf("deferred cleanup credited: %+v", g)
		}
	}
}
