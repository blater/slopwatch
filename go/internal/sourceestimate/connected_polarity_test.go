package sourceestimate

import (
	"strings"
	"testing"
)

func TestConnectedPolarityAdmissionOutcomes(t *testing.T) {
	for _, tc := range []struct{ lang, path, source, field, reject string }{
		{"java", "Example.java", `class Driver{boolean active;void begin(){active=true;}int use(){BODY}void end(){active=false;}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`, "active", `throw new RuntimeException();`},
		{"typescript", "example.ts", `class Driver{active=false;begin(){this.active=true;}use(){BODY}end(){this.active=false;}}export class Example{private d=new Driver();run(){this.d.begin();try{return this.d.use();}finally{this.d.end();}}}`, "this.active", `throw new Error();`},
		{"go", "example.go", `package sample;type Driver struct{active bool};func(d *Driver)Begin(){d.active=true};func(d *Driver)Use()int{BODY};func(d *Driver)End(){d.active=false};type Example struct{d Driver};func(e *Example)Run()int{e.d.Begin();defer e.d.End();return e.d.Use()}`, "d.active", `panic("inactive");`},
		{"rust", "lib.rs", `struct Driver{active:bool}impl Driver{fn begin(&mut self){self.active=true;}fn use_value(&self)->i32{BODY}fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self)->i32{self.d.begin();let guard=Guard(&mut self.d);let out=guard.0.use_value();drop(guard);out}}`, "self.active", `panic!("inactive");`},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			for _, inverted := range []bool{false, true} {
				source, admit, reject := tc.source, tc.field, "!"+tc.field
				if inverted {
					source = strings.NewReplacer("true", "false", "false", "true").Replace(source)
					admit, reject = reject, admit
				}
				condition := func(value string) string {
					if tc.lang == "java" || tc.lang == "typescript" {
						return "(" + value + ")"
					}
					return value
				}
				valid := []string{"if " + condition(reject) + "{" + tc.reject + "} return 1;", "if " + condition(admit) + "{return 1;} " + tc.reject, "if " + condition(admit) + "{return 1;}else{" + tc.reject + "}"}
				invalid := []string{"if " + condition(reject) + "{return 1;} " + tc.reject, "if " + condition(reject) + "{} if " + condition("true") + "{" + tc.reject + "}return 1;"}
				for _, body := range valid {
					g := structuralResult(tc.lang, tc.path, strings.ReplaceAll(source, "BODY", body)).Grade
					if g == nil || g.Responsibilities["resource"] != DefaultCalibration().Resource {
						t.Fatalf("valid admission inverted=%v body=%s: %+v", inverted, body, g)
					}
				}
				for _, body := range invalid {
					g := structuralResult(tc.lang, tc.path, strings.ReplaceAll(source, "BODY", body)).Grade
					if g == nil || g.Responsibilities["resource"] != 0 {
						t.Fatalf("invalid admission inverted=%v body=%s: %+v", inverted, body, g)
					}
				}
			}
		})
	}
}

func TestConnectedPolarityInterveningRelease(t *testing.T) {
	for _, tc := range []struct{ lang, path, source, acquire, release string }{
		{"java", "Example.java", `class Driver{boolean active;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;}void finish(){end();}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`, "d.begin();", "d.finish();"},
		{"typescript", "example.ts", `class Driver{active=false;begin(){this.active=true;}use(){if(!this.active)throw new Error();return 1;}end(){this.active=false;}finish(){this.end();}}export class Example{private d=new Driver();run(){this.d.begin();try{return this.d.use();}finally{this.d.end();}}}`, "this.d.begin();", "this.d.finish();"},
		{"go", "example.go", `package sample;type Driver struct{active bool};func(d *Driver)Begin(){d.active=true};func(d *Driver)Use()int{if !d.active {panic("inactive")};return 1};func(d *Driver)End(){d.active=false};func(d *Driver)Finish(){d.End()};type Example struct{d Driver};func(e *Example)Run()int{e.d.Begin();defer e.d.End();return e.d.Use()}`, "e.d.Begin();", "e.d.Finish();"},
		{"rust", "lib.rs", `struct Driver{active:bool}impl Driver{fn begin(&mut self){self.active=true;}fn use_value(&self)->i32{assert!(self.active);1}fn end(&mut self){self.active=false;}fn finish(&mut self){self.end();}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self)->i32{self.d.begin();let guard=Guard(&mut self.d);let out=guard.0.use_value();drop(guard);out}}`, "self.d.begin();", "self.d.finish();"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			for _, release := range []string{tc.release, strings.NewReplacer("finish", "end", "Finish", "End").Replace(tc.release)} {
				source := strings.ReplaceAll(tc.source, tc.acquire, tc.acquire+release)
				got := structuralResult(tc.lang, tc.path, source).Grade
				if got == nil || got.Responsibilities["resource"] != 0 {
					t.Fatalf("released before use: %+v", got)
				}
			}
		})
	}
}

func TestConnectedPolaritySameStorageAndLatestTransition(t *testing.T) {
	for index, driver := range []string{
		`boolean active,other;void begin(){active=true;}int use(){if(!other)throw new RuntimeException();return 1;}void end(){other=false;}`,
		`boolean active;void begin(){active=true;active=false;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;}`,
		`boolean active;void begin(){active=true;}int use(){if(!active)return 1;throw new RuntimeException();}void end(){active=false;}`,
	} {
		g := structuralResult("java", "Example.java", `class Driver{`+driver+`}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`).Grade
		expected := 0.0
		// A distinct externally acquired field may still have a protected
		// cleanup duty, but must not receive full lifecycle resource credit.
		if index == 0 {
			expected = DefaultCalibration().Cleanup
		}
		if g == nil || g.Responsibilities["resource"] != expected {
			t.Fatalf("unconnected state gets responsibility: %+v", g)
		}
	}
	source := `struct Driver{active:bool}impl Driver{fn begin(&mut self){self.active=true;}fn use_value(&self)->i32{assert!(self.active);1}fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self)->i32{self.d.begin();let guard=Guard(&mut self.d);guard.0.end();let out=guard.0.use_value();drop(guard);out}}`
	g := structuralResult("rust", "lib.rs", source).Grade
	if g == nil || g.Responsibilities["resource"] != 0 {
		t.Fatalf("released captured guard gets responsibility: %+v", g)
	}
}
