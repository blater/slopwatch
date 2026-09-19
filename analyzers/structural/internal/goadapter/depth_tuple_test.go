package goadapter

import (
	"reflect"
	"testing"
)

func TestDepthTupleHelperExtraction(t *testing.T) {
	for _, pair := range [][2]string{
		{`package p;func Calc(x int)int{return x+1}`, `package p;func pair(x int)(int,int){return x+1,x*2};func Calc(x int)int{a,_:=pair(x);return a}`},
		{`package p;func Calc(x int)(int,int){return x+1,x*2}`, `package p;func pair(x int)(int,int){return x+1,x*2};func Calc(x int)(int,int){return pair(x)}`},
		{`package p;func Calc(x int)(int,int){return x+1,x+1}`, `package p;func pair(x int)(int,int){return x+1,x+1};func Calc(x int)(int,int){a,b:=pair(x);return a,b}`},
		{`package p;func Calc(x int)int{return x*2}`, `package p;func pair(x int)(int,int){return x+1,x*2};func Calc(x int)int{_,b:=pair(x);return b}`},
		{`package p;func Calc(a,b int)(int,int){return b+1,a*2}`, `package p;func pair(a,b int)(int,int){return a+1,b*2};func Calc(a,b int)(int,int){a,b=pair(b,a);return a,b}`},
		{`package p;func Calc(a int)int{return a*2}`, `package p;func pair(a int)(int,int){return a+1,a*2};func Calc(a int)int{a,a=pair(a);return a}`},
		{`package p;func Calc(b bool,x int)(int,bool){if b{return x+1,true};return x*2,false}`, `package p;func pair(b bool,x int)(int,bool){if b{return x+1,true};return x*2,false};func Calc(b bool,x int)(int,bool){return pair(b,x)}`},
	} {
		direct, helper := depthCallScore(t, pair[0]), depthCallScore(t, pair[1])
		if direct.Shallow == nil || helper.Shallow == nil || *direct.Shallow != *helper.Shallow || !reflect.DeepEqual(direct.Alternatives, helper.Alternatives) {
			t.Fatalf("tuple extraction changed responsibilities: %+v / %+v", direct, helper)
		}
	}
}

func TestDepthTupleDiscardDoesNotDiscardEffects(t *testing.T) {
	score := depthCallScore(t, `package p;var state int;func pair(x int)(int,int){state=x;return x,x+1};func Calc(x int)int{_,b:=pair(x);return b}`)
	if score.Shallow != nil {
		t.Fatalf("discard hid effect: %+v", score)
	}
	errorResult := depthCallScore(t, `package p;import "errors";func pair(x int)(int,error){if x<0{return 0,errors.New("bad")};return x,nil};func Calc(x int)(int,error){v,e:=pair(x);return v,e}`)
	if errorResult.Shallow == nil || errorResult.H != 1 {
		t.Fatalf("error channel lost validation: %+v", errorResult)
	}
}

func TestDepthTupleDistinctAndSharedOutcomes(t *testing.T) {
	for _, test := range []struct {
		results string
		hidden  uint64
	}{
		{"x+1,x*2", 4}, {"x+1,x+1", 2},
	} {
		score := depthCallScore(t, "package p;func pair(x int)(int,int){return "+test.results+"};func Calc(x int)(int,int){return pair(x)}")
		if score.Shallow == nil || score.H != test.hidden {
			t.Fatalf("tuple outcome union incorrect: %+v", score)
		}
	}
}
