package goadapter

import (
	"reflect"
	"testing"
)

func TestDepthErrorHelperExtraction(t *testing.T) {
	direct := depthCallScore(t, `package p;import "errors";func Calc(x int)(int,error){if x<0{return 0,errors.New("bad")};return x+1,nil}`)
	for _, body := range []string{
		`return checked(x)`,
		`v,err:=checked(x);return v,err`,
		`v,err:=checked(x);if err!=nil{return 0,err};return v,nil`,
		`v,err:=checked(x);if nil==err{return v,nil};return 0,err`,
	} {
		helper := depthCallScore(t, `package p;import "errors";func checked(x int)(int,error){if x<0{return 0,errors.New("bad")};return x+1,nil};func Calc(x int)(int,error){`+body+`}`)
		if helper.Shallow == nil || direct.Shallow == nil || *helper.Shallow != *direct.Shallow || !reflect.DeepEqual(helper.Alternatives, direct.Alternatives) {
			t.Fatalf("error extraction changed responsibilities: %s: %+v / %+v", body, direct, helper)
		}
	}
}

func TestDepthReturnedErrorsAreCallerData(t *testing.T) {
	for _, test := range []struct {
		source string
		hidden uint64
	}{
		{`package p;import "errors";func fail()error{return errors.New("bad")};func Calc(x int)int{fail();return x}`, 0},
		{`package p;import "errors";func checked(x int)(int,error){if x<0{return x+7,errors.New("bad")};return x,nil};func Calc(x int)int{v,_:=checked(x);return v}`, 2},
		{`package p;import "errors";func checked(x int)(int,error){if x<0{return 0,errors.New("bad")};return x,nil};func Calc(x int)(int,error){v,err:=checked(x);if err!=nil{return 0,nil};return v,nil}`, 2},
		{`package p;import "errors";func checked(x int)(int,error){if x<0{return 0,errors.New("bad")};return x,nil};func Calc(x int)(int,error){v,err:=checked(x);err=nil;return v,err}`, 2},
		{`package p;import "errors";func Check(x int)error{if x<0{return errors.New("bad")};return nil}`, 1},
		{`package p;import "errors";func check(x int)error{if x<0{return errors.New("bad")};return nil};func Check(x int)error{return check(x)}`, 1},
	} {
		score := depthCallScore(t, test.source)
		if score.Shallow == nil || score.H != test.hidden {
			t.Fatalf("returned error semantics: %s: %+v", test.source, score)
		}
	}
}

func TestDepthConditionalErrorPhi(t *testing.T) {
	score := depthCallScore(t, `package p;import "errors";func Calc(x int)(int,error){err:=error(nil);if x<0{err=errors.New("bad")};return x,err}`)
	if score.Shallow == nil || score.H != 1 {
		t.Fatalf("nil-interface conversion lost validation: %+v", score)
	}
	phi := depthCallScore(t, `package p;import "errors";func check(x int)error{if x<0{return errors.New("bad")};return nil};func Calc(x int)(int,error){err:=check(x);if x<0{err=nil};return x,err}`)
	if phi.Shallow == nil || phi.H != 0 {
		t.Fatalf("cleared error phi retained rejection: %+v", phi)
	}
}

func TestDepthZeroInitializedErrorVariable(t *testing.T) {
	for _, source := range []string{
		`package p;import "errors";func Calc(x int)(int,error){var err error;if x<0{err=errors.New("bad")};return x,err}`,
		`package p;import "errors";func check(x int)error{var err error;if x<0{err=errors.New("bad")};return err};func Calc(x int)(int,error){var err=check(x);if err!=nil{return 0,err};return x,nil}`,
	} {
		score := depthCallScore(t, source)
		if score.Shallow == nil || score.H != 1 {
			t.Fatalf("zero error variable lost validation: %+v", score)
		}
	}
	pointer := depthCallScore(t, `package p;type custom struct{};func(*custom)Error()string{return "bad"};func Calc(x int)(int,error){var e *custom;return x,error(e)}`)
	if pointer.Shallow != nil {
		t.Fatalf("typed nil treated as nil interface: %+v", pointer)
	}
}
