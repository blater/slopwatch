package goadapter

import "testing"

func TestDepthSourceTextComposition(t *testing.T) {
	cases := []struct {
		name, source string
		want         *int
	}{
		{"direct", `package p;func Join(a,b string) string{return a+b}`, intPtr(33)},
		{"helper", `package p;func join(a,b string)string{return a+b};func Join(a,b string)string{return join(a,b)}`, intPtr(33)},
		{"empty", `package p;func Join(a string) string{return ""+a}`, intPtr(100)},
		{"constant", `package p;func Join() string{return "a"+"b"}`, intPtr(100)},
		{"error_message", `package p;import "errors";func Join(a string)(string,error){if a==""{return "",errors.New("bad")};return a,nil}`, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			score := depthCallScore(t, test.source)
			if test.want == nil {
				if score.Shallow != nil {
					t.Fatalf("error message created text X: %+v", score)
				}
				return
			}
			if score.Shallow == nil || *score.Shallow != *test.want {
				t.Fatalf("text score: %d %+v", *score.Shallow, score)
			}
		})
	}
}

func intPtr(value int) *int { return &value }
